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

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// The test binary doubles as the input guard and as an owner that dies holding a key.
func TestMain(m *testing.M) {
	switch os.Getenv("OCU_TEST_ROLE") {
	case "guard":
		GuardMain(os.Getenv("DISPLAY"))
		os.Exit(0)
	case "holder":
		holdShiftUntilKilled()
	}
	os.Exit(m.Run())
}

func testGuard(display string) *exec.Cmd {
	command := exec.Command(os.Args[0], "-test.run=^$")
	command.Env = append(os.Environ(), "OCU_TEST_ROLE=guard", "DISPLAY="+display)
	command.Stderr = os.Stderr
	return command
}

func holdShiftUntilKilled() {
	engine := New(Config{InputGuard: testGuard})
	in, err := engine.globalInput(context.Background())
	if err != nil {
		fmt.Println("error", err)
		os.Exit(1)
	}
	m, _ := in.keymap()
	shift, _, _ := m.lookup(shiftKeysym)
	_ = in.press(held{'k', byte(shift)}, xproto.KeyPress, 0, 0)
	in.sync()
	fmt.Println("held")
	select {}
}

func TestParseKeyChord(t *testing.T) {
	for value, want := range map[string]keyChord{
		"Return":         {key: 0xff0d},
		"ctrl+shift+Tab": {modifiers: []xproto.Keysym{0xffe3, 0xffe1}, key: 0xff09},
		"ctrl+A":         {modifiers: []xproto.Keysym{0xffe3}, key: 'a'},
		"A":              {key: 'A'},
		"F12":            {key: 0xffbe + 11},
		"ctrl++":         {modifiers: []xproto.Keysym{0xffe3}, key: '+'},
		"中":              {key: 0x1000000 + '中'},
	} {
		got, err := parseKeyChord(value)
		if err != nil || fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("parseKeyChord(%q) = %v, %v; want %v", value, got, err, want)
		}
	}
	for _, value := range []string{"hyper+a", "F25", "NotAKey", ""} {
		var native *Error
		if _, err := parseKeyChord(value); !errors.As(err, &native) || native.Code != "INVALID_ARGUMENT" {
			t.Fatalf("parseKeyChord(%q) must be refused before input: %v", value, err)
		}
	}
}

func TestKeymapLookupUsesTwoLevelsAndFindsSpareCodes(t *testing.T) {
	m := &keymap{min: 8, perCode: 3, keysyms: []xproto.Keysym{
		0, 0, 0, // 8 reserved
		'a', 'A', 0xe6, // 9
		0, 0, 0, // 10 spare
		'1', '!', 0, // 11
	}, assigned: map[xproto.Keysym]xproto.Keycode{}}
	if code, shift, ok := m.lookup('A'); !ok || code != 9 || !shift {
		t.Fatalf("A = %d %v %v", code, shift, ok)
	}
	if _, _, ok := m.lookup(0xe6); ok {
		t.Fatal("third-level symbols need layout modifiers and must be remapped instead")
	}
	if spare := m.spare(); len(spare) != 1 || spare[0] != 10 {
		t.Fatalf("spare = %v", spare)
	}
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
	window, err := findX11Window(probe, app.PID, title)
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
		return Point{n.Extents.X + n.Extents.Width/2, n.Extents.Y + n.Extents.Height/2}
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
	spareBefore := func() int {
		in, _ := engine.globalInput(ctx)
		m, _ := in.keymap()
		return len(m.spare())
	}()

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
	from := Point{slider.Extents.X + 6, slider.Extents.Y + slider.Extents.Height/2}
	to := Point{slider.Extents.X + slider.Extents.Width - 4, from.Y}
	if err := engine.Drag(ctx, target, from, to); err != nil {
		t.Fatal(err)
	}
	eventually("slider dragged", func() bool { v := node("Slider").Value; return v != nil && *v > 50 })
	if anythingHeld() {
		t.Fatal("button left pressed after drag")
	}
	if c, effect := code(engine.Drag(ctx, target, from, Point{5000, 5000})); c != "INVALID_ARGUMENT" || effect != "none" {
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
	if in, _ := engine.globalInput(ctx); func() int { m, _ := in.keymap(); return len(m.spare()) }() != spareBefore {
		t.Fatal("cancellation left a temporary key mapping")
	}

	// An owner that dies holding Shift is cleaned up by its guard; another owner's
	// close does not release it first.
	holder := exec.Command(os.Args[0], "-test.run=^$")
	holder.Env = append(os.Environ(), "OCU_TEST_ROLE=holder")
	out, _ := holder.StdoutPipe()
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	if line, _ := bufio.NewReader(out).ReadString('\n'); strings.TrimSpace(line) != "held" {
		_ = holder.Process.Kill()
		t.Fatalf("holder did not press: %q", line)
	}
	if !anythingHeld() {
		t.Fatal("holder's key is not down")
	}
	_ = engine.Close()
	time.Sleep(200 * time.Millisecond)
	if !anythingHeld() {
		t.Fatal("closing one owner released another owner's key")
	}
	_ = holder.Process.Kill()
	_ = holder.Wait()
	eventually("guard releases a killed owner's key", func() bool { return !anythingHeld() })
}
