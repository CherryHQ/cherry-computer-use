package main

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	sdk "github.com/CherryHQ/cherry-computer-use/packages/runtime-go"
	"github.com/godbus/dbus/v5"
)

type accessibleRef struct {
	Bus  string
	Path dbus.ObjectPath
}
type linuxTarget struct {
	Ref accessibleRef
	PID uint32
}
type linuxElement struct {
	Ref                accessibleRef
	Name, Role, Action string
	Index              int32
}
type linuxObservation struct {
	Window accessibleRef
	Title  string
}
type linuxDesktop struct{ bus *dbus.Conn }

func (d *linuxDesktop) connect(ctx context.Context) error {
	if d.bus != nil {
		if !d.bus.Connected() {
			return sdk.Error("TARGET_UNAVAILABLE", "AT-SPI connection closed; start a new session")
		}
		return nil
	}
	session, err := dbus.SessionBusPrivateNoAutoStartup(dbus.WithContext(ctx))
	if err != nil {
		return sdk.Error("DEPENDENCY_MISSING", "A desktop D-Bus session is required")
	}
	defer session.Close()
	if err = session.Auth(nil); err != nil {
		return sdk.Error("DEPENDENCY_MISSING", "Cannot authenticate to the desktop D-Bus session")
	}
	if err = session.Hello(); err != nil {
		return sdk.Error("DEPENDENCY_MISSING", "Cannot join the desktop D-Bus session")
	}
	var address string
	if err = session.Object("org.a11y.Bus", "/org/a11y/bus").CallWithContext(ctx, "org.a11y.Bus.GetAddress", 0).Store(&address); err != nil {
		return sdk.Error("DEPENDENCY_MISSING", "The desktop AT-SPI bus is unavailable")
	}
	d.bus, err = dbus.Connect(address)
	if err != nil {
		return sdk.Error("DEPENDENCY_MISSING", "Cannot connect to the AT-SPI bus")
	}
	return nil
}

func (d *linuxDesktop) Capabilities(ctx context.Context) map[string]sdk.Availability {
	available := sdk.Availability{Status: "available"}
	if err := d.connect(ctx); err != nil {
		available = sdk.Unavailable("DEPENDENCY_MISSING", err.Error())
	}
	capture := sdk.Unavailable("DEPENDENCY_MISSING", "An X11 display is required for window capture")
	if os.Getenv("WAYLAND_DISPLAY") != "" || strings.EqualFold(os.Getenv("XDG_SESSION_TYPE"), "wayland") {
		capture = sdk.Availability{Status: "unsupported", Reason: &sdk.Reason{Code: "UNSUPPORTED_CAPABILITY", Message: "Wayland capture is not connected"}}
	} else if os.Getenv("DISPLAY") != "" {
		capture = sdk.Availability{Status: "available"}
	}
	return map[string]sdk.Availability{"accessibility": available, "click": available, "screenshot": capture}
}

func (d *linuxDesktop) Close(context.Context) error {
	if d.bus != nil {
		err := d.bus.Close()
		d.bus = nil
		return err
	}
	return nil
}

func (d *linuxDesktop) call(ctx context.Context, ref accessibleRef, method string, args ...any) *dbus.Call {
	callCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	return d.bus.Object(ref.Bus, ref.Path).CallWithContext(callCtx, "org.a11y.atspi."+method, 0, args...)
}

func (d *linuxDesktop) property(ctx context.Context, ref accessibleRef, name string, result any) error {
	var value dbus.Variant
	callCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := d.bus.Object(ref.Bus, ref.Path).CallWithContext(callCtx, "org.freedesktop.DBus.Properties.Get", 0, "org.a11y.atspi.Accessible", name).Store(&value); err != nil {
		return err
	}
	return dbus.Store([]any{value.Value()}, result)
}

func (d *linuxDesktop) Apps(ctx context.Context) ([]sdk.Target, error) {
	if err := d.connect(ctx); err != nil {
		return nil, err
	}
	var refs []accessibleRef
	root := accessibleRef{"org.a11y.atspi.Registry", "/org/a11y/atspi/accessible/root"}
	if err := d.call(ctx, root, "Accessible.GetChildren").Store(&refs); err != nil {
		return nil, err
	}
	targets := []sdk.Target{}
	for _, ref := range refs {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Unique bus names are lifetime identities; never accept a recyclable well-known name.
		if !strings.HasPrefix(ref.Bus, ":") {
			continue
		}
		var name string
		if d.property(ctx, ref, "Name", &name) != nil || name == "" {
			continue
		}
		var pid uint32
		if err := d.bus.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetConnectionUnixProcessID", 0, ref.Bus).Store(&pid); err != nil {
			continue
		}
		targets = append(targets, sdk.Target{Key: ref.Bus + string(ref.Path), Name: name, Native: linuxTarget{ref, pid}})
	}
	return targets, nil
}

