package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/iFurySt/open-codex-computer-use/apps/opencomputeruselinux/internal/desktop"
)

const (
	defaultTextLimit    = 500
	defaultMaxTreeNodes = 1200
	defaultMaxTreeDepth = 64
	toolTimeout         = 30 * time.Second
)

// snapshotOptions are the get_app_state budgets; textLimit < 0 means "max".
type snapshotOptions struct {
	textLimit, maxTreeNodes, maxTreeDepth int
}

// service serves the CLI and MCP tools from the shared native engine.
type service struct {
	engine    *desktop.Engine
	snapshots map[string]*appSnapshot
}

func newService() *service {
	env := envSliceToMap(linuxRuntimeEnvironment(os.Environ()))
	// The CLI owns its process; X11 authentication only reads XAUTHORITY from it.
	if os.Getenv("XAUTHORITY") == "" && env["XAUTHORITY"] != "" {
		_ = os.Setenv("XAUTHORITY", env["XAUTHORITY"])
	}
	return &service{engine: desktop.New(desktop.Config{Env: env, InputGuard: inputGuard}), snapshots: map[string]*appSnapshot{}}
}
func (s *service) callTool(name string, args map[string]any) toolCallResult {
	switch name {
	case "list_apps":
		return s.listApps()
	case "get_app_state":
		maxTreeNodes, err := optionalPositiveInt(args, "max_tree_nodes")
		if err != nil {
			return textResult(err.Error(), true)
		}
		maxTreeDepth, err := optionalPositiveInt(args, "max_tree_depth")
		if err != nil {
			return textResult(err.Error(), true)
		}
		textLimit, err := optionalTextLimit(args, "text_limit")
		if err != nil {
			return textResult(err.Error(), true)
		}
		return s.getAppState(requiredString(args, "app"), textLimit, maxTreeNodes, maxTreeDepth)
	case "click":
		clickMethod, err := parseClickMethod(optionalString(args, "click_method"))
		if err != nil {
			return textResult(err.Error(), true)
		}
		return s.click(
			requiredString(args, "app"),
			optionalElementIndex(args),
			optionalFloat(args, "x"),
			optionalFloat(args, "y"),
			intValue(optionalFloat(args, "click_count"), 1),
			defaultString(optionalString(args, "mouse_button"), "left"),
			clickMethod,
		)
	case "perform_secondary_action":
		return s.performSecondaryAction(
			requiredString(args, "app"),
			requiredElementIndex(args),
			requiredString(args, "action"),
		)
	case "scroll":
		return s.scroll(
			requiredString(args, "app"),
			requiredString(args, "direction"),
			requiredElementIndex(args),
			floatValue(optionalFloat(args, "pages"), 1),
		)
	case "drag":
		return s.drag(
			requiredString(args, "app"),
			requiredFloat(args, "from_x"),
			requiredFloat(args, "from_y"),
			requiredFloat(args, "to_x"),
			requiredFloat(args, "to_y"),
		)
	case "type_text":
		return s.typeText(requiredString(args, "app"), requiredString(args, "text"))
	case "press_key":
		return s.pressKey(requiredString(args, "app"), requiredString(args, "key"))
	case "set_value":
		return s.setValue(requiredString(args, "app"), requiredElementIndex(args), requiredString(args, "value"))
	default:
		return textResult(fmt.Sprintf("unsupportedTool(%q)", name), true)
	}
}

func errorResult(err error) toolCallResult {
	var native *desktop.Error
	if errors.As(err, &native) && native.Effect != "none" {
		return textResult(native.Message+". The action may have taken effect and was not retried; run get_app_state before acting again.", true)
	}
	return textResult(err.Error(), true)
}

// connect replaces a closed AT-SPI connection; snapshots from the old one are dropped.
func (s *service) connect(ctx context.Context) error {
	if s.engine.Disconnected() {
		_ = s.engine.Close()
		clear(s.snapshots)
	}
	return s.engine.Connect(ctx)
}

