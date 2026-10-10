// Package desktop is the Linux AT-SPI/X11 engine shared by the SDK runtime and
// the CLI/MCP service. It owns native connections and references; callers own
// their protocol, identities, snapshot validity and output formatting.
package desktop

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/iFurySt/open-codex-computer-use/apps/opencomputeruselinux/internal/display"
	"github.com/iFurySt/open-codex-computer-use/apps/opencomputeruselinux/internal/display/wayland"
	"github.com/iFurySt/open-codex-computer-use/apps/opencomputeruselinux/internal/display/x11"
)

// Error carries a stable code and whether the desktop may already have changed.
type Error = display.Error

func fail(code, format string, args ...any) *Error { return display.Fail(code, format, args...) }

func uncertain(format string, args ...any) *Error { return display.Uncertain(format, args...) }

// Ref is an AT-SPI object: a unique bus name and an object path on it.
type Ref struct {
	Bus  string
	Path dbus.ObjectPath
}

// Config selects the desktop session. A nil Env reads the process environment;
// the engine never writes the process environment itself.
type Config struct {
	Env map[string]string
	// InputGuard starts the guard process for global input on a display; nil
	// leaves global input unavailable. See x11.RunInputGuard.
	InputGuard func(display string) *exec.Cmd
	// WaylandHelper is the path of the Rust Wayland helper; empty means not installed.
	WaylandHelper string
}

// Engine is private to one runtime: code is shared, connections and references are not.
type Engine struct {
	config Config
	bus    *dbus.Conn
	x11    *x11.Backend
	helper *wayland.Helper
}

func New(config Config) *Engine { return &Engine{config: config} }

func (e *Engine) env(key string) string {
	if e.config.Env != nil {
		return e.config.Env[key]
	}
	return os.Getenv(key)
}

// Connect opens the AT-SPI bus once. A connection that later closes is reported
// instead of silently replaced, because references from it may no longer be valid.
func (e *Engine) Connect(ctx context.Context) error {
	if e.bus != nil {
		if !e.bus.Connected() {
			return &Error{Code: "TARGET_UNAVAILABLE", Message: "AT-SPI connection closed; start a new session", Effect: "none"}
		}
		return nil
	}
	address := e.env("AT_SPI_BUS_ADDRESS")
	if address == "" {
		var err error
		if address, err = e.accessibilityBusAddress(ctx); err != nil {
			return err
		}
	}
	bus, err := dbus.Connect(address)
	if err != nil {
		return fail("DEPENDENCY_MISSING", "Cannot connect to the AT-SPI bus")
	}
	e.bus = bus
	return nil
}

// Disconnected reports whether a previously opened connection has closed.
func (e *Engine) Disconnected() bool { return e.bus != nil && !e.bus.Connected() }

func (e *Engine) accessibilityBusAddress(ctx context.Context) (string, error) {
	var session *dbus.Conn
	var err error
	if address := e.env("DBUS_SESSION_BUS_ADDRESS"); address != "" {
		session, err = dbus.Dial(address, dbus.WithContext(ctx))
	} else if e.config.Env == nil {
		session, err = dbus.SessionBusPrivateNoAutoStartup(dbus.WithContext(ctx))
	} else {
		err = fmt.Errorf("no session bus address")
	}
	if err != nil {
		return "", fail("DEPENDENCY_MISSING", "A desktop D-Bus session is required")
	}
	defer session.Close()
	if err = session.Auth(nil); err != nil {
		return "", fail("DEPENDENCY_MISSING", "Cannot authenticate to the desktop D-Bus session")
	}
	if err = session.Hello(); err != nil {
		return "", fail("DEPENDENCY_MISSING", "Cannot join the desktop D-Bus session")
	}
	var address string
	if err = session.Object("org.a11y.Bus", "/org/a11y/bus").CallWithContext(ctx, "org.a11y.Bus.GetAddress", 0).Store(&address); err != nil {
		return "", fail("DEPENDENCY_MISSING", "The desktop AT-SPI bus is unavailable")
	}
	return address, nil
}

func (e *Engine) Close() error {
	e.closeDisplay()
	if e.bus == nil {
		return nil
	}
	err := e.bus.Close()
	e.bus = nil
	return err
}

// Wayland reports a Wayland session, where X11 capture and input do not apply.
// A declared session type wins over a Wayland socket that merely exists.
func (e *Engine) Wayland() bool {
	if session := e.env("XDG_SESSION_TYPE"); session != "" {
		return strings.EqualFold(session, "wayland")
	}
	return e.env("WAYLAND_DISPLAY") != ""
}

// Display is the X11 display used for capture, empty when none is configured.
func (e *Engine) Display() string { return e.env("DISPLAY") }

func (e *Engine) call(ctx context.Context, ref Ref, method string, args ...any) *dbus.Call {
	callCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	return e.bus.Object(ref.Bus, ref.Path).CallWithContext(callCtx, "org.a11y.atspi."+method, 0, args...)
}

func (e *Engine) property(ctx context.Context, ref Ref, iface, name string, result any) error {
	var value dbus.Variant
	callCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := e.bus.Object(ref.Bus, ref.Path).CallWithContext(callCtx, "org.freedesktop.DBus.Properties.Get", 0, "org.a11y.atspi."+iface, name).Store(&value); err != nil {
		return err
	}
	return dbus.Store([]any{value.Value()}, result)
}

// State is an AT-SPI StateType bit.
type State uint

const (
	StateActive   State = 1
	StateChecked  State = 4
	StateEditable State = 7
	StateEnabled  State = 8
	StateFocused  State = 12
	StateShowing  State = 25
	StateReadOnly State = 43
)

type States []uint32

func (s States) Has(state State) bool {
	word := int(state / 32)
	return word < len(s) && s[word]&(1<<(state%32)) != 0
}

func (e *Engine) states(ctx context.Context, ref Ref) (States, error) {
	var states []uint32
	err := e.call(ctx, ref, "Accessible.GetState").Store(&states)
	return states, err
}

func (e *Engine) name(ctx context.Context, ref Ref) (string, error) {
	var name string
	err := e.property(ctx, ref, "Accessible", "Name", &name)
	return name, err
}

func (e *Engine) role(ctx context.Context, ref Ref) (string, error) {
	var role string
	err := e.call(ctx, ref, "Accessible.GetRoleName").Store(&role)
	return role, err
}

func (e *Engine) children(ctx context.Context, ref Ref) ([]Ref, error) {
	var children []Ref
	err := e.call(ctx, ref, "Accessible.GetChildren").Store(&children)
	return children, err
}
