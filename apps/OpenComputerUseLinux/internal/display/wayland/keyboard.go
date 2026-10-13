package wayland

import (
	"context"

	"github.com/iFurySt/open-codex-computer-use/apps/opencomputeruselinux/internal/display"
)

// Grant routes for keyboard input. GrantPortal shows the system dialog;
// GrantMutterTest skips it and is honoured only by a helper started for an
// isolated test session.
const (
	GrantPortal     = "portal"
	GrantMutterTest = "mutter-test"
)

// KeyboardAvailable reports whether the session's portal can offer a keyboard.
func (p Probe) KeyboardAvailable() display.Availability {
	if p.Portal.RemoteDesktop == nil {
		return display.Unavailable("DEPENDENCY_MISSING", "The RemoteDesktop portal is not available")
	}
	if p.Portal.RemoteDesktop.Version < 2 {
		return display.Unavailable("DEPENDENCY_MISSING", "The RemoteDesktop portal is older than version 2 and cannot connect to EIS")
	}
	if p.Portal.RemoteDesktop.Types&1 == 0 {
		return display.Unsupported("The RemoteDesktop portal offers no keyboard")
	}
	return display.Available()
}

func (h *Helper) KeyboardGranted(ctx context.Context) (bool, error) {
	var status struct {
		Granted bool `json:"granted"`
	}
	err := h.Call(ctx, "keyboard.status", nil, &status)
	return status.Granted, err
}

// GrantKeyboard blocks while the user answers the portal dialog.
func (h *Helper) GrantKeyboard(ctx context.Context, via string) error {
	return h.Call(ctx, "keyboard.grant", map[string]any{"via": via}, nil)
}

// MissingCharacters lists characters the compositor's keyboard layout cannot type.
func (h *Helper) MissingCharacters(ctx context.Context, text string) ([]string, error) {
	var result struct {
		Missing []string `json:"missing"`
	}
	err := h.Call(ctx, "keyboard.missing", map[string]any{"text": text}, &result)
	return result.Missing, err
}

func (h *Helper) Chord(ctx context.Context, chord display.KeyChord) error {
	modifiers := make([]uint32, 0, len(chord.Modifiers))
	for _, modifier := range chord.Modifiers {
		modifiers = append(modifiers, uint32(modifier))
	}
	return h.Call(ctx, "keyboard.chord", map[string]any{"modifiers": modifiers, "key": uint32(chord.Key)}, nil)
}

func (h *Helper) Text(ctx context.Context, text string) error {
	return h.Call(ctx, "keyboard.text", map[string]any{"text": text}, nil)
}

// VirtualKeyboardAvailable reports whether the compositor offers the wlroots
// virtual keyboard, which needs no dialog and types any character.
func (p Probe) VirtualKeyboardAvailable() bool {
	return p.HasGlobal("zwp_virtual_keyboard_manager_v1") && p.HasGlobal("wl_seat")
}

// ActiveWindow is the compositor's focused window from its IPC.
type ActiveWindow struct {
	Available bool    `json:"available"`
	PID       *uint32 `json:"pid"`
	Title     string  `json:"title"`
	Rect      []int64 `json:"rect"`
}

// ActiveWindow asks the compositor which window has focus; Available is false
// when the compositor has no IPC the helper knows.
func (h *Helper) ActiveWindow(ctx context.Context) (ActiveWindow, error) {
	var window ActiveWindow
	err := h.Call(ctx, "window.active", nil, &window)
	return window, err
}

func (h *Helper) VirtualChord(ctx context.Context, chord display.KeyChord) error {
	modifiers := make([]uint32, 0, len(chord.Modifiers))
	for _, modifier := range chord.Modifiers {
		modifiers = append(modifiers, uint32(modifier))
	}
	return h.Call(ctx, "vkeyboard.chord", map[string]any{"modifiers": modifiers, "key": uint32(chord.Key)}, nil)
}

func (h *Helper) VirtualText(ctx context.Context, text string) error {
	return h.Call(ctx, "vkeyboard.text", map[string]any{"text": text}, nil)
}
