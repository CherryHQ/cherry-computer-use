package desktop

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestXWaylandXTEST drives an XWayland window with XTEST, as the CLI does in a
// Wayland session, inside a session script run with OCU_XWAYLAND=1. The other
// tests for each compositor cover the same windows through the compositor.
func TestXWaylandXTEST(t *testing.T) {
	if os.Getenv("OPEN_COMPUTER_USE_LINUX_XWAYLAND_TEST") != "1" {
		t.Skip("run inside a Wayland session script with OCU_XWAYLAND=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	title := fmt.Sprintf("XWayland XTEST %d", os.Getpid())
	startGTKFixture(t, title)
	engine := New(Config{WaylandHelper: os.Getenv("OPEN_COMPUTER_USE_WAYLAND_HELPER"), XWaylandInput: true, InputGuard: testGuard})
	t.Cleanup(func() { _ = engine.Close() })
	app := resolveEventually(t, ctx, engine, title)
	// An X11 window's accessible tree can fill in after the window appears.
	find := func(match func(Node) bool) (Node, Window) {
		for attempt := 0; attempt < 50; attempt++ {
			observation, err := engine.Observe(ctx, app, TreeOptions{MaxNodes: 200, MaxDepth: 20, TextReadLimit: -1})
			if err != nil {
				t.Fatal(err)
			}
			for _, node := range observation.Nodes {
				if match(node) {
					return node, observation.Window
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatal("no matching node")
		return Node{}, Window{}
	}
	field := func() (Node, Window) { return find(func(n Node) bool { return n.Name == "Second field" }) }
	button := func() (Node, Window) { return find(func(n Node) bool { return strings.HasPrefix(n.Name, "Count: ") }) }
	eventually := func(get func() (Node, Window), read func(Node) string, want string) {
		t.Helper()
		for attempt := 0; attempt < 30; attempt++ {
			if node, _ := get(); read(node) == want {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		node, _ := get()
		t.Fatalf("got %q, want %q", read(node), want)
	}
	text := func(n Node) string { return n.Text }
	name := func(n Node) string { return n.Name }

	node, window := field()
	target := GlobalTarget{PID: app.PID, Title: title, Window: window.Ref}
	if !engine.x11Input(ctx, target) {
		t.Fatal("an XWayland window in a Wayland session must take XTEST input when the caller allows it")
	}
	if err := engine.SetValue(ctx, window, node, "abc"); err != nil {
		t.Fatal(err)
	}
	eventually(field, text, "abc")
	// XTEST keys need X input focus, which follows the compositor's focus.
	var err error
	for attempt := 0; attempt < 50; attempt++ {
		if err = engine.PressKey(ctx, target, "End"); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("the XWayland window never had X focus: %v", err)
	}
	if err := engine.PressKey(ctx, target, "ctrl+a"); err != nil {
		t.Fatal(err)
	}
	if err := engine.TypeKeys(ctx, target, "Z 中文 é!"); err != nil {
		t.Fatal(err)
	}
	eventually(field, text, "Z 中文 é!")

	counter, _ := button()
	counter, err = engine.Revalidate(ctx, window, counter)
	if err != nil {
		t.Fatal(err)
	}
	point, err := engine.ElementCenter(ctx, target, counter)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.PointerClick(ctx, target, point, 1, 1); err != nil {
		t.Fatal(err)
	}
	eventually(button, name, "Count: 1")
}
