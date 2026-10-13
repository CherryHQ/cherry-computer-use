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
	"strings"
	"testing"
	"time"
)

// shellEval runs JavaScript in the isolated test shell (unsafe mode only).
func shellEval(t *testing.T, js string) string {
	t.Helper()
	out, err := exec.Command("gdbus", "call", "--session", "--dest", "org.gnome.Shell", "--object-path", "/org/gnome/Shell",
		"--method", "org.gnome.Shell.Eval", js).CombinedOutput()
	if err != nil {
		t.Fatalf("Shell.Eval: %v %s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// answerShellDialog answers the extension's authorization dialog once it opens.
func answerShellDialog(t *testing.T, allow bool) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for attempt := 0; attempt < 100; attempt++ {
			out, _ := exec.Command("gdbus", "call", "--session", "--dest", "org.gnome.Shell", "--object-path", "/org/gnome/Shell",
				"--method", "org.gnome.Shell.Eval",
				fmt.Sprintf("Main.extensionManager.lookup('computer-use@cherryai').stateObj._answerForTest(%v)", allow)).CombinedOutput()
			if strings.Contains(string(out), "'true'") {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()
	return done
}

// TestGNOMEShellExtension drives capture, pointer and keyboard input through the
// computer-use@cherryai extension inside testdata/gnome-session.sh, started with
// OCU_GNOME_SHELL_EXTENSION and OPEN_COMPUTER_USE_WAYLAND_HELPER.
func TestGNOMEShellExtension(t *testing.T) {
	helper := os.Getenv("OPEN_COMPUTER_USE_WAYLAND_HELPER")
	if os.Getenv("OPEN_COMPUTER_USE_LINUX_GNOME_TEST") != "1" || helper == "" || os.Getenv("OCU_GNOME_SHELL_EXTENSION") == "" {
		t.Skip("run inside testdata/gnome-session.sh with OCU_GNOME_SHELL_EXTENSION and OPEN_COMPUTER_USE_WAYLAND_HELPER set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	title := fmt.Sprintf("Shell Target %d", os.Getpid())
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
	field := func() Node {
		observation, err := engine.Observe(ctx, app, TreeOptions{MaxNodes: 200, MaxDepth: 20, TextReadLimit: -1})
		if err != nil {
			t.Fatal(err)
		}
		for _, node := range observation.Nodes {
			if node.Name == "Second field" {
				return node
			}
		}
		t.Fatal("no Second field")
		return Node{}
	}
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

	// Nothing is served before the user allows it.
	if status, applies := engine.ShellPermission(ctx); !applies || status != "notDetermined" {
		t.Fatalf("permission before the grant = %s, %v", status, applies)
	}
	if capture := engine.Capture(ctx, app.PID, title); capture.Reason == nil || capture.Reason.Code != "PERMISSION_REQUIRED" {
		t.Fatalf("capture before the grant: %+v", capture.Reason)
	}
	if c, effect := code(engine.PressKey(ctx, target, "Return")); c != "PERMISSION_REQUIRED" || effect != "none" {
		t.Fatalf("keys before the grant: %s/%s", c, effect)
	}
	answered := answerShellDialog(t, false)
	if status, err := engine.RequestShell(ctx); status != "denied" || err != nil {
		t.Fatalf("denied grant = %s, %v", status, err)
	}
	<-answered
	answered = answerShellDialog(t, true)
	if status, err := engine.RequestShell(ctx); status != "granted" || err != nil {
		t.Fatalf("allowed grant = %s, %v", status, err)
	}
	<-answered

	// Capture: the window's logical size, with real content.
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

	// Another window covers the target: capture still shows the target, clicks are refused.
	cover := title + " cover"
	startGTKFixture(t, cover)
	place := func(name string, x, y, width, height int) {
		shellEval(t, fmt.Sprintf("(() => { const w = global.get_window_actors().map(a => a.meta_window).find(w => w.get_title() === %q); w.activate(global.get_current_time()); w.move_resize_frame(false, %d, %d, %d, %d); })()", name, x, y, width, height))
	}
	place(title, 100, 100, 400, 360)
	time.Sleep(300 * time.Millisecond)
	place(cover, 50, 50, 600, 500)
	time.Sleep(time.Second)
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

	// On top, clicks, double clicks and drags reach the target. A refused click
	// sends nothing, so wait for the raise by retrying the first one.
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

	// Keys and any Unicode text go to the focused field of the focus window.
	var focused bool
	if err := engine.call(ctx, field().Ref, "Component.GrabFocus").Store(&focused); err != nil || !focused {
		t.Fatalf("focus the field: %v %v", focused, err)
	}
	time.Sleep(300 * time.Millisecond)
	if err := engine.PressKey(ctx, target, "ctrl+a"); err != nil {
		t.Fatal(err)
	}
	// X11 clients (XWayland) take no input method text from the Shell, so text
	// outside ASCII is refused before anything is sent.
	text := "Hello, 中文 😀"
	if os.Getenv("OPEN_COMPUTER_USE_LINUX_XWAYLAND_TEST") == "1" {
		if c, effect := code(engine.TypeKeys(ctx, target, text)); c != "UNSUPPORTED_CAPABILITY" || effect != "none" {
			t.Fatalf("Unicode text into an X11 window: %s/%s", c, effect)
		}
		text = "Hello, X11!"
	}
	if err := engine.TypeKeys(ctx, target, text); err != nil {
		t.Fatal(err)
	}
	eventually("field", func() string { return field().Text }, text)
	place(cover, 50, 50, 600, 500)
	time.Sleep(500 * time.Millisecond)
	if c, effect := code(engine.TypeKeys(ctx, target, "x")); c != "TARGET_UNAVAILABLE" || effect != "none" {
		t.Fatalf("typing while another window has focus: %s/%s", c, effect)
	}

	// Revoking from the top bar stops everything.
	shellEval(t, "Main.extensionManager.lookup('computer-use@cherryai').stateObj._revoke()")
	if c, _ := code(engine.PressKey(ctx, target, "Return")); c != "PERMISSION_REQUIRED" {
		t.Fatalf("keys after revoking: %s", c)
	}
	eventually("field", func() string { return field().Text }, text)
}