func (s *service) listApps() toolCallResult {
	ctx, cancel := context.WithTimeout(context.Background(), toolTimeout)
	defer cancel()
	if err := s.connect(ctx); err != nil {
		return errorResult(err)
	}
	apps, _, err := s.engine.Apps(ctx)
	if err != nil {
		return errorResult(err)
	}
	sort.SliceStable(apps, func(i, j int) bool {
		left, right := strings.ToLower(apps[i].Name), strings.ToLower(apps[j].Name)
		return left < right || (left == right && apps[i].PID < apps[j].PID)
	})
	lines := []string{}
	for _, app := range apps {
		windows, err := s.engine.Windows(ctx, app)
		if err != nil || len(windows) == 0 {
			continue
		}
		title := windows[0].Title
		if title == "" {
			title = "untitled"
		}
		lines = append(lines, fmt.Sprintf("%s -- %s [running, pid=%d, window=%s]", app.Name, app.Name, app.PID, title))
	}
	if len(lines) == 0 {
		return textResult("No running top-level apps are visible to this Linux runtime.", false)
	}
	return textResult(strings.Join(lines, "\n"), false)
}

func (s *service) getAppState(app string, textLimit *textLimit, maxTreeNodes, maxTreeDepth *int) toolCallResult {
	if app == "" {
		return textResult("Missing required argument: app", true)
	}
	options := snapshotOptions{defaultTextLimit, defaultMaxTreeNodes, defaultMaxTreeDepth}
	if textLimit != nil {
		options.textLimit = textLimit.count
		if textLimit.max {
			options.textLimit = -1
		}
	}
	if maxTreeNodes != nil {
		options.maxTreeNodes = *maxTreeNodes
	}
	if maxTreeDepth != nil {
		options.maxTreeDepth = *maxTreeDepth
	}
	ctx, cancel := context.WithTimeout(context.Background(), toolTimeout)
	defer cancel()
	if err := s.connect(ctx); err != nil {
		return errorResult(err)
	}
	target, err := s.engine.Resolve(ctx, app)
	if err != nil {
		return errorResult(err)
	}
	snapshot, err := s.observe(ctx, app, target, options)
	if err != nil {
		return errorResult(err)
	}
	return snapshot.result()
}

func (s *service) observe(ctx context.Context, query string, app desktop.App, options snapshotOptions) (*appSnapshot, error) {
	observation, err := s.engine.Observe(ctx, app, desktop.TreeOptions{MaxNodes: options.maxTreeNodes, MaxDepth: options.maxTreeDepth, TextReadLimit: options.textLimit})
	if err != nil {
		return nil, err
	}
	snapshot := renderSnapshot(observation, options)
	if focused, err := s.engine.Focused(ctx, observation.Window); err == nil && len(focused) > 0 {
		snapshot.FocusedSummary = strings.TrimSpace(focused[0].Role + " " + limitText(focused[0].Name, options.textLimit))
		if selection, ok := s.engine.Selection(ctx, focused[0], options.textLimit); ok {
			snapshot.SelectedText = limitText(selection, options.textLimit)
		}
	}
	if capture := s.engine.Capture(ctx, app.PID, observation.Window.Title); capture.Reason == nil {
		snapshot.ScreenshotPNGBase64 = base64.StdEncoding.EncodeToString(capture.PNG)
	}
	s.rememberSnapshot(query, snapshot)
	return snapshot, nil
}

// renderSnapshot keeps the established text tree: depth-first indices, window-relative frames.
func renderSnapshot(observation desktop.Observation, options snapshotOptions) *appSnapshot {
	window := observation.Window.Extents
	snapshot := &appSnapshot{
		App:         appDescriptor{Name: observation.App.Name, BundleIdentifier: observation.App.Name, PID: int(observation.App.PID)},
		WindowTitle: limitText(observation.Window.Title, options.textLimit),
		Elements:    make([]elementRecord, 0, len(observation.Nodes)),
		TreeLines:   make([]string, 0, len(observation.Nodes)),
		observation: observation,
		options:     options,
	}
	if window != nil {
		snapshot.WindowBounds = &frame{window.X, window.Y, window.Width, window.Height}
	}
	for index, node := range observation.Nodes {
		record := elementRecord{
			Index:                index,
			RuntimeID:            node.Path,
			AutomationID:         node.AccessibleID,
			Name:                 limitText(node.Name, options.textLimit),
			ControlType:          node.Role,
			LocalizedControlType: node.Role,
			ClassName:            observation.App.Toolkit,
			Value:                limitText(nodeValue(node), options.textLimit),
		}
		for _, action := range node.Actions {
			if label := action.Label(); label != "" && !slices.Contains(record.Actions, label) {
				record.Actions = append(record.Actions, label)
			}
		}
		if node.Extents != nil {
			record.Frame = &frame{node.Extents.X, node.Extents.Y, node.Extents.Width, node.Extents.Height}
			if window != nil {
				record.Frame.X -= window.X
				record.Frame.Y -= window.Y
			}
		}
		snapshot.Elements = append(snapshot.Elements, record)
		snapshot.TreeLines = append(snapshot.TreeLines, treeLine(record, node.Depth))
	}
	return snapshot
}

