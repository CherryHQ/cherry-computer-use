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

// TestRealDesktopSemanticActions drives the GTK fixture through the engine only,
// so no step can fall back to global input. It needs a signed-in desktop session,
// python3 with GTK 3, and OPEN_COMPUTER_USE_LINUX_DESKTOP_TEST=1.
func TestRealDesktopSemanticActions(t *testing.T) {
	if os.Getenv("OPEN_COMPUTER_USE_LINUX_DESKTOP_TEST") != "1" {
		t.Skip("set OPEN_COMPUTER_USE_LINUX_DESKTOP_TEST=1 to drive the GTK fixture")
	}
	title := fmt.Sprintf("Cherry Engine Fixture %d", os.Getpid())
	fixture := exec.Command("python3", filepath.Join("..", "..", "testdata", "gtk_fixture.py"), title)
	stdout, err := fixture.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fixture.Process.Kill(); _ = fixture.Wait() })
	if line, _ := bufio.NewReader(stdout).ReadString('\n'); strings.TrimSpace(line) != "ready" {
		t.Fatalf("fixture did not start: %q", line)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	engine := New(Config{})
	t.Cleanup(func() { _ = engine.Close() })
	var app App
	for attempt := 0; ; attempt++ {
		if app, err = engine.Resolve(ctx, title); err == nil {
			break
		}
		if attempt > 50 {
			t.Fatalf("fixture is not discoverable: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	observe := func() Observation {
		t.Helper()
		observation, err := engine.Observe(ctx, app, TreeOptions{MaxNodes: 1200, MaxDepth: 64, TextReadLimit: -1})
		if err != nil {
			t.Fatal(err)
		}
		return observation
	}
	find := func(observation Observation, name string) Node {
		t.Helper()
		for _, node := range observation.Nodes {
			if node.Name == name {
				return node
			}
		}
		t.Fatalf("no element named %q", name)
		return Node{}
	}
	code := func(err error) (string, string) {
		var native *Error
		if !errors.As(err, &native) {
			return "", ""
		}
		return native.Code, native.Effect
	}
	eventually := func(what string, check func(Observation) bool) Observation {
		t.Helper()
		for attempt := 0; attempt < 30; attempt++ {
			if observation := observe(); check(observation) {
				return observation
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("fixture never reached: %s", what)
		return Observation{}
	}

	initial := observe()
	if initial.Window.Title != title {
		t.Fatalf("main window = %q", initial.Window.Title)
	}
	button := find(initial, "Count: 0")
	click, ok := ClickAction(button.Actions)
	if !ok {
		t.Fatalf("button has no click action: %+v", button.Actions)
	}
	if err := engine.DoAction(ctx, initial.Window, button, click); err != nil {
		t.Fatal(err)
	}
	current := eventually("Count: 1", func(o Observation) bool { return hasName(o, "Count: 1") })
	if c, effect := code(engine.DoAction(ctx, initial.Window, button, click)); c != "STALE_SNAPSHOT" || effect != "none" {
		t.Fatalf("stale element must be refused before dispatch: %s/%s", c, effect)
	}

	disabled := find(current, "Disabled action")
	action, _ := ClickAction(disabled.Actions)
	if c, effect := code(engine.DoAction(ctx, current.Window, disabled, action)); c != "TARGET_UNAVAILABLE" || effect != "none" {
		t.Fatalf("disabled element: %s/%s", c, effect)
	}

	check := find(current, "Enabled option")
	action, _ = ClickAction(check.Actions)
	if err := engine.DoAction(ctx, current.Window, check, action); err != nil {
		t.Fatal(err)
	}
	current = eventually("checked option", func(o Observation) bool { return find(o, "Enabled option").States.Has(StateChecked) })

	details := find(current, "Details")
	matches := MatchActions(details.Actions, "ACTIVATE")
	if len(matches) != 1 {
		t.Fatalf("secondary action lookup: %+v", details.Actions)
	}
	if err := engine.DoAction(ctx, current.Window, details, matches[0]); err != nil {
		t.Fatal(err)
	}
	current = eventually("expanded details", func(o Observation) bool { return find(o, "Hidden details").States.Has(StateShowing) })

	level := find(current, "Level")
	if err := engine.SetValue(ctx, current.Window, level, "7"); err != nil {
		t.Fatal(err)
	}
	current = eventually("level 7", func(o Observation) bool { v := find(o, "Level").Value; return v != nil && *v == 7 })
	level = find(current, "Level")
	for _, invalid := range []string{"11", "seven", "NaN"} {
		if c, effect := code(engine.SetValue(ctx, current.Window, level, invalid)); c != "INVALID_ARGUMENT" || effect != "none" {
			t.Fatalf("SetValue(%q) must be refused before dispatch: %s/%s", invalid, c, effect)
		}
	}

	unicode := "hello 世界🙂"
	if err := engine.SetValue(ctx, current.Window, find(current, "First field"), unicode); err != nil {
		t.Fatal(err)
	}
	current = eventually("unicode text", func(o Observation) bool { return find(o, "First field").Text == unicode })
	if c, _ := code(engine.SetValue(ctx, current.Window, find(current, "Locked field"), "x")); c != "TARGET_UNAVAILABLE" {
		t.Fatalf("read-only text must be refused: %s", c)
	}

	// Typing needs the fixture window to hold focus; a compositor may refuse to give it.
	focused, err := engine.Focused(ctx, current.Window)
	if err != nil {
		t.Fatal(err)
	}
	if len(focused) == 0 || focused[0].Name != "Second field" {
		t.Logf("skipping typeText: the fixture window is not focused (%d focused nodes)", len(focused))
		return
	}
	field := find(current, "Second field")
	var selected bool
	// GTK may already select everything on focus-in; replace that selection if so.
	if engine.call(ctx, field.Ref, "Text.SetSelection", int32(0), int32(1), int32(3)).Store(&selected) != nil || !selected {
		_ = engine.call(ctx, field.Ref, "Text.AddSelection", int32(1), int32(3)).Store(&selected)
	}
	if !selected {
		t.Fatal("cannot select inside the focused field")
	}
	if err := engine.TypeText(ctx, current.Window, "中🙂"); err != nil {
		t.Fatal(err)
	}
	current = eventually("selection replaced", func(o Observation) bool { return find(o, "Second field").Text == "b中🙂a" })
	if got := find(current, "First field").Text; got != unicode {
		t.Fatalf("typing changed the unfocused field: %q", got)
	}
	if err := engine.TypeText(ctx, current.Window, "!"); err != nil {
		t.Fatal(err)
	}
	eventually("caret insertion", func(o Observation) bool { return find(o, "Second field").Text == "b中🙂!a" })
}

func hasName(observation Observation, name string) bool {
	for _, node := range observation.Nodes {
		if node.Name == name {
			return true
		}
	}
	return false
}
