package sdkruntime

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
)

type DomainError struct {
	Code, Message, Effect string
}

func (e *DomainError) Error() string { return e.Message }

func Error(code, message string) *DomainError {
	return &DomainError{Code: code, Message: message, Effect: "none"}
}

type Reason struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Availability struct {
	Status string  `json:"status"`
	Reason *Reason `json:"reason,omitempty"`
}

func Unavailable(code, message string) Availability {
	return Availability{Status: "unavailable", Reason: &Reason{code, message}}
}

type App struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Target struct {
	Key, Name string
	Native    any
}

type Window struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type Rect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type SecondaryAction struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type Element struct {
	ID               string            `json:"id"`
	ParentID         string            `json:"parentId,omitempty"`
	Role             string            `json:"role"`
	Name             string            `json:"name"`
	Value            string            `json:"value,omitempty"`
	Actions          []string          `json:"actions"`
	Bounds           *Rect             `json:"bounds,omitempty"`
	SecondaryActions []SecondaryAction `json:"secondaryActions"`
	Native           any               `json:"-"`
}

type Tree struct {
	Status    string    `json:"status"`
	Elements  []Element `json:"elements"`
	Truncated []string  `json:"truncated"`
}

type Image struct {
	MimeType   string `json:"mimeType"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	DataBase64 string `json:"dataBase64"`
}

type Capture struct {
	Status string  `json:"status"`
	Image  *Image  `json:"image,omitempty"`
	Reason *Reason `json:"reason,omitempty"`
}

type Observation struct {
	Window     Window  `json:"window"`
	Tree       Tree    `json:"tree"`
	Screenshot Capture `json:"screenshot"`
	Native     any     `json:"-"`
}

type Snapshot struct {
	ID           string `json:"id"`
	AppSessionID string `json:"appSessionId"`
	App          App    `json:"app"`
	Observation
}

type ObserveOptions struct {
	IncludeScreenshot *bool  `json:"includeScreenshot,omitempty"`
	AppSessionID      string `json:"appSessionId"`
	Activation        string `json:"activation"`
	MaxTreeNodes      int    `json:"maxTreeNodes"`
	MaxTreeDepth      int    `json:"maxTreeDepth"`
	TextLimit         any    `json:"textLimit"`
}

func (o ObserveOptions) LimitText(value string, tree *Tree) string {
	limit := 500
	if text, ok := o.TextLimit.(string); ok && text == "max" {
		return value
	}
	if number, ok := o.TextLimit.(float64); ok {
		limit = int(number)
	}
	characters := []rune(value)
	if len(characters) <= limit {
		return value
	}
	if !slices.Contains(tree.Truncated, "text") {
		tree.Truncated = append(tree.Truncated, "text")
	}
	return string(characters[:limit])
}

// PermissionDriver lets a desktop backend report and request OS permissions.
// RequestPermissions receives validated, unique IDs and returns the observed
// state; a declined request is a status, not an error.
type PermissionDriver interface {
	Permissions(context.Context) ([]Permission, error)
	RequestPermissions(context.Context, []string) ([]Permission, error)
}

// Drivers retain platform references inside Native fields, never in the wire identity.
type DesktopDriver interface {
	Capabilities(context.Context) map[string]Availability
	Apps(context.Context) ([]Target, error)
	Observe(context.Context, Target, ObserveOptions) (Observation, error)
	Click(context.Context, Target, Observation, Element) error
	Close(context.Context) error
}

type desktopSnapshot struct {
	Snapshot
	options ObserveOptions
}

type Desktop struct {
	LifecycleBackend
	driver    DesktopDriver
	apps      map[string]Target
	mu        sync.Mutex
	sessions  map[string]*appSession
	uncertain atomic.Bool
}

func NewDesktop(driver DesktopDriver) *Desktop {
	return &Desktop{driver: driver, apps: map[string]Target{}, sessions: map[string]*appSession{}}
}

func NewID() string {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(id[:])
}

// AppID derives the opaque app ID from the driver's process key, so it survives runtime restarts.
func AppID(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:16])
}

// Permissions reports the driver's permissions; drivers without any report none.
func (d *Desktop) Permissions(ctx context.Context) ([]Permission, error) {
	if driver, ok := d.driver.(PermissionDriver); ok {
		return driver.Permissions(ctx)
	}
	return []Permission{}, nil
}

func (d *Desktop) Capabilities(ctx context.Context) map[string]Availability {
	return d.driver.Capabilities(ctx)
}

func (d *Desktop) Close(ctx context.Context) error {
	clear(d.apps)
	d.mu.Lock()
	clear(d.sessions)
	d.mu.Unlock()
	return d.driver.Close(ctx)
}

func (d *Desktop) Call(ctx context.Context, method string, params json.RawMessage) (any, error) {
	if d.uncertain.Load() {
		return nil, Error("CLOSED", "An earlier action has an uncertain outcome; start a new session")
	}
	if err := ctx.Err(); err != nil {
		return nil, Error("CANCELLED", "Request cancelled before execution")
	}
	switch method {
	case "listApps":
		if decodeObject(params, &struct{}{}) != nil {
			return nil, Error("INVALID_ARGUMENT", "Expected empty parameters")
		}
		targets, err := d.driver.Apps(ctx)
		if err != nil {
			return nil, err
		}
		apps := make([]App, 0, len(targets))
		present := map[string]bool{}
		for _, target := range targets {
			id := AppID(target.Key)
			d.apps[id] = target
			present[id] = true
			apps = append(apps, App{id, target.Name})
		}
		for id := range d.apps {
			if !present[id] {
				delete(d.apps, id)
				d.mu.Lock()
				for _, session := range d.sessions {
					if session.App.ID == id {
						clear(session.snapshots)
						session.keyboardTarget = nil
						session.Status = "stopped"
					}
				}
				d.mu.Unlock()
			}
		}
		return apps, nil
	case "requestPermissions":
		driver, ok := d.driver.(PermissionDriver)
		if !ok {
			return nil, Error("UNSUPPORTED_CAPABILITY", "This backend has no permission requests")
		}
		var input struct {
			IDs []string `json:"ids"`
		}
		if decodeObject(params, &input) != nil || len(input.IDs) == 0 {
			return nil, Error("INVALID_ARGUMENT", "Expected permission IDs")
		}
		for i, id := range input.IDs {
			if id == "" || slices.Contains(input.IDs[:i], id) {
				return nil, Error("INVALID_ARGUMENT", "Permission IDs must be unique and non-empty")
			}
		}
		permissions, err := driver.RequestPermissions(ctx, input.IDs)
		if err != nil {
			return nil, err
		}
		return map[string]any{"permissions": permissions}, nil
	case "resolveAppIdentity":
		return nil, Error("UNSUPPORTED_CAPABILITY", "Installed application identity resolution is unavailable on this platform")
	case "resolveApp":
		var input struct {
			Query  string `json:"query"`
			Launch *bool  `json:"launch"`
		}
		if decodeObject(params, &input) != nil || input.Query == "" || len(input.Query) > 512 {
			return nil, Error("INVALID_ARGUMENT", "Expected an application name or ID")
		}
		value, err := d.Call(ctx, "listApps", json.RawMessage(`{}`))
		if err != nil {
			return nil, err
		}
		matches := []App{}
		for _, app := range value.([]App) {
			if app.ID == input.Query || strings.EqualFold(app.Name, input.Query) {
				matches = append(matches, app)
			}
		}
		if len(matches) == 1 {
			return matches[0], nil
		}
		if len(matches) > 1 {
			return nil, Error("TARGET_UNAVAILABLE", "Application name is ambiguous; use an ID from listApps")
		}
		if input.Launch != nil && !*input.Launch {
			return nil, Error("TARGET_UNAVAILABLE", "Application is no longer running; bind it explicitly to launch again")
		}
		return nil, Error("UNSUPPORTED_CAPABILITY", "Application is not running; launching applications is unavailable on this platform")
	case "openAppSession":
		var input struct {
			AppID string `json:"appId"`
		}
		if decodeObject(params, &input) != nil || input.AppID == "" {
			return nil, Error("INVALID_ARGUMENT", "Expected an application ID")
		}
		return d.openAppSession(input.AppID)
	case "getAppState":
		options := ObserveOptions{Activation: "never", MaxTreeNodes: 1200, MaxTreeDepth: 64}
		if decodeObject(params, &options) != nil || options.AppSessionID == "" ||
			(options.Activation != "never" && options.Activation != "allow") ||
			options.MaxTreeNodes < 1 || options.MaxTreeDepth < 1 || !validTextLimit(options.TextLimit) {
			return nil, Error("INVALID_ARGUMENT", "Invalid observation parameters")
		}
		options.MaxTreeNodes = min(options.MaxTreeNodes, 10000)
		options.MaxTreeDepth = min(options.MaxTreeDepth, 128)
		session, ctx, finish, err := d.beginAppRequest(ctx, options.AppSessionID)
		if err != nil {
			return nil, err
		}
		defer finish()
		return d.observe(ctx, session, options)
	case "act", "perform":
		action, err := decodeAction(params)
		if err != nil {
			return nil, err
		}
		engine, fullActions := d.driver.(ActionDriver)
		if !fullActions && (action.Type != "click" || action.ElementID == "" || action.Button != "left" || action.Count != 1) {
			return nil, Error("UNSUPPORTED_CAPABILITY", "This backend supports one semantic left click on an element")
		}
		session, ctx, finish, err := d.beginAppRequest(ctx, action.AppSessionID)
		if err != nil {
			return nil, err
		}
		defer finish()
		snapshot, exists := session.snapshots[action.SnapshotID]
		if !exists && method == "perform" && (action.Type == "typeText" || action.Type == "pressKey") && session.keyboardTarget != nil && session.keyboardTarget.ID == action.SnapshotID {
			snapshot, exists = *session.keyboardTarget, true
		}
		if !exists {
			return nil, Error("STALE_SNAPSHOT", "Snapshot is expired or belongs to another session")
		}
		if snapshot.options.IncludeScreenshot != nil && !*snapshot.options.IncludeScreenshot && (action.X != nil || action.Type == "drag") {
			return nil, Error("STALE_SNAPSHOT", "Observe with a screenshot before coordinate input")
		}
		var element *Element
		for i := range snapshot.Tree.Elements {
			if snapshot.Tree.Elements[i].ID == action.ElementID {
				element = &snapshot.Tree.Elements[i]
				break
			}
		}
		if action.ElementID != "" && element == nil {
			return nil, Error("STALE_SNAPSHOT", "Element does not belong to this snapshot")
		}
		if element != nil && !slices.Contains(element.Actions, action.Type) {
			return nil, Error("UNSUPPORTED_CAPABILITY", "Element does not support the requested action")
		}
		if action.Type == "performSecondaryAction" && !slices.ContainsFunc(element.SecondaryActions, func(a SecondaryAction) bool { return a.ID == action.ActionID }) {
			return nil, Error("UNSUPPORTED_CAPABILITY", "Element does not expose this secondary action")
		}
		clear(session.snapshots)
		session.keyboardTarget = nil
		if fullActions {
			err = engine.Act(ctx, session.target, snapshot.Observation, element, action)
		} else {
			err = d.driver.Click(ctx, session.target, snapshot.Observation, *element)
		}
		if err != nil {
			var domain *DomainError
			if !errors.As(err, &domain) {
				err = &DomainError{"TARGET_UNAVAILABLE", err.Error(), "possible"}
				domain = err.(*DomainError)
			}
			if domain.Effect != "none" {
				d.uncertain.Store(true)
			}
			return nil, err
		}
		if method == "perform" {
			session.keyboardTarget = &snapshot
			return map[string]string{"status": "completed", "appSessionId": session.ID}, nil
		}
		observation := map[string]any{}
		updated, err := d.observe(ctx, session, snapshot.options)
		if err != nil {
			observation["status"], observation["reason"] = "unavailable", errorReason(err)
		} else {
			observation["status"], observation["snapshot"] = "available", updated
		}
		return map[string]any{"status": "completed", "observation": observation}, nil
	default:
		return nil, Error("UNSUPPORTED_CAPABILITY", "Method is not connected to this desktop backend")
	}
}

func validTextLimit(value any) bool {
	if value == nil {
		return true
	}
	if text, ok := value.(string); ok {
		return text == "max"
	}
	number, ok := value.(float64)
	return ok && number >= 1 && number <= 1e9 && number == float64(int(number))
}

func errorReason(err error) Reason {
	var domain *DomainError
	if errors.As(err, &domain) {
		return Reason{domain.Code, domain.Message}
	}
	if errors.Is(err, context.Canceled) {
		return Reason{"CANCELLED", "Observation cancelled"}
	}
	return Reason{"TARGET_UNAVAILABLE", err.Error()}
}

func (d *Desktop) observe(ctx context.Context, session *appSession, options ObserveOptions) (Snapshot, error) {
	clear(session.snapshots)
	session.keyboardTarget = nil
	if ctx.Err() != nil {
		return Snapshot{}, Error("CANCELLED", "Observation cancelled")
	}
	observation, err := d.driver.Observe(ctx, session.target, options)
	if err != nil {
		return Snapshot{}, err
	}
	if ctx.Err() != nil {
		return Snapshot{}, Error("CANCELLED", "Observation cancelled")
	}
	id := NewID()
	for i := range observation.Tree.Elements {
		element := &observation.Tree.Elements[i]
		element.ID = fmt.Sprintf("%s:%s", id, element.ID)
		if element.ParentID != "" {
			element.ParentID = fmt.Sprintf("%s:%s", id, element.ParentID)
		}
		if element.Actions == nil {
			element.Actions = []string{}
		}
		if element.SecondaryActions == nil {
			element.SecondaryActions = []SecondaryAction{}
		}
	}
	if observation.Tree.Elements == nil {
		observation.Tree.Elements = []Element{}
	}
	if observation.Tree.Truncated == nil {
		observation.Tree.Truncated = []string{}
	}
	observation.Window.ID = NewID()
	snapshot := Snapshot{ID: id, AppSessionID: session.ID, App: session.App, Observation: observation}
	session.snapshots[id] = desktopSnapshot{snapshot, options}
	return snapshot, nil
}
