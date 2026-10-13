package wayland

import (
	"context"
	"strings"

	"github.com/iFurySt/open-codex-computer-use/apps/opencomputeruselinux/internal/display"
)

// Route names a compositor-assisted input route: the helper checks the target
// right before each press or key, inside the compositor (the GNOME Shell
// extension) or through it (KWin scripts with fake_input).
type Route string

const (
	RouteShell Route = "shell"
	RouteKWin  Route = "kwin"
)

// KDE reports whether this is a KDE session.
func (p Probe) KDE() bool {
	for _, desktop := range strings.Split(p.Session.Desktop, ":") {
		if strings.EqualFold(desktop, "KDE") {
			return true
		}
	}
	return false
}

// KWinInput reports whether KWin offers this helper fake_input, which it does
// only for an executable a .desktop file authorizes.
func (p Probe) KWinInput() bool { return p.KWin != nil && p.HasGlobal("org_kde_kwin_fake_input") }

// KWinCapture reports whether KWin's ScreenShot2 serves this helper.
func (p Probe) KWinCapture() bool { return p.KWin != nil && p.KWin.Screenshot }

// KDEMissing explains what a KDE session lacks without the integration.
func (p Probe) KDEMissing() display.Availability {
	return display.Unavailable("DEPENDENCY_MISSING", "KDE needs the Cherry Computer Use integration for this helper: run `open-computer-use install-kde-integration`")
}

// AssistedRoute is the compositor-assisted input route this session offers, or "".
func (p Probe) AssistedRoute() Route {
	switch {
	case p.ShellAvailable():
		return RouteShell
	case p.KWinInput():
		return RouteKWin
	}
	return ""
}

func targetParams(target display.Target, params map[string]any) map[string]any {
	params["pid"], params["title"] = target.PID, target.Title
	return params
}

// TargetedClick moves the pointer to point and clicks if the target owns it.
func (h *Helper) TargetedClick(ctx context.Context, route Route, target display.Target, point display.Point, button byte, count int) error {
	return h.Call(ctx, string(route)+".click", targetParams(target, map[string]any{"x": point.X, "y": point.Y, "button": button, "count": count}), nil)
}

// TargetedDrag presses on from, if the target owns it, and releases on to.
func (h *Helper) TargetedDrag(ctx context.Context, route Route, target display.Target, from, to display.Point) error {
	return h.Call(ctx, string(route)+".drag", targetParams(target, map[string]any{"from_x": from.X, "from_y": from.Y, "to_x": to.X, "to_y": to.Y}), nil)
}

// TargetedChord sends a key chord if the target has keyboard focus.
func (h *Helper) TargetedChord(ctx context.Context, route Route, target display.Target, chord display.KeyChord) error {
	modifiers := make([]uint32, 0, len(chord.Modifiers))
	for _, modifier := range chord.Modifiers {
		modifiers = append(modifiers, uint32(modifier))
	}
	return h.Call(ctx, string(route)+".chord", targetParams(target, map[string]any{"modifiers": modifiers, "key": uint32(chord.Key)}), nil)
}

// TargetedText types text if the target has keyboard focus. The GNOME Shell
// extension commits any Unicode to text-input clients; KWin types what the
// keyboard layout holds and refuses the rest before sending.
func (h *Helper) TargetedText(ctx context.Context, route Route, target display.Target, text string) error {
	return h.Call(ctx, string(route)+".text", targetParams(target, map[string]any{"text": text}), nil)
}
