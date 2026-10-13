package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"strings"
	"time"

	"github.com/iFurySt/open-codex-computer-use/apps/opencomputeruselinux/internal/display"
	"github.com/iFurySt/open-codex-computer-use/apps/opencomputeruselinux/internal/display/wayland"
	"github.com/iFurySt/open-codex-computer-use/apps/opencomputeruselinux/internal/display/x11"
)

// Point is in screen coordinates.
type Point = display.Point

// GlobalTarget is the observed window that global input must reach. X11 matches
// it by PID and title; Wayland checks the accessible window's ACTIVE state.
type GlobalTarget struct {
	PID    uint32
	Title  string
	Window Ref
}

func (t GlobalTarget) native() display.Target { return display.Target{PID: t.PID, Title: t.Title} }

// Capture is a PNG of one window, or the reason none is available.
type Capture struct {
	PNG           []byte
	Width, Height int
	Reason        *Error
}

// x11Backend serves X11 sessions and XWayland windows; it opens nothing until used.
func (e *Engine) x11Backend() *x11.Backend {
	if e.x11 == nil {
		e.x11 = x11.New(e.Display(), e.config.InputGuard)
	}
	return e.x11
}

func (e *Engine) closeDisplay() {
	if e.x11 != nil {
		e.x11.Close()
		e.x11 = nil
	}
	if e.helper != nil {
		e.helper.Close()
		e.helper = nil
	}
}

// waylandHelper starts the helper once per engine in a Wayland session. A helper
// that exited is not restarted: whatever it held is gone, and callers must
// report the loss rather than silently recreate state.
func (e *Engine) waylandHelper(ctx context.Context) (*wayland.Helper, error) {
	if !e.Wayland() {
		return nil, fail("UNSUPPORTED_CAPABILITY", "The Wayland helper only runs in a Wayland session")
	}
	if e.helper != nil {
		select {
		case <-e.helper.Exited():
			return nil, fail("TARGET_UNAVAILABLE", "The Wayland helper exited; start a new session")
		default:
			return e.helper, nil
		}
	}
	env := os.Environ()
	if e.config.Env != nil {
		env = env[:0:0]
		for key, value := range e.config.Env {
			env = append(env, key+"="+value)
		}
	}
	helper, err := wayland.Start(ctx, e.config.WaylandHelper, env, 5*time.Second)
	if err != nil {
		return nil, err
	}
	e.helper = helper
	return helper, nil
}

// Report describes what this session offers, for doctor output and diagnostics.
type Report struct {
	Session     string               `json:"session"`
	X11Display  string               `json:"x11Display,omitempty"`
	Capture     display.Availability `json:"capture"`
	GlobalInput display.Availability `json:"globalInput"`
	Helper      *HelperReport        `json:"waylandHelper,omitempty"`
}

type HelperReport struct {
	display.Availability
	Version string          `json:"version,omitempty"`
	Probe   json.RawMessage `json:"probe,omitempty"`
}

func (e *Engine) Report(ctx context.Context) Report {
	report := Report{Session: "x11", X11Display: e.Display(), Capture: display.Available(), GlobalInput: display.Available()}
	if e.Display() == "" {
		report.Capture = display.Unavailable("DEPENDENCY_MISSING", "No X11 display is configured")
		report.GlobalInput = report.Capture
	}
	if !e.Wayland() {
		return report
	}
	report.Session = "wayland"
	report.Helper = &HelperReport{Availability: display.Available()}
	helper, err := e.waylandHelper(ctx)
	if err != nil {
		report.Helper.Availability = availabilityOf(err)
		report.Capture, report.GlobalInput = report.Helper.Availability, report.Helper.Availability
		return report
	}
	report.Helper.Version, report.Helper.Probe = helper.Version(), helper.Probe().Raw
	report.Capture = helper.Probe().CaptureAvailable()
	report.GlobalInput = helper.Probe().PointerAvailable()
	if report.GlobalInput.Status != "available" && e.Display() != "" {
		report.GlobalInput.Message += "; XTEST reaches XWayland windows only"
	}
	return report
}

func availabilityOf(err error) display.Availability {
	var native *Error
	if !errors.As(err, &native) {
		native = fail("TARGET_UNAVAILABLE", "%v", err)
	}
	return display.Unavailable(native.Code, native.Message)
}

