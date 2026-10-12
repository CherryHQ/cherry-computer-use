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

func TestSDKOnWaylandRefusesPointerAndNeedsTheHelperForKeys(t *testing.T) {
	driver := testDesktop(map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":0"})
	observation := sdk.Observation{Native: desktop.Observation{}}
	clickable := &sdk.Element{Native: desktop.Node{Actions: []desktop.Action{{Index: 0, Name: "click"}}}}
	for name, testCase := range globalActions() {
		element := clickable
		if !testCase.element {
			element = nil
		}
		testCase.action.AllowGlobalInput = true
		// Pointer input is never translated into XWayland input; keys need the helper.
		want := "UNSUPPORTED_CAPABILITY"
		if testCase.action.Type == "pressKey" || testCase.action.Type == "scroll" {
			want = "DEPENDENCY_MISSING"
		}
		err := driver.Act(context.Background(), sdk.Target{}, observation, element, testCase.action)
		var domain *sdk.DomainError
		if !errors.As(err, &domain) || domain.Code != want || domain.Effect != "none" {
			t.Fatalf("%s: %v, want %s", name, err, want)
		}
	}
}

func TestSDKWaylandPermissionsNeedTheSession(t *testing.T) {
	x11 := testDesktop(map[string]string{"DISPLAY": ":99"})
	if permissions, _ := x11.Permissions(context.Background()); len(permissions) != 0 {
		t.Fatalf("X11 sessions have no OS permission flow: %+v", permissions)
	}
	var domain *sdk.DomainError
	if _, err := x11.RequestPermissions(context.Background(), []string{"remoteDesktop"}); !errors.As(err, &domain) || domain.Code != "INVALID_ARGUMENT" {
		t.Fatalf("remoteDesktop does not apply to X11: %v", err)
	}
	wayland := testDesktop(map[string]string{"XDG_SESSION_TYPE": "wayland"})
	permissions, _ := wayland.Permissions(context.Background())
	if len(permissions) != 1 || permissions[0].ID != "remoteDesktop" || permissions[0].Status != "unavailable" {
		t.Fatalf("without the helper the permission is unavailable: %+v", permissions)
	}
	if _, err := wayland.RequestPermissions(context.Background(), []string{"screenRecording"}); !errors.As(err, &domain) || domain.Code != "INVALID_ARGUMENT" {
		t.Fatalf("unknown permission: %v", err)
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
			expected := want
			if env == "wayland" && name != "drag" {
				// Keys go through the Wayland helper, which this test does not install.
				expected = "unavailable"
			}
			if capabilities[name].Status != expected {
				t.Fatalf("%s with %q = %+v, want %s", name, env, capabilities[name], expected)
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
