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
)

// linuxDesktop adapts the shared engine to SDK sessions; runtime-go owns identities and snapshots.
type linuxDesktop struct{ engine *desktop.Engine }

func newLinuxDesktop() *linuxDesktop { return &linuxDesktop{desktop.New(desktop.Config{})} }

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
	capture := sdkruntime.Unavailable("DEPENDENCY_MISSING", "An X11 display is required for window capture")
	if d.engine.Wayland() {
		capture = sdkruntime.Availability{Status: "unsupported", Reason: &sdkruntime.Reason{Code: "UNSUPPORTED_CAPABILITY", Message: "Wayland capture is not connected"}}
	} else if d.engine.Display() != "" {
		capture = sdkruntime.Availability{Status: "available"}
	}
	globalInput := sdkruntime.Availability{Status: "unsupported", Reason: &sdkruntime.Reason{Code: "UNSUPPORTED_CAPABILITY", Message: "Linux global input is not connected to the SDK"}}
	return map[string]sdkruntime.Availability{
		"accessibility": available, "screenshot": capture,
		"click": available, "performSecondaryAction": available, "setValue": available, "typeText": available,
		"scroll": globalInput, "drag": globalInput, "pressKey": globalInput,
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
	for index, node := range native.Nodes {
		element := sdkruntime.Element{ID: strconv.Itoa(index), Role: node.Role, Name: options.LimitText(node.Name, &tree), Native: node}
		if node.Parent >= 0 {
			element.ParentID = strconv.Itoa(node.Parent)
		}
		if value := nodeValue(node); value != "" {
			element.Value = options.LimitText(value, &tree)
		}
		if _, ok := desktop.ClickAction(node.Actions); ok {
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

// Act runs semantic actions only. Variants that need the pointer or keyboard are
// refused before any side effect, whatever allowGlobalInput says.
func (d *linuxDesktop) Act(ctx context.Context, _ sdkruntime.Target, observation sdkruntime.Observation, element *sdkruntime.Element, action sdkruntime.Action) error {
	native := observation.Native.(desktop.Observation)
	var node desktop.Node
	if element != nil {
		node = element.Native.(desktop.Node)
	}
	switch action.Type {
	case "click":
		if element == nil || action.Button != "left" || action.Count != 1 {
			return sdkruntime.Error("UNSUPPORTED_CAPABILITY", "Linux supports one semantic left click on an element; pointer clicks need global input, which is not connected")
		}
		click, ok := desktop.ClickAction(node.Actions)
		if !ok {
			return sdkruntime.Error("UNSUPPORTED_CAPABILITY", "Element has no semantic click action")
		}
		return sdkError(d.engine.DoAction(ctx, native.Window, node, click))
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
		if errors.As(err, &engineError) && engineError.Code == desktop.ErrNoFocusedText {
			return sdkruntime.Error("TARGET_UNAVAILABLE", engineError.Message+"; typing without one needs global input, which is not connected")
		}
		return sdkError(err)
	default:
		return sdkruntime.Error("UNSUPPORTED_CAPABILITY", "Linux "+action.Type+" needs global input, which is not connected to the SDK")
	}
}
