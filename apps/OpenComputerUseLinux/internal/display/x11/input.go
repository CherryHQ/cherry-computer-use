package x11

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"github.com/iFurySt/open-codex-computer-use/apps/opencomputeruselinux/internal/display"
	"io"
	"math"
	"os/exec"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"
)

// input is one runtime's XTEST connection, its guard and what it currently holds.
type input struct {
	conn   *xgb.Conn
	root   xproto.Window
	guard  *exec.Cmd
	report io.WriteCloser
	exited chan struct{}
	held   []held
}

// Backend is one runtime's X11 display: window capture and XTEST input. It
// implements display.Capturer and display.Injector.
type Backend struct {
	display string
	guard   func(display string) *exec.Cmd
	input   *input
}

// New uses the X11 display name; guard starts the input guard (see RunInputGuard),
// and nil leaves global input unavailable.
func New(name string, guard func(display string) *exec.Cmd) *Backend {
	return &Backend{display: name, guard: guard}
}

// Close releases held input and stops the guard.
func (e *Backend) Close() {
	if e.input != nil {
		e.input.close()
		e.input = nil
	}
}

func (e *Backend) globalInput(ctx context.Context) (*input, error) {
	if in := e.input; in != nil {
		select {
		case <-in.exited:
			return nil, display.Fail("TARGET_UNAVAILABLE", "The input guard exited; start a new session before using global input")
		default:
			return in, nil
		}
	}
	name := e.display
	if name == "" {
		return nil, display.Fail("DEPENDENCY_MISSING", "Global input needs an X11 display")
	}
	if e.guard == nil {
		return nil, display.Fail("DEPENDENCY_MISSING", "Global input needs an input guard")
	}
	conn, err := xgb.NewConnDisplay(name)
	if err != nil {
		return nil, display.Fail("DEPENDENCY_MISSING", "Cannot connect to the X11 display")
	}
	if err := xtest.Init(conn); err != nil {
		conn.Close()
		return nil, display.Fail("DEPENDENCY_MISSING", "The X11 display does not offer XTEST input")
	}
	in := &input{conn: conn, root: xproto.Setup(conn).DefaultScreen(conn).Root, guard: e.guard(name), exited: make(chan struct{})}
	in.report, err = in.guard.StdinPipe()
	var ready io.ReadCloser
	if err == nil {
		ready, err = in.guard.StdoutPipe()
	}
	if err == nil {
		err = in.guard.Start()
	}
	if err != nil {
		conn.Close()
		return nil, display.Fail("DEPENDENCY_MISSING", "Cannot start the input guard: %v", err)
	}
	go func() { _ = in.guard.Wait(); close(in.exited) }()
	started := make(chan bool, 1)
	go func() {
		line, _ := bufio.NewReader(ready).ReadString('\n')
		started <- line == "ready\n"
	}()
	select {
	case ok := <-started:
		if ok {
			e.input = in
			return in, nil
		}
	case <-time.After(5 * time.Second):
	case <-ctx.Done():
	}
	_ = in.report.Close()
	_ = in.guard.Process.Kill()
	conn.Close()
	return nil, display.Fail("DEPENDENCY_MISSING", "The input guard did not start")
}

// close releases anything still held, then lets the guard exit with nothing to do.
func (in *input) close() {
	in.releaseAll()
	_ = in.report.Close()
	select {
	case <-in.exited:
	case <-time.After(2 * time.Second):
		_ = in.guard.Process.Kill()
	}
	in.conn.Close()
}

// press records input with the guard before sending it, so it is released even
// if this process dies between the press and its release.
func (in *input) press(item held, event byte, x, y int16) error {
	if _, err := fmt.Fprintf(in.report, "+%c %d\n", item.kind, item.detail); err != nil {
		return display.Fail("TARGET_UNAVAILABLE", "The input guard is not running")
	}
	in.held = append(in.held, item)
	xtest.FakeInput(in.conn, event, item.detail, 0, in.root, x, y, 0)
	return nil
}

func (in *input) release(item held, event byte) {
	index := slices.Index(in.held, item)
	if index < 0 {
		return
	}
	xtest.FakeInput(in.conn, event, item.detail, 0, in.root, 0, 0, 0)
	in.sync()
	in.held = slices.Delete(in.held, index, index+1)
	fmt.Fprintf(in.report, "-%c %d\n", item.kind, item.detail)
}

func (in *input) releaseAll() {
	for len(in.held) > 0 {
		item := in.held[len(in.held)-1]
		switch item.kind {
		case 'k':
			in.release(item, xproto.KeyRelease)
		case 'b':
			in.release(item, xproto.ButtonRelease)
		case 'm':
			in.unmap(xproto.Keycode(item.detail))
		}
	}
}

