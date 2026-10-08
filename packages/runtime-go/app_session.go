package sdkruntime

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"
)

type AppSession struct {
	ID     string `json:"id"`
	App    App    `json:"app"`
	Status string `json:"status"`
}

type appSession struct {
	AppSession
	target    Target
	snapshots map[string]desktopSnapshot
	cancel    context.CancelFunc
	done      chan struct{}
}

func (d *Desktop) openAppSession(appID string) (AppSession, error) {
	target, exists := d.apps[appID]
	if !exists {
		return AppSession{}, Error("TARGET_UNAVAILABLE", "Application ID is not in this runtime")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, session := range d.sessions {
		if session.App.ID == appID && session.Status != "stopped" {
			if session.Status == "stopping" {
				return AppSession{}, Error("APP_SESSION_STOPPED", "Application control is stopping")
			}
			return session.AppSession, nil
		}
	}
	session := &appSession{AppSession: AppSession{NewID(), App{appID, target.Name}, "active"}, target: target, snapshots: map[string]desktopSnapshot{}}
	d.sessions[session.ID] = session
	return session.AppSession, nil
}

func (d *Desktop) listAppSessions() []AppSession {
	d.mu.Lock()
	defer d.mu.Unlock()
	result := make([]AppSession, 0, len(d.sessions))
	for _, session := range d.sessions {
		result = append(result, session.AppSession)
	}
	slices.SortFunc(result, func(a, b AppSession) int { return strings.Compare(a.ID, b.ID) })
	return result
}

func (d *Desktop) beginAppRequest(ctx context.Context, id string) (*appSession, context.Context, func(), error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	session, exists := d.sessions[id]
	if !exists {
		return nil, nil, nil, Error("APP_SESSION_NOT_FOUND", "Application session belongs to another runtime or does not exist")
	}
	if session.Status != "active" {
		return nil, nil, nil, Error("APP_SESSION_STOPPED", "Application control has stopped")
	}
	ctx, cancel := context.WithCancel(ctx)
	session.cancel, session.done = cancel, make(chan struct{})
	return session, ctx, func() {
		d.mu.Lock()
		defer d.mu.Unlock()
		cancel()
		close(session.done)
		session.cancel = nil
	}, nil
}

// The reader marks stopping before dispatching the bounded cleanup wait.
func (d *Desktop) prepareAppStop(params json.RawMessage) (func() (any, error), error) {
	var input struct {
		AppSessionID string `json:"appSessionId"`
	}
	if decodeObject(params, &input) != nil || input.AppSessionID == "" {
		return nil, Error("INVALID_ARGUMENT", "Expected an application session ID")
	}
	d.mu.Lock()
	session, exists := d.sessions[input.AppSessionID]
	if !exists {
		d.mu.Unlock()
		return nil, Error("APP_SESSION_NOT_FOUND", "Application session belongs to another runtime or does not exist")
	}
	if session.Status == "active" {
		session.Status = "stopping"
	}
	if session.cancel != nil {
		session.cancel()
	}
	done := session.done
	d.mu.Unlock()
	return func() (any, error) {
		if done != nil {
			select {
			case <-done:
			case <-time.After(cleanupTimeout):
				d.uncertain.Store(true)
				return nil, &DomainError{"CLEANUP_FAILED", "Application request did not settle; cleanup is unconfirmed", "possible"}
			}
		}
		d.mu.Lock()
		defer d.mu.Unlock()
		clear(session.snapshots)
		session.Status = "stopped"
		return map[string]string{"appSessionId": session.ID, "status": "stopped", "cleanup": "complete"}, nil
	}, nil
}
