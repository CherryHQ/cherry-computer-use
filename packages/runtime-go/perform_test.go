package sdkruntime

import (
	"context"
	"errors"
	"testing"
)

type sequenceDesktop struct {
	desktopFixture
	value        string
	observations int
}

func (f *sequenceDesktop) Observe(ctx context.Context, target Target, options ObserveOptions) (Observation, error) {
	f.observations++
	state, err := f.desktopFixture.Observe(ctx, target, options)
	return state, err
}

func (f *sequenceDesktop) Act(ctx context.Context, target Target, state Observation, element *Element, action Action) error {
	if action.Type == "typeText" {
		f.value += action.Text
		return nil
	}
	if action.Type == "click" && element != nil {
		return f.Click(ctx, target, state, *element)
	}
	return errors.New("unsupported fixture action")
}

func TestPerformSequencesKeyboardWithoutCapturingOrReusingElementRefs(t *testing.T) {
	fixture := &sequenceDesktop{}
	d := NewDesktop(fixture)
	snapshot := observed(t, d)
	fixture.observeError = errors.New("capture unavailable")
	result, err := desktopCall(t, d, "perform", clickParams(snapshot))
	if err != nil {
		t.Fatal(err)
	}
	if result.(map[string]string)["appSessionId"] != snapshot.AppSessionID || fixture.count != 1 {
		t.Fatal("action did not complete in the requested app")
	}
	_, err = desktopCall(t, d, "perform", clickParams(snapshot))
	expectCode(t, err, "STALE_SNAPSHOT")
	params := map[string]any{"type": "typeText", "appSessionId": snapshot.AppSessionID, "snapshotId": snapshot.ID, "text": "128*37+456"}
	_, err = desktopCall(t, d, "perform", params)
	if err != nil || fixture.value != "128*37+456" || fixture.observations != 1 {
		t.Fatalf("keyboard batch failed or captured intermediate state: %v", err)
	}
	fixture.observeError = nil
	observed(t, d)
	_, err = desktopCall(t, d, "perform", params)
	expectCode(t, err, "STALE_SNAPSHOT")
	if fixture.value != "128*37+456" || fixture.count != 1 {
		t.Fatal("stale request replayed input")
	}
}

func TestResolveAppDoesNotLaunchAnUnknownTarget(t *testing.T) {
	d := NewDesktop(&desktopFixture{})
	app, err := desktopCall(t, d, "resolveApp", map[string]string{"query": "Counter"})
	if err != nil || app.(App).Name != "Counter" {
		t.Fatalf("known app could not be resolved: %v", err)
	}
	_, err = desktopCall(t, d, "resolveApp", map[string]string{"query": "Not Installed"})
	expectCode(t, err, "UNSUPPORTED_CAPABILITY")
	_, err = desktopCall(t, d, "resolveApp", map[string]any{"query": "Not Installed", "launch": false})
	expectCode(t, err, "TARGET_UNAVAILABLE")
}

func TestAXOnlySnapshotCannotAuthorizeCoordinateInput(t *testing.T) {
	fixture := &sequenceDesktop{}
	d := NewDesktop(fixture)
	previous := observed(t, d)
	value, err := desktopCall(t, d, "getAppState", map[string]any{"appSessionId": previous.AppSessionID, "includeScreenshot": false})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := value.(Snapshot)
	_, err = desktopCall(t, d, "perform", map[string]any{"type": "click", "appSessionId": snapshot.AppSessionID, "snapshotId": snapshot.ID, "x": 1, "y": 1})
	expectCode(t, err, "STALE_SNAPSHOT")
	_, err = desktopCall(t, d, "perform", clickParams(snapshot))
	if err != nil || fixture.count != 1 {
		t.Fatalf("AX-only snapshot must still authorize semantic input: %v", err)
	}
}
