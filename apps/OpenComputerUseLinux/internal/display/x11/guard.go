package x11

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"
)

// X servers keep XTEST keys and buttons pressed after their client disconnects,
// so a dead owner would leave input stuck. Each owner therefore starts a private
// guard process and reports every press before sending it. When the owner's pipe
// closes for any reason, the guard releases what that owner still holds and exits.
// It never touches input another owner pressed.
//
// Guard lines: "+k <code>", "-k <code>", "+b <button>", "-b <button>",
// "+m <code>" and "-m <code>" for temporary keyboard mappings.

type held struct {
	kind   byte // 'k' key, 'b' button, 'm' temporary mapping
	detail byte
}

// RunInputGuard is the guard process body. It reports "ready" once it can reach
// the display, ignores interrupts that typically hit a whole process group, and
// returns after releasing at end of input.
func RunInputGuard(owner io.Reader, ready io.Writer, display string) error {
	signal.Ignore(syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGPIPE)
	conn, err := xgb.NewConnDisplay(display)
	if err != nil {
		return fmt.Errorf("input guard cannot reach the X display: %w", err)
	}
	defer conn.Close()
	if err := xtest.Init(conn); err != nil {
		return fmt.Errorf("input guard needs XTEST: %w", err)
	}
	fmt.Fprintln(ready, "ready")
	state := map[held]bool{}
	scanner := bufio.NewScanner(owner)
	for scanner.Scan() {
		var sign, kind byte
		var detail int
		if _, err := fmt.Sscanf(scanner.Text(), "%c%c %d", &sign, &kind, &detail); err != nil || detail < 0 || detail > 255 {
			continue
		}
		state[held{kind, byte(detail)}] = sign == '+'
	}
	releaseHeld(conn, state)
	return nil
}

// releaseHeld releases recorded input that is still down and clears temporary mappings.
func releaseHeld(conn *xgb.Conn, state map[held]bool) {
	setup := xproto.Setup(conn)
	root := setup.DefaultScreen(conn).Root
	keys, _ := xproto.QueryKeymap(conn).Reply()
	pointer, _ := xproto.QueryPointer(conn, root).Reply()
	for item, active := range state {
		if !active {
			continue
		}
		switch item.kind {
		case 'k':
			if keys != nil && keys.Keys[item.detail/8]&(1<<(item.detail%8)) != 0 {
				xtest.FakeInput(conn, xproto.KeyRelease, item.detail, 0, root, 0, 0, 0)
			}
		case 'b':
			if pointer != nil && item.detail >= 1 && item.detail <= 5 && pointer.Mask&(xproto.ButtonMask1<<(item.detail-1)) != 0 {
				xtest.FakeInput(conn, xproto.ButtonRelease, item.detail, 0, root, 0, 0, 0)
			}
		case 'm':
			reply, err := xproto.GetKeyboardMapping(conn, xproto.Keycode(item.detail), 1).Reply()
			if err == nil {
				xproto.ChangeKeyboardMapping(conn, 1, xproto.Keycode(item.detail), reply.KeysymsPerKeycode, make([]xproto.Keysym, reply.KeysymsPerKeycode))
			}
		}
	}
	_, _ = xproto.GetInputFocus(conn).Reply() // flush before the connection closes
}

// GuardMain runs the guard on standard streams for the executable's hidden subcommand.
func GuardMain(display string) {
	if err := RunInputGuard(os.Stdin, os.Stdout, display); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
