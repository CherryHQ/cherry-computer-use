package webcdp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

// Session is one DevTools connection to a page target.
type Session struct {
	conn   *wsConn
	nextID atomic.Int64
}

// Dial opens a DevTools session for a page target.
func Dial(ctx context.Context, webSocketURL string) (*Session, error) {
	conn, err := dialWS(ctx, webSocketURL)
	if err != nil {
		return nil, err
	}
	return &Session{conn: conn}, nil
}

// Close releases the session.
func (s *Session) Close() error { return s.conn.Close() }

// Call sends one protocol method and waits for its response, skipping events and unsolicited frames.
func (s *Session) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if params == nil {
		params = map[string]any{}
	}
	id := s.nextID.Add(1)
	payload, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(10 * time.Second)
	if contextDeadline, ok := ctx.Deadline(); ok {
		deadline = contextDeadline
	}
	if err := s.conn.conn.SetDeadline(deadline); err != nil {
		return nil, err
	}
	if err := s.conn.writeFrame(opText, payload); err != nil {
		return nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		message, err := s.conn.readMessage()
		if err != nil {
			return nil, err
		}
		var response struct {
			ID     int64           `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(message, &response) != nil {
			continue
		}
		if response.ID != id {
			continue
		}
		if response.Error != nil {
			return nil, fmt.Errorf("webcdp: %s: %s", method, response.Error.Message)
		}
		return response.Result, nil
	}
}

// EvalJSON evaluates an expression in the page and decodes its value into out. A page exception is an
// error so callers never treat a failed evaluation as an empty result.
func (s *Session) EvalJSON(ctx context.Context, expression string, out any) error {
	raw, err := s.Call(ctx, "Runtime.evaluate", map[string]any{
		"expression":    expression,
		"returnByValue": true,
		"awaitPromise":  true,
	})
	if err != nil {
		return err
	}
	var payload struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		Exception *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return err
	}
	if payload.Exception != nil {
		return fmt.Errorf("webcdp: page exception: %s", payload.Exception.Text)
	}
	if out == nil || len(payload.Result.Value) == 0 || string(payload.Result.Value) == "null" {
		return nil
	}
	return json.Unmarshal(payload.Result.Value, out)
}

const focusEditableExpression = `(function () {
  function editable(element) {
    if (!element) return false;
    if (element.isContentEditable) return true;
    return /^(INPUT|TEXTAREA)$/.test(element.tagName) && !element.disabled && !element.readOnly;
  }
  var active = document.activeElement;
  if (editable(active)) {
    return { focused: true, value: active.value == null ? '' : String(active.value) };
  }
  var selector = 'input:not([type=hidden]):not([type=button]):not([type=submit]):not([type=checkbox]):not([type=radio]),textarea,[contenteditable=true]';
  var candidates = Array.prototype.slice.call(document.querySelectorAll(selector));
  for (var index = 0; index < candidates.length; index++) {
    var element = candidates[index];
    var rect = element.getBoundingClientRect();
    if (rect.width <= 0 || rect.height <= 0) continue;
    element.focus();
    return { focused: true, value: element.value == null ? '' : String(element.value) };
  }
  return { focused: false };
})()`

const activeValueExpression = `document.activeElement
  ? String(document.activeElement.value == null ? document.activeElement.textContent : document.activeElement.value)
  : ''`

// InsertText focuses an editable element in the page and inserts text, then verifies that the element
// grew. ErrUnavailable means the application has no DevTools endpoint; any other error means the page
// refused or ignored the input, so the caller should fall back.
func (in Input) InsertText(ctx context.Context, text string) error {
	if text == "" {
		return nil
	}
	session, err := in.open(ctx)
	if err != nil {
		return err
	}
	defer session.Close()
	var before struct {
		Focused bool   `json:"focused"`
		Value   string `json:"value"`
	}
	if err := session.EvalJSON(ctx, focusEditableExpression, &before); err != nil {
		return err
	}
	if !before.Focused {
		return errors.New("webcdp: page exposes no editable element")
	}
	if _, err := session.Call(ctx, "Input.insertText", map[string]any{"text": text}); err != nil {
		return err
	}
	var after string
	if err := session.EvalJSON(ctx, activeValueExpression, &after); err != nil {
		return err
	}
	if len([]rune(after)) <= len([]rune(before.Value)) {
		return errors.New("webcdp: page did not accept the text")
	}
	return nil
}

// PressKey sends one key, with optional modifiers, to the focused page element.
func (in Input) PressKey(ctx context.Context, key string) error {
	descriptor, ok := keyDescriptor(key)
	if !ok {
		return fmt.Errorf("webcdp: unsupported key %q", key)
	}
	session, err := in.open(ctx)
	if err != nil {
		return err
	}
	defer session.Close()
	down := map[string]any{
		"type":                  "keyDown",
		"key":                   descriptor.Key,
		"code":                  descriptor.Code,
		"windowsVirtualKeyCode": descriptor.VirtualKey,
		"nativeVirtualKeyCode":  descriptor.VirtualKey,
		"modifiers":             descriptor.Modifiers,
	}
	if descriptor.Text != "" {
		down["text"] = descriptor.Text
		down["unmodifiedText"] = descriptor.Text
	}
	if _, err := session.Call(ctx, "Input.dispatchKeyEvent", down); err != nil {
		return err
	}
	_, err = session.Call(ctx, "Input.dispatchKeyEvent", map[string]any{
		"type":                  "keyUp",
		"key":                   descriptor.Key,
		"code":                  descriptor.Code,
		"windowsVirtualKeyCode": descriptor.VirtualKey,
		"nativeVirtualKeyCode":  descriptor.VirtualKey,
		"modifiers":             descriptor.Modifiers,
	})
	return err
}

// keySpec is the CDP description of one key press.
type keySpec struct {
	Key        string
	Code       string
	VirtualKey int
	Text       string
	Modifiers  int
}

// CDP modifier bits.
const (
	modifierAlt   = 1
	modifierCtrl  = 2
	modifierMeta  = 4
	modifierShift = 8
)

var namedKeys = map[string]keySpec{
	"return":    {Key: "Enter", Code: "Enter", VirtualKey: 13, Text: "\r"},
	"enter":     {Key: "Enter", Code: "Enter", VirtualKey: 13, Text: "\r"},
	"tab":       {Key: "Tab", Code: "Tab", VirtualKey: 9},
	"escape":    {Key: "Escape", Code: "Escape", VirtualKey: 27},
	"esc":       {Key: "Escape", Code: "Escape", VirtualKey: 27},
	"backspace": {Key: "Backspace", Code: "Backspace", VirtualKey: 8},
	"delete":    {Key: "Delete", Code: "Delete", VirtualKey: 46},
	"space":     {Key: " ", Code: "Space", VirtualKey: 32, Text: " "},
	"left":      {Key: "ArrowLeft", Code: "ArrowLeft", VirtualKey: 37},
	"up":        {Key: "ArrowUp", Code: "ArrowUp", VirtualKey: 38},
	"right":     {Key: "ArrowRight", Code: "ArrowRight", VirtualKey: 39},
	"down":      {Key: "ArrowDown", Code: "ArrowDown", VirtualKey: 40},
	"home":      {Key: "Home", Code: "Home", VirtualKey: 36},
	"end":       {Key: "End", Code: "End", VirtualKey: 35},
	"page_up":   {Key: "PageUp", Code: "PageUp", VirtualKey: 33},
	"prior":     {Key: "PageUp", Code: "PageUp", VirtualKey: 33},
	"page_down": {Key: "PageDown", Code: "PageDown", VirtualKey: 34},
	"next":      {Key: "PageDown", Code: "PageDown", VirtualKey: 34},
	"insert":    {Key: "Insert", Code: "Insert", VirtualKey: 45},
}

// keyDescriptor parses "ctrl+shift+s", "Return" or a bare character into a CDP key press.
func keyDescriptor(key string) (keySpec, bool) {
	main, shift, control, alt, meta, ok := splitChord(key)
	if !ok || main == "" {
		return keySpec{}, false
	}
	spec, known := namedKeys[strings.ToLower(main)]
	if !known {
		switch {
		case len([]rune(main)) == 1:
			upper := strings.ToUpper(main)
			spec = keySpec{Key: main, Code: "Key" + upper, VirtualKey: int(upper[0]), Text: main}
		case isFunctionKey(main):
			spec = keySpec{Key: main, Code: main, VirtualKey: 0x70 + functionKeyIndex(main) - 1}
		default:
			return keySpec{}, false
		}
	}
	if alt {
		spec.Modifiers |= modifierAlt
	}
	if control {
		spec.Modifiers |= modifierCtrl
	}
	if meta {
		spec.Modifiers |= modifierMeta
	}
	if shift {
		spec.Modifiers |= modifierShift
	}
	return spec, true
}
