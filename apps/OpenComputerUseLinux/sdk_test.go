package main

import (
	"context"
	"errors"
	"testing"

	sdk "github.com/CherryHQ/cherry-computer-use/packages/runtime-go"
	"github.com/iFurySt/open-codex-computer-use/apps/opencomputeruselinux/internal/desktop"
)

func testDesktop(env map[string]string) *linuxDesktop {
	return &linuxDesktop{desktop.New(desktop.Config{Env: env, InputGuard: inputGuard})}
}

func globalActions() map[string]struct {
	action  sdk.Action
	element bool
} {
	x, y := 10.0, 20.0
	return map[string]struct {
		action  sdk.Action
		element bool
	}{
		"coordinate click": {sdk.Action{Type: "click", Button: "left", Count: 1, X: &x, Y: &y}, false},
		"double click":     {sdk.Action{Type: "click", Button: "left", Count: 2}, true},
		"right click":      {sdk.Action{Type: "click", Button: "right", Count: 1}, true},
		"drag":             {sdk.Action{Type: "drag", From: &sdk.Point{X: 1, Y: 1}, To: &sdk.Point{X: 2, Y: 2}}, false},
		"press key":        {sdk.Action{Type: "pressKey", Key: "Return"}, false},
		"scroll":           {sdk.Action{Type: "scroll", Direction: "down", Pages: 1}, false},
	}
}

func TestSDKGlobalInputNeedsExplicitPermissionOnX11(t *testing.T) {
	driver := testDesktop(map[string]string{"DISPLAY": ":99"})
	observation := sdk.Observation{Native: desktop.Observation{}}
	clickable := &sdk.Element{Native: desktop.Node{Actions: []desktop.Action{{Index: 0, Name: "click"}}, Extents: &desktop.Rect{Width: 10, Height: 10}}}
	for name, testCase := range globalActions() {
		element := clickable
		if !testCase.element {
			element = nil
		}
		err := driver.Act(context.Background(), sdk.Target{}, observation, element, testCase.action)
		var domain *sdk.DomainError
		if !errors.As(err, &domain) || domain.Code != "PERMISSION_REQUIRED" || domain.Effect != "none" {
			t.Fatalf("%s without allowGlobalInput must be refused without side effects: %v", name, err)
		}
	}
}

func TestSDKRefusesGlobalInputOnWayland(t *testing.T) {
	driver := testDesktop(map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":0"})
	observation := sdk.Observation{Native: desktop.Observation{}}
	clickable := &sdk.Element{Native: desktop.Node{Actions: []desktop.Action{{Index: 0, Name: "click"}}}}
	for name, testCase := range globalActions() {
		element := clickable
		if !testCase.element {
			element = nil
		}
		testCase.action.AllowGlobalInput = true
		err := driver.Act(context.Background(), sdk.Target{}, observation, element, testCase.action)
		var domain *sdk.DomainError
		if !errors.As(err, &domain) || domain.Code != "UNSUPPORTED_CAPABILITY" || domain.Effect != "none" {
			t.Fatalf("%s must not be translated into XWayland input: %v", name, err)
		}
	}
}

func TestSDKCapabilitiesFollowTheDisplayServer(t *testing.T) {
	for env, want := range map[string]string{"": "unavailable", ":99": "available", "wayland": "unsupported"} {
		vars := map[string]string{"DISPLAY": env}
		if env == "wayland" {
			vars = map[string]string{"DISPLAY": ":0", "XDG_SESSION_TYPE": "wayland"}
		}
		capabilities := testDesktop(vars).Capabilities(context.Background())
		for _, name := range []string{"scroll", "drag", "pressKey"} {
			if capabilities[name].Status != want {
				t.Fatalf("%s with %q = %+v, want %s", name, env, capabilities[name], want)
			}
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
