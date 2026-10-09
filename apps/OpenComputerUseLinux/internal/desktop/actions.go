package desktop

import (
	"context"
	"math"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/godbus/dbus/v5"
)

// Revalidate confirms that a node observed earlier is still the same element in
// the same window, and returns its current state. Nothing is dispatched.
func (e *Engine) Revalidate(ctx context.Context, window Window, node Node) (Node, error) {
	if err := e.Connect(ctx); err != nil {
		return Node{}, err
	}
	title, err := e.name(ctx, window.Ref)
	if err != nil || title != window.Title {
		return Node{}, fail("STALE_SNAPSHOT", "Window changed since observation")
	}
	current, ok := e.readNode(ctx, node.Ref, TreeOptions{TextReadLimit: 0})
	if !ok || current.Name != node.Name || current.Role != node.Role {
		return Node{}, fail("STALE_SNAPSHOT", "Element changed since observation")
	}
	parent := node.Ref
	for depth := 0; parent != window.Ref; depth++ {
		var next Ref
		if depth > 128 || e.property(ctx, parent, "Accessible", "Parent", &next) != nil || next == parent {
			return Node{}, fail("STALE_SNAPSHOT", "Element no longer belongs to the observed window")
		}
		parent = next
	}
	current.Path, current.Parent, current.Depth = node.Path, node.Parent, node.Depth
	return current, nil
}

// dispatch runs one state-changing call. Once sent, it is awaited even if the
// caller cancels, because the application may already be applying it. Callers
// check cancellation once before their first dispatch, never between steps.
func (e *Engine) dispatch(ctx context.Context, ref Ref, method string, result any, args ...any) error {
	execution, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	call := e.bus.Object(ref.Bus, ref.Path).CallWithContext(execution, method, 0, args...)
	if call.Err != nil {
		return uncertain("Semantic action outcome is unknown: %v", call.Err)
	}
	if result != nil {
		return call.Store(result)
	}
	return nil
}

// DoAction performs one named action after confirming the index still names it.
// It never retries and never falls back to pointer input.
func (e *Engine) DoAction(ctx context.Context, window Window, node Node, action Action) error {
	current, err := e.Revalidate(ctx, window, node)
	if err != nil {
		return err
	}
	if !current.States.Has(StateEnabled) {
		return fail("TARGET_UNAVAILABLE", "Element is not enabled")
	}
	index := -1
	for _, candidate := range current.Actions {
		if candidate.Index == action.Index && candidate.Name == action.Name && candidate.Description == action.Description {
			index = int(candidate.Index)
		}
	}
	if index < 0 {
		return fail("STALE_SNAPSHOT", "Element action %q changed since observation", action.Label())
	}
	if ctx.Err() != nil {
		return cancelled()
	}
	var applied bool
	if err := e.dispatch(ctx, node.Ref, "org.a11y.atspi.Action.DoAction", &applied, int32(index)); err != nil {
		return err
	}
	if !applied {
		return uncertain("Application did not confirm the action")
	}
	return nil
}

// SetValue writes numbers through Value and text through EditableText, then reads
// the result back. A mismatch is reported with the value actually present.
func (e *Engine) SetValue(ctx context.Context, window Window, node Node, value string) error {
	current, err := e.Revalidate(ctx, window, node)
	if err != nil {
		return err
	}
	if current.States.Has(StateReadOnly) || !current.States.Has(StateEnabled) {
		return fail("TARGET_UNAVAILABLE", "Element is read-only or disabled")
	}
	if ctx.Err() != nil {
		return cancelled()
	}
	if current.Supports("Value") {
		return e.setNumber(ctx, current, value)
	}
	if !current.Supports("EditableText") {
		return fail("UNSUPPORTED_CAPABILITY", "Cannot set a value for an element that is not settable")
	}
	if !current.States.Has(StateEditable) {
		return fail("TARGET_UNAVAILABLE", "Text element is not editable")
	}
	var applied bool
	if err := e.dispatch(ctx, current.Ref, "org.a11y.atspi.EditableText.SetTextContents", &applied, value); err != nil {
		return err
	}
	actual, length := e.readText(ctx, current.Ref, -1)
	if !applied || actual != value {
		return uncertain("Text did not update as requested; it now reads %d characters", length)
	}
	return nil
}

