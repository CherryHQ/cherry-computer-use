// Package webcdp delivers input to Chromium and Electron pages over the Chrome DevTools Protocol.
//
// It is platform neutral on purpose: the driver that owns an application supplies its process ID and
// window title, this package finds the DevTools endpoint, picks the matching page target and sends
// input through it. Every failure means "fall back to the platform accessibility path", never "the
// action succeeded but did nothing".
package webcdp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrUnavailable reports that the application exposes no DevTools endpoint, so the caller should use
// the platform input path instead.
var ErrUnavailable = errors.New("webcdp: application exposes no DevTools endpoint")

// Target is one DevTools page target.
type Target struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	URL          string `json:"url"`
	Type         string `json:"type"`
	WebSocketURL string `json:"webSocketDebuggerUrl"`
}

// Enabled reports whether the CDP channel is allowed. OPEN_COMPUTER_USE_DISABLE_CDP=1 turns it off,
// which keeps every action on the platform accessibility path.
func Enabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("OPEN_COMPUTER_USE_DISABLE_CDP"))) {
	case "1", "true", "yes", "on":
		return false
	}
	return true
}

// Discover resolves the DevTools port of a running process. Chromium and Electron either record the
// switch on the command line or write DevToolsActivePort into the directory given to --user-data-dir.
func Discover(pid int) (int, error) {
	if !Enabled() {
		return 0, ErrUnavailable
	}
	line, err := commandLine(pid)
	if err != nil {
		return 0, ErrUnavailable
	}
	if port := portFromCommandLine(line); port > 0 {
		return port, nil
	}
	directory := userDataDirFromCommandLine(line)
	if directory == "" {
		return 0, ErrUnavailable
	}
	port, err := portFromActivePortFile(filepath.Join(directory, "DevToolsActivePort"))
	if err != nil {
		return 0, ErrUnavailable
	}
	return port, nil
}

// portFromCommandLine reads --remote-debugging-port. --remote-debugging-pipe is not reachable over HTTP.
func portFromCommandLine(line string) int {
	if line == "" {
		return 0
	}
	const prefix = "--remote-debugging-port="
	index := strings.Index(line, prefix)
	if index < 0 {
		return 0
	}
	value := line[index+len(prefix):]
	if end := strings.IndexAny(value, " \t\""); end >= 0 {
		value = value[:end]
	}
	port := 0
	for _, char := range value {
		if char < '0' || char > '9' {
			break
		}
		port = port*10 + int(char-'0')
	}
	if port <= 0 || port > 65535 {
		return 0
	}
	return port
}

// userDataDirFromCommandLine returns the --user-data-dir value, quoted or not.
func userDataDirFromCommandLine(line string) string {
	if line == "" {
		return ""
	}
	const prefix = "--user-data-dir="
	index := strings.Index(line, prefix)
	if index < 0 {
		return ""
	}
	value := line[index+len(prefix):]
	if strings.HasPrefix(value, "\"") {
		if end := strings.Index(value[1:], "\""); end >= 0 {
			return value[1 : 1+end]
		}
		return ""
	}
	if end := strings.IndexAny(value, " \t"); end >= 0 {
		value = value[:end]
	}
	return strings.TrimSpace(value)
}

// portFromActivePortFile reads the first line of DevToolsActivePort, which Chromium writes as the port.
func portFromActivePortFile(path string) (int, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	line := string(content)
	if newline := strings.IndexAny(line, "\r\n"); newline >= 0 {
		line = line[:newline]
	}
	port := 0
	for _, char := range strings.TrimSpace(line) {
		if char < '0' || char > '9' {
			return 0, ErrUnavailable
		}
		port = port*10 + int(char-'0')
	}
	if port <= 0 || port > 65535 {
		return 0, ErrUnavailable
	}
	return port, nil
}

// PageTargets lists the page targets of a DevTools endpoint.
func PageTargets(ctx context.Context, port int) ([]Target, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/json/list", port), nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("webcdp: target list returned %s", response.Status)
	}
	var targets []Target
	if err := json.NewDecoder(response.Body).Decode(&targets); err != nil {
		return nil, err
	}
	return targets, nil
}

// SelectTarget picks the page target that belongs to the observed window: an exact title match first,
// then a prefix match in either direction (browsers append the profile name), then the first page.
func SelectTarget(targets []Target, windowTitle string) *Target {
	pages := make([]Target, 0, len(targets))
	for _, target := range targets {
		if target.Type == "page" && target.WebSocketURL != "" {
			pages = append(pages, target)
		}
	}
	if len(pages) == 0 {
		return nil
	}
	if windowTitle != "" {
		for index := range pages {
			if pages[index].Title == windowTitle {
				return &pages[index]
			}
		}
		for index := range pages {
			title := pages[index].Title
			if title != "" && (strings.HasPrefix(windowTitle, title) || strings.HasPrefix(title, windowTitle)) {
				return &pages[index]
			}
		}
	}
	return &pages[0]
}

// Input delivers text and key input to one application.
type Input struct {
	PID         int
	WindowTitle string
}

func (in Input) open(ctx context.Context) (*Session, error) {
	port, err := Discover(in.PID)
	if err != nil {
		return nil, err
	}
	targets, err := PageTargets(ctx, port)
	if err != nil {
		return nil, err
	}
	target := SelectTarget(targets, in.WindowTitle)
	if target == nil {
		return nil, ErrUnavailable
	}
	return Dial(ctx, target.WebSocketURL)
}
