package sdkruntime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type desktopFixture struct {
	count        int
	observeError error
	clickError   error
	afterClick   func()
}

func (*desktopFixture) Capabilities(context.Context) map[string]Availability {
	return map[string]Availability{"click": {Status: "available"}}
}
func (*desktopFixture) Apps(context.Context) ([]Target, error) {
	return []Target{{Key: "process-start-1", Name: "Counter"}, {Key: "process-start-2", Name: "Other"}}, nil
}
func (f *desktopFixture) Observe(context.Context, Target, ObserveOptions) (Observation, error) {
	if f.observeError != nil {
		return Observation{}, f.observeError
	}
	return Observation{Window: Window{Title: "Counter"}, Tree: Tree{Status: "available", Elements: []Element{{ID: "button", Role: "button", Name: "Increment", Actions: []string{"click"}, Native: "real-button"}}}, Screenshot: Capture{Status: "unavailable", Reason: &Reason{"CAPTURE_FAILED", "Capture permission missing"}}}, nil
}
func (f *desktopFixture) Click(_ context.Context, _ Target, _ Observation, element Element) error {
	if element.Native != "real-button" {
		return errors.New("lost native reference")
	}
	if f.clickError != nil {
		return f.clickError
	}
	f.count++
	if f.afterClick != nil {
		f.afterClick()
	}
	return nil
}
func (*desktopFixture) Close(context.Context) error { return nil }
func desktopCall(t *testing.T, d *Desktop, method string, params any) (any, error) {
	t.Helper()
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	return d.Call(context.Background(), method, data)
}
func observed(t *testing.T, d *Desktop) Snapshot {
	t.Helper()
	apps, err := desktopCall(t, d, "listApps", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	opened, err := desktopCall(t, d, "openAppSession", map[string]any{"appId": apps.([]App)[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	value, err := desktopCall(t, d, "getAppState", map[string]any{"appSessionId": opened.(AppSession).ID})
	if err != nil {
		t.Fatal(err)
	}
	return value.(Snapshot)
}
func clickParams(s Snapshot) map[string]any {
	return map[string]any{"type": "click", "appSessionId": s.AppSessionID, "snapshotId": s.ID, "elementId": s.Tree.Elements[0].ID}
}
func expectCode(t *testing.T, err error, code string) {
	t.Helper()
	var domain *DomainError
	if !errors.As(err, &domain) || domain.Code != code {
		t.Fatalf("wanted %s, got %v", code, err)
	}
}

func TestDesktopRejectsForeignAndReobservedSnapshots(t *testing.T) {
	fixture := &desktopFixture{}
	d := NewDesktop(fixture)
	old := observed(t, d)
	other := NewDesktop(&desktopFixture{})
	_, err := desktopCall(t, other, "act", clickParams(old))
	expectCode(t, err, "APP_SESSION_NOT_FOUND")
	current := observed(t, d)
	_, err = desktopCall(t, d, "act", clickParams(old))
	expectCode(t, err, "STALE_SNAPSHOT")
	foreignElement := clickParams(current)
	foreignElement["elementId"] = old.Tree.Elements[0].ID
	_, err = desktopCall(t, d, "act", foreignElement)
	expectCode(t, err, "STALE_SNAPSHOT")
	if fixture.count != 0 {
		t.Fatal("invalid identity executed an action")
	}
	result, err := desktopCall(t, d, "act", clickParams(current))
	if err != nil || result.(map[string]any)["status"] != "completed" || fixture.count != 1 {
		t.Fatalf("valid click failed: %v %v", result, err)
	}
	_, err = desktopCall(t, d, "act", clickParams(current))
	expectCode(t, err, "STALE_SNAPSHOT")
	if fixture.count != 1 {
		t.Fatal("old snapshot repeated the click")
	}
}
func TestCompletedClickSurvivesObservationFailureAndCancellation(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		fixture := &desktopFixture{}
		d := NewDesktop(fixture)
		snapshot := observed(t, d)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		fixture.afterClick = func() {
			if cancelled {
				cancel()
			} else {
				fixture.observeError = Error("CAPTURE_FAILED", "capture failed")
			}
		}
		params, _ := json.Marshal(clickParams(snapshot))
		result, err := d.Call(ctx, "act", params)
		if err != nil {
			t.Fatal(err)
		}
		outcome := result.(map[string]any)
		if outcome["status"] != "completed" || outcome["observation"].(map[string]any)["status"] != "unavailable" || fixture.count != 1 {
			t.Fatalf("lost action receipt: %v", result)
		}
	}
}
func TestUncertainClickCannotBeReplayedInSession(t *testing.T) {
	fixture := &desktopFixture{}
	d := NewDesktop(fixture)
	snapshot := observed(t, d)
	fixture.clickError = &DomainError{"TARGET_UNAVAILABLE", "reply lost", "possible"}
	_, err := desktopCall(t, d, "act", clickParams(snapshot))
	var domain *DomainError
	if !errors.As(err, &domain) || domain.Effect != "possible" {
		t.Fatalf("lost uncertainty: %v", err)
	}
	_, err = desktopCall(t, d, "getAppState", map[string]any{"appSessionId": snapshot.AppSessionID})
	expectCode(t, err, "CLOSED")
}
func TestFailedObservationInvalidatesOldSnapshot(t *testing.T) {
	fixture := &desktopFixture{}
	d := NewDesktop(fixture)
	snapshot := observed(t, d)
	fixture.observeError = Error("TARGET_UNAVAILABLE", "window closed")
	_, err := desktopCall(t, d, "getAppState", map[string]any{"appSessionId": snapshot.AppSessionID})
	expectCode(t, err, "TARGET_UNAVAILABLE")
	_, err = desktopCall(t, d, "act", clickParams(snapshot))
	expectCode(t, err, "STALE_SNAPSHOT")
}
func TestMalformedObservationAndUnsupportedInputDoNotExecute(t *testing.T) {
	fixture := &desktopFixture{}
	d := NewDesktop(fixture)
	snapshot := observed(t, d)
	for _, limit := range []any{nil, map[string]any{}, []any{}, true, 0, 1.5, "bad"} {
		_, err := desktopCall(t, d, "getAppState", map[string]any{"appSessionId": snapshot.AppSessionID, "textLimit": limit})
		expectCode(t, err, "INVALID_ARGUMENT")
	}
	_, err := desktopCall(t, d, "act", map[string]any{"type": "click", "appSessionId": snapshot.AppSessionID, "snapshotId": snapshot.ID, "x": 0, "y": 0})
	expectCode(t, err, "UNSUPPORTED_CAPABILITY")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	params, _ := json.Marshal(clickParams(snapshot))
	_, err = d.Call(ctx, "act", params)
	expectCode(t, err, "CANCELLED")
	if fixture.count != 0 {
		t.Fatal("unsupported or cancelled input was executed")
	}
}
