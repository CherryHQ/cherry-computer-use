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

	"github.com/iFurySt/open-codex-computer-use/apps/opencomputeruselinux/internal/display/x11"
	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// The test binary doubles as the input guard that the engine starts.
func TestMain(m *testing.M) {
	if os.Getenv("OCU_TEST_ROLE") == "guard" {
		x11.GuardMain(os.Getenv("DISPLAY"))
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func testGuard(display string) *exec.Cmd {
	command := exec.Command(os.Args[0], "-test.run=^$")
	command.Env = append(os.Environ(), "OCU_TEST_ROLE=guard", "DISPLAY="+display)
	command.Stderr = os.Stderr
	return command
}

// TestX11GlobalInput needs an X11 display with XTEST and AT-SPI and must not run
// on a desktop in use: it moves the pointer and types. Use testdata/x11-session.sh.
func TestX11GlobalInput(t *testing.T) {
	if os.Getenv("OPEN_COMPUTER_USE_LINUX_X11_INPUT_TEST") != "1" {
		t.Skip("run inside testdata/x11-session.sh with OPEN_COMPUTER_USE_LINUX_X11_INPUT_TEST=1")
	}
	title := fmt.Sprintf("Cherry Input Fixture %d", os.Getpid())
	fixture := exec.Command("python3", filepath.Join("..", "..", "testdata", "gtk_fixture.py"), title)
	stdout, _ := fixture.StdoutPipe()
	if err := fixture.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fixture.Process.Kill(); _ = fixture.Wait() })
	if line, _ := bufio.NewReader(stdout).ReadString('\n'); strings.TrimSpace(line) != "ready" {
		t.Fatalf("fixture did not start: %q", line)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	engine := New(Config{InputGuard: testGuard})
	t.Cleanup(func() { _ = engine.Close() })
	var app App
	var err error
	for attempt := 0; ; attempt++ {
		if app, err = engine.Resolve(ctx, title); err == nil {
			break
		}
		if attempt > 50 {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	target := GlobalTarget{PID: app.PID, Title: title}
	probe, err := xgb.NewConn()
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()
	window, err := x11.FindWindow(probe, app.PID, title)
	if err != nil {
		t.Fatal(err)
	}
	// No window manager runs in the sandbox, so the test gives focus itself.
	focus := func(w xproto.Window) {
		_ = xproto.SetInputFocus(probe, xproto.InputFocusParent, w, xproto.TimeCurrentTime).Check()
		time.Sleep(200 * time.Millisecond)
	}
	observe := func() Observation {
		observation, err := engine.Observe(ctx, app, TreeOptions{MaxNodes: 200, MaxDepth: 20, TextReadLimit: -1})
		if err != nil {
			t.Fatal(err)
		}
		return observation
	}
	node := func(name string) Node {
		for _, candidate := range observe().Nodes {
			if candidate.Name == name || strings.HasPrefix(candidate.Name, name) {
				return candidate
			}
		}
		t.Fatalf("no element %q", name)
		return Node{}
	}
	eventually := func(what string, check func() bool) {
		t.Helper()
		for attempt := 0; attempt < 30; attempt++ {
			if check() {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("never reached: %s", what)
	}
	center := func(n Node) Point {
		return Point{X: n.Extents.X + n.Extents.Width/2, Y: n.Extents.Y + n.Extents.Height/2}
	}
	code := func(err error) (string, string) {
		var native *Error
		if errors.As(err, &native) {
			return native.Code, native.Effect
		}
		return fmt.Sprint(err), ""
	}
	anythingHeld := func() bool {
		keys, _ := xproto.QueryKeymap(probe).Reply()
		for _, b := range keys.Keys {
			if b != 0 {
				return true
			}
		}
		pointer, _ := xproto.QueryPointer(probe, xproto.Setup(probe).DefaultScreen(probe).Root).Reply()
		return pointer.Mask&(xproto.ButtonMask1|xproto.ButtonMask2|xproto.ButtonMask3) != 0
	}
	// Unassigned keycodes; temporary character mappings must not leak.
	spareCodes := func() int {
		setup := xproto.Setup(probe)
		reply, _ := xproto.GetKeyboardMapping(probe, setup.MinKeycode, byte(setup.MaxKeycode-setup.MinKeycode+1)).Reply()
		count, per := 0, int(reply.KeysymsPerKeycode)
		for index := 0; (index+1)*per <= len(reply.Keysyms); index++ {
			empty := true
			for _, keysym := range reply.Keysyms[index*per : (index+1)*per] {
				empty = empty && keysym == 0
			}
			if empty {
				count++
			}
		}
		return count
	}
	spareBefore := spareCodes()

	focus(window)
	if err := engine.PressKey(ctx, target, "ctrl+a"); err != nil {
		t.Fatal(err)
	}
	if err := engine.TypeKeys(ctx, target, "Ab中é🙂"); err != nil {
		t.Fatal(err)
	}
	eventually("typed Unicode replaces the selection", func() bool { return node("Second field").Text == "Ab中é🙂" })
	if anythingHeld() {
		t.Fatal("keys left pressed after typing")
	}

	if err := engine.PointerClick(ctx, target, center(node("Count: 0")), 1, 1); err != nil {
		t.Fatal(err)
	}
	eventually("one click", func() bool { return node("Count: ").Name == "Count: 1" })
	if err := engine.PointerClick(ctx, target, center(node("Count: 1")), 1, 2); err != nil {
		t.Fatal(err)
	}
	eventually("double click", func() bool { return node("Count: ").Name == "Count: 3" })

	slider := node("Slider")
	from := Point{X: slider.Extents.X + 6, Y: slider.Extents.Y + slider.Extents.Height/2}
	to := Point{X: slider.Extents.X + slider.Extents.Width - 4, Y: from.Y}
	if err := engine.Drag(ctx, target, from, to); err != nil {
		t.Fatal(err)
	}
	eventually("slider dragged", func() bool { v := node("Slider").Value; return v != nil && *v > 50 })
	if anythingHeld() {
		t.Fatal("button left pressed after drag")
	}
	if c, effect := code(engine.Drag(ctx, target, from, Point{X: 5000, Y: 5000})); c != "INVALID_ARGUMENT" || effect != "none" {
		t.Fatalf("drag outside the window: %s/%s", c, effect)
	}

	// Another window takes focus and covers the button.
	root := xproto.Setup(probe).DefaultScreen(probe).Root
	cover, _ := xproto.NewWindowId(probe)
	button := node("Count: ")
	_ = xproto.CreateWindowChecked(probe, 0, cover, root, int16(button.Extents.X), int16(button.Extents.Y), 200, 30, 0,
		xproto.WindowClassInputOutput, 0, xproto.CwOverrideRedirect, []uint32{1}).Check()
	_ = xproto.MapWindowChecked(probe, cover).Check()
	focus(cover)
	before := node("Second field").Text
	for name, err := range map[string]error{
		"key":    engine.PressKey(ctx, target, "x"),
		"type":   engine.TypeKeys(ctx, target, "x"),
		"scroll": engine.PageScroll(ctx, target, "down", 1),
		"click":  engine.PointerClick(ctx, target, center(button), 1, 1),
	} {
		if c, effect := code(err); c != "TARGET_UNAVAILABLE" || effect != "none" {
			t.Fatalf("%s while another window has focus or covers the point: %s/%s", name, c, effect)
		}
	}
	if node("Second field").Text != before || node("Count: ").Name != "Count: 3" {
		t.Fatal("refused input reached the fixture")
	}
	xproto.DestroyWindow(probe, cover)
	focus(window)

	cancelled, stop := context.WithCancel(ctx)
	time.AfterFunc(60*time.Millisecond, stop)
	if c, _ := code(engine.TypeKeys(cancelled, target, strings.Repeat("中z", 40))); c != "CANCELLED" && c != "TARGET_UNAVAILABLE" {
		t.Fatalf("cancelled typing returned %s", c)
	}
	if anythingHeld() {
		t.Fatal("cancellation left keys pressed")
	}
	if spareCodes() != spareBefore {
		t.Fatal("cancellation left a temporary key mapping")
	}

}
