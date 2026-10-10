package x11

import (
	"errors"
	"fmt"
	"testing"

	"github.com/iFurySt/open-codex-computer-use/apps/opencomputeruselinux/internal/display"
	"github.com/jezek/xgb/xproto"
)

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
		var native *display.Error
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
