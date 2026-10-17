package sdkruntime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type launchFixture struct {
	desktopFixture
	identity     AppIdentity
	targets      []Target
	launches     int
	reads        int
	readError    error
	beforeLaunch func()
}

func (f *launchFixture) ResolveAppIdentity(context.Context, string) (AppIdentity, error) {
	return f.identity, nil
}
func (f *launchFixture) Apps(context.Context) ([]Target, error) { return f.targets, nil }
func (f *launchFixture) LaunchApp(ctx context.Context, identity AppIdentity) error {
	if identity != f.identity {
		return Error("TARGET_UNAVAILABLE", "identity changed")
	}
	f.launches++
	f.targets = []Target{{Key: "new-process", Name: "Localized Counter", Identity: identity.ID}}
	if f.beforeLaunch != nil {
		f.beforeLaunch()
	}
	return nil
}
func (f *launchFixture) Observe(ctx context.Context, target Target, options ObserveOptions) (Observation, error) {
	f.reads++
	if f.readError != nil {
		return Observation{}, f.readError
	}
	if options.Activation != "never" || options.IncludeScreenshot == nil || *options.IncludeScreenshot {
		return Observation{}, errors.New("readiness check must not activate or capture")
	}
	if f.reads == 1 {
		return Observation{}, Error("TARGET_UNAVAILABLE", "window not ready")
	}
	return f.desktopFixture.Observe(ctx, target, options)
}

func TestPostLaunchPermissionFailureRetainsCodeAndEffect(t *testing.T) {
	f := newLaunchFixture()
	f.readError = Error("PERMISSION_REQUIRED", "Accessibility was revoked")
	_, err := desktopCall(t, NewDesktop(f), "resolveApp", map[string]any{"query": "Counter"})
	expectCode(t, err, "PERMISSION_REQUIRED")
	if err.(*DomainError).Effect != "possible" || f.launches != 1 {
		t.Fatalf("lost launch effect: %v", err)
	}
}
func newLaunchFixture() *launchFixture {
	return &launchFixture{identity: AppIdentity{ID: "installed.counter", Name: "Counter", Query: "/installed/counter"}}
}

func TestInstalledLookupDoesNotLaunchAndSessionsRetainIdentity(t *testing.T) {
	f := newLaunchFixture()
	d := NewDesktop(f)
	value, err := desktopCall(t, d, "resolveAppIdentity", map[string]any{"query": "Counter"})
	if err != nil || value.(AppIdentity) != f.identity || f.launches != 0 {
		t.Fatalf("lookup had an effect: %v %v", value, err)
	}
	value, err = desktopCall(t, d, "resolveApp", map[string]any{"query": f.identity.Query, "expectedIdentity": f.identity.ID})
	if err != nil {
		t.Fatal(err)
	}
	app := value.(App)
	if app.Identity != f.identity.ID || f.launches != 1 || f.reads < 2 {
		t.Fatalf("launch did not wait for the authorized target: %+v", f)
	}
	value, err = desktopCall(t, d, "openAppSession", map[string]any{"appId": app.ID})
	if err != nil || value.(AppSession).App.Identity != f.identity.ID {
		t.Fatalf("session lost installed identity: %v %v", value, err)
	}
}

func TestAppIdentityMismatchNeverLaunches(t *testing.T) {
	for _, running := range []bool{false, true} {
		f := newLaunchFixture()
		if running {
			f.targets = []Target{{Key: "old", Name: "Counter", Identity: f.identity.ID}}
		}
		_, err := desktopCall(t, NewDesktop(f), "resolveApp", map[string]any{"query": "Counter", "expectedIdentity": "other.application"})
		expectCode(t, err, "TARGET_UNAVAILABLE")
		if err.(*DomainError).Effect != "none" || f.launches != 0 {
			t.Fatal("mismatch reached native launch")
		}
	}
}

func TestRunningOnlyAliasesAndAmbiguityNeverLaunch(t *testing.T) {
	f := newLaunchFixture()
	d := NewDesktop(f)
	_, err := desktopCall(t, d, "resolveApp", map[string]any{"query": f.identity.Query, "launch": false})
	expectCode(t, err, "TARGET_UNAVAILABLE")
	f.targets = []Target{{Key: "old", Name: "Localized Counter", Identity: f.identity.ID}}
	value, err := desktopCall(t, d, "resolveApp", map[string]any{"query": f.identity.Query, "launch": false})
	if err != nil || value.(App).Identity != f.identity.ID {
		t.Fatalf("running alias failed: %v", err)
	}
	f.targets = append(f.targets, Target{Key: "second", Name: "Counter 2", Identity: f.identity.ID})
	_, err = desktopCall(t, d, "resolveApp", map[string]any{"query": f.identity.Query})
	expectCode(t, err, "TARGET_UNAVAILABLE")
	if f.launches != 0 {
		t.Fatal("running-only or ambiguous request launched")
	}
}

func TestLaunchCancellationPreservesPossibleEffect(t *testing.T) {
	f := newLaunchFixture()
	ctx, cancel := context.WithCancel(context.Background())
	f.beforeLaunch = cancel
	_, err := NewDesktop(f).Call(ctx, "resolveApp", json.RawMessage(`{"query":"Counter"}`))
	expectCode(t, err, "CANCELLED")
	if err.(*DomainError).Effect != "possible" || f.launches != 1 {
		t.Fatalf("lost launch effect: %v", err)
	}
	f = newLaunchFixture()
	_, err = NewDesktop(f).Call(ctx, "resolveApp", json.RawMessage(`{"query":"Counter"}`))
	expectCode(t, err, "CANCELLED")
	if f.launches != 0 || err.(*DomainError).Effect != "none" {
		t.Fatal("cancelled request launched")
	}
}

func TestInvalidAppResolutionHasNoEffect(t *testing.T) {
	for _, input := range []string{`{"query":" "}`, `{"query":"Counter","expectedIdentity":""}`, `{"query":"Counter","launch":"yes"}`, `{"query":"Counter","extra":true}`} {
		f := newLaunchFixture()
		_, err := NewDesktop(f).Call(context.Background(), "resolveApp", json.RawMessage(input))
		expectCode(t, err, "INVALID_ARGUMENT")
		if f.launches != 0 {
			t.Fatal("invalid request launched")
		}
	}
}
