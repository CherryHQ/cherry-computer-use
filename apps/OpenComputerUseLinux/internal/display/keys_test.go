package display

import (
	"errors"
	"fmt"
	"testing"
)

func TestParseKeyChord(t *testing.T) {
	for value, want := range map[string]KeyChord{
		"Return":         {Key: 0xff0d},
		"ctrl+shift+Tab": {Modifiers: []Keysym{0xffe3, 0xffe1}, Key: 0xff09},
		"ctrl+A":         {Modifiers: []Keysym{0xffe3}, Key: 'a'},
		"A":              {Key: 'A'},
		"F12":            {Key: 0xffbe + 11},
		"ctrl++":         {Modifiers: []Keysym{0xffe3}, Key: '+'},
		"中":              {Key: 0x1000000 + '中'},
	} {
		got, err := ParseKeyChord(value)
		if err != nil || fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("ParseKeyChord(%q) = %v, %v; want %v", value, got, err, want)
		}
	}
	for _, value := range []string{"hyper+a", "F25", "NotAKey", ""} {
		var native *Error
		if _, err := ParseKeyChord(value); !errors.As(err, &native) || native.Code != "INVALID_ARGUMENT" {
			t.Fatalf("ParseKeyChord(%q) must be refused before input: %v", value, err)
		}
	}
}