// sync waits until the server has processed everything sent so far.
func (in *input) sync() { _, _ = xproto.GetInputFocus(in.conn).Reply() }

func (in *input) keymap() (*keymap, error) {
	setup := xproto.Setup(in.conn)
	reply, err := xproto.GetKeyboardMapping(in.conn, setup.MinKeycode, byte(setup.MaxKeycode-setup.MinKeycode+1)).Reply()
	if err != nil {
		return nil, display.Fail("TARGET_UNAVAILABLE", "Cannot read the keyboard mapping")
	}
	return &keymap{min: setup.MinKeycode, perCode: int(reply.KeysymsPerKeycode), keysyms: reply.Keysyms, assigned: map[xproto.Keysym]xproto.Keycode{}}, nil
}

// assign maps a character to an unused keycode until unmap restores it.
func (in *input) assign(m *keymap, code xproto.Keycode, keysym xproto.Keysym) error {
	item := held{'m', byte(code)}
	if _, err := fmt.Fprintf(in.report, "+m %d\n", code); err != nil {
		return display.Fail("TARGET_UNAVAILABLE", "The input guard is not running")
	}
	in.held = append(in.held, item)
	symbols := make([]xproto.Keysym, m.perCode)
	symbols[0] = keysym
	if m.perCode > 1 {
		symbols[1] = keysym
	}
	xproto.ChangeKeyboardMapping(in.conn, 1, code, byte(m.perCode), symbols)
	m.assigned[keysym] = code
	return nil
}

func (in *input) unmap(code xproto.Keycode) {
	item := held{'m', byte(code)}
	index := slices.Index(in.held, item)
	if index < 0 {
		return
	}
	reply, err := xproto.GetKeyboardMapping(in.conn, code, 1).Reply()
	if err == nil {
		xproto.ChangeKeyboardMapping(in.conn, 1, code, reply.KeysymsPerKeycode, make([]xproto.Keysym, reply.KeysymsPerKeycode))
	}
	in.sync()
	in.held = slices.Delete(in.held, index, index+1)
	fmt.Fprintf(in.report, "-m %d\n", code)
}

// globalAction is one action's use of global input. Errors after the first
// press report a possible effect; whatever is held is released on return.
type globalAction struct {
	ctx    context.Context
	in     *input
	window xproto.Window
	sent   bool
}

func (e *Backend) withGlobalInput(ctx context.Context, target display.Target, run func(*globalAction) error) (err error) {
	if ctx.Err() != nil {
		return display.Cancelled()
	}
	in, err := e.globalInput(ctx)
	if err != nil {
		return err
	}
	window, err := FindWindow(in.conn, target.PID, target.Title)
	if err != nil {
		return display.Fail("TARGET_UNAVAILABLE", "%s", err.Error())
	}
	action := &globalAction{ctx: ctx, in: in, window: window}
	defer func() {
		in.releaseAll()
		in.sync()
		var native *display.Error
		if err != nil && action.sent && (!errors.As(err, &native) || native.Effect == "none") {
			err = display.Uncertain("Global input stopped part way: %v", err)
		}
	}()
	return run(action)
}

func (a *globalAction) checkCancelled() error {
	if a.ctx.Err() != nil {
		return display.Fail("CANCELLED", "Global input cancelled")
	}
	return nil
}

// within reports whether window is target or one of its descendants.
func (a *globalAction) within(window xproto.Window) bool {
	for depth := 0; window != 0 && depth < 64; depth++ {
		if window == a.window {
			return true
		}
		tree, err := xproto.QueryTree(a.in.conn, window).Reply()
		if err != nil || tree.Parent == window {
			return false
		}
		window = tree.Parent
	}
	return false
}

// requireFocus refuses keys unless the target window holds keyboard focus. Focus
// can still change between this check and delivery; the host must coordinate.
func (a *globalAction) requireFocus() error {
	focus, err := xproto.GetInputFocus(a.in.conn).Reply()
	if err != nil || focus.Focus <= xproto.InputFocusPointerRoot || !a.within(focus.Focus) {
		return display.Fail("TARGET_UNAVAILABLE", "The target window does not have keyboard focus; keys would reach another window")
	}
	return nil
}

// contains reports whether point lies inside the target window's area.
func (a *globalAction) contains(point display.Point) bool {
	geometry, err := xproto.GetGeometry(a.in.conn, xproto.Drawable(a.window)).Reply()
	if err != nil {
		return false
	}
	local, err := xproto.TranslateCoordinates(a.in.conn, a.in.root, a.window, int16(math.Round(point.X)), int16(math.Round(point.Y))).Reply()
	return err == nil && local.DstX >= 0 && local.DstY >= 0 && int(local.DstX) < int(geometry.Width) && int(local.DstY) < int(geometry.Height)
}

