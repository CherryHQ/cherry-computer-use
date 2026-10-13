package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/CherryHQ/cherry-computer-use/packages/runtime-go"
	"github.com/iFurySt/open-codex-computer-use/apps/opencomputeruselinux/internal/desktop"
	"github.com/iFurySt/open-codex-computer-use/apps/opencomputeruselinux/internal/display"
)

// linuxDesktop adapts the shared engine to SDK sessions; runtime-go owns identities and snapshots.
type linuxDesktop struct{ engine *desktop.Engine }

func newLinuxDesktop() *linuxDesktop {
	return &linuxDesktop{desktop.New(desktop.Config{InputGuard: inputGuard, WaylandHelper: waylandHelperPath()})}
}

// pointerInput reports whether pointer input can be offered: XTEST on X11, and
// on Wayland the wlroots virtual pointer with compositor IPC (Sway, Hyprland).
func (d *linuxDesktop) pointerInput(ctx context.Context) sdkruntime.Availability {
	return availability(d.engine.PointerAvailability(ctx))
}

func availability(native display.Availability) sdkruntime.Availability {
	if native.Status == "available" {
		return sdkruntime.Availability{Status: "available"}
	}
	return sdkruntime.Availability{Status: native.Status, Reason: &sdkruntime.Reason{Code: native.Code, Message: native.Message}}
}

// keyboardInput reports whether keys can be sent: XTEST on X11, and on Wayland
// libei through the RemoteDesktop portal, which also needs the remoteDesktop permission.
func (d *linuxDesktop) keyboardInput(ctx context.Context) sdkruntime.Availability {
	if !d.engine.Wayland() {
		return d.pointerInput(ctx)
	}
	return availability(d.engine.KeyboardAvailability(ctx))
}

const remoteDesktopPermission = "remoteDesktop"

// Permissions reports the Wayland grants this session has. GNOME has the Shell
// extension's desktop control ("unavailable" until the extension runs); keys
// through the portal need remoteDesktop. X11 sessions and wlroots compositors need none.
func (d *linuxDesktop) Permissions(ctx context.Context) ([]sdkruntime.Permission, error) {
	permissions := []sdkruntime.Permission{}
	if status, applies := d.engine.ShellPermission(ctx); applies {
		permissions = append(permissions, shellPermission(status))
	}
	if d.engine.KeyboardNeedsGrant(ctx) {
		status, _ := d.engine.KeyboardPermission(ctx)
		permissions = append(permissions, sdkruntime.Permission{ID: remoteDesktopPermission, Label: "Remote keyboard input", Status: status, Interaction: "systemPrompt"})
	}
	return permissions, nil
}

// shellPermission describes GNOME desktop control: the Shell asks once the
// extension runs; before that the user must install it and log in again.
func shellPermission(status string) sdkruntime.Permission {
	interaction := "systemPrompt"
	if status == "unavailable" {
		interaction = "systemSettings"
	}
	return sdkruntime.Permission{ID: desktop.ShellPermissionID, Label: "GNOME desktop control", Status: status, Interaction: interaction}
}

// RequestPermissions shows the Shell's or the RemoteDesktop portal's dialog and
// reports the result; declining is a "denied" status, not an error.
func (d *linuxDesktop) RequestPermissions(ctx context.Context, ids []string) ([]sdkruntime.Permission, error) {
	_, shellApplies := d.engine.ShellPermission(ctx)
	for _, id := range ids {
		known := (id == desktop.ShellPermissionID && shellApplies) || (id == remoteDesktopPermission && d.engine.KeyboardNeedsGrant(ctx))
		if !known {
			return nil, sdkruntime.Error("INVALID_ARGUMENT", "Unknown permission for this Linux session: "+id)
		}
	}
	for _, id := range ids {
		if id == desktop.ShellPermissionID {
			// Without the extension there is nothing to ask; the status says so.
			if _, err := d.engine.RequestShell(ctx); err != nil && !isCode(err, "DEPENDENCY_MISSING") {
				return nil, sdkError(err)
			}
			continue
		}
		if err := d.engine.RequestKeyboard(ctx); err != nil {
			if !isCode(err, "PERMISSION_REQUIRED") {
				return nil, sdkError(err)
			}
			permissions, _ := d.Permissions(ctx)
			for i := range permissions {
				if permissions[i].ID == remoteDesktopPermission {
					permissions[i].Status = "denied"
				}
			}
			return permissions, nil
		}
	}
	return d.Permissions(ctx)
}

