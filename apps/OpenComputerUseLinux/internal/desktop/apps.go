package desktop

import (
	"context"
	"fmt"
	"github.com/iFurySt/open-codex-computer-use/apps/opencomputeruselinux/internal/display"
	"slices"
	"strconv"
	"strings"
)

// App is a registered AT-SPI application with a lifetime bus identity.
type App struct {
	Ref     Ref
	PID     uint32
	Name    string
	Toolkit string
}

// Rect is in screen coordinates as reported by AT-SPI.
type Rect = display.Rect

// Window is a top-level child of an application.
type Window struct {
	Ref     Ref
	Index   int
	Title   string
	Role    string
	States  States
	Extents *Rect
}

var windowRoles = []string{"frame", "window", "dialog", "alert"}

// Apps lists readable applications and the reasons any registered entry was skipped.
func (e *Engine) Apps(ctx context.Context) ([]App, []error, error) {
	if err := e.Connect(ctx); err != nil {
		return nil, nil, err
	}
	root := Ref{"org.a11y.atspi.Registry", "/org/a11y/atspi/accessible/root"}
	refs, err := e.children(ctx, root)
	if err != nil {
		return nil, nil, fail("TARGET_UNAVAILABLE", "Cannot read the AT-SPI registry: %v", err)
	}
	apps := []App{}
	skipped := []error{}
	for _, ref := range refs {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		// Unique bus names are lifetime identities; never accept a recyclable well-known name.
		if !strings.HasPrefix(ref.Bus, ":") {
			skipped = append(skipped, fmt.Errorf("%s: registry entry has no unique bus name", ref.Bus))
			continue
		}
		name, err := e.name(ctx, ref)
		if err != nil {
			skipped = append(skipped, fmt.Errorf("%s: %w", ref.Bus, err))
			continue
		}
		if name == "" {
			skipped = append(skipped, fmt.Errorf("%s: accessible name is empty", ref.Bus))
			continue
		}
		var pid uint32
		if err := e.bus.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetConnectionUnixProcessID", 0, ref.Bus).Store(&pid); err != nil {
			skipped = append(skipped, fmt.Errorf("%s: %w", ref.Bus, err))
			continue
		}
		var toolkit string
		_ = e.property(ctx, ref, "Application", "ToolkitName", &toolkit)
		apps = append(apps, App{ref, pid, name, toolkit})
	}
	if err := emptyDesktopError(len(refs), len(apps), skipped); err != nil {
		return nil, skipped, err
	}
	return apps, skipped, nil
}

// A desktop that registers applications but yields none is a failed read, not an
// empty desktop: an empty list would let a caller treat unreadable targets as absent.
func emptyDesktopError(registered, readable int, reasons []error) error {
	if registered == 0 || readable > 0 {
		return nil
	}
	message := fmt.Sprintf("none of the %d registered applications could be read", registered)
	if len(reasons) > 0 {
		message += "; first reason: " + reasons[0].Error()
	}
	return fail("TARGET_UNAVAILABLE", "%s", message)
}

// Windows lists top-level children that are windows by role or have on-screen extents.
func (e *Engine) Windows(ctx context.Context, app App) ([]Window, error) {
	if err := e.Connect(ctx); err != nil {
		return nil, err
	}
	children, err := e.children(ctx, app.Ref)
	if err != nil {
		return nil, fail("TARGET_UNAVAILABLE", "Application is no longer available")
	}
	windows := []Window{}
	for index, child := range children {
		role, _ := e.role(ctx, child)
		extents := e.extents(ctx, child)
		if !slices.Contains(windowRoles, strings.ToLower(role)) && extents == nil {
			continue
		}
		title, _ := e.name(ctx, child)
		states, _ := e.states(ctx, child)
		windows = append(windows, Window{child, index, title, role, states, extents})
	}
	return windows, nil
}

// MainWindow prefers the active window, then a showing one, then the first.
func MainWindow(app App, windows []Window) (Window, error) {
	if len(windows) == 0 {
		return Window{}, fail("TARGET_UNAVAILABLE", "No top-level AT-SPI window is available for %s", app.Name)
	}
	for _, state := range []State{StateActive, StateShowing} {
		for _, window := range windows {
			if window.States.Has(state) {
				return window, nil
			}
		}
	}
	return windows[0], nil
}

// Resolve matches a CLI app query: exact PID, then exact name or window title,
// then a substring of either. Ties keep registry order.
func (e *Engine) Resolve(ctx context.Context, query string) (App, error) {
	normalized := strings.ToLower(strings.TrimSpace(query))
	if normalized == "" {
		return App{}, fail("INVALID_ARGUMENT", "Missing required argument: app")
	}
	apps, _, err := e.Apps(ctx)
	if err != nil {
		return App{}, err
	}
	best, bestRank := App{}, 0
	for _, app := range apps {
		rank := 0
		if pid, err := strconv.ParseUint(normalized, 10, 32); err == nil && uint32(pid) == app.PID {
			rank = 3
		} else {
			names := []string{strings.ToLower(app.Name)}
			windows, _ := e.Windows(ctx, app)
			for _, window := range windows {
				names = append(names, strings.ToLower(window.Title))
			}
			for _, name := range names {
				if name == normalized {
					rank = max(rank, 2)
				} else if name != "" && strings.Contains(name, normalized) {
					rank = max(rank, 1)
				}
			}
		}
		if rank > bestRank {
			best, bestRank = app, rank
		}
	}
	if bestRank == 0 {
		return App{}, fail("TARGET_UNAVAILABLE", "appNotFound(%q)", query)
	}
	return best, nil
}

func (e *Engine) extents(ctx context.Context, ref Ref) *Rect {
	var box struct{ X, Y, Width, Height int32 }
	if e.call(ctx, ref, "Component.GetExtents", uint32(0)).Store(&box) != nil {
		return nil
	}
	if box.Width <= 0 || box.Height <= 0 || box.Width > 100000 || box.Height > 100000 {
		return nil
	}
	return &Rect{X: float64(box.X), Y: float64(box.Y), Width: float64(box.Width), Height: float64(box.Height)}
}
