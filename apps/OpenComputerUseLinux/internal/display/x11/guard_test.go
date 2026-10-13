package x11

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
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
	backend := New(os.Getenv("DISPLAY"), testGuard)
	in, err := backend.globalInput(context.Background())
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

// TestGuardReleasesOnlyItsOwnersInput needs an isolated X11 display with XTEST
// (testdata/x11-session.sh); it presses Shift on that display.
func TestGuardReleasesOnlyItsOwnersInput(t *testing.T) {
	if os.Getenv("OPEN_COMPUTER_USE_LINUX_X11_INPUT_TEST") != "1" {
		t.Skip("run inside testdata/x11-session.sh with OPEN_COMPUTER_USE_LINUX_X11_INPUT_TEST=1")
	}
	probe, err := xgb.NewConn()
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()
	anythingHeld := func() bool {
		keys, _ := xproto.QueryKeymap(probe).Reply()
		for _, b := range keys.Keys {
			if b != 0 {
				return true
			}
		}
		return false
	}
	other := New(os.Getenv("DISPLAY"), testGuard)
	if _, err := other.globalInput(context.Background()); err != nil {
		t.Fatal(err)
	}

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
	other.Close()
	time.Sleep(200 * time.Millisecond)
	if !anythingHeld() {
		t.Fatal("closing one owner released another owner's key")
	}
	_ = holder.Process.Kill()
	_ = holder.Wait()
	for attempt := 0; attempt < 30 && anythingHeld(); attempt++ {
		time.Sleep(100 * time.Millisecond)
	}
	if anythingHeld() {
		t.Fatal("the guard did not release a killed owner's key")
	}
}