func isCode(err error, code string) bool {
	var native *desktop.Error
	return errors.As(err, &native) && native.Code == code
}

func sdkError(err error) error {
	var native *desktop.Error
	if errors.As(err, &native) {
		return &sdkruntime.DomainError{Code: native.Code, Message: native.Message, Effect: native.Effect}
	}
	if errors.Is(err, context.Canceled) {
		return sdkruntime.Error("CANCELLED", "Request cancelled")
	}
	return err
}

func (d *linuxDesktop) Capabilities(ctx context.Context) map[string]sdkruntime.Availability {
	available := sdkruntime.Availability{Status: "available"}
	if err := d.engine.Connect(ctx); err != nil {
		available = sdkruntime.Unavailable("DEPENDENCY_MISSING", err.Error())
	}
	capture := availability(d.engine.CaptureAvailability(ctx))
	keyboard := d.keyboardInput(ctx)
	return map[string]sdkruntime.Availability{
		"accessibility": available, "screenshot": capture,
		"click": available, "performSecondaryAction": available, "setValue": available, "typeText": available,
		"scroll": keyboard, "drag": d.pointerInput(ctx), "pressKey": keyboard,
	}
}

func (d *linuxDesktop) Close(context.Context) error { return d.engine.Close() }

func (d *linuxDesktop) Apps(ctx context.Context) ([]sdkruntime.Target, error) {
	apps, skipped, err := d.engine.Apps(ctx)
	if len(skipped) > 0 {
		fmt.Fprintf(os.Stderr, "open-computer-use: skipped %d registered applications; first reason: %v\n", len(skipped), skipped[0])
	}
	if err != nil {
		return nil, sdkError(err)
	}
	targets := make([]sdkruntime.Target, 0, len(apps))
	for _, app := range apps {
		targets = append(targets, sdkruntime.Target{Key: app.Ref.Bus + string(app.Ref.Path), Name: app.Name, Native: app})
	}
	return targets, nil
}

