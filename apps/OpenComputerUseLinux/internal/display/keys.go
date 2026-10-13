package display

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// Keysym is an X11/XKB keysym; both display backends resolve it to their own keycodes.
type Keysym uint32

// Keysyms from X11 keysymdef.h for the key names the tools accept.
var namedKeysyms = map[string]Keysym{
	"return": 0xff0d, "enter": 0xff0d, "tab": 0xff09, "escape": 0xff1b, "esc": 0xff1b,
	"backspace": 0xff08, "back_space": 0xff08, "delete": 0xffff, "insert": 0xff63,
	"space": 0x20, "home": 0xff50, "left": 0xff51, "up": 0xff52, "right": 0xff53, "down": 0xff54,
	"page_up": 0xff55, "prior": 0xff55, "page_down": 0xff56, "next": 0xff56, "end": 0xff57,
	"menu": 0xff67,
}

var modifierKeysyms = map[string]Keysym{
	"ctrl": 0xffe3, "control": 0xffe3, "shift": 0xffe1, "alt": 0xffe9,
	"super": 0xffeb, "win": 0xffeb, "cmd": 0xffeb, "meta": 0xffeb,
}

// ShiftKeysym is Shift_L.
const ShiftKeysym Keysym = 0xffe1

// RuneKeysym follows the X11 convention: Latin-1 maps directly, other code points
// use the 0x1000000 Unicode range.
func RuneKeysym(r rune) Keysym {
	if (r >= 0x20 && r <= 0x7e) || (r >= 0xa0 && r <= 0xff) {
		return Keysym(r)
	}
	switch r {
	case '\n', '\r':
		return 0xff0d
	case '\t':
		return 0xff09
	}
	return Keysym(0x1000000 + r)
}

// KeyChord is a parsed press_key value: modifiers held around one main key.
type KeyChord struct {
	Modifiers []Keysym
	Key       Keysym
}

// ParseKeyChord accepts "Return", "a", "ctrl+shift+Tab" or "F5". Unknown names are
// refused before any input is sent.
func ParseKeyChord(value string) (KeyChord, error) {
	parts := strings.Split(value, "+")
	if strings.HasSuffix(value, "++") || value == "+" {
		parts = append(parts[:len(parts)-2], "+")
	}
	chord := KeyChord{}
	for _, part := range parts[:len(parts)-1] {
		keysym, ok := modifierKeysyms[strings.ToLower(strings.TrimSpace(part))]
		if !ok {
			return KeyChord{}, Fail("INVALID_ARGUMENT", "Unsupported modifier %q", part)
		}
		chord.Modifiers = append(chord.Modifiers, keysym)
	}
	main := strings.TrimSpace(parts[len(parts)-1])
	lower := strings.ToLower(main)
	switch {
	case main == "" && parts[len(parts)-1] == " ":
		chord.Key = 0x20
	case namedKeysyms[lower] != 0:
		chord.Key = namedKeysyms[lower]
	case len(lower) >= 2 && lower[0] == 'f':
		number, err := strconv.Atoi(lower[1:])
		if err != nil || number < 1 || number > 24 {
			return KeyChord{}, Fail("INVALID_ARGUMENT", "Unsupported key %q", main)
		}
		chord.Key = Keysym(0xffbe + number - 1)
	case utf8.RuneCountInString(main) == 1:
		r, _ := utf8.DecodeRuneInString(main)
		// Shortcuts name the unshifted key: ctrl+A means ctrl+a.
		if len(chord.Modifiers) > 0 && r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		chord.Key = RuneKeysym(r)
	default:
		return KeyChord{}, Fail("INVALID_ARGUMENT", "Unsupported key %q", main)
	}
	return chord, nil
}