// CaptureAvailability reports whether windows can be captured in this session:
// always on X11, and on Wayland where the helper supports the compositor.
func (e *Engine) CaptureAvailability(ctx context.Context) display.Availability {
	if !e.Wayland() {
		if e.Display() == "" {
			return display.Unavailable("DEPENDENCY_MISSING", "An X11 display is required for window capture")
		}
		return display.Available()
	}
	helper, err := e.waylandHelper(ctx)
	if err != nil {
		return availabilityOf(err)
	}
	return helper.Probe().CaptureAvailable()
}

// PointerAvailability reports whether pointer input can be offered: XTEST on
// X11, and on Wayland the GNOME Shell extension or the wlroots virtual pointer
// with compositor IPC.
func (e *Engine) PointerAvailability(ctx context.Context) display.Availability {
	if !e.Wayland() {
		if e.Display() == "" {
			return display.Unavailable("DEPENDENCY_MISSING", "Global input needs an X11 display")
		}
		return display.Available()
	}
	helper, err := e.waylandHelper(ctx)
	if err != nil {
		return availabilityOf(err)
	}
	return helper.Probe().PointerAvailable()
}

// granted runs op, which may go through the GNOME Shell extension. When the
// extension has not been allowed and this engine grants on demand (the CLI), it
// shows the Shell's dialog once and retries.
func (e *Engine) granted(ctx context.Context, op func() error) error {
	err := op()
	var native *Error
	if !errors.As(err, &native) || native.Code != "PERMISSION_REQUIRED" || !e.Wayland() {
		return err
	}
	helper, helperErr := e.waylandHelper(ctx)
	if helperErr != nil || !helper.Probe().ShellAvailable() {
		return err
	}
	if !e.config.GrantOnDemand {
		return fail("PERMISSION_REQUIRED", "GNOME desktop control has not been allowed; request the %s permission", ShellPermissionID)
	}
	status, grantErr := helper.ShellAuthorize(ctx)
	if grantErr != nil {
		return grantErr
	}
	if status != "granted" {
		return fail("PERMISSION_REQUIRED", "GNOME desktop control was not allowed")
	}
	return op()
}

// ShellPermissionID names the GNOME Shell extension's grant in SDK permissions.
const ShellPermissionID = "gnomeShell"

// ShellPermission reports the GNOME desktop-control grant, and whether this
// session has one at all: "unavailable" until the extension runs (install it and
// log in again), then "notDetermined", "granted" or "denied".
func (e *Engine) ShellPermission(ctx context.Context) (status string, applies bool) {
	if !e.Wayland() {
		return "", false
	}
	helper, err := e.waylandHelper(ctx)
	if err != nil {
		gnome := false
		for _, desktop := range strings.Split(e.env("XDG_CURRENT_DESKTOP"), ":") {
			gnome = gnome || strings.EqualFold(desktop, "GNOME")
		}
		return "unavailable", gnome
	}
	if !helper.Probe().ShellAvailable() {
		return "unavailable", helper.Probe().GNOME()
	}
	status, err = helper.ShellAuthorization(ctx)
	if err != nil {
		return "unavailable", true
	}
	return status, true
}

// KDEPermissionID names the KDE integration in SDK permissions.
const KDEPermissionID = "kdeIntegration"

// KDEPermission reports the KDE integration, and whether this session has one at
// all: "granted" once KWin serves the helper both ScreenShot2 and fake_input,
// otherwise "unavailable" (install it with install-kde-integration). KDE asks
// nothing at run time; installing is the grant.
func (e *Engine) KDEPermission(ctx context.Context) (status string, applies bool) {
	if !e.Wayland() {
		return "", false
	}
	helper, err := e.waylandHelper(ctx)
	if err != nil {
		kde := false
		for _, desktop := range strings.Split(e.env("XDG_CURRENT_DESKTOP"), ":") {
			kde = kde || strings.EqualFold(desktop, "KDE")
		}
		return "unavailable", kde
	}
	probe := helper.Probe()
	if !probe.KDE() && probe.KWin == nil {
		return "", false
	}
	if probe.KWinInput() && probe.KWinCapture() {
		return "granted", true
	}
	return "unavailable", true
}

