package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os/exec"

	sdk "github.com/CherryHQ/cherry-computer-use/packages/runtime-go"
)

//go:embed sdk.ps1
var sdkWindowsScript string

type sdkBridge interface {
	Run(context.Context, any) (json.RawMessage, error)
	Close(context.Context) error
}
type windowsDesktop struct{ bridge sdkBridge }
type windowsTarget struct {
	PID     int    `json:"pid"`
	Started string `json:"started"`
	HWND    int64  `json:"hwnd"`
	Name    string `json:"name"`
}
type windowsObservation struct {
	Target windowsTarget
}

func (d *windowsDesktop) Capabilities(context.Context) map[string]sdk.Availability {
	availability := sdk.Availability{Status: "available"}
	if _, err := exec.LookPath("powershell.exe"); err != nil {
		availability = sdk.Unavailable("DEPENDENCY_MISSING", "Windows PowerShell is required for UI Automation")
	}
	return map[string]sdk.Availability{"accessibility": availability, "click": availability, "screenshot": availability}
}
func (d *windowsDesktop) run(ctx context.Context, operation any, result any) error {
	if d.bridge == nil {
		var err error
		d.bridge, err = newSDKBridge()
		if err != nil {
			return err
		}
	}
	data, err := d.bridge.Run(ctx, operation)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, result)
}
func (d *windowsDesktop) Apps(ctx context.Context) ([]sdk.Target, error) {
	var apps []windowsTarget
	if err := d.run(ctx, map[string]any{"method": "apps"}, &apps); err != nil {
		return nil, err
	}
	targets := make([]sdk.Target, 0, len(apps))
	for _, app := range apps {
		targets = append(targets, sdk.Target{Key: fmt.Sprintf("%d:%s", app.PID, app.Started), Name: app.Name, Native: app})
	}
	return targets, nil
}
func (d *windowsDesktop) Observe(ctx context.Context, target sdk.Target, options sdk.ObserveOptions) (sdk.Observation, error) {
	var result struct {
		sdk.Observation
		Target     windowsTarget              `json:"target"`
		References map[string]json.RawMessage `json:"references"`
	}
	if err := d.run(ctx, map[string]any{"method": "observe", "target": target.Native, "options": options}, &result); err != nil {
		return sdk.Observation{}, err
	}
	for i := range result.Tree.Elements {
		result.Tree.Elements[i].Native = result.References[result.Tree.Elements[i].ID]
	}
	result.Observation.Native = windowsObservation{Target: result.Target}
	return result.Observation, nil
}
func (d *windowsDesktop) Click(ctx context.Context, _ sdk.Target, observation sdk.Observation, element sdk.Element) error {
	if ctx.Err() != nil {
		return sdk.Error("CANCELLED", "Click cancelled before dispatch")
	}
	var applied bool
	err := d.run(ctx, map[string]any{"method": "click", "target": observation.Native.(windowsObservation).Target, "element": element.Native}, &applied)
	if err != nil {
		return err
	}
	if !applied {
		return &sdk.DomainError{Code: "TARGET_UNAVAILABLE", Message: "Application did not confirm the action", Effect: "possible"}
	}
	return nil
}
func (d *windowsDesktop) Close(ctx context.Context) error {
	if d.bridge != nil {
		return d.bridge.Close(ctx)
	}
	return nil
}
