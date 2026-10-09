package sdkruntime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type editableDesktop struct {
	desktopFixture
	value       string
	actionError error
}

func (f *editableDesktop) Observe(ctx context.Context, target Target, options ObserveOptions) (Observation, error) {
	observation, err := f.desktopFixture.Observe(ctx, target, options)
	if err != nil {
		return observation, err
	}
	observation.Tree.Elements[0].Value = f.value
	observation.Tree.Elements[0].Actions = []string{"setValue", "performSecondaryAction", "click"}
	observation.Tree.Elements[0].SecondaryActions = []SecondaryAction{{"clear", "Clear"}}
	return observation, nil
}
func (f *editableDesktop) Act(_ context.Context, _ Target, _ Observation, element *Element, action Action) error {
	if f.actionError != nil {
		return f.actionError
	}
	if element == nil || element.Native != "real-button" {
		return errors.New("lost native element")
	}
	switch action.Type {
	case "setValue":
		f.value = action.Value
	case "performSecondaryAction":
		f.value = ""
	default:
		return errors.New("unexpected action")
	}
	return nil
}
func TestEngineActionsPreserveOwnershipAndInvalidateSnapshots(t *testing.T) {
	fixture := &editableDesktop{}
	d := NewDesktop(fixture)
	snapshot := observed(t, d)
	params := map[string]any{"type": "setValue", "appSessionId": snapshot.AppSessionID, "snapshotId": snapshot.ID, "elementId": snapshot.Tree.Elements[0].ID, "value": "显示侧边栏 😀"}
	_, err := desktopCall(t, NewDesktop(&editableDesktop{}), "act", params)
	expectCode(t, err, "APP_SESSION_NOT_FOUND")
	result, err := desktopCall(t, d, "act", params)
	if err != nil {
		t.Fatal(err)
	}
	updated := result.(map[string]any)["observation"].(map[string]any)["snapshot"].(Snapshot)
	if updated.Tree.Elements[0].Value != "显示侧边栏 😀" {
		t.Fatal("text did not reach the edited element")
	}
	_, err = desktopCall(t, d, "act", params)
	expectCode(t, err, "STALE_SNAPSHOT")
	params = map[string]any{"type": "performSecondaryAction", "appSessionId": snapshot.AppSessionID, "snapshotId": updated.ID, "elementId": updated.Tree.Elements[0].ID, "actionId": "foreign"}
	_, err = desktopCall(t, d, "act", params)
	expectCode(t, err, "UNSUPPORTED_CAPABILITY")
	if fixture.value == "" {
		t.Fatal("unknown secondary action changed the element")
	}
	params["actionId"] = "clear"
	_, err = desktopCall(t, d, "act", params)
	if err != nil || fixture.value != "" {
		t.Fatalf("exposed secondary action failed: %v", err)
	}
}
func TestRejectedPayloadDoesNotCloseDesktop(t *testing.T) {
	fixture := &editableDesktop{actionError: Error("INVALID_ARGUMENT", "Malformed command payload")}
	d := NewDesktop(fixture)
	snapshot := observed(t, d)
	_, err := desktopCall(t, d, "act", map[string]any{"type": "setValue", "appSessionId": snapshot.AppSessionID, "snapshotId": snapshot.ID, "elementId": snapshot.Tree.Elements[0].ID, "value": ""})
	expectCode(t, err, "INVALID_ARGUMENT")
	if _, err = desktopCall(t, d, "listApps", map[string]any{}); err != nil {
		t.Fatalf("pre-dispatch rejection closed the desktop: %v", err)
	}
	fixture.actionError = nil
	snapshot = observed(t, d)
	if _, err = desktopCall(t, d, "act", map[string]any{"type": "setValue", "appSessionId": snapshot.AppSessionID, "snapshotId": snapshot.ID, "elementId": snapshot.Tree.Elements[0].ID, "value": "recovered"}); err != nil || fixture.value != "recovered" {
		t.Fatalf("could not recover: %v", err)
	}
}
func TestActionValidationRejectsMalformedInput(t *testing.T) {
	for _, fields := range []string{
		`"type":"drag","from":{"x":null,"y":1},"to":{"x":1,"y":1}`,
		`"type":"drag","from":{"y":1},"to":{"x":1,"y":1}`,
		`"type":"drag","from":{"x":1,"y":1,"other":1},"to":{"x":1,"y":1}`,
		`"type":"click","elementId":"e","x":1,"y":1`,
		`"type":"click","x":-1,"y":1`,
		`"type":"scroll","pages":0,"direction":"down"`,
		`"type":"setValue","elementId":"e"`,
		`"type":"pressKey","key":"","text":"extra"`,
		`"type":"typeText","text":"hello","count":2`,
		`"type":"performSecondaryAction","elementId":"e","actionId":null`,
	} {
		_, err := decodeAction(json.RawMessage(`{"appSessionId":"a","snapshotId":"s",` + fields + `}`))
		expectCode(t, err, "INVALID_ARGUMENT")
	}
}
func TestDiscoveryIDsSurviveRepeatedEnumeration(t *testing.T) {
	d := NewDesktop(&desktopFixture{})
	snapshot := observed(t, d)
	apps, err := desktopCall(t, d, "listApps", map[string]any{})
	if err != nil || apps.([]App)[0].ID != snapshot.App.ID {
		t.Fatalf("application identity changed across discovery: %v", err)
	}
	if _, err := desktopCall(t, d, "act", clickParams(snapshot)); err != nil {
		t.Fatalf("discovery invalidated a live snapshot: %v", err)
	}
}
