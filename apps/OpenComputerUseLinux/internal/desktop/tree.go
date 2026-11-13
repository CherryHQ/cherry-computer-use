package desktop

import (
	"context"
	"slices"
	"strings"
	"sync"
)

// Action is one AT-SPI Action entry; Index is only meaningful with Name/Description.
type Action struct {
	Index             int32
	Name, Description string
}

// Label is what users see and match: the name, or the description when unnamed.
func (a Action) Label() string {
	if a.Name != "" {
		return a.Name
	}
	return a.Description
}

// Node is one observed accessible. Name and Text keep native text for revalidation;
// callers apply their own display limits.
type Node struct {
	Ref          Ref
	Path         []int // child indices from the application root
	Parent       int   // index into Observation.Nodes, -1 for the window
	Depth        int
	Name, Role   string
	AccessibleID string
	Interfaces   []string
	States       States
	Extents      *Rect
	Actions      []Action
	// Text holds at most the requested read limit plus one character; TextLength is the full count.
	Text       string
	TextLength int32
	Value      *float64
}

func (n Node) Supports(iface string) bool {
	return slices.Contains(n.Interfaces, "org.a11y.atspi."+iface)
}

type TreeOptions struct {
	WithoutAX          bool
	MaxNodes, MaxDepth int
	// TextReadLimit caps characters read from Text interfaces; negative reads everything.
	TextReadLimit int
}

type Observation struct {
	App            App
	Window         Window
	Nodes          []Node
	NodesTruncated bool
	DepthTruncated bool
}

// Observe reserves the bounded tree for visible controls, then emits document order.
func (e *Engine) Observe(ctx context.Context, app App, options TreeOptions) (Observation, error) {
	windows, err := e.Windows(ctx, app)
	if err != nil {
		return Observation{}, err
	}
	window, err := MainWindow(app, windows)
	if err != nil {
		return Observation{}, err
	}
	observation := Observation{App: app, Window: window, Nodes: []Node{}}
	if options.WithoutAX {
		return observation, nil
	}
	plan, err := planTree(window.Ref, window.Index, options, func(ref Ref) (treeTraits, error) {
		if ctx.Err() != nil {
			return treeTraits{}, ctx.Err()
		}
		var role string
		var states States
		var roleErr, stateErr error
		parallel(func() { role, roleErr = e.role(ctx, ref) }, func() { states, stateErr = e.states(ctx, ref) })
		if roleErr != nil {
			return treeTraits{}, roleErr
		}
		return treeTraits{
			control:    slices.Contains([]string{"push button", "check box", "radio button", "text", "entry", "password text", "combo box", "slider", "spin button", "link"}, role),
			collection: slices.Contains([]string{"list", "table", "tree", "tree table"}, role),
			hidden:     stateErr == nil && !states.Has(StateShowing),
		}, nil
	}, func(ref Ref) ([]Ref, error) { return e.children(ctx, ref) })
	if err != nil {
		return Observation{}, err
	}
	observation.NodesTruncated, observation.DepthTruncated = plan.nodesTruncated, plan.depthTruncated
	indexes := map[Ref]int{}
	for _, work := range plan.nodes {
		if ctx.Err() != nil {
			return Observation{}, fail("CANCELLED", "Observation cancelled")
		}
		node, ok := e.readNode(ctx, work.ref, options)
		if !ok {
			return Observation{}, fail("TARGET_UNAVAILABLE", "Accessibility tree changed while reading")
		}
		node.Path, node.Depth, node.Parent = work.path, work.depth, -1
		if work.ref != window.Ref {
			parent, exists := indexes[work.parent]
			if !exists {
				return Observation{}, fail("TARGET_UNAVAILABLE", "Accessibility parent changed while reading")
			}
			node.Parent = parent
		}
		indexes[work.ref] = len(observation.Nodes)
		observation.Nodes = append(observation.Nodes, node)
	}
	return observation, nil
}

// readNode issues independent property reads concurrently; one connection
// pipelines them, which keeps large trees within the CLI time budget.
func (e *Engine) readNode(ctx context.Context, ref Ref, options TreeOptions) (Node, bool) {
	node := Node{Ref: ref}
	var nameErr, roleErr error
	parallel(
		func() { node.Name, nameErr = e.name(ctx, ref) },
		func() { node.Role, roleErr = e.role(ctx, ref) },
		func() { _ = e.call(ctx, ref, "Accessible.GetInterfaces").Store(&node.Interfaces) },
		func() { node.States, _ = e.states(ctx, ref) },
		func() { _ = e.property(ctx, ref, "Accessible", "AccessibleId", &node.AccessibleID) },
		func() { node.Actions = e.actions(ctx, ref) },
	)
	if nameErr != nil || roleErr != nil {
		return node, false
	}
	reads := []func(){}
	if node.Supports("Component") {
		reads = append(reads, func() { node.Extents = e.extents(ctx, ref) })
	}
	if node.Supports("Text") {
		reads = append(reads, func() {
			if node.IsPassword() {
				_ = e.property(ctx, ref, "Text", "CharacterCount", &node.TextLength)
			} else {
				node.Text, node.TextLength = e.readText(ctx, ref, options.TextReadLimit)
			}
		})
	}
	if node.Supports("Value") && !node.IsPassword() {
		reads = append(reads, func() {
			var value float64
			if e.property(ctx, ref, "Value", "CurrentValue", &value) == nil {
				node.Value = &value
			}
		})
	}
	parallel(reads...)
	return node, true
}