// RequestShell shows the Shell's dialog unless desktop control is already allowed.
func (e *Engine) RequestShell(ctx context.Context) (string, error) {
	helper, err := e.waylandHelper(ctx)
	if err != nil {
		return "", err
	}
	if !helper.Probe().ShellAvailable() {
		availability := helper.Probe().ShellMissing()
		return "unavailable", fail(availability.Code, "%s", availability.Message)
	}
	return helper.ShellAuthorize(ctx)
}

// capturer selects the backend for window capture in this session.
func (e *Engine) capturer(ctx context.Context) (display.Capturer, *Error) {
	if !e.Wayland() {
		return e.x11Backend(), nil
	}
	helper, err := e.waylandHelper(ctx)
	if err == nil {
		if availability := helper.Probe().CaptureAvailable(); availability.Status != "available" {
			err = fail(availability.Code, "%s", availability.Message)
		}
	}
	if err != nil {
		var native *Error
		if !errors.As(err, &native) {
			native = fail("TARGET_UNAVAILABLE", "%v", err)
		}
		return nil, native
	}
	return helper, nil
}

// injector selects the backend for global input. XTEST reaches X11 sessions and,
// in a Wayland session, only XWayland windows; callers decide whether to allow that.
func (e *Engine) injector() display.Injector { return e.x11Backend() }

// Capture captures the window of pid whose title is the observed accessible title.
func (e *Engine) Capture(ctx context.Context, pid uint32, title string) Capture {
	capturer, reason := e.capturer(ctx)
	if reason != nil {
		return Capture{Reason: reason}
	}
	var image display.Image
	err := e.granted(ctx, func() (err error) {
		image, err = capturer.Capture(ctx, display.Target{PID: pid, Title: title})
		return err
	})
	if err != nil {
		var native *Error
		if !errors.As(err, &native) {
			native = fail("CAPTURE_FAILED", "%v", err)
		}
		return Capture{Reason: native}
	}
	return Capture{PNG: image.PNG, Width: image.Width, Height: image.Height}
}

// WindowRect is the target window in screen coordinates: X11 root coordinates,
// or Wayland layout coordinates from the compositor's IPC.
func (e *Engine) WindowRect(ctx context.Context, target GlobalTarget) (Rect, error) {
	if e.x11Input(ctx, target) {
		return e.injector().WindowRect(ctx, target.native())
	}
	window, err := e.waylandWindow(ctx, target)
	return window.Bounds(), err
}

// waylandWindow locates target through the compositor's IPC or the GNOME Shell extension.
func (e *Engine) waylandWindow(ctx context.Context, target GlobalTarget) (window wayland.Window, err error) {
	helper, err := e.waylandPointer(ctx)
	if err != nil {
		return window, err
	}
	err = e.granted(ctx, func() (err error) {
		window, err = helper.FindWindow(ctx, target.native())
		return err
	})
	return window, err
}

