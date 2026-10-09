package main

import (
	"context"
	"errors"
	"testing"

	sdk "github.com/CherryHQ/cherry-computer-use/packages/runtime-go"
	"github.com/iFurySt/open-codex-computer-use/apps/opencomputeruselinux/internal/desktop"
)

func TestSDKRefusesGlobalInputVariantsBeforeDispatch(t *testing.T) {
	driver := newLinuxDesktop()
	observation := sdk.Observation{Native: desktop.Observation{}}
	clickable := &sdk.Element{Native: desktop.Node{Actions: []desktop.Action{{Index: 0, Name: "click"}}}}
	x, y := 10.0, 20.0
	for name, action := range map[string]sdk.Action{
		"coordinate click": {Type: "click", Button: "left", Count: 1, X: &x, Y: &y, AllowGlobalInput: true},
		"double click":     {Type: "click", Button: "left", Count: 2, AllowGlobalInput: true},
		"right click":      {Type: "click", Button: "right", Count: 1, AllowGlobalInput: true},
		"drag":             {Type: "drag", AllowGlobalInput: true},
		"press key":        {Type: "pressKey", Key: "Return", AllowGlobalInput: true},
		"scroll":           {Type: "scroll", Direction: "down", Pages: 1, AllowGlobalInput: true},
	} {
		element := clickable
		if action.X != nil || action.Type != "click" {
			element = nil
		}
		err := driver.Act(context.Background(), sdk.Target{}, observation, element, action)
		var domain *sdk.DomainError
		if !errors.As(err, &domain) || domain.Code != "UNSUPPORTED_CAPABILITY" || domain.Effect != "none" {
			t.Fatalf("%s must be refused without side effects even when global input is allowed: %v", name, err)
		}
	}
}

func TestSDKCapabilitiesSeparateSemanticAndGlobalActions(t *testing.T) {
	capabilities := newLinuxDesktop().Capabilities(context.Background())
	for _, name := range []string{"scroll", "drag", "pressKey"} {
		if capabilities[name].Status != "unsupported" {
			t.Fatalf("%s needs global input and must be unsupported: %+v", name, capabilities[name])
		}
	}
	for _, name := range []string{"accessibility", "click", "performSecondaryAction", "setValue", "typeText"} {
		if _, ok := capabilities[name]; !ok {
			t.Fatalf("missing capability %s", name)
		}
	}
}

func TestEngineErrorsKeepCodeAndEffect(t *testing.T) {
	err := sdkError(&desktop.Error{Code: "STALE_SNAPSHOT", Message: "changed", Effect: "none"})
	var domain *sdk.DomainError
	if !errors.As(err, &domain) || domain.Code != "STALE_SNAPSHOT" || domain.Effect != "none" {
		t.Fatalf("engine error lost its protocol meaning: %v", err)
	}
	err = sdkError(&desktop.Error{Code: "TARGET_UNAVAILABLE", Message: "unknown", Effect: "possible"})
	if !errors.As(err, &domain) || domain.Effect != "possible" {
		t.Fatalf("uncertain outcomes must stay uncertain: %v", err)
	}
}
