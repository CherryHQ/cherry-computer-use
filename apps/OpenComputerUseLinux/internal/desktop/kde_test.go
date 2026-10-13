package desktop

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// kwinScript runs JavaScript in the isolated test session's KWin.
func kwinScript(t *testing.T, js string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "script.js")
	if err := os.WriteFile(path, []byte(js), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("gdbus", "call", "--session", "--dest", "org.kde.KWin", "--object-path", "/Scripting",
		"--method", "org.kde.kwin.Scripting.loadScript", path, "ocu-test").CombinedOutput()
	if err != nil {
		t.Fatalf("loadScript: %v %s", err, out)
	}
	id := strings.Trim(strings.TrimSpace(string(out)), "(),")
	if out, err := exec.Command("gdbus", "call", "--session", "--dest", "org.kde.KWin", "--object-path", "/Scripting/Script"+id,
		"--method", "org.kde.kwin.Script.run").CombinedOutput(); err != nil {
		t.Fatalf("run: %v %s", err, out)
	}
	time.Sleep(300 * time.Millisecond)
	_ = exec.Command("gdbus", "call", "--session", "--dest", "org.kde.KWin", "--object-path", "/Scripting",
		"--method", "org.kde.kwin.Scripting.unloadScript", "ocu-test").Run()
}

