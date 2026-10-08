package sdkruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func stopApp(t *testing.T, d *Desktop, id string) func() (any, error) {
	t.Helper()
	params, _ := json.Marshal(map[string]string{"appSessionId": id})
	finish, err := d.prepareAppStop(params)
	if err != nil {
		t.Fatal(err)
	}
	return finish
}

func TestAppStopInvalidatesOnlyItsContextAndCannotReuseSnapshots(t *testing.T) {
	f := &desktopFixture{}
	d := NewDesktop(f)
	first := observed(t, d)
	apps, _ := desktopCall(t, d, "listApps", map[string]any{})
	opened, err := desktopCall(t, d, "openAppSession", map[string]any{"appId": apps.([]App)[1].ID})
	if err != nil {
		t.Fatal(err)
	}
	second, err := desktopCall(t, d, "getAppState", map[string]any{"appSessionId": opened.(AppSession).ID})
	if err != nil {
		t.Fatal(err)
	}
	finish := stopApp(t, d, first.AppSessionID)
	_, err = desktopCall(t, d, "act", clickParams(first))
	expectCode(t, err, "APP_SESSION_STOPPED")
	if _, err := finish(); err != nil {
		t.Fatal(err)
	}
	if _, err := stopApp(t, d, first.AppSessionID)(); err != nil {
		t.Fatal(err)
	}
	_, err = desktopCall(t, d, "getAppState", map[string]any{"appSessionId": first.AppSessionID})
	expectCode(t, err, "APP_SESSION_STOPPED")
	if _, err = desktopCall(t, d, "act", clickParams(second.(Snapshot))); err != nil {
		t.Fatal(err)
	}
	if f.count != 1 {
		t.Fatal("stopping one app affected the other app's action")
	}
	reopened, err := desktopCall(t, d, "openAppSession", map[string]any{"appId": first.App.ID})
	if err != nil {
		t.Fatal(err)
	}
	if reopened.(AppSession).ID == first.AppSessionID {
		t.Fatal("reopened a stopped identity")
	}
	stale := clickParams(first)
	stale["appSessionId"] = reopened.(AppSession).ID
	_, err = desktopCall(t, d, "act", stale)
	expectCode(t, err, "STALE_SNAPSHOT")
	if f.count != 1 {
		t.Fatal("old snapshot executed in a new application session")
	}
}

type gatedDesktop struct {
	desktopFixture
	started, cancelled, release chan struct{}
}

func (f *gatedDesktop) Click(ctx context.Context, _ Target, _ Observation, _ Element) error {
	close(f.started)
	<-ctx.Done()
	close(f.cancelled)
	<-f.release
	return Error("CANCELLED", "Input settled without execution")
}

func TestAppStopBypassesBlockedExecutionAndRejectsQueuedActions(t *testing.T) {
	f := &gatedDesktop{started: make(chan struct{}), cancelled: make(chan struct{}), release: make(chan struct{})}
	d := NewDesktop(f)
	snapshot := observed(t, d)
	h := start(t, d)
	h.initialize(t)
	h.send(t, "running", "act", clickParams(snapshot))
	select {
	case <-f.started:
	case <-time.After(time.Second):
		t.Fatal("action did not start")
	}
	for i := 0; i < queueLimit-1; i++ {
		h.send(t, fmt.Sprintf("queued-%d", i), "act", clickParams(snapshot))
	}
	h.send(t, "stop", "stopAppSession", map[string]string{"appSessionId": snapshot.AppSessionID})
	select {
	case <-f.cancelled:
	case <-time.After(time.Second):
		t.Fatal("stop waited behind the running action")
	}
	h.send(t, "state", "listAppSessions", map[string]any{})
	state := h.receive(t)
	if state.ID != "state" || state.Result.([]any)[0].(map[string]any)["status"] != "stopping" {
		t.Fatalf("stop acknowledged before active work released resources: %+v", state)
	}
	close(f.release)
	seen := map[string]bool{}
	for range queueLimit + 1 {
		resp := h.receive(t)
		seen[resp.ID] = true
		switch {
		case resp.ID == "running":
			if resp.Error == nil || resp.Error.Data["code"] != "CANCELLED" {
				t.Fatalf("running action did not settle: %+v", resp)
			}
		case strings.HasPrefix(resp.ID, "queued-"):
			if resp.Error == nil || resp.Error.Data["code"] != "APP_SESSION_STOPPED" {
				t.Fatalf("queued action was accepted: %+v", resp)
			}
		case resp.ID == "stop":
			if resp.Error != nil || resp.Result.(map[string]any)["cleanup"] != "complete" {
				t.Fatalf("cleanup not acknowledged: %+v", resp)
			}
		default:
			t.Fatalf("unexpected response: %+v", resp)
		}
	}
	if len(seen) != queueLimit+1 || f.count != 0 {
		t.Fatal("lost response or executed cancelled input")
	}
	h.send(t, "close", "shutdown", map[string]string{"sessionId": "test-session"})
	if h.receive(t).Error != nil {
		t.Fatal("runtime no longer closable")
	}
	finish(t, h, false)
}

func TestAppStopFailureNeverReportsCleanupAndDisablesFurtherInput(t *testing.T) {
	d := NewDesktop(&desktopFixture{})
	snapshot := observed(t, d)
	_, _, release, err := d.beginAppRequest(context.Background(), snapshot.AppSessionID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = stopApp(t, d, snapshot.AppSessionID)()
	expectCode(t, err, "CLEANUP_FAILED")
	if d.listAppSessions()[0].Status != "stopping" {
		t.Fatal("unconfirmed cleanup reported stopped")
	}
	release()
	_, err = desktopCall(t, d, "act", clickParams(snapshot))
	expectCode(t, err, "CLOSED")
}
