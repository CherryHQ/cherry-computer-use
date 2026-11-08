package webcdp

import (
	"strconv"
	"strings"
)

// splitChord splits "ctrl+shift+s" into its key and modifiers. A trailing '+' is the key itself, so a
// bare "+" and "ctrl++" both work.
func splitChord(key string) (main string, shift, control, alt, meta, ok bool) {
	main = key
	for {
		index := strings.Index(main, "+")
		if index <= 0 || index == len(main)-1 {
			break
		}
		switch strings.ToLower(main[:index]) {
		case "ctrl", "control":
			control = true
		case "shift":
			shift = true
		case "alt":
			alt = true
		case "super", "win", "cmd", "meta":
			meta = true
		default:
			return "", false, false, false, false, false
		}
		main = main[index+1:]
	}
	if main == "" {
		return "", false, false, false, false, false
	}
	return main, shift, control, alt, meta, true
}

func isFunctionKey(key string) bool {
	if len(key) < 2 || len(key) > 3 {
		return false
	}
	if key[0] != 'f' && key[0] != 'F' {
		return false
	}
	index, err := strconv.Atoi(key[1:])
	return err == nil && index >= 1 && index <= 12
}

func functionKeyIndex(key string) int {
	index, err := strconv.Atoi(key[1:])
	if err != nil {
		return 1
	}
	return index
}