// TestKDEIntegration drives capture, pointer and keyboard input through KWin
// inside testdata/kwin-session.sh, started with OCU_KDE_INTEGRATION and
// OPEN_COMPUTER_USE_WAYLAND_HELPER.
func TestKDEIntegration(t *testing.T) {
	helper := os.Getenv("OPEN_COMPUTER_USE_WAYLAND_HELPER")
	if os.Getenv("OPEN_COMPUTER_USE_LINUX_KDE_TEST") != "1" || helper == "" || os.Getenv("OCU_KDE_INTEGRATION") == "" {
		t.Skip("run inside testdata/kwin-session.sh with OCU_KDE_INTEGRATION and OPEN_COMPUTER_USE_WAYLAND_HELPER set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	title := fmt.Sprintf("KDE Target %d", os.Getpid())
	startGTKFixture(t, title)
	engine := New(Config{WaylandHelper: helper})
	t.Cleanup(func() { _ = engine.Close() })
	app := resolveEventually(t, ctx, engine, title)
	code := func(err error) (string, string) {
		var native *Error
		if errors.As(err, &native) {
			return native.Code, native.Effect
		}
		return fmt.Sprint(err), ""
	}
	observe := func(name func(string) bool) (Node, Window) {
		observation, err := engine.Observe(ctx, app, TreeOptions{MaxNodes: 200, MaxDepth: 20, TextReadLimit: -1})
		if err != nil {
			t.Fatal(err)
		}
		for _, node := range observation.Nodes {
			if name(node.Name) {
				return node, observation.Window
			}
		}
		t.Fatal("no such element")
		return Node{}, Window{}
	}
	button := func() (Node, Window) { return observe(func(n string) bool { return strings.HasPrefix(n, "Count: ") }) }
	field := func() Node { node, _ := observe(func(n string) bool { return n == "Second field" }); return node }
	eventually := func(what string, read func() string, want string) {
		t.Helper()
		for attempt := 0; attempt < 30; attempt++ {
			if read() == want {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("%s = %q, want %q", what, read(), want)
	}
	count := func() string { node, _ := button(); return node.Name }
	_, window := button()
	target := GlobalTarget{PID: app.PID, Title: title, Window: window.Ref}

	if status, applies := engine.KDEPermission(ctx); !applies || status != "granted" {
		t.Fatalf("KDE permission = %s, %v", status, applies)
	}

	// Capture: the window's logical content size, with real content.
	rect, err := engine.WindowRect(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	capture := engine.Capture(ctx, app.PID, title)
	if capture.Reason != nil {
		t.Fatalf("capture: %v", capture.Reason)
	}
	if capture.Width != int(rect.Width) || capture.Height != int(rect.Height) {
		t.Fatalf("capture is %dx%d, window is %+v", capture.Width, capture.Height, rect)
	}
	decoded, err := png.Decode(bytes.NewReader(capture.PNG))
	if err != nil {
		t.Fatal(err)
	}
	colours := map[color.Color]bool{}
	for y := 0; y < capture.Height; y += 7 {
		for x := 0; x < capture.Width; x += 7 {
			colours[decoded.At(x, y)] = true
		}
	}
	if len(colours) < 3 {
		t.Fatalf("capture looks blank: %d colours", len(colours))
	}

	// Another window covers the target: capture still works, clicks are refused.
	cover := title + " cover"
	startGTKFixture(t, cover)
	place := func(name string, x, y, width, height int) {
		kwinScript(t, fmt.Sprintf("const w = workspace.stackingOrder.find(w => w.caption === %q); w.frameGeometry = { x: %d, y: %d, width: %d, height: %d }; workspace.activeWindow = w;", name, x, y, width, height))
	}
	place(title, 100, 100, 400, 360)
	place(cover, 50, 50, 600, 500)
	if capture := engine.Capture(ctx, app.PID, title); capture.Reason != nil {
		t.Fatalf("capture while covered: %v", capture.Reason)
	}
	center := func() Point {
		var err error
		for attempt := 0; attempt < 50; attempt++ {
			node, window := button()
			current, revalidateErr := engine.Revalidate(ctx, window, node)
			if revalidateErr != nil {
				t.Fatal(revalidateErr)
			}
			var point Point
			if point, err = engine.ElementCenter(ctx, target, current); err == nil {
				return point
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatal(err)
		return Point{}
	}
	covered := engine.PointerClick(ctx, target, center(), 1, 1)
	if c, effect := code(covered); c != "TARGET_UNAVAILABLE" || effect != "none" || !strings.Contains(covered.Error(), cover) {
		t.Fatalf("click under another window: %s/%s %v", c, effect, covered)
	}
	eventually("count", count, "Count: 0")

	// On top, clicks, double clicks and drags reach the target.
	place(title, 100, 100, 400, 360)
	for attempt := 0; ; attempt++ {
		err := engine.PointerClick(ctx, target, center(), 1, 1)
		if err == nil {
			break
		}
		if c, effect := code(err); c != "TARGET_UNAVAILABLE" || effect != "none" || attempt > 30 {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	eventually("count", count, "Count: 1")
	if err := engine.PointerClick(ctx, target, center(), 1, 2); err != nil {
		t.Fatal(err)
	}
	eventually("count", count, "Count: 3")
	from := center()
	if err := engine.Drag(ctx, target, Point{X: from.X - 20, Y: from.Y}, Point{X: from.X + 20, Y: from.Y}); err != nil {
		t.Fatal(err)
	}
	eventually("count", count, "Count: 4")

	// Keys and layout text go to the focused field of the active window.
	var focused bool
	if err := engine.call(ctx, field().Ref, "Component.GrabFocus").Store(&focused); err != nil || !focused {
		t.Fatalf("focus the field: %v %v", focused, err)
	}
	time.Sleep(300 * time.Millisecond)
	if err := engine.PressKey(ctx, target, "ctrl+a"); err != nil {
		t.Fatal(err)
	}
	if err := engine.TypeKeys(ctx, target, "Hello, KDE! 123"); err != nil {
		t.Fatal(err)
	}
	eventually("field", func() string { return field().Text }, "Hello, KDE! 123")
	if c, effect := code(engine.TypeKeys(ctx, target, "中")); c != "UNSUPPORTED_CAPABILITY" || effect != "none" {
		t.Fatalf("text the layout lacks: %s/%s", c, effect)
	}
	place(cover, 50, 50, 600, 500)
	if c, effect := code(engine.TypeKeys(ctx, target, "x")); c != "TARGET_UNAVAILABLE" || effect != "none" {
		t.Fatalf("typing while another window is active: %s/%s", c, effect)
	}
	eventually("field", func() string { return field().Text }, "Hello, KDE! 123")
}
