package desktop

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	title := fmt.Sprintf("Wlroots Keys %d", os.Getpid())
	startGTKFixture(t, title)
	engine := New(Config{WaylandHelper: helper})
	t.Cleanup(func() { _ = engine.Close() })
	app := resolveEventually(t, ctx, engine, title)
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
	startGTKFixture(t, title+" other")
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

// TestWlrootsPointerAndCapture captures the fixture window and drives the wlroots
// virtual pointer through the Wayland helper inside testdata/wlroots-session.sh.
func TestWlrootsPointerAndCapture(t *testing.T) {
	helper := os.Getenv("OPEN_COMPUTER_USE_WAYLAND_HELPER")
	compositor := os.Getenv("OPEN_COMPUTER_USE_LINUX_WLROOTS_TEST")
	if compositor == "" || helper == "" {
		t.Skip("run inside testdata/wlroots-session.sh with OPEN_COMPUTER_USE_WAYLAND_HELPER set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	title := fmt.Sprintf("Wlroots Pointer %d", os.Getpid())
	startGTKFixture(t, title)
	engine := New(Config{WaylandHelper: helper})
	t.Cleanup(func() { _ = engine.Close() })
	app := resolveEventually(t, ctx, engine, title)
	button := func() (Node, Window) {
		observation, err := engine.Observe(ctx, app, TreeOptions{MaxNodes: 200, MaxDepth: 20, TextReadLimit: -1})
		if err != nil {
			t.Fatal(err)
		}
		for _, node := range observation.Nodes {
			if strings.HasPrefix(node.Name, "Count: ") {
				return node, observation.Window
			}
		}
		t.Fatal("no counter button")
		return Node{}, Window{}
	}
	eventually := func(want string) {
		t.Helper()
		for attempt := 0; attempt < 30; attempt++ {
			if node, _ := button(); node.Name == want {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		node, _ := button()
		t.Fatalf("button = %q, want %q", node.Name, want)
	}
	code := func(err error) (string, string) {
		var native *Error
		if errors.As(err, &native) {
			return native.Code, native.Effect
		}
		return fmt.Sprint(err), ""
	}
	if availability := engine.PointerAvailability(ctx); availability.Status != "available" {
		t.Fatalf("pointer on %s: %+v", compositor, availability)
	}
	if availability := engine.CaptureAvailability(ctx); availability.Status != "available" {
		t.Fatalf("capture on %s: %+v", compositor, availability)
	}
	_, window := button()
	target := GlobalTarget{PID: app.PID, Title: title, Window: window.Ref}

	// The capture has the window's logical size and shows more than one colour.
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

	center := func() Point {
		node, window := button()
		current, err := engine.Revalidate(ctx, window, node)
		if err != nil {
			t.Fatal(err)
		}
		point, err := engine.ElementCenter(ctx, target, current)
		if err != nil {
			t.Fatal(err)
		}
		return point
	}
	if err := engine.PointerClick(ctx, target, center(), 1, 1); err != nil {
		t.Fatal(err)
	}
	eventually("Count: 1")
	if err := engine.PointerClick(ctx, target, center(), 1, 2); err != nil {
		t.Fatal(err)
	}
	eventually("Count: 3")
	// A press and release on the button with motion in between is a click too.
	from := center()
	if err := engine.Drag(ctx, target, Point{X: from.X - 5, Y: from.Y}, Point{X: from.X + 5, Y: from.Y}); err != nil {
		t.Fatal(err)
	}
	eventually("Count: 4")
	if c, effect := code(engine.PointerClick(ctx, target, Point{X: rect.X + rect.Width + 10, Y: rect.Y}, 1, 1)); c != "TARGET_UNAVAILABLE" || effect != "none" {
		t.Fatalf("click outside the window: %s/%s", c, effect)
	}

	// A floating window covers the target: the pointer lands on it, focus follows,
	// and the click is refused before any button is pressed. Sway can only copy an
	// output region, so its capture is refused too; Hyprland exports the window itself.
	cover := title + " cover"
	startGTKFixture(t, cover)
	command := []string{"swaymsg", fmt.Sprintf("[title=%q] floating enable, resize set 1024 768, move position 0 0", cover)}
	if compositor == "hyprland" {
		window := fmt.Sprintf("title:^(%s)$", regexp.QuoteMeta(cover))
		command = []string{"hyprctl", "--batch", fmt.Sprintf("dispatch setfloating %[1]s ; dispatch resizewindowpixel exact 1024 768,%[1]s ; dispatch movewindowpixel exact 0 0,%[1]s ; dispatch focuswindow %[1]s", window)}
	}
	for attempt := 0; ; attempt++ {
		output, err := exec.Command(command[0], command[1:]...).CombinedOutput()
		if err == nil && !strings.Contains(string(output), "No such window") && !strings.Contains(string(output), "No matching") {
			break
		}
		if attempt > 50 {
			t.Fatalf("%s: %v %s", command[0], err, output)
		}
		time.Sleep(100 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	point := center()
	err = engine.PointerClick(ctx, target, point, 1, 1)
	if c, effect := code(err); c != "TARGET_UNAVAILABLE" || effect != "none" || !strings.Contains(err.Error(), "did not take focus") {
		t.Fatalf("click under a floating window: %s/%s %v", c, effect, err)
	}
	if capture := engine.Capture(ctx, app.PID, title); (capture.Reason == nil) != (compositor == "hyprland") {
		t.Fatalf("capture under a floating window on %s: %v", compositor, capture.Reason)
	}
	eventually("Count: 4")
}

func startGTKFixture(t *testing.T, title string) {
	t.Helper()
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

func resolveEventually(t *testing.T, ctx context.Context, engine *Engine, title string) App {
	t.Helper()
	for attempt := 0; ; attempt++ {
		app, err := engine.Resolve(ctx, title)
		if err == nil {
			return app
		}
		if attempt > 100 {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