func (d *linuxDesktop) Observe(ctx context.Context, target sdk.Target, options sdk.ObserveOptions) (sdk.Observation, error) {
	if err := d.connect(ctx); err != nil {
		return sdk.Observation{}, err
	}
	app := target.Native.(linuxTarget)
	var children []accessibleRef
	if err := d.call(ctx, app.Ref, "Accessible.GetChildren").Store(&children); err != nil {
		return sdk.Observation{}, sdk.Error("TARGET_UNAVAILABLE", "Application is no longer available")
	}
	var window accessibleRef
	for _, child := range children {
		var role string
		if d.call(ctx, child, "Accessible.GetRoleName").Store(&role) == nil && slices.Contains([]string{"frame", "dialog", "window", "alert"}, role) {
			window = child
			break
		}
	}
	if window.Bus == "" {
		return sdk.Observation{}, sdk.Error("TARGET_UNAVAILABLE", "Application has no accessible window")
	}
	var title string
	if err := d.property(ctx, window, "Name", &title); err != nil {
		return sdk.Observation{}, err
	}
	observation := sdk.Observation{Window: sdk.Window{Title: title}, Tree: sdk.Tree{Status: "available", Elements: []sdk.Element{}, Truncated: []string{}}, Native: linuxObservation{window, title}}
	type node struct {
		ref    accessibleRef
		parent string
		depth  int
	}
	queue := []node{{ref: window}}
	visited := map[accessibleRef]bool{}
	for len(queue) > 0 {
		if ctx.Err() != nil {
			return sdk.Observation{}, ctx.Err()
		}
		current := queue[0]
		queue = queue[1:]
		if visited[current.ref] {
			continue
		}
		visited[current.ref] = true
		if len(observation.Tree.Elements) >= options.MaxTreeNodes {
			observation.Tree.Truncated = append(observation.Tree.Truncated, "nodes")
			break
		}
		var name, role string
		if err := d.property(ctx, current.ref, "Name", &name); err != nil {
			return sdk.Observation{}, err
		}
		if err := d.call(ctx, current.ref, "Accessible.GetRoleName").Store(&role); err != nil {
			return sdk.Observation{}, err
		}
		index, action := d.clickAction(ctx, current.ref)
		id := fmt.Sprint(len(observation.Tree.Elements))
		actions := []string{}
		if index >= 0 {
			actions = append(actions, "click")
		}
		observation.Tree.Elements = append(observation.Tree.Elements, sdk.Element{ID: id, ParentID: current.parent, Role: role,
			Name: options.LimitText(name, &observation.Tree), Actions: actions, Native: linuxElement{current.ref, name, role, action, index}})
		var descendants []accessibleRef
		if err := d.call(ctx, current.ref, "Accessible.GetChildren").Store(&descendants); err != nil {
			return sdk.Observation{}, err
		}
		if current.depth >= options.MaxTreeDepth {
			if len(descendants) > 0 && !slices.Contains(observation.Tree.Truncated, "depth") {
				observation.Tree.Truncated = append(observation.Tree.Truncated, "depth")
			}
			continue
		}
		for _, child := range descendants {
			queue = append(queue, node{child, id, current.depth + 1})
		}
	}
	observation.Screenshot = captureLinuxWindow(ctx, app.PID, title)
	return observation, nil
}

func (d *linuxDesktop) clickAction(ctx context.Context, ref accessibleRef) (int32, string) {
	var interfaces []string
	if d.call(ctx, ref, "Accessible.GetInterfaces").Store(&interfaces) != nil || !slices.Contains(interfaces, "org.a11y.atspi.Action") {
		return -1, ""
	}
	for index := int32(0); index < 8; index++ {
		var name string
		if d.call(ctx, ref, "Action.GetName", index).Store(&name) != nil {
			break
		}
		if slices.Contains([]string{"click", "press", "activate"}, strings.ToLower(name)) {
			return index, name
		}
	}
	return -1, ""
}

func (d *linuxDesktop) Click(ctx context.Context, target sdk.Target, observation sdk.Observation, element sdk.Element) error {
	native := element.Native.(linuxElement)
	window := observation.Native.(linuxObservation)
	var name, role, title string
	if d.property(ctx, native.Ref, "Name", &name) != nil || d.call(ctx, native.Ref, "Accessible.GetRoleName").Store(&role) != nil ||
		name != native.Name || role != native.Role || d.property(ctx, window.Window, "Name", &title) != nil || title != window.Title {
		return sdk.Error("STALE_SNAPSHOT", "Element or window changed since observation")
	}
	parent := native.Ref
	belongs := false
	for depth := 0; depth <= 128; depth++ {
		if parent == window.Window {
			belongs = true
			break
		}
		var next accessibleRef
		if d.property(ctx, parent, "Parent", &next) != nil || next == parent {
			break
		}
		parent = next
	}
	index, action := d.clickAction(ctx, native.Ref)
	if !belongs || index != native.Index || action != native.Action {
		return sdk.Error("STALE_SNAPSHOT", "Element no longer belongs to the observed window/action")
	}
	var states []uint32
	if d.call(ctx, native.Ref, "Accessible.GetState").Store(&states) != nil || len(states) == 0 || states[0]&(1<<8) == 0 {
		return sdk.Error("TARGET_UNAVAILABLE", "Element is not enabled")
	}
	if ctx.Err() != nil {
		return sdk.Error("CANCELLED", "Click cancelled before dispatch")
	}
	// A dispatched semantic action is atomic; wait for its reply rather than claiming early cancellation.
	execution, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	var applied bool
	err := d.bus.Object(native.Ref.Bus, native.Ref.Path).CallWithContext(execution, "org.a11y.atspi.Action.DoAction", 0, index).Store(&applied)
	if err != nil {
		return &sdk.DomainError{Code: "TARGET_UNAVAILABLE", Message: "Semantic action outcome is unknown", Effect: "possible"}
	}
	if !applied {
		return &sdk.DomainError{Code: "TARGET_UNAVAILABLE", Message: "Application did not confirm the action", Effect: "possible"}
	}
	return nil
}