func treeLine(record elementRecord, depth int) string {
	role := record.LocalizedControlType
	if role == "" {
		role = record.ControlType
	}
	if role == "" {
		role = "element"
	}
	title := record.Name
	if title == "" {
		title = record.AutomationID
	}
	line := fmt.Sprintf("%d %s %s", record.Index, role, title)
	if record.Value != "" && record.Value != title {
		line += " Value: " + strings.NewReplacer("\r", "\\r", "\n", "\\n").Replace(record.Value)
	}
	if len(record.Actions) > 0 {
		line += " Secondary Actions: " + strings.Join(record.Actions, ", ")
	}
	if f := record.Frame; f != nil {
		line += fmt.Sprintf(" Frame: {x: %.0f, y: %.0f, width: %.0f, height: %.0f}", math.RoundToEven(f.X), math.RoundToEven(f.Y), math.RoundToEven(f.Width), math.RoundToEven(f.Height))
	}
	return strings.Repeat("\t", depth+1) + strings.TrimRight(line, " \t")
}

// limitText truncates by characters and marks the cut; limit < 0 keeps everything.
func limitText(value string, limit int) string {
	characters := []rune(value)
	if limit < 0 || len(characters) <= limit {
		return value
	}
	return string(characters[:limit]) + "..."
}

func (s *service) currentSnapshot(app string) *appSnapshot {
	return s.snapshots[strings.ToLower(app)]
}

func (s *service) rememberSnapshot(query string, snapshot *appSnapshot) {
	keys := []string{query, snapshot.App.Name, snapshot.App.BundleIdentifier, strconv.Itoa(snapshot.App.PID)}
	for _, key := range keys {
		key = strings.ToLower(strings.TrimSpace(key))
		if key != "" {
			s.snapshots[key] = snapshot
		}
	}
}

func (s *service) forgetSnapshot(snapshot *appSnapshot) {
	for key, candidate := range s.snapshots {
		if candidate == snapshot {
			delete(s.snapshots, key)
		}
	}
}

func lookupElement(snapshot *appSnapshot, elementIndex string) (desktop.Node, *elementRecord, error) {
	index, err := strconv.Atoi(elementIndex)
	if err != nil || index < 0 || index >= len(snapshot.observation.Nodes) || index >= len(snapshot.Elements) {
		return desktop.Node{}, nil, fmt.Errorf("unknown element_index %q", elementIndex)
	}
	record := snapshot.Elements[index]
	return snapshot.observation.Nodes[index], &record, nil
}

// actionTarget returns the current snapshot for an action tool, or the error result to send.
func (s *service) actionTarget(app string) (*appSnapshot, *toolCallResult) {
	snapshot := s.currentSnapshot(app)
	if snapshot == nil {
		result := textResult("No app state is available for "+app+". Run get_app_state before action tools.", true)
		return nil, &result
	}
	return snapshot, nil
}

// act runs one native action and then observes again. A completed action stays
// completed when the follow-up observation fails.
func (s *service) act(app string, snapshot *appSnapshot, action func(context.Context) error) toolCallResult {
	ctx, cancel := context.WithTimeout(context.Background(), toolTimeout)
	defer cancel()
	if err := s.connect(ctx); err != nil {
		return errorResult(err)
	}
	if err := action(ctx); err != nil {
		var native *desktop.Error
		if !errors.As(err, &native) || native.Effect != "none" {
			s.forgetSnapshot(snapshot)
		}
		return errorResult(err)
	}
	s.forgetSnapshot(snapshot)
	time.Sleep(120 * time.Millisecond)
	updated, err := s.observe(ctx, app, snapshot.observation.App, snapshot.options)
	if err != nil {
		return textResult("Action completed, but the follow-up observation failed: "+err.Error()+". Run get_app_state before the next action.", false)
	}
	return updated.result()
}

