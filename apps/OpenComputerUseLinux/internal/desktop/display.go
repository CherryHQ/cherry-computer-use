package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/iFurySt/open-codex-computer-use/apps/opencomputeruselinux/internal/display"
	"github.com/iFurySt/open-codex-computer-use/apps/opencomputeruselinux/internal/display/wayland"
	"github.com/iFurySt/open-codex-computer-use/apps/opencomputeruselinux/internal/display/x11"
)

// GlobalTarget and Point are the display package's types.
type (
	GlobalTarget = display.Target
	Point        = display.Point
)

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
	return e.injector().WindowRect(ctx, target)
}

func (e *Engine) PointerClick(ctx context.Context, target GlobalTarget, point Point, button byte, count int) error {
	return e.injector().PointerClick(ctx, target, point, button, count)
}

func (e *Engine) Drag(ctx context.Context, target GlobalTarget, from, to Point) error {
	return e.injector().Drag(ctx, target, from, to)
}

func (e *Engine) PressKey(ctx context.Context, target GlobalTarget, key string) error {
	return e.injector().PressKey(ctx, target, key)
}

func (e *Engine) TypeKeys(ctx context.Context, target GlobalTarget, text string) error {
	return e.injector().TypeKeys(ctx, target, text)
}

func (e *Engine) PageScroll(ctx context.Context, target GlobalTarget, direction string, pages float64) error {
	return e.injector().PageScroll(ctx, target, direction, pages)
}
