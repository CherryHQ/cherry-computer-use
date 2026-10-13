package x11

import (
	"github.com/iFurySt/open-codex-computer-use/apps/opencomputeruselinux/internal/display"

	"github.com/jezek/xgb/xproto"
)

const shiftKeysym xproto.Keysym = xproto.Keysym(display.ShiftKeysym)

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
