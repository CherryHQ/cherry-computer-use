package wayland

import (
	"context"
	"strings"

	"github.com/iFurySt/open-codex-computer-use/apps/opencomputeruselinux/internal/display"
)

// ShellProtocol is the GNOME Shell extension interface version this runtime speaks.
const ShellProtocol = 1

// GNOME reports whether this is a GNOME session.
func (p Probe) GNOME() bool {
	for _, desktop := range strings.Split(p.Session.Desktop, ":") {
		if strings.EqualFold(desktop, "GNOME") {
			return true
		}
	}
	return false
}

// ShellAvailable reports whether the computer-use@cherryai extension is running
// with the interface this runtime speaks.
func (p Probe) ShellAvailable() bool {
	return p.GnomeShell != nil && p.GnomeShell.Protocol == ShellProtocol
}

// ShellMissing explains what a GNOME session lacks without the extension.
func (p Probe) ShellMissing() display.Availability {
	if p.GnomeShell != nil {
		return display.Unavailable("DEPENDENCY_MISSING", "The Cherry Computer Use GNOME Shell extension is a different version; run `open-computer-use install-gnome-extension`, then log out and back in")
	}
	return display.Unavailable("DEPENDENCY_MISSING", "GNOME needs the Cherry Computer Use Shell extension: run `open-computer-use install-gnome-extension`, then log out and back in")
}

// ShellAuthorization is "granted", "denied" or "notDetermined" for this runtime's helper.
func (h *Helper) ShellAuthorization(ctx context.Context) (string, error) {
	var result struct {
		Status string `json:"status"`
	}
	err := h.Call(ctx, "shell.authorization", nil, &result)
	return result.Status, err
}

// ShellAuthorize shows the Shell's dialog unless already granted, blocking until
// the user answers, and returns the resulting status.
func (h *Helper) ShellAuthorize(ctx context.Context) (string, error) {
	var result struct {
		Status string `json:"status"`
	}
	err := h.Call(ctx, "shell.authorize", nil, &result)
	return result.Status, err
}

func shellTarget(target display.Target, params map[string]any) map[string]any {
	params["pid"], params["title"] = target.PID, target.Title
	return params
}

// ShellClick moves the pointer to point and, if the target owns it, clicks.
func (h *Helper) ShellClick(ctx context.Context, target display.Target, point display.Point, button byte, count int) error {
	return h.Call(ctx, "shell.click", shellTarget(target, map[string]any{"x": point.X, "y": point.Y, "button": button, "count": count}), nil)
}

// ShellDrag presses on from, if the target owns it, and releases on to.
func (h *Helper) ShellDrag(ctx context.Context, target display.Target, from, to display.Point) error {
	return h.Call(ctx, "shell.drag", shellTarget(target, map[string]any{"from_x": from.X, "from_y": from.Y, "to_x": to.X, "to_y": to.Y}), nil)
}

// ShellChord sends a key chord if the target is the focus window.
func (h *Helper) ShellChord(ctx context.Context, target display.Target, chord display.KeyChord) error {
	modifiers := make([]uint32, 0, len(chord.Modifiers))
	for _, modifier := range chord.Modifiers {
		modifiers = append(modifiers, uint32(modifier))
	}
	return h.Call(ctx, "shell.chord", shellTarget(target, map[string]any{"modifiers": modifiers, "key": uint32(chord.Key)}), nil)
}

// ShellText types text into the target if it is the focus window; text-input
// clients receive any Unicode through the input method.
func (h *Helper) ShellText(ctx context.Context, target display.Target, text string) error {
	return h.Call(ctx, "shell.text", shellTarget(target, map[string]any{"text": text}), nil)
}