// pointAt moves the pointer and confirms the target window is what lies under it.
func (a *globalAction) pointAt(point display.Point) error {
	if err := a.checkCancelled(); err != nil {
		return err
	}
	x, y := int16(math.Round(point.X)), int16(math.Round(point.Y))
	xtest.FakeInput(a.in.conn, xproto.MotionNotify, 0, 0, a.in.root, x, y, 0)
	a.in.sync()
	window := a.in.root
	for depth := 0; depth < 64; depth++ {
		reply, err := xproto.QueryPointer(a.in.conn, window).Reply()
		if err != nil {
			return display.Fail("TARGET_UNAVAILABLE", "Cannot read the pointer position")
		}
		if reply.Child == 0 {
			break
		}
		window = reply.Child
	}
	if !a.within(window) {
		return display.Fail("TARGET_UNAVAILABLE", "Another window covers the point; the input would reach it instead")
	}
	return nil
}

func (a *globalAction) key(code xproto.Keycode, shift xproto.Keycode) error {
	if err := a.checkCancelled(); err != nil {
		return err
	}
	if err := a.requireFocus(); err != nil {
		return err
	}
	if shift != 0 {
		if err := a.in.press(held{'k', byte(shift)}, xproto.KeyPress, 0, 0); err != nil {
			return err
		}
	}
	if err := a.in.press(held{'k', byte(code)}, xproto.KeyPress, 0, 0); err != nil {
		return err
	}
	a.sent = true
	a.in.release(held{'k', byte(code)}, xproto.KeyRelease)
	if shift != 0 {
		a.in.release(held{'k', byte(shift)}, xproto.KeyRelease)
	}
	return nil
}

// WindowRect is the X window for target in root coordinates; SDK coordinates are
// relative to it because its capture is the screenshot callers see.
func (e *Backend) WindowRect(ctx context.Context, target display.Target) (display.Rect, error) {
	var rect display.Rect
	err := e.withGlobalInput(ctx, target, func(a *globalAction) error {
		geometry, err := xproto.GetGeometry(a.in.conn, xproto.Drawable(a.window)).Reply()
		if err != nil {
			return display.Fail("TARGET_UNAVAILABLE", "Window geometry is unavailable")
		}
		origin, err := xproto.TranslateCoordinates(a.in.conn, a.window, a.in.root, 0, 0).Reply()
		if err != nil {
			return display.Fail("TARGET_UNAVAILABLE", "Window position is unavailable")
		}
		rect = display.Rect{X: float64(origin.DstX), Y: float64(origin.DstY), Width: float64(geometry.Width), Height: float64(geometry.Height)}
		return nil
	})
	return rect, err
}

// PointerClick clicks button 1 (left), 2 (middle) or 3 (right) count times at point.
func (e *Backend) PointerClick(ctx context.Context, target display.Target, point display.Point, button byte, count int) error {
	if button < 1 || button > 3 || count < 1 || count > 3 {
		return display.Fail("INVALID_ARGUMENT", "Unsupported click button or count")
	}
	return e.withGlobalInput(ctx, target, func(a *globalAction) error {
		if err := a.pointAt(point); err != nil {
			return err
		}
		for i := 0; i < count; i++ {
			if err := a.checkCancelled(); err != nil {
				return err
			}
			if err := a.in.press(held{'b', button}, xproto.ButtonPress, 0, 0); err != nil {
				return err
			}
			a.sent = true
			time.Sleep(20 * time.Millisecond)
			a.in.release(held{'b', button}, xproto.ButtonRelease)
			time.Sleep(30 * time.Millisecond)
		}
		return nil
	})
}

// Drag holds the left button from one point to another inside the target window.
func (e *Backend) Drag(ctx context.Context, target display.Target, from, to display.Point) error {
	return e.withGlobalInput(ctx, target, func(a *globalAction) error {
		if !a.contains(to) {
			return display.Fail("INVALID_ARGUMENT", "The drag must end inside the target window")
		}
		if err := a.pointAt(from); err != nil {
			return err
		}
		if err := a.in.press(held{'b', 1}, xproto.ButtonPress, 0, 0); err != nil {
			return err
		}
		a.sent = true
		const steps = 12
		for step := 1; step <= steps; step++ {
			if err := a.checkCancelled(); err != nil {
				return err
			}
			time.Sleep(20 * time.Millisecond)
			x := from.X + (to.X-from.X)*float64(step)/steps
			y := from.Y + (to.Y-from.Y)*float64(step)/steps
			xtest.FakeInput(a.in.conn, xproto.MotionNotify, 0, 0, a.in.root, int16(math.Round(x)), int16(math.Round(y)), 0)
		}
		a.in.sync()
		time.Sleep(20 * time.Millisecond)
		a.in.release(held{'b', 1}, xproto.ButtonRelease)
		return nil
	})
}