func (e *Engine) setNumber(ctx context.Context, node Node, value string) error {
	number, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
		return fail("INVALID_ARGUMENT", "%s expects a finite number", node.Role)
	}
	var minimum, maximum float64
	if e.property(ctx, node.Ref, "Value", "MinimumValue", &minimum) == nil && e.property(ctx, node.Ref, "Value", "MaximumValue", &maximum) == nil &&
		minimum <= maximum && (number < minimum || number > maximum) {
		return fail("INVALID_ARGUMENT", "%s must be between %s and %s", node.Role, formatNumber(minimum), formatNumber(maximum))
	}
	if err := e.dispatch(ctx, node.Ref, "org.freedesktop.DBus.Properties.Set", nil, "org.a11y.atspi.Value", "CurrentValue", dbus.MakeVariant(number)); err != nil {
		return err
	}
	var actual float64
	if err := e.property(ctx, node.Ref, "Value", "CurrentValue", &actual); err != nil {
		return uncertain("Value was sent but cannot be read back")
	}
	if math.Abs(actual-number) > 1e-9*max(1, math.Abs(number)) {
		return uncertain("Value reads %s after requesting %s", formatNumber(actual), formatNumber(number))
	}
	return nil
}

func cancelled() *Error { return fail("CANCELLED", "Action cancelled before dispatch") }

func formatNumber(value float64) string { return strconv.FormatFloat(value, 'f', -1, 64) }

// ErrNoFocusedText distinguishes "nothing to type into" from an ambiguous target.
const ErrNoFocusedText = "NO_FOCUSED_TEXT"

// TypeText inserts at the caret of the one focused editable text in the window,
// replacing its selection. It refuses rather than guessing a target.
func (e *Engine) TypeText(ctx context.Context, window Window, text string) error {
	focused, err := e.Focused(ctx, window)
	if err != nil {
		return err
	}
	targets := []Node{}
	for _, node := range focused {
		if node.Supports("EditableText") && node.Supports("Text") {
			targets = append(targets, node)
		}
	}
	if len(targets) == 0 {
		return &Error{Code: ErrNoFocusedText, Message: "No focused editable text field in the target window", Effect: "none"}
	}
	if len(targets) > 1 {
		return fail("TARGET_UNAVAILABLE", "%d focused editable text fields; the target is ambiguous", len(targets))
	}
	node, err := e.Revalidate(ctx, window, targets[0])
	if err != nil {
		return err
	}
	if !node.States.Has(StateEditable) || node.States.Has(StateReadOnly) {
		return fail("TARGET_UNAVAILABLE", "Focused text field is not editable")
	}
	if ctx.Err() != nil {
		return cancelled()
	}
	position, end, selected := e.selection(ctx, node.Ref)
	if !selected || position == end {
		if e.property(ctx, node.Ref, "Text", "CaretOffset", &position) != nil || position < 0 || position > node.TextLength {
			position = node.TextLength
		}
	} else {
		var deleted bool
		if err := e.dispatch(ctx, node.Ref, "org.a11y.atspi.EditableText.DeleteText", &deleted, position, end); err != nil {
			return err
		}
		if !deleted {
			return uncertain("Application did not confirm replacing the selection")
		}
	}
	// Offsets count characters; the inserted length counts UTF-8 bytes, as toolkits pass it to their editors.
	var inserted bool
	if err := e.dispatch(ctx, node.Ref, "org.a11y.atspi.EditableText.InsertText", &inserted, position, text, int32(len(text))); err != nil {
		return err
	}
	characters := int32(utf8.RuneCountInString(text))
	var actual string
	if e.call(ctx, node.Ref, "Text.GetText", position, position+characters).Store(&actual) != nil || actual != text {
		return uncertain("Typed text could not be confirmed in the focused field")
	}
	var moved bool
	_ = e.call(ctx, node.Ref, "Text.SetCaretOffset", position+characters).Store(&moved)
	return nil
}