func globalTarget(snapshot *appSnapshot) desktop.GlobalTarget {
	return desktop.GlobalTarget{PID: snapshot.observation.App.PID, Title: snapshot.observation.Window.Title}
}

// screenPoint maps an element centre or window-relative x/y to the screen using
// the accessible window bounds that the element frames are relative to.
func screenPoint(snapshot *appSnapshot, record *elementRecord, x, y *float64) (desktop.Point, error) {
	bounds := snapshot.WindowBounds
	if bounds == nil {
		return desktop.Point{}, errors.New("coordinate action requires window bounds; the window reports none")
	}
	if record != nil {
		if record.Frame == nil {
			return desktop.Point{}, fmt.Errorf("element %d has no frame for a pointer click", record.Index)
		}
		return desktop.Point{X: bounds.X + record.Frame.X + record.Frame.Width/2, Y: bounds.Y + record.Frame.Y + record.Frame.Height/2}, nil
	}
	return desktop.Point{X: bounds.X + *x, Y: bounds.Y + *y}, nil
}

func (s *service) click(app, elementIndex string, x, y *float64, clickCount int, mouseButton, clickMethod string) toolCallResult {
	if app == "" {
		return textResult("Missing required argument: app", true)
	}
	if elementIndex == "" && (x == nil || y == nil) {
		return textResult("click requires either element_index or x/y", true)
	}
	if clickMethod == "accessibility" && elementIndex == "" {
		return textResult("click_method 'accessibility' requires element_index", true)
	}
	if clickMethod == "app_post" {
		return textResult("click_method 'app_post' is not supported on Linux", true)
	}
	if clickMethod == "sky_click" {
		return textResult("click_method 'sky_click' is not supported on Linux", true)
	}
	if clickMethod == "global" && !globalPointerFallbacksEnabled() {
		return textResult("click_method 'global' requires OPEN_COMPUTER_USE_ALLOW_GLOBAL_POINTER_FALLBACKS=1 because it may move the system pointer and change foreground focus", true)
	}
	snapshot, failure := s.actionTarget(app)
	if failure != nil {
		return *failure
	}
	var node desktop.Node
	var record *elementRecord
	if elementIndex != "" {
		var err error
		if node, record, err = lookupElement(snapshot, elementIndex); err != nil {
			return textResult(err.Error(), true)
		}
	}
	semantic, hasSemantic := desktop.ClickAction(node.Actions)
	if clickMethod == "accessibility" {
		if mouseButton != "left" {
			return textResult("click_method 'accessibility' only supports mouse_button 'left'", true)
		}
		if clickCount != 1 {
			return textResult("click_method 'accessibility' performs one semantic activation; click_count must be 1", true)
		}
		if !hasSemantic {
			return textResult("click_method 'accessibility' could not find a semantic click action for the requested element", true)
		}
	}
	window := snapshot.observation.Window
	// The route is chosen before dispatch; a failed semantic click never becomes a pointer click.
	if record != nil && hasSemantic && clickMethod != "global" && mouseButton == "left" && clickCount == 1 {
		return s.act(app, snapshot, func(ctx context.Context) error { return s.engine.DoAction(ctx, window, node, semantic) })
	}
	button, ok := map[string]byte{"left": 1, "middle": 2, "right": 3}[mouseButton]
	if !ok {
		return textResult("Invalid mouse_button: "+mouseButton, true)
	}
	if clickCount < 1 || clickCount > 3 {
		return textResult("click_count must be between 1 and 3", true)
	}
	point, err := screenPoint(snapshot, record, x, y)
	if err != nil {
		return textResult(err.Error(), true)
	}
	return s.act(app, snapshot, func(ctx context.Context) error {
		if record != nil {
			if _, err := s.engine.Revalidate(ctx, window, node); err != nil {
				return err
			}
		}
		return s.engine.PointerClick(ctx, globalTarget(snapshot), point, button, clickCount)
	})
}

func (s *service) performSecondaryAction(app, elementIndex, action string) toolCallResult {
	if app == "" {
		return textResult("Missing required argument: app", true)
	}
	if elementIndex == "" {
		return textResult("Missing required argument: element_index", true)
	}
	if action == "" {
		return textResult("Missing required argument: action", true)
	}
	snapshot, failure := s.actionTarget(app)
	if failure != nil {
		return *failure
	}
	node, _, err := lookupElement(snapshot, elementIndex)
	if err != nil {
		return textResult(err.Error(), true)
	}
	matches := desktop.MatchActions(node.Actions, action)
	if len(matches) == 0 {
		return textResult(fmt.Sprintf("%s is not a valid secondary action for element", action), true)
	}
	if len(matches) > 1 {
		return textResult(fmt.Sprintf("%s matches %d actions on this element; the action is ambiguous", action, len(matches)), true)
	}
	return s.act(app, snapshot, func(ctx context.Context) error {
		return s.engine.DoAction(ctx, snapshot.observation.Window, node, matches[0])
	})
}