func (d *linuxDesktop) Observe(ctx context.Context, target sdkruntime.Target, options sdkruntime.ObserveOptions) (sdkruntime.Observation, error) {
	app := target.Native.(desktop.App)
	readLimit := -1
	if limit, ok := observeTextLimit(options); ok {
		readLimit = limit
	}
	native, err := d.engine.Observe(ctx, app, desktop.TreeOptions{MaxNodes: options.MaxTreeNodes, MaxDepth: options.MaxTreeDepth, TextReadLimit: readLimit})
	if err != nil {
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return sdkruntime.Observation{}, sdkruntime.Error("CANCELLED", "Observation cancelled")
		}
		return sdkruntime.Observation{}, sdkError(err)
	}
	tree := sdkruntime.Tree{Status: "available", Elements: make([]sdkruntime.Element, 0, len(native.Nodes)), Truncated: []string{}}
	if native.NodesTruncated {
		tree.Truncated = append(tree.Truncated, "nodes")
	}
	if native.DepthTruncated {
		tree.Truncated = append(tree.Truncated, "depth")
	}
	pointer := d.pointerInput(ctx).Status == "available"
	for index, node := range native.Nodes {
		element := sdkruntime.Element{ID: strconv.Itoa(index), Role: node.Role, Name: options.LimitText(node.Name, &tree), Native: node}
		if node.Parent >= 0 {
			element.ParentID = strconv.Itoa(node.Parent)
		}
		if value := nodeValue(node); value != "" {
			element.Value = options.LimitText(value, &tree)
		}
		if _, ok := desktop.ClickAction(node.Actions); ok || (pointer && node.Extents != nil) {
			element.Actions = append(element.Actions, "click")
		}
		for _, action := range node.Actions {
			if action.Label() != "" {
				element.SecondaryActions = append(element.SecondaryActions, sdkruntime.SecondaryAction{ID: strconv.Itoa(int(action.Index)), Label: action.Label()})
			}
		}
		if len(element.SecondaryActions) > 0 {
			element.Actions = append(element.Actions, "performSecondaryAction")
		}
		if node.Settable() {
			element.Actions = append(element.Actions, "setValue")
		}
		tree.Elements = append(tree.Elements, element)
	}
	observation := sdkruntime.Observation{Window: sdkruntime.Window{Title: native.Window.Title}, Tree: tree, Native: native}
	capture := d.engine.Capture(ctx, app.PID, native.Window.Title)
	if capture.Reason != nil {
		observation.Screenshot = sdkruntime.Capture{Status: "unavailable", Reason: &sdkruntime.Reason{Code: capture.Reason.Code, Message: capture.Reason.Message}}
	} else {
		observation.Screenshot = sdkruntime.Capture{Status: "available", Image: &sdkruntime.Image{MimeType: "image/png", Width: capture.Width, Height: capture.Height, DataBase64: base64.StdEncoding.EncodeToString(capture.PNG)}}
	}
	return observation, nil
}

// observeTextLimit mirrors ObserveOptions.LimitText so the engine reads no more text than is shown.
func observeTextLimit(options sdkruntime.ObserveOptions) (int, bool) {
	if text, ok := options.TextLimit.(string); ok && text == "max" {
		return 0, false
	}
	if number, ok := options.TextLimit.(float64); ok {
		return int(number), true
	}
	return 500, true
}

func nodeValue(node desktop.Node) string {
	if node.Text != "" {
		return node.Text
	}
	if node.Value != nil {
		return strconv.FormatFloat(*node.Value, 'f', -1, 64)
	}
	return ""
}

func (d *linuxDesktop) Click(ctx context.Context, target sdkruntime.Target, observation sdkruntime.Observation, element sdkruntime.Element) error {
	return d.Act(ctx, target, observation, &element, sdkruntime.Action{Type: "click", Button: "left", Count: 1})
}

