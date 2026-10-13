package x11

import (
	"testing"

	"github.com/jezek/xgb/xproto"
)

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