// scroll sends page keys to the target window, which must have keyboard focus.
func (s *service) scroll(app, direction, elementIndex string, pages float64) toolCallResult {
	if app == "" {
		return textResult("Missing required argument: app", true)
	}
	if elementIndex == "" {
		return textResult("Missing required argument: element_index", true)
	}
	normalized := strings.ToLower(direction)
	if normalized != "up" && normalized != "down" && normalized != "left" && normalized != "right" {
		return textResult("Invalid scroll direction: "+direction, true)
	}
	if pages <= 0 {
		return textResult("pages must be > 0", true)
	}
	snapshot, failure := s.actionTarget(app)
	if failure != nil {
		return *failure
	}
	node, _, err := lookupElement(snapshot, elementIndex)
	if err != nil {
		return textResult(err.Error(), true)
	}
	return s.act(app, snapshot, func(ctx context.Context) error {
		if _, err := s.engine.Revalidate(ctx, snapshot.observation.Window, node); err != nil {
			return err
		}
		return s.engine.PageScroll(ctx, globalTarget(snapshot), normalized, pages)
	})
}

func (s *service) drag(app string, fromX, fromY, toX, toY *float64) toolCallResult {
	if app == "" {
		return textResult("Missing required argument: app", true)
	}
	for _, required := range []struct {
		name  string
		value *float64
	}{{"from_x", fromX}, {"from_y", fromY}, {"to_x", toX}, {"to_y", toY}} {
		if required.value == nil {
			return textResult("Missing required argument: "+required.name, true)
		}
	}
	snapshot, failure := s.actionTarget(app)
	if failure != nil {
		return *failure
	}
	from, err := screenPoint(snapshot, nil, fromX, fromY)
	if err != nil {
		return textResult(err.Error(), true)
	}
	to, _ := screenPoint(snapshot, nil, toX, toY)
	return s.act(app, snapshot, func(ctx context.Context) error {
		return s.engine.Drag(ctx, globalTarget(snapshot), from, to)
	})
}

// typeText edits the focused field natively; with no focused editable field it
// types keys into the target window, which must have keyboard focus.
func (s *service) typeText(app, text string) toolCallResult {
	if app == "" {
		return textResult("Missing required argument: app", true)
	}
	if text == "" {
		return textResult("Missing required argument: text", true)
	}
	snapshot, failure := s.actionTarget(app)
	if failure != nil {
		return *failure
	}
	return s.act(app, snapshot, func(ctx context.Context) error {
		err := s.engine.TypeText(ctx, snapshot.observation.Window, text)
		var native *desktop.Error
		if errors.As(err, &native) && native.Code == desktop.ErrNoFocusedText {
			return s.engine.TypeKeys(ctx, globalTarget(snapshot), text)
		}
		return err
	})
}

func (s *service) pressKey(app, key string) toolCallResult {
	if app == "" {
		return textResult("Missing required argument: app", true)
	}
	if key == "" {
		return textResult("Missing required argument: key", true)
	}
	snapshot, failure := s.actionTarget(app)
	if failure != nil {
		return *failure
	}
	return s.act(app, snapshot, func(ctx context.Context) error {
		return s.engine.PressKey(ctx, globalTarget(snapshot), key)
	})
}

func (s *service) setValue(app, elementIndex, value string) toolCallResult {
	if app == "" {
		return textResult("Missing required argument: app", true)
	}
	if elementIndex == "" {
		return textResult("Missing required argument: element_index", true)
	}
	snapshot, failure := s.actionTarget(app)
	if failure != nil {
		return *failure
	}
	node, _, err := lookupElement(snapshot, elementIndex)
	if err != nil {
		return textResult(err.Error(), true)
	}
	return s.act(app, snapshot, func(ctx context.Context) error {
		return s.engine.SetValue(ctx, snapshot.observation.Window, node, value)
	})
}
