package sdkruntime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type permissionDriver struct {
	desktopFixture
	requested []string
}

func (d *permissionDriver) Permissions(context.Context) ([]Permission, error) {
	return []Permission{{ID: "remoteDesktop", Label: "Remote input", Status: "notDetermined", Interaction: "systemPrompt"}}, nil
}

func (d *permissionDriver) RequestPermissions(_ context.Context, ids []string) ([]Permission, error) {
	d.requested = ids
	return []Permission{{ID: "remoteDesktop", Label: "Remote input", Status: "granted", Interaction: "systemPrompt"}}, nil
}

func TestPermissionDriverReportsAndRequests(t *testing.T) {
	driver := &permissionDriver{}
	desktop := NewDesktop(driver)
	permissions, err := desktop.Permissions(context.Background())
	if err != nil || len(permissions) != 1 || permissions[0].Status != "notDetermined" {
		t.Fatalf("permissions = %+v, %v", permissions, err)
	}
	result, err := desktop.Call(context.Background(), "requestPermissions", json.RawMessage(`{"ids":["remoteDesktop"]}`))
	if err != nil || len(driver.requested) != 1 || result.(map[string]any)["permissions"].([]Permission)[0].Status != "granted" {
		t.Fatalf("request = %+v, %v, %v", result, err, driver.requested)
	}
	for _, params := range []string{`{"ids":[]}`, `{"ids":["a","a"]}`, `{"ids":[""]}`, `{}`} {
		var domain *DomainError
		if _, err := desktop.Call(context.Background(), "requestPermissions", json.RawMessage(params)); !errors.As(err, &domain) || domain.Code != "INVALID_ARGUMENT" {
			t.Fatalf("%s: %v", params, err)
		}
	}
}

func TestDriversWithoutPermissionsStayEmpty(t *testing.T) {
	desktop := NewDesktop(&desktopFixture{})
	if permissions, err := desktop.Permissions(context.Background()); err != nil || len(permissions) != 0 {
		t.Fatalf("permissions = %+v, %v", permissions, err)
	}
	var domain *DomainError
	if _, err := desktop.Call(context.Background(), "requestPermissions", json.RawMessage(`{"ids":["x"]}`)); !errors.As(err, &domain) || domain.Code != "UNSUPPORTED_CAPABILITY" {
		t.Fatalf("request = %v", err)
	}
}