func parallel(reads ...func()) {
	var group sync.WaitGroup
	group.Add(len(reads))
	for _, read := range reads {
		go func() {
			defer group.Done()
			read()
		}()
	}
	group.Wait()
}

// actions reads unlocalized names, as callers match them. Some toolkits (Chromium)
// implement Action without listing it in GetInterfaces, so the count is always asked.
func (e *Engine) actions(ctx context.Context, ref Ref) []Action {
	var count int32
	if e.property(ctx, ref, "Action", "NActions", &count) != nil || count <= 0 {
		return nil
	}
	actions := make([]Action, min(count, 64))
	reads := []func(){}
	for i := range actions {
		action := &actions[i]
		action.Index = int32(i)
		reads = append(reads,
			func() { _ = e.call(ctx, ref, "Action.GetName", action.Index).Store(&action.Name) },
			func() { _ = e.call(ctx, ref, "Action.GetDescription", action.Index).Store(&action.Description) })
	}
	parallel(reads...)
	return actions
}

func (e *Engine) readText(ctx context.Context, ref Ref, limit int) (string, int32) {
	var count int32
	if e.property(ctx, ref, "Text", "CharacterCount", &count) != nil || count <= 0 {
		return "", 0
	}
	end := count
	if limit >= 0 && int64(limit)+1 < int64(count) {
		end = int32(limit + 1)
	}
	var text string
	if e.call(ctx, ref, "Text.GetText", int32(0), end).Store(&text) != nil {
		return "", count
	}
	return text, count
}

var preferredClickActions = []string{"click", "press", "activate", "default.activate", "invoke", "select", "toggle", "open"}

func (n Node) IsPassword() bool { return strings.EqualFold(strings.TrimSpace(n.Role), "password text") }

// ClickAction picks the action that best means a primary click, if any.
func ClickAction(actions []Action) (Action, bool) {
	// GTK entries expose activate for their default button. Prefer an actual click
	// when both are present; activating a text entry can submit a form.
	for _, name := range preferredClickActions {
		for _, action := range actions {
			if strings.EqualFold(action.Label(), name) {
				return action, true
			}
		}
	}
	for _, action := range actions {
		label := strings.ToLower(action.Label())
		if strings.Contains(label, "click") || strings.Contains(label, "press") || strings.Contains(label, "activate") {
			return action, true
		}
	}
	return Action{}, false
}

// MatchActions returns actions whose name or description equals label, ignoring case.
func MatchActions(actions []Action, label string) []Action {
	matches := []Action{}
	for _, action := range actions {
		if strings.EqualFold(action.Name, label) || strings.EqualFold(action.Description, label) {
			matches = append(matches, action)
		}
	}
	return matches
}

// Settable reports whether SetValue may dispatch to the node; it does not promise success.
func (n Node) Settable() bool {
	if n.States.Has(StateReadOnly) || !n.States.Has(StateEnabled) {
		return false
	}
	return n.Supports("Value") || (n.Supports("EditableText") && n.States.Has(StateEditable))
}

// Focused finds focused descendants of a window beyond any observation budget.
func (e *Engine) Focused(ctx context.Context, window Window) ([]Node, error) {
	if err := e.Connect(ctx); err != nil {
		return nil, err
	}
	focused := []Node{}
	queue := []Ref{window.Ref}
	visited := map[Ref]bool{}
	for len(queue) > 0 && len(visited) < 5000 {
		if ctx.Err() != nil {
			return nil, fail("CANCELLED", "Focus lookup cancelled")
		}
		ref := queue[0]
		queue = queue[1:]
		if visited[ref] {
			continue
		}
		visited[ref] = true
		states, err := e.states(ctx, ref)
		if err != nil {
			continue
		}
		if states.Has(StateFocused) {
			if node, ok := e.readNode(ctx, ref, TreeOptions{TextReadLimit: 0}); ok {
				focused = append(focused, node)
			}
		}
		children, _ := e.children(ctx, ref)
		queue = append(queue, children...)
	}
	return focused, nil
}

// Selection returns the first text selection of a node, if it has one.
func (e *Engine) Selection(ctx context.Context, node Node, limit int) (string, bool) {
	if !node.Supports("Text") || node.IsPassword() {
		return "", false
	}
	start, end, ok := e.selection(ctx, node.Ref)
	if !ok || start == end {
		return "", false
	}
	if limit >= 0 && int64(end) > int64(start)+int64(limit)+1 {
		end = start + int32(limit) + 1
	}
	var text string
	if e.call(ctx, node.Ref, "Text.GetText", start, end).Store(&text) != nil {
		return "", false
	}
	return text, true
}

func (e *Engine) selection(ctx context.Context, ref Ref) (int32, int32, bool) {
	var count int32
	if e.call(ctx, ref, "Text.GetNSelections").Store(&count) != nil || count <= 0 {
		return 0, 0, false
	}
	var start, end int32
	if e.call(ctx, ref, "Text.GetSelection", int32(0)).Store(&start, &end) != nil {
		return 0, 0, false
	}
	return min(start, end), max(start, end), true
}
