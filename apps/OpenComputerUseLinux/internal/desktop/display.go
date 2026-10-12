package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
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
	report.Capture = display.Unsupported("Wayland capture is not connected")
	if report.GlobalInput.Status == "available" {
		report.GlobalInput.Message = "XTEST reaches XWayland windows only; native Wayland windows are refused"
	}
	report.Helper = &HelperReport{Availability: display.Available()}
	helper, err := e.waylandHelper(ctx)
	if err != nil {
		var native *Error
		if !errors.As(err, &native) {
			native = fail("TARGET_UNAVAILABLE", "%v", err)
		}
		report.Helper.Availability = display.Unavailable(native.Code, native.Message)
		return report
	}
	report.Helper.Version, report.Helper.Probe = helper.Version(), helper.Probe().Raw
	return report
}

// capturer selects the backend for window capture in this session.
func (e *Engine) capturer() (display.Capturer, *Error) {
	if e.Wayland() {
		return nil, fail("UNSUPPORTED_CAPABILITY", "Wayland capture is not connected")
	}
	return e.x11Backend(), nil
}

// injector selects the backend for global input. XTEST reaches X11 sessions and,
// in a Wayland session, only XWayland windows; callers decide whether to allow that.
func (e *Engine) injector() display.Injector { return e.x11Backend() }

// Capture captures the window of pid whose title is the observed accessible title.
func (e *Engine) Capture(ctx context.Context, pid uint32, title string) Capture {
	capturer, reason := e.capturer()
	if reason != nil {
		return Capture{Reason: reason}
	}
	image, err := capturer.Capture(ctx, display.Target{PID: pid, Title: title})
	if err != nil {
		var native *Error
		if !errors.As(err, &native) {
			native = fail("CAPTURE_FAILED", "%v", err)
		}
		return Capture{Reason: native}
	}
	return Capture{PNG: image.PNG, Width: image.Width, Height: image.Height}
}

func (e *Engine) WindowRect(ctx context.Context, target GlobalTarget) (Rect, error) {
	return e.injector().WindowRect(ctx, target.native())
}

func (e *Engine) PointerClick(ctx context.Context, target GlobalTarget, point Point, button byte, count int) error {
	return e.injector().PointerClick(ctx, target.native(), point, button, count)
}

func (e *Engine) Drag(ctx context.Context, target GlobalTarget, from, to Point) error {
	return e.injector().Drag(ctx, target.native(), from, to)
}

// x11Keys reports whether keys for target go through XTEST: always in X11, and in
// Wayland only for XWayland windows when the caller allows it.
func (e *Engine) x11Keys(ctx context.Context, target GlobalTarget) bool {
	return !e.Wayland() || (e.config.XWaylandInput && e.x11Backend().HasWindow(ctx, target.native()))
}

func (e *Engine) PressKey(ctx context.Context, target GlobalTarget, key string) error {
	if e.x11Keys(ctx, target) {
		return e.injector().PressKey(ctx, target.native(), key)
	}
	chord, err := display.ParseKeyChord(key)
	if err != nil {
		return err
	}
	return e.waylandKeys(ctx, target, 1, func(_ int, keyboard *wayland.Helper) error { return keyboard.Chord(ctx, chord) })
}

func (e *Engine) TypeKeys(ctx context.Context, target GlobalTarget, text string) error {
	if e.x11Keys(ctx, target) {
		return e.injector().TypeKeys(ctx, target.native(), text)
	}
	keyboard, err := e.keyboard(ctx)
	if err != nil {
		return err
	}
	missing, err := keyboard.MissingCharacters(ctx, text)
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		return fail("UNSUPPORTED_CAPABILITY", "The keyboard layout cannot type %q; Wayland input has no way to enter it", missing)
	}
	characters := []rune(text)
	return e.waylandKeys(ctx, target, len(characters), func(i int, keyboard *wayland.Helper) error { return keyboard.Text(ctx, string(characters[i])) })
}

func (e *Engine) PageScroll(ctx context.Context, target GlobalTarget, direction string, pages float64) error {
	if e.x11Keys(ctx, target) {
		return e.injector().PageScroll(ctx, target.native(), direction, pages)
	}
	key := map[string]string{"up": "Page_Up", "down": "Page_Down", "left": "Left", "right": "Right"}[direction]
	if key == "" || pages <= 0 || pages > 100 {
		return fail("INVALID_ARGUMENT", "Invalid scroll direction or page count")
	}
	chord, _ := display.ParseKeyChord(key)
	count := int(math.Ceil(pages))
	return e.waylandKeys(ctx, target, count, func(_ int, keyboard *wayland.Helper) error { return keyboard.Chord(ctx, chord) })
}

// waylandKeys sends count key steps through the libei keyboard, confirming
// before each one that the target window is active. A refusal after the first
// step reports a possible effect.
func (e *Engine) waylandKeys(ctx context.Context, target GlobalTarget, count int, step func(int, *wayland.Helper) error) error {
	keyboard, err := e.keyboard(ctx)
	if err != nil {
		return err
	}
	for i := 0; i < count; i++ {
		err := e.requireActive(ctx, target)
		if err == nil && ctx.Err() != nil {
			err = cancelled()
		}
		if err == nil {
			err = step(i, keyboard)
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

// keyboard returns the helper once remote keyboard input is granted.
func (e *Engine) keyboard(ctx context.Context) (*wayland.Helper, error) {
	helper, err := e.waylandHelper(ctx)
	if err != nil {
		return nil, err
	}
	if availability := helper.Probe().KeyboardAvailable(); availability.Status != "available" {
		return nil, fail(availability.Code, "%s", availability.Message)
	}
	granted, err := helper.KeyboardGranted(ctx)
	if err != nil {
		return nil, err
	}
	if !granted {
		if !e.config.GrantOnDemand {
			return nil, fail("PERMISSION_REQUIRED", "Wayland keyboard input needs the remoteDesktop permission; request it first")
		}
		if err := helper.GrantKeyboard(ctx, e.grantRoute()); err != nil {
			return nil, err
		}
	}
	return helper, nil
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
	return helper.Probe().KeyboardAvailable()
}

// KeyboardPermission reports the remoteDesktop permission in a Wayland session.
func (e *Engine) KeyboardPermission(ctx context.Context) (status string, err error) {
	helper, err := e.waylandHelper(ctx)
	if err != nil {
		return "unavailable", err
	}
	if helper.Probe().KeyboardAvailable().Status != "available" {
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
