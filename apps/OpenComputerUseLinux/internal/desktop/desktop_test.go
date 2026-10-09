package desktop

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/godbus/dbus/v5"
	"github.com/jezek/xgb/xproto"
)

func TestCaptureDecodesDepth32Windows(t *testing.T) {
	cases := []struct {
		name   string
		order  byte
		depth  byte
		width  uint16
		height uint16
		data   []byte
		want   bool
	}{
		{"plain depth 24 window", xproto.ImageOrderLSBFirst, 24, 320, 160, make([]byte, 320*160*4), true},
		{"ARGB depth 32 window", xproto.ImageOrderLSBFirst, 32, 320, 160, make([]byte, 320*160*4), true},
		{"depth 16 window", xproto.ImageOrderLSBFirst, 16, 320, 160, make([]byte, 320*160*2), false},
		{"big endian server", xproto.ImageOrderMSBFirst, 24, 320, 160, make([]byte, 320*160*4), false},
		{"short reply", xproto.ImageOrderLSBFirst, 24, 320, 160, make([]byte, 320*160*4-4), false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := decodablePixelFormat(testCase.order, testCase.depth, testCase.width, testCase.height, testCase.data); got != testCase.want {
				t.Fatalf("decodablePixelFormat(depth=%d, order=%v) = %v, want %v", testCase.depth, testCase.order, got, testCase.want)
			}
		})
	}
}

func TestUnreadableDesktopIsNotReportedAsEmpty(t *testing.T) {
	reasons := []error{errors.New(":1.25: the accessible name is unavailable")}
	var native *Error
	if err := emptyDesktopError(3, 0, reasons); !errors.As(err, &native) || native.Code != "TARGET_UNAVAILABLE" {
		t.Fatalf("registered but unreadable applications must not look like an empty desktop: %v", err)
	}
	if err := emptyDesktopError(3, 1, reasons); err != nil {
		t.Fatalf("a partially readable desktop must still list what it could read: %v", err)
	}
	if err := emptyDesktopError(0, 0, nil); err != nil {
		t.Fatalf("a desktop with no registered applications is legitimately empty: %v", err)
	}
}

func TestClosedBusIsReportedInsteadOfReplaced(t *testing.T) {
	client, peer := net.Pipe()
	t.Cleanup(func() { peer.Close() })
	bus, err := dbus.NewConn(client)
	if err != nil {
		t.Fatal(err)
	}
	bus.Close()
	engine := New(Config{Env: map[string]string{"DBUS_SESSION_BUS_ADDRESS": "unix:path=/nonexistent-cherry-sdk-test-bus"}})
	engine.bus = bus
	_, _, err = engine.Apps(context.Background())
	var native *Error
	if !errors.As(err, &native) || native.Code != "TARGET_UNAVAILABLE" || native.Effect != "none" {
		t.Fatalf("closed bus must require a new session, not discover another bus: %v", err)
	}
	if !engine.Disconnected() {
		t.Fatal("a closed bus must be visible to callers that may reconnect")
	}
}

func TestExplicitEnvironmentDoesNotFallBackToProcessSession(t *testing.T) {
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/nonexistent-process-bus")
	engine := New(Config{Env: map[string]string{"WAYLAND_DISPLAY": "wayland-9"}})
	if !engine.Wayland() || engine.Display() != "" {
		t.Fatal("engine must read the configured session, not the process environment")
	}
	var native *Error
	if err := engine.Connect(context.Background()); !errors.As(err, &native) || native.Code != "DEPENDENCY_MISSING" {
		t.Fatalf("missing configured bus must be a dependency error: %v", err)
	}
}

func TestStatesReadBothWords(t *testing.T) {
	states := States{1<<StateEnabled | 1<<StateFocused, 1 << (StateReadOnly - 32)}
	for _, state := range []State{StateEnabled, StateFocused, StateReadOnly} {
		if !states.Has(state) {
			t.Fatalf("state %d missing", state)
		}
	}
	if states.Has(StateEditable) || (States{}).Has(StateReadOnly) {
		t.Fatal("absent states must not be reported")
	}
}

func TestClickActionPrefersExactNames(t *testing.T) {
	actions := []Action{{0, "expand or contract", ""}, {1, "activate-link", ""}, {2, "", "Toggle"}}
	if action, ok := ClickAction(actions); !ok || action.Index != 2 {
		t.Fatalf("exact description should win over substring fallback: %+v", action)
	}
	if action, ok := ClickAction(actions[:2]); !ok || action.Index != 1 {
		t.Fatalf("substring fallback expected: %+v", action)
	}
	if _, ok := ClickAction([]Action{{0, "menu", ""}}); ok {
		t.Fatal("no click-like action must not be invented")
	}
}

func TestMatchActionsReportsDuplicates(t *testing.T) {
	actions := []Action{{0, "Open", ""}, {1, "copy", "open"}, {2, "close", ""}}
	if got := MatchActions(actions, "OPEN"); len(got) != 2 {
		t.Fatalf("duplicate labels must all be reported so callers can refuse: %+v", got)
	}
	if got := MatchActions(actions, "missing"); len(got) != 0 {
		t.Fatalf("unexpected match: %+v", got)
	}
}

func TestSettableRespectsReadOnlyAndEditableStates(t *testing.T) {
	text := Node{Interfaces: []string{"org.a11y.atspi.Text", "org.a11y.atspi.EditableText"}, States: States{1<<StateEnabled | 1<<StateEditable}}
	if !text.Settable() {
		t.Fatal("enabled editable text should be settable")
	}
	locked := text
	locked.States = States{1 << StateEnabled}
	if locked.Settable() {
		t.Fatal("non-editable text must not advertise setValue")
	}
	number := Node{Interfaces: []string{"org.a11y.atspi.Value"}, States: States{1 << StateEnabled, 1 << (StateReadOnly - 32)}}
	if number.Settable() {
		t.Fatal("read-only values must not advertise setValue")
	}
}

func TestMainWindowPrefersActiveThenShowing(t *testing.T) {
	windows := []Window{{Title: "first"}, {Title: "showing", States: States{1 << StateShowing}}, {Title: "active", States: States{1 << StateActive}}}
	if window, _ := MainWindow(App{}, windows); window.Title != "active" {
		t.Fatalf("active window expected, got %q", window.Title)
	}
	if window, _ := MainWindow(App{}, windows[:2]); window.Title != "showing" {
		t.Fatalf("showing window expected, got %q", window.Title)
	}
	if _, err := MainWindow(App{Name: "x"}, nil); err == nil {
		t.Fatal("an application without windows must be an error")
	}
}