// ElementCenter is the screen point at the center of a revalidated element. On
// Wayland, AT-SPI cannot know where a window is, so the element's window-relative
// extents are placed at the compositor's window origin.
func (e *Engine) ElementCenter(ctx context.Context, target GlobalTarget, node Node) (Point, error) {
	if node.Extents == nil {
		return Point{}, fail("TARGET_UNAVAILABLE", "Element no longer has bounds")
	}
	if e.x11Input(ctx, target) {
		return Point{X: node.Extents.X + node.Extents.Width/2, Y: node.Extents.Y + node.Extents.Height/2}, nil
	}
	var box struct{ X, Y, Width, Height int32 }
	if err := e.call(ctx, node.Ref, "Component.GetExtents", uint32(1)).Store(&box); err != nil || box.Width <= 0 || box.Height <= 0 {
		return Point{}, fail("TARGET_UNAVAILABLE", "Element no longer has bounds")
	}
	located, err := e.waylandWindow(ctx, target)
	if err != nil {
		return Point{}, err
	}
	if e.x11Backend().HasWindow(ctx, target.native()) {
		// XWayland windows report screen coordinates, XWayland's root coordinates;
		// their window coordinates are unusable where the toolkit draws shadows
		// inside the X window (GTK under Mutter). The root coordinates are the
		// compositor's while the accessible window lies on the content it shows.
		var frame, element struct{ X, Y, Width, Height int32 }
		content := located.Bounds()
		if err := e.call(ctx, target.Window, "Component.GetExtents", uint32(0)).Store(&frame); err != nil ||
			math.Abs(float64(frame.X)-content.X) > 2 || math.Abs(float64(frame.Y)-content.Y) > 2 ||
			math.Abs(float64(frame.Width)-content.Width) > 2 || math.Abs(float64(frame.Height)-content.Height) > 2 {
			return Point{}, fail("TARGET_UNAVAILABLE", "The accessible X11 window is at %d,%d %dx%d but the compositor shows %.0f,%.0f %.0fx%.0f; element positions cannot be mapped", frame.X, frame.Y, frame.Width, frame.Height, content.X, content.Y, content.Width, content.Height)
		}
		if err := e.call(ctx, node.Ref, "Component.GetExtents", uint32(0)).Store(&element); err != nil || element.Width <= 0 || element.Height <= 0 {
			return Point{}, fail("TARGET_UNAVAILABLE", "Element no longer has bounds")
		}
		return Point{X: float64(element.X) + float64(element.Width)/2, Y: float64(element.Y) + float64(element.Height)/2}, nil
	}
	// Toolkit window coordinates start at the client surface (on GNOME it includes
	// client-side shadows). They map to the screen only while the accessible window
	// has the surface's size; a pending resize would shift every point.
	window := located.ClientBounds()
	var frame struct{ X, Y, Width, Height int32 }
	if err := e.call(ctx, target.Window, "Component.GetExtents", uint32(1)).Store(&frame); err != nil ||
		math.Abs(float64(frame.Width)-window.Width) > 2 || math.Abs(float64(frame.Height)-window.Height) > 2 {
		return Point{}, fail("TARGET_UNAVAILABLE", "The accessible window is %dx%d but the compositor shows %.0fx%.0f; element positions cannot be mapped", frame.Width, frame.Height, window.Width, window.Height)
	}
	return Point{X: window.X + float64(box.X) + float64(box.Width)/2, Y: window.Y + float64(box.Y) + float64(box.Height)/2}, nil
}

func (e *Engine) PointerClick(ctx context.Context, target GlobalTarget, point Point, button byte, count int) error {
	if e.x11Input(ctx, target) {
		return e.injector().PointerClick(ctx, target.native(), point, button, count)
	}
	name, ok := buttonNames[button]
	if !ok || count < 1 || count > 3 {
		return fail("INVALID_ARGUMENT", "Unsupported button or click count")
	}
	if helper, route := e.assisted(ctx); route != "" {
		// The helper confirms the window at the point right before it clicks.
		return e.granted(ctx, func() error { return helper.TargetedClick(ctx, route, target.native(), point, button, count) })
	}
	pointer, err := e.pointAt(ctx, target, point)
	if err != nil {
		return err
	}
	if err := pointer.PointerClick(ctx, name, count); err != nil {
		return possibly(err)
	}
	return nil
}

