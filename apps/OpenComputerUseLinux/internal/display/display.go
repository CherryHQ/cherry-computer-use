// Package display holds the types shared by the Linux engine and its display
// backends: the X11 backend in package x11 and the Wayland helper client in
// package wayland. See docs/design-docs/linux-display-backends.md.
package display

import (
	"context"
	"fmt"
)

// Error carries a stable code and whether the desktop may already have changed.
type Error struct {
	Code, Message string
	// Effect is "none" when nothing was dispatched, "possible" when the outcome is unknown.
	Effect string
}

func (e *Error) Error() string { return e.Message }

// Fail is an error raised before anything was dispatched.
func Fail(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...), Effect: "none"}
}

// Uncertain is an error after dispatch, when the target may have changed.
func Uncertain(format string, args ...any) *Error {
	return &Error{Code: "TARGET_UNAVAILABLE", Message: fmt.Sprintf(format, args...), Effect: "possible"}
}

// Cancelled is a cancellation before the first dispatch.
func Cancelled() *Error { return Fail("CANCELLED", "Action cancelled before dispatch") }

// Target names the native window behind an observed accessible window: the
// window of PID whose title equals the observed title.
type Target struct {
	PID   uint32
	Title string
}

// Rect is in screen coordinates.
type Rect struct{ X, Y, Width, Height float64 }

// Point is in screen coordinates.
type Point struct{ X, Y float64 }

// Image is a PNG of one window.
type Image struct {
	PNG           []byte
	Width, Height int
}

// Availability reports one capability for this session, matching SDK wording.
type Availability struct {
	Status  string `json:"status"` // available, unavailable or unsupported
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

func Available() Availability { return Availability{Status: "available"} }

func Unavailable(code, message string) Availability {
	return Availability{Status: "unavailable", Code: code, Message: message}
}

func Unsupported(message string) Availability {
	return Availability{Status: "unsupported", Code: "UNSUPPORTED_CAPABILITY", Message: message}
}

// Capturer captures one target window.
type Capturer interface {
	Capture(ctx context.Context, target Target) (Image, error)
}

// Injector sends global input to one target window. Implementations confirm
// focus or the window under the point before each event and refuse with
// effect "none" when they cannot.
type Injector interface {
	WindowRect(ctx context.Context, target Target) (Rect, error)
	PointerClick(ctx context.Context, target Target, point Point, button byte, count int) error
	Drag(ctx context.Context, target Target, from, to Point) error
	PressKey(ctx context.Context, target Target, key string) error
	TypeKeys(ctx context.Context, target Target, text string) error
	PageScroll(ctx context.Context, target Target, direction string, pages float64) error
}