// Act prefers semantic actions. Pointer and keyboard variants need
// allowGlobalInput; pointer input needs X11 or the wlroots virtual pointer, keys
// need X11, the wlroots virtual keyboard or the Wayland remoteDesktop permission. Targets are checked before any input is sent.
func (d *linuxDesktop) Act(ctx context.Context, _ sdkruntime.Target, observation sdkruntime.Observation, element *sdkruntime.Element, action sdkruntime.Action) error {
	native := observation.Native.(desktop.Observation)
	var node desktop.Node
	if element != nil {
		node = element.Native.(desktop.Node)
	}
	target := desktop.GlobalTarget{PID: native.App.PID, Title: native.Window.Title, Window: native.Window.Ref}
	switch action.Type {
	case "click":
		click, semantic := desktop.ClickAction(node.Actions)
		if element != nil && semantic && action.Button == "left" && action.Count == 1 {
			return sdkError(d.engine.DoAction(ctx, native.Window, node, click))
		}
		if err := d.requireGlobalInput(action, d.pointerInput(ctx)); err != nil {
			return err
		}
		button := map[string]byte{"left": 1, "middle": 2, "right": 3}[action.Button]
		if element == nil {
			point, err := d.windowPoint(ctx, target, sdkruntime.Point{X: *action.X, Y: *action.Y})
			if err != nil {
				return err
			}
			return sdkError(d.engine.PointerClick(ctx, target, point, button, action.Count))
		}
		if node.Extents == nil {
			return sdkruntime.Error("UNSUPPORTED_CAPABILITY", "Element has no bounds for a pointer click")
		}
		current, err := d.engine.Revalidate(ctx, native.Window, node)
		if err != nil {
			return sdkError(err)
		}
		center, err := d.engine.ElementCenter(ctx, target, current)
		if err != nil {
			return sdkError(err)
		}
		return sdkError(d.engine.PointerClick(ctx, target, center, button, action.Count))
	case "performSecondaryAction":
		for _, candidate := range node.Actions {
			if strconv.Itoa(int(candidate.Index)) == action.ActionID {
				return sdkError(d.engine.DoAction(ctx, native.Window, node, candidate))
			}
		}
		return sdkruntime.Error("UNSUPPORTED_CAPABILITY", "Element does not expose this secondary action")
	case "setValue":
		return sdkError(d.engine.SetValue(ctx, native.Window, node, action.Value))
	case "typeText":
		err := d.engine.TypeText(ctx, native.Window, action.Text)
		var engineError *desktop.Error
		if !errors.As(err, &engineError) || engineError.Code != desktop.ErrNoFocusedText {
			return sdkError(err)
		}
		if err := d.requireGlobalInput(action, d.keyboardInput(ctx)); err != nil {
			var domain *sdkruntime.DomainError
			errors.As(err, &domain)
			return sdkruntime.Error(domain.Code, engineError.Message+"; typing without one needs global input: "+domain.Message)
		}
		return sdkError(d.engine.TypeKeys(ctx, target, action.Text))
	case "pressKey":
		if err := d.requireGlobalInput(action, d.keyboardInput(ctx)); err != nil {
			return err
		}
		return sdkError(d.engine.PressKey(ctx, target, action.Key))
	case "scroll":
		if err := d.requireGlobalInput(action, d.keyboardInput(ctx)); err != nil {
			return err
		}
		return sdkError(d.engine.PageScroll(ctx, target, action.Direction, action.Pages))
	case "drag":
		if err := d.requireGlobalInput(action, d.pointerInput(ctx)); err != nil {
			return err
		}
		from, err := d.windowPoint(ctx, target, *action.From)
		if err != nil {
			return err
		}
		to, err := d.windowPoint(ctx, target, *action.To)
		if err != nil {
			return err
		}
		return sdkError(d.engine.Drag(ctx, target, from, to))
	default:
		return sdkruntime.Error("UNSUPPORTED_CAPABILITY", "Unknown action")
	}
}

// requireGlobalInput refuses before dispatch unless the caller allowed global
// input and the session can deliver it.
func (d *linuxDesktop) requireGlobalInput(action sdkruntime.Action, availability sdkruntime.Availability) error {
	if availability.Status != "available" {
		return sdkruntime.Error(availability.Reason.Code, availability.Reason.Message)
	}
	if !action.AllowGlobalInput {
		return sdkruntime.Error("PERMISSION_REQUIRED", "This Linux action moves the shared pointer or keyboard; it needs allowGlobalInput: true")
	}
	return nil
}

// windowPoint maps screenshot coordinates, relative to the observed window, to
// the screen and refuses points outside that window.
func (d *linuxDesktop) windowPoint(ctx context.Context, target desktop.GlobalTarget, point sdkruntime.Point) (desktop.Point, error) {
	window, err := d.engine.WindowRect(ctx, target)
	if err != nil {
		return desktop.Point{}, sdkError(err)
	}
	if point.X < 0 || point.Y < 0 || point.X >= window.Width || point.Y >= window.Height {
		return desktop.Point{}, sdkruntime.Error("INVALID_ARGUMENT", "Point is outside the observed window")
	}
	return desktop.Point{X: window.X + point.X, Y: window.Y + point.Y}, nil
}
