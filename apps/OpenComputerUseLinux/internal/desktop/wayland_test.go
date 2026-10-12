package desktop

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestGNOMEWaylandKeyboard drives libei keyboard input through the Wayland helper
// in testdata/gnome-session.sh; set OPEN_COMPUTER_USE_WAYLAND_HELPER to a built helper.
func TestGNOMEWaylandKeyboard(t *testing.T) {
	helper := os.Getenv("OPEN_COMPUTER_USE_WAYLAND_HELPER")
	if os.Getenv("OPEN_COMPUTER_USE_LINUX_GNOME_TEST") != "1" || helper == "" {
		t.Skip("run inside testdata/gnome-session.sh with OPEN_COMPUTER_USE_WAYLAND_HELPER set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	startFixture := func(title string) *exec.Cmd {
		fixture := exec.Command("python3", filepath.Join("..", "..", "testdata", "gtk_fixture.py"), title)
		stdout, _ := fixture.StdoutPipe()
		if err := fixture.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = fixture.Process.Kill(); _ = fixture.Wait() })
		if line, _ := bufio.NewReader(stdout).ReadString('\n'); strings.TrimSpace(line) != "ready" {
			t.Fatalf("fixture did not start: %q", line)
		}
		return fixture
	}
	title := fmt.Sprintf("Wayland Keys %d", os.Getpid())
	startFixture(title)
	engine := New(Config{WaylandHelper: helper})
	t.Cleanup(func() { _ = engine.Close() })
	var app App
	var err error
	for attempt := 0; ; attempt++ {
		if app, err = engine.Resolve(ctx, title); err == nil {
			break
		}
		if attempt > 100 {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	field := func() (string, Window) {
		observation, err := engine.Observe(ctx, app, TreeOptions{MaxNodes: 200, MaxDepth: 20, TextReadLimit: -1})
		if err != nil {
			t.Fatal(err)
		}
		for _, node := range observation.Nodes {
			if node.Name == "Second field" {
				return node.Text, observation.Window
			}
		}
		t.Fatal("no Second field")
		return "", Window{}
	}
	eventually := func(want string) {
		t.Helper()
		for attempt := 0; attempt < 30; attempt++ {
			if text, _ := field(); text == want {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		text, _ := field()
		t.Fatalf("field = %q, want %q", text, want)
	}
	code := func(err error) (string, string) {
		var native *Error
		if errors.As(err, &native) {
			return native.Code, native.Effect
		}
		return fmt.Sprint(err), ""
	}
	_, window := field()
	target := GlobalTarget{PID: app.PID, Title: title, Window: window.Ref}
	if c, effect := code(engine.PressKey(ctx, target, "ctrl+a")); c != "PERMISSION_REQUIRED" || effect != "none" {
		t.Fatalf("keys before the grant: %s/%s", c, effect)
	}
	if status, err := engine.KeyboardPermission(ctx); status != "notDetermined" || err != nil {
		t.Fatalf("permission before the grant = %s, %v", status, err)
	}
	if err := engine.RequestKeyboard(ctx); err != nil {
		t.Fatal(err)
	}
	if status, _ := engine.KeyboardPermission(ctx); status != "granted" {
		t.Fatalf("permission after the grant = %s", status)
	}
	// A headless compositor starts without any keyboard: the granted EIS keyboard
	// is the seat's first, so focus arrives only now and clients bind their
	// keyboard asynchronously; keys sent before that are lost. Desktops with a
	// physical keyboard do not have this window.
	for attempt := 0; attempt < 50 && engine.requireActive(ctx, target) != nil; attempt++ {
		time.Sleep(100 * time.Millisecond)
	}
	time.Sleep(time.Second)

	// Existing text with the caret at its end and no selection: Ctrl+A must select it.
	observation, err := engine.Observe(ctx, app, TreeOptions{MaxNodes: 200, MaxDepth: 20, TextReadLimit: -1})
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range observation.Nodes {
		if node.Name == "Second field" {
			if err := engine.SetValue(ctx, observation.Window, node, "abc"); err != nil {
				t.Fatal(err)
			}
		}
	}
	eventually("abc")
	if err := engine.PressKey(ctx, target, "End"); err != nil {
		t.Fatal(err)
	}
	if err := engine.PressKey(ctx, target, "ctrl+a"); err != nil {
		t.Fatal(err)
	}
	if err := engine.TypeKeys(ctx, target, "Hello, Wayland! 123"); err != nil {
		t.Fatal(err)
	}
	eventually("Hello, Wayland! 123")
	if c, effect := code(engine.TypeKeys(ctx, target, "ok中")); c != "UNSUPPORTED_CAPABILITY" || effect != "none" {
		t.Fatalf("characters the layout lacks: %s/%s", c, effect)
	}
	eventually("Hello, Wayland! 123")

	// Another window becomes active: keys for the observed window are refused.
	startFixture(title + " other")
	for attempt := 0; attempt < 50 && engine.requireActive(ctx, target) == nil; attempt++ {
		time.Sleep(100 * time.Millisecond)
	}
	if c, effect := code(engine.TypeKeys(ctx, target, "x")); c != "TARGET_UNAVAILABLE" || effect != "none" {
		t.Fatalf("typing while another window is active: %s/%s", c, effect)
	}
	if c, _ := code(engine.PressKey(ctx, target, "Return")); c != "TARGET_UNAVAILABLE" {
		t.Fatalf("keys while another window is active: %s", c)
	}
	eventually("Hello, Wayland! 123")
}
