package desktop

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jezek/xgb/xproto"
)

// Keysyms from X11 keysymdef.h for the key names the tools accept.
var namedKeysyms = map[string]xproto.Keysym{
	"return": 0xff0d, "enter": 0xff0d, "tab": 0xff09, "escape": 0xff1b, "esc": 0xff1b,
	"backspace": 0xff08, "back_space": 0xff08, "delete": 0xffff, "insert": 0xff63,
	"space": 0x20, "home": 0xff50, "left": 0xff51, "up": 0xff52, "right": 0xff53, "down": 0xff54,
	"page_up": 0xff55, "prior": 0xff55, "page_down": 0xff56, "next": 0xff56, "end": 0xff57,
	"menu": 0xff67,
}

var modifierKeysyms = map[string]xproto.Keysym{
	"ctrl": 0xffe3, "control": 0xffe3, "shift": 0xffe1, "alt": 0xffe9,
	"super": 0xffeb, "win": 0xffeb, "cmd": 0xffeb, "meta": 0xffeb,
}

const shiftKeysym xproto.Keysym = 0xffe1

// runeKeysym follows the X11 convention: Latin-1 maps directly, other code points
// use the 0x1000000 Unicode range.
func runeKeysym(r rune) xproto.Keysym {
	if (r >= 0x20 && r <= 0x7e) || (r >= 0xa0 && r <= 0xff) {
		return xproto.Keysym(r)
	}
	switch r {
	case '\n', '\r':
		return 0xff0d
	case '\t':
		return 0xff09
	}
	return xproto.Keysym(0x1000000 + r)
}

// keyChord is a parsed press_key value: modifiers held around one main key.
type keyChord struct {
	modifiers []xproto.Keysym
	key       xproto.Keysym
}

// parseKeyChord accepts "Return", "a", "ctrl+shift+Tab" or "F5". Unknown names are
// refused before any input is sent.
func parseKeyChord(value string) (keyChord, error) {
	parts := strings.Split(value, "+")
	if strings.HasSuffix(value, "++") || value == "+" {
		parts = append(parts[:len(parts)-2], "+")
	}
	chord := keyChord{}
	for _, part := range parts[:len(parts)-1] {
		keysym, ok := modifierKeysyms[strings.ToLower(strings.TrimSpace(part))]
		if !ok {
			return keyChord{}, fail("INVALID_ARGUMENT", "Unsupported modifier %q", part)
		}
		chord.modifiers = append(chord.modifiers, keysym)
	}
	main := strings.TrimSpace(parts[len(parts)-1])
	lower := strings.ToLower(main)
	switch {
	case main == "" && parts[len(parts)-1] == " ":
		chord.key = 0x20
	case namedKeysyms[lower] != 0:
		chord.key = namedKeysyms[lower]
	case len(lower) >= 2 && lower[0] == 'f':
		number, err := strconv.Atoi(lower[1:])
		if err != nil || number < 1 || number > 24 {
			return keyChord{}, fail("INVALID_ARGUMENT", "Unsupported key %q", main)
		}
		chord.key = xproto.Keysym(0xffbe + number - 1)
	case utf8.RuneCountInString(main) == 1:
		r, _ := utf8.DecodeRuneInString(main)
		// Shortcuts name the unshifted key: ctrl+A means ctrl+a.
		if len(chord.modifiers) > 0 && r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		chord.key = runeKeysym(r)
	default:
		return keyChord{}, fail("INVALID_ARGUMENT", "Unsupported key %q", main)
	}
	return chord, nil
}

// keymap locates keysyms on the current keyboard mapping.
type keymap struct {
	min      xproto.Keycode
	perCode  int
	keysyms  []xproto.Keysym
	assigned map[xproto.Keysym]xproto.Keycode // temporary mappings for unmapped characters
}

// lookup returns the keycode producing keysym and whether Shift is needed. Only
// the first two levels count; other levels depend on layout-specific modifiers.
func (m *keymap) lookup(keysym xproto.Keysym) (xproto.Keycode, bool, bool) {
	if code, ok := m.assigned[keysym]; ok {
		return code, false, true
	}
	for level := 0; level < min(2, m.perCode); level++ {
		for index := 0; index*m.perCode+level < len(m.keysyms); index++ {
			if m.keysyms[index*m.perCode+level] == keysym {
				return m.min + xproto.Keycode(index), level == 1, true
			}
		}
	}
	return 0, false, false
}

// spare lists keycodes with no symbols, which can temporarily carry a character.
func (m *keymap) spare() []xproto.Keycode {
	codes := []xproto.Keycode{}
	for index := 0; (index+1)*m.perCode <= len(m.keysyms); index++ {
		empty := true
		for _, keysym := range m.keysyms[index*m.perCode : (index+1)*m.perCode] {
			empty = empty && keysym == 0
		}
		if empty && m.min+xproto.Keycode(index) > 8 {
			codes = append(codes, m.min+xproto.Keycode(index))
		}
	}
	return codes
}