func (e *Engine) Drag(ctx context.Context, target GlobalTarget, from, to Point) error {
	if e.x11Input(ctx, target) {
		return e.injector().Drag(ctx, target.native(), from, to)
	}
	if helper, route := e.assisted(ctx); route != "" {
		return e.granted(ctx, func() error { return helper.TargetedDrag(ctx, route, target.native(), from, to) })
	}
	pointer, err := e.pointAt(ctx, target, from)
	if err != nil {
		return err
	}
	if err := pointer.PointerPress(ctx, "left"); err != nil {
		return possibly(err)
	}
	// Intermediate motion lets the application see a drag rather than a jump.
	const steps = 10
	for i := 1; i <= steps; i++ {
		point := Point{X: from.X + (to.X-from.X)*float64(i)/steps, Y: from.Y + (to.Y-from.Y)*float64(i)/steps}
		if err := pointer.PointerMove(ctx, point); err != nil {
			_ = pointer.PointerRelease(context.WithoutCancel(ctx), "left")
			return possibly(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := pointer.PointerRelease(ctx, "left"); err != nil {
		return possibly(err)
	}
	return nil
}

var buttonNames = map[byte]string{1: "left", 2: "middle", 3: "right"}

// possibly marks a failure after the pointer reached the target as one whose
// effect is unknown.
func possibly(err error) error {
	var native *Error
	if errors.As(err, &native) && native.Effect == "possible" {
		return err
	}
	return uncertain("Wayland pointer input stopped part way: %v", err)
}

// assisted is the helper and its compositor-assisted input route (the GNOME Shell
// extension or KWin), when this session has one.
func (e *Engine) assisted(ctx context.Context) (*wayland.Helper, wayland.Route) {
	if !e.Wayland() {
		return nil, ""
	}
	helper, err := e.waylandHelper(ctx)
	if err != nil {
		return nil, ""
	}
	return helper, helper.Probe().AssistedRoute()
}

// waylandPointer is the helper when this session can locate windows and bind
// pointer input to them: the GNOME Shell extension or the wlroots virtual pointer.
func (e *Engine) waylandPointer(ctx context.Context) (*wayland.Helper, error) {
	helper, err := e.waylandHelper(ctx)
	if err != nil {
		return nil, err
	}
	if availability := helper.Probe().PointerAvailable(); availability.Status != "available" {
		return nil, fail(availability.Code, "%s", availability.Message)
	}
	return helper, nil
}

// pointAt moves the pointer onto point in the target window and confirms it
// landed there before any button is pressed. The compositor moves keyboard focus
// to the window under the pointer (focus follows the mouse, the Sway and
// Hyprland default), so its IPC must then name the target, and the target must
// still contain the point. Surfaces that never take focus, such as panels and
// notifications, cannot be told apart this way.
func (e *Engine) pointAt(ctx context.Context, target GlobalTarget, point Point) (*wayland.Helper, error) {
	pointer, err := e.waylandPointer(ctx)
	if err != nil {
		return nil, err
	}
	contains := func() error {
		window, err := pointer.FindWindow(ctx, target.native())
		if err != nil {
			return err
		}
		bounds := window.Bounds()
		if !window.Visible || point.X < bounds.X || point.Y < bounds.Y || point.X >= bounds.X+bounds.Width || point.Y >= bounds.Y+bounds.Height {
			return fail("TARGET_UNAVAILABLE", "The point is not on the visible target window")
		}
		return nil
	}
	if err := contains(); err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, cancelled()
	}
	if err := pointer.PointerMove(ctx, point); err != nil {
		return nil, err
	}
	var focused bool
	for attempt := 0; attempt < 20 && !focused; attempt++ {
		if attempt > 0 {
			time.Sleep(25 * time.Millisecond)
		}
		active, err := pointer.ActiveWindow(ctx)
		if err != nil {
			return nil, err
		}
		focused = active.Available && active.PID != nil && *active.PID == target.PID && active.Title == target.Title
	}
	if !focused {
		return nil, fail("TARGET_UNAVAILABLE", "The target window did not take focus under the pointer; the click could reach another window. Wayland pointer input needs focus to follow the mouse")
	}
	if err := contains(); err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, cancelled()
	}
	return pointer, nil
}

// x11Input reports whether input for target goes through XTEST: always in X11,
// and in Wayland only for XWayland windows when the caller allows it.
func (e *Engine) x11Input(ctx context.Context, target GlobalTarget) bool {
	return !e.Wayland() || (e.config.XWaylandInput && e.x11Backend().HasWindow(ctx, target.native()))
}

func (e *Engine) PressKey(ctx context.Context, target GlobalTarget, key string) error {
	if e.x11Input(ctx, target) {
		return e.injector().PressKey(ctx, target.native(), key)
	}
	chord, err := display.ParseKeyChord(key)
	if err != nil {
		return err
	}
	return e.waylandKeys(ctx, target, 1, func(_ int, keyboard waylandKeyboard) error { return keyboard.chord(ctx, target, chord) })
}

func (e *Engine) TypeKeys(ctx context.Context, target GlobalTarget, text string) error {
	if e.x11Input(ctx, target) {
		return e.injector().TypeKeys(ctx, target.native(), text)
	}
	keyboard, err := e.keyboard(ctx)
	if err != nil {
		return err
	}
	if keyboard.route != "" {
		// One call: the helper checks focus before each character it sends.
		return e.granted(ctx, func() error { return keyboard.helper.TargetedText(ctx, keyboard.route, target.native(), text) })
	}
	if !keyboard.virtual {
		// The portal keyboard can only type what the compositor's layout holds.
		missing, err := keyboard.helper.MissingCharacters(ctx, text)
		if err != nil {
			return err
		}
		if len(missing) > 0 {
			return fail("UNSUPPORTED_CAPABILITY", "The keyboard layout cannot type %q; Wayland input has no way to enter it", missing)
		}
	}
	characters := []rune(text)
	return e.waylandKeys(ctx, target, len(characters), func(i int, keyboard waylandKeyboard) error {
		return keyboard.text(ctx, target, string(characters[i]))
	})
}

func (e *Engine) PageScroll(ctx context.Context, target GlobalTarget, direction string, pages float64) error {
	if e.x11Input(ctx, target) {
		return e.injector().PageScroll(ctx, target.native(), direction, pages)
	}
	key := map[string]string{"up": "Page_Up", "down": "Page_Down", "left": "Left", "right": "Right"}[direction]
	if key == "" || pages <= 0 || pages > 100 {
		return fail("INVALID_ARGUMENT", "Invalid scroll direction or page count")
	}
	chord, _ := display.ParseKeyChord(key)
	count := int(math.Ceil(pages))
	return e.waylandKeys(ctx, target, count, func(_ int, keyboard waylandKeyboard) error { return keyboard.chord(ctx, target, chord) })
}

// waylandKeyboard is the route Wayland keys take in this session: a
// compositor-assisted route (the GNOME Shell extension, KWin), the wlroots
// virtual keyboard, or the RemoteDesktop portal's libei keyboard.
type waylandKeyboard struct {
	helper  *wayland.Helper
	virtual bool
	// route keys are checked against the focus window by the helper itself.
	route wayland.Route
}

func (k waylandKeyboard) chord(ctx context.Context, target GlobalTarget, chord display.KeyChord) error {
	switch {
	case k.route != "":
		return k.helper.TargetedChord(ctx, k.route, target.native(), chord)
	case k.virtual:
		return k.helper.VirtualChord(ctx, chord)
	}
	return k.helper.Chord(ctx, chord)
}

func (k waylandKeyboard) text(ctx context.Context, target GlobalTarget, text string) error {
	switch {
	case k.route != "":
		return k.helper.TargetedText(ctx, k.route, target.native(), text)
	case k.virtual:
		return k.helper.VirtualText(ctx, text)
	}
	return k.helper.Text(ctx, text)
}

// waylandKeys sends count key steps, confirming before each one that the target
// window has focus. A refusal after the first step reports a possible effect.
func (e *Engine) waylandKeys(ctx context.Context, target GlobalTarget, count int, step func(int, waylandKeyboard) error) error {
	keyboard, err := e.keyboard(ctx)
	if err != nil {
		return err
	}
	for i := 0; i < count; i++ {
		err := e.requireFocus(ctx, target, keyboard)
		if err == nil && ctx.Err() != nil {
			err = cancelled()
		}
		if err == nil {
			err = e.granted(ctx, func() error { return step(i, keyboard) })
		}
		if err != nil {
			var native *Error
			if i > 0 && (!errors.As(err, &native) || native.Effect == "none") {
				return uncertain("Wayland keyboard input stopped part way: %v", err)
			}
			return err
		}
	}
	return nil
}

// requireFocus binds keys to the target. When the compositor's IPC names the
// focused window, its PID and title must be the target's; otherwise the
// accessible window must be active.
func (e *Engine) requireFocus(ctx context.Context, target GlobalTarget, keyboard waylandKeyboard) error {
	if keyboard.route != "" {
		return nil
	}
	if keyboard.virtual {
		active, err := keyboard.helper.ActiveWindow(ctx)
		if err != nil {
			return err
		}
		if active.Available {
			if active.PID == nil || *active.PID != target.PID || active.Title != target.Title {
				return fail("TARGET_UNAVAILABLE", "The compositor's focused window is not the target; keys would reach another window")
			}
			return nil
		}
	}
	return e.requireActive(ctx, target)
}

// requireActive refuses keys unless the observed window is the active one.
// Focus can still change between this check and delivery.
func (e *Engine) requireActive(ctx context.Context, target GlobalTarget) error {
	if target.Window.Bus == "" {
		return fail("TARGET_UNAVAILABLE", "The target window is unknown")
	}
	states, err := e.states(ctx, target.Window)
	title, titleErr := e.name(ctx, target.Window)
	if err != nil || titleErr != nil || title != target.Title {
		return fail("STALE_SNAPSHOT", "The target window changed since observation")
	}
	if !states.Has(StateActive) {
		return fail("TARGET_UNAVAILABLE", "The target window is not active; keys would reach another window")
	}
	return nil
}

// grantRoute picks the portal, or Mutter's API in an isolated test session.
func (e *Engine) grantRoute() string {
	if e.env("OPEN_COMPUTER_USE_WAYLAND_TEST") == "1" {
		return wayland.GrantMutterTest
	}
	return wayland.GrantPortal
}

// keyboard picks the session's keyboard route: the virtual keyboard when the
// compositor offers it, else the portal once remote input is granted.
func (e *Engine) keyboard(ctx context.Context) (waylandKeyboard, error) {
	helper, err := e.waylandHelper(ctx)
	if err != nil {
		return waylandKeyboard{}, err
	}
	if route := helper.Probe().AssistedRoute(); route != "" {
		return waylandKeyboard{helper: helper, route: route}, nil
	}
	if helper.Probe().VirtualKeyboardAvailable() {
		return waylandKeyboard{helper: helper, virtual: true}, nil
	}
	if availability := e.keyboardRouteAvailable(helper); availability.Status != "available" {
		return waylandKeyboard{}, fail(availability.Code, "%s", availability.Message)
	}
	granted, err := helper.KeyboardGranted(ctx)
	if err != nil {
		return waylandKeyboard{}, err
	}
	if !granted {
		if !e.config.GrantOnDemand {
			return waylandKeyboard{}, fail("PERMISSION_REQUIRED", "Wayland keyboard input needs the remoteDesktop permission; request it first")
		}
		if err := helper.GrantKeyboard(ctx, e.grantRoute()); err != nil {
			return waylandKeyboard{}, err
		}
	}
	return waylandKeyboard{helper: helper}, nil
}

// KeyboardNeedsGrant reports whether Wayland keys in this session go through the
// portal and therefore the remoteDesktop permission.
func (e *Engine) KeyboardNeedsGrant(ctx context.Context) bool {
	if !e.Wayland() {
		return false
	}
	helper, err := e.waylandHelper(ctx)
	return err != nil || !(helper.Probe().VirtualKeyboardAvailable() || helper.Probe().AssistedRoute() != "")
}

// KeyboardAvailability reports whether Wayland keyboard input can be offered at
// all, before any permission: the helper must run and the portal must support EIS.
func (e *Engine) KeyboardAvailability(ctx context.Context) display.Availability {
	helper, err := e.waylandHelper(ctx)
	if err != nil {
		var native *Error
		if !errors.As(err, &native) {
			native = fail("TARGET_UNAVAILABLE", "%v", err)
		}
		return display.Unavailable(native.Code, native.Message)
	}
	return e.keyboardRouteAvailable(helper)
}

// keyboardRouteAvailable checks what the grant route needs: the portal, or in an
// isolated test session Mutter's API, which the helper checks when granting.
func (e *Engine) keyboardRouteAvailable(helper *wayland.Helper) display.Availability {
	if helper.Probe().AssistedRoute() != "" || helper.Probe().VirtualKeyboardAvailable() || e.grantRoute() == wayland.GrantMutterTest {
		return display.Available()
	}
	return helper.Probe().KeyboardAvailable()
}

// KeyboardPermission reports the remoteDesktop permission in a Wayland session.
func (e *Engine) KeyboardPermission(ctx context.Context) (status string, err error) {
	helper, err := e.waylandHelper(ctx)
	if err != nil {
		return "unavailable", err
	}
	if e.keyboardRouteAvailable(helper).Status != "available" {
		return "unavailable", nil
	}
	granted, err := helper.KeyboardGranted(ctx)
	if err != nil {
		return "unavailable", err
	}
	if granted {
		return "granted", nil
	}
	return "notDetermined", nil
}

// RequestKeyboard asks for remote keyboard input; the portal shows its dialog.
func (e *Engine) RequestKeyboard(ctx context.Context) error {
	helper, err := e.waylandHelper(ctx)
	if err != nil {
		return err
	}
	return helper.GrantKeyboard(ctx, e.grantRoute())
}
