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
