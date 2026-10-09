package sdkruntime

import (
	"context"
	"encoding/json"
	"slices"
)

type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type Action struct {
	Type             string   `json:"type"`
	AppSessionID     string   `json:"appSessionId"`
	SnapshotID       string   `json:"snapshotId"`
	ElementID        string   `json:"elementId,omitempty"`
	AllowGlobalInput bool     `json:"allowGlobalInput"`
	Button           string   `json:"button,omitempty"`
	Count            int      `json:"count,omitempty"`
	X                *float64 `json:"x,omitempty"`
	Y                *float64 `json:"y,omitempty"`
	ActionID         string   `json:"actionId,omitempty"`
	Direction        string   `json:"direction,omitempty"`
	Pages            float64  `json:"pages,omitempty"`
	From             *Point   `json:"from,omitempty"`
	To               *Point   `json:"to,omitempty"`
	Text             string   `json:"text,omitempty"`
	Key              string   `json:"key,omitempty"`
	Value            string   `json:"value"`
}

// ActionDriver adapts an engine with the complete action surface to session ownership.
type ActionDriver interface {
	Act(context.Context, Target, Observation, *Element, Action) error
}

func decodeAction(params json.RawMessage) (Action, error) {
	a := Action{Button: "left", Count: 1}
	var fields map[string]json.RawMessage
	if decodeObject(params, &a) != nil || json.Unmarshal(params, &fields) != nil || a.AppSessionID == "" || a.SnapshotID == "" {
		return a, Error("INVALID_ARGUMENT", "Invalid action parameters")
	}
	allowed := []string{"type", "appSessionId", "snapshotId", "allowGlobalInput"}
	required := []string{}
	valid := true
	switch a.Type {
	case "click":
		allowed = append(allowed, "button", "count", "elementId", "x", "y")
		valid = slices.Contains([]string{"left", "middle", "right"}, a.Button) && a.Count >= 1 && a.Count <= 3
		if _, element := fields["elementId"]; element {
			valid = valid && a.ElementID != "" && a.X == nil && a.Y == nil
		} else {
			valid = valid && a.X != nil && a.Y != nil && *a.X >= 0 && *a.Y >= 0
		}
	case "performSecondaryAction":
		required = []string{"elementId", "actionId"}
		valid = a.ElementID != "" && a.ActionID != ""
	case "scroll":
		required = []string{"direction", "pages"}
		allowed = append(allowed, "elementId")
		valid = slices.Contains([]string{"up", "down", "left", "right"}, a.Direction) && a.Pages > 0
		if _, element := fields["elementId"]; element {
			valid = valid && a.ElementID != ""
		}
	case "drag":
		required = []string{"from", "to"}
		valid = a.From != nil && a.To != nil && a.From.X >= 0 && a.From.Y >= 0 && a.To.X >= 0 && a.To.Y >= 0
		for _, name := range required {
			var point map[string]json.RawMessage
			if decodeObject(fields[name], new(Point)) != nil || json.Unmarshal(fields[name], &point) != nil || point["x"] == nil || point["y"] == nil {
				valid = false
			}
		}
	case "typeText":
		required = []string{"text"}
		valid = a.Text != ""
	case "pressKey":
		required = []string{"key"}
		valid = a.Key != ""
	case "setValue":
		required = []string{"elementId", "value"}
		valid = a.ElementID != ""
	default:
		return a, Error("INVALID_ARGUMENT", "Unknown action type")
	}
	allowed = append(allowed, required...)
	for _, name := range required {
		if fields[name] == nil {
			valid = false
		}
	}
	for name := range fields {
		if !slices.Contains(allowed, name) {
			valid = false
		}
	}
	if !valid {
		return a, Error("INVALID_ARGUMENT", "Invalid action parameters")
	}
	return a, nil
}