// PressKey sends one chord to the focused target window.
func (e *Backend) PressKey(ctx context.Context, target display.Target, value string) error {
	chord, err := parseKeyChord(value)
	if err != nil {
		return err
	}
	return e.withGlobalInput(ctx, target, func(a *globalAction) error {
		m, err := a.in.keymap()
		if err != nil {
			return err
		}
		modifiers := []xproto.Keycode{}
		for _, keysym := range chord.modifiers {
			code, _, ok := m.lookup(keysym)
			if !ok {
				return display.Fail("UNSUPPORTED_CAPABILITY", "The keyboard has no modifier key %#x", keysym)
			}
			modifiers = append(modifiers, code)
		}
		code, shifted, ok := m.lookup(chord.key)
		if !ok {
			spare := m.spare()
			if len(spare) == 0 {
				return display.Fail("UNSUPPORTED_CAPABILITY", "No free keycode can carry key %q", value)
			}
			if err := a.in.assign(m, spare[0], chord.key); err != nil {
				return err
			}
			a.in.sync()
			code = spare[0]
		}
		if shifted && !slices.Contains(chord.modifiers, shiftKeysym) {
			shift, _, _ := m.lookup(shiftKeysym)
			modifiers = append(modifiers, shift)
		}
		if err := a.checkCancelled(); err != nil {
			return err
		}
		if err := a.requireFocus(); err != nil {
			return err
		}
		for _, modifier := range modifiers {
			if err := a.in.press(held{'k', byte(modifier)}, xproto.KeyPress, 0, 0); err != nil {
				return err
			}
			a.sent = true
		}
		if err := a.key(code, 0); err != nil {
			return err
		}
		// Mapped characters must outlive event processing before the mapping is restored.
		time.Sleep(50 * time.Millisecond)
		return nil
	})
}

// TypeKeys types text into the focused target window, temporarily mapping
// characters the keyboard layout lacks.
func (e *Backend) TypeKeys(ctx context.Context, target display.Target, text string) error {
	if !utf8.ValidString(text) {
		return display.Fail("INVALID_ARGUMENT", "Text is not valid UTF-8")
	}
	return e.withGlobalInput(ctx, target, func(a *globalAction) error {
		m, err := a.in.keymap()
		if err != nil {
			return err
		}
		shift, _, _ := m.lookup(shiftKeysym)
		spare := m.spare()
		runes := []rune(text)
		for start := 0; start < len(runes); {
			// Map this chunk's missing characters at once, then type it.
			end := start
			free := slices.Clone(spare)
			for ; end < len(runes); end++ {
				keysym := runeKeysym(runes[end])
				if _, _, ok := m.lookup(keysym); ok {
					continue
				}
				if len(free) == 0 {
					break
				}
				if err := a.in.assign(m, free[0], keysym); err != nil {
					return err
				}
				free = free[1:]
			}
			if end == start {
				return display.Fail("UNSUPPORTED_CAPABILITY", "No free keycode can carry %q", string(runes[start]))
			}
			a.in.sync()
			time.Sleep(30 * time.Millisecond)
			for _, r := range runes[start:end] {
				code, shifted, _ := m.lookup(runeKeysym(r))
				withShift := xproto.Keycode(0)
				if shifted {
					withShift = shift
				}
				if err := a.key(code, withShift); err != nil {
					return err
				}
				time.Sleep(5 * time.Millisecond)
			}
			// Clients translate keycodes when they read events, so keep mappings until then.
			time.Sleep(80 * time.Millisecond)
			for keysym, code := range m.assigned {
				a.in.unmap(code)
				delete(m.assigned, keysym)
			}
			start = end
		}
		return nil
	})
}

// PageScroll sends page keys to the focused target: up/down page vertically and
// left/right move horizontally, ceil(pages) times.
func (e *Backend) PageScroll(ctx context.Context, target display.Target, direction string, pages float64) error {
	key := map[string]string{"up": "Page_Up", "down": "Page_Down", "left": "Left", "right": "Right"}[direction]
	if key == "" || pages <= 0 || pages > 100 {
		return display.Fail("INVALID_ARGUMENT", "Invalid scroll direction or page count")
	}
	chord, _ := parseKeyChord(key)
	return e.withGlobalInput(ctx, target, func(a *globalAction) error {
		m, err := a.in.keymap()
		if err != nil {
			return err
		}
		code, _, ok := m.lookup(chord.key)
		if !ok {
			return display.Fail("UNSUPPORTED_CAPABILITY", "The keyboard has no %s key", key)
		}
		for i := 0; i < int(math.Ceil(pages)); i++ {
			if err := a.key(code, 0); err != nil {
				return err
			}
			time.Sleep(40 * time.Millisecond)
		}
		return nil
	})
}
