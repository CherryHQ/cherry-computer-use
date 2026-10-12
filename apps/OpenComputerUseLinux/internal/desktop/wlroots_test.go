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

// TestWlrootsVirtualKeyboard drives the wlroots virtual keyboard through the
// Wayland helper inside testdata/wlroots-session.sh (Sway or nested Hyprland).
func TestWlrootsVirtualKeyboard(t *testing.T) {
	helper := os.Getenv("OPEN_COMPUTER_USE_WAYLAND_HELPER")
	compositor := os.Getenv("OPEN_COMPUTER_USE_LINUX_WLROOTS_TEST")
	if compositor == "" || helper == "" {
		t.Skip("run inside testdata/wlroots-session.sh with OPEN_COMPUTER_USE_WAYLAND_HELPER set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	startFixture := func(title string) {
		fixture := exec.Command("python3", filepath.Join("..", "..", "testdata", "gtk_fixture.py"), title)
		stdout, _ := fixture.StdoutPipe()
		if err := fixture.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = fixture.Process.Kill(); _ = fixture.Wait() })
		if line, _ := bufio.NewReader(stdout).ReadString('\n'); strings.TrimSpace(line) != "ready" {
			t.Fatalf("fixture did not start: %q", line)
		}
	}
	title := fmt.Sprintf("Wlroots Keys %d", os.Getpid())
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
	field := func() (Node, Window) {
		observation, err := engine.Observe(ctx, app, TreeOptions{MaxNodes: 200, MaxDepth: 20, TextReadLimit: -1})
		if err != nil {
			t.Fatal(err)
		}
		for _, node := range observation.Nodes {
			if node.Name == "Second field" {
				return node, observation.Window
			}
		}
		t.Fatal("no Second field")
		return Node{}, Window{}
	}
	eventually := func(want string) {
		t.Helper()
		for attempt := 0; attempt < 30; attempt++ {
			if node, _ := field(); node.Text == want {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		node, _ := field()
		t.Fatalf("field = %q, want %q", node.Text, want)
	}
	code := func(err error) (string, string) {
		var native *Error
		if errors.As(err, &native) {
			return native.Code, native.Effect
		}
		return fmt.Sprint(err), ""
	}

	keyboard, err := engine.keyboard(ctx)
	if err != nil || !keyboard.virtual {
		t.Fatalf("%s must use the virtual keyboard without a grant: %+v, %v", compositor, keyboard, err)
	}
	if engine.KeyboardNeedsGrant(ctx) {
		t.Fatal("the virtual keyboard needs no remoteDesktop permission")
	}
	node, window := field()
	target := GlobalTarget{PID: app.PID, Title: title, Window: window.Ref}
	for attempt := 0; attempt < 50 && engine.requireFocus(ctx, target, keyboard) != nil; attempt++ {
		time.Sleep(100 * time.Millisecond)
	}
	if err := engine.requireFocus(ctx, target, keyboard); err != nil {
		t.Fatalf("the fixture never had focus: %v", err)
	}

	// Existing text with the caret at its end and no selection: Ctrl+A must select it.
	if err := engine.SetValue(ctx, window, node, "abc"); err != nil {
		t.Fatal(err)
	}
	eventually("abc")
	if err := engine.PressKey(ctx, target, "End"); err != nil {
		t.Fatal(err)
	}
	if err := engine.PressKey(ctx, target, "ctrl+a"); err != nil {
		t.Fatal(err)
	}
	if err := engine.TypeKeys(ctx, target, "Z 中文 é!"); err != nil {
		t.Fatal(err)
	}
	eventually("Z 中文 é!")

	// Another window takes focus: keys for the observed window are refused.
	startFixture(title + " other")
	for attempt := 0; attempt < 50 && engine.requireFocus(ctx, target, keyboard) == nil; attempt++ {
		time.Sleep(100 * time.Millisecond)
	}
	if c, effect := code(engine.TypeKeys(ctx, target, "x")); c != "TARGET_UNAVAILABLE" || effect != "none" {
		t.Fatalf("typing while another window has focus: %s/%s", c, effect)
	}
	if c, _ := code(engine.PressKey(ctx, target, "Return")); c != "TARGET_UNAVAILABLE" {
		t.Fatalf("keys while another window has focus: %s", c)
	}
	eventually("Z 中文 é!")
}
