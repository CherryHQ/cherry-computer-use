// Package wayland runs the Rust Wayland helper (wayland-helper/) for one runtime.
// The helper owns Wayland connections, portal sessions and EIS devices; this side
// only frames requests. End of the helper's input makes it release what it holds
// and exit, so closing or losing this runtime cleans up the helper's state.
package wayland

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iFurySt/open-codex-computer-use/apps/opencomputeruselinux/internal/display"
)

// Protocol is the helper protocol version this runtime speaks.
const Protocol = 1

// Probe is the helper's view of the session; Raw keeps everything it reported.
type Probe struct {
	Wayland struct {
		Connected bool `json:"connected"`
		Globals   []struct {
			Interface string `json:"interface"`
			Version   uint32 `json:"version"`
		} `json:"globals"`
	} `json:"wayland"`
	Portal struct {
		RemoteDesktop *PortalInterface `json:"remote_desktop"`
		Screencast    *PortalInterface `json:"screencast"`
	} `json:"portal"`
	Raw json.RawMessage `json:"-"`
}

type PortalInterface struct {
	Version uint32 `json:"version"`
	Types   uint32 `json:"types"`
}

// HasGlobal reports whether the compositor advertises a Wayland interface.
func (p Probe) HasGlobal(name string) bool {
	for _, global := range p.Wayland.Globals {
		if global.Interface == name {
			return true
		}
	}
	return false
}

// Helper is one running helper process. Calls are serialized; the helper never
// restarts on its own, because state it held is gone with it.
type Helper struct {
	command *exec.Cmd
	input   io.WriteCloser
	exited  chan struct{}
	probe   Probe
	version string

	mu      sync.Mutex
	nextID  uint64
	pending map[uint64]chan reply
	broken  error
}

type reply struct {
	Result json.RawMessage `json:"result"`
	Error  *display.Error  `json:"error"`
}

// Start launches the helper at path and completes the hello handshake within
// the timeout; on failure the process is stopped and DEPENDENCY_MISSING returned.
func Start(ctx context.Context, path string, env []string, timeout time.Duration) (*Helper, error) {
	if path == "" {
		return nil, display.Fail("DEPENDENCY_MISSING", "The Wayland helper is not installed")
	}
	if _, err := os.Stat(path); err != nil {
		return nil, display.Fail("DEPENDENCY_MISSING", "The Wayland helper is not installed at %s", path)
	}
	command := exec.Command(path)
	command.Env = env
	command.Stderr = os.Stderr
	input, err := command.StdinPipe()
	if err != nil {
		return nil, display.Fail("DEPENDENCY_MISSING", "Cannot start the Wayland helper: %v", err)
	}
	output, err := command.StdoutPipe()
	if err != nil {
		return nil, display.Fail("DEPENDENCY_MISSING", "Cannot start the Wayland helper: %v", err)
	}
	if err := command.Start(); err != nil {
		return nil, display.Fail("DEPENDENCY_MISSING", "Cannot start the Wayland helper: %v", err)
	}
	h := &Helper{command: command, input: input, exited: make(chan struct{}), pending: map[uint64]chan reply{}}
	go func() { _ = command.Wait(); close(h.exited) }()
	go h.read(bufio.NewReader(output))

	handshake, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var hello struct {
		Protocol int             `json:"protocol"`
		Version  string          `json:"version"`
		Probe    json.RawMessage `json:"probe"`
	}
	err = h.Call(handshake, "hello", map[string]any{"protocol": Protocol}, &hello)
	if err == nil && hello.Protocol != Protocol {
		err = fmt.Errorf("helper protocol %d", hello.Protocol)
	}
	if err == nil {
		err = json.Unmarshal(hello.Probe, &h.probe)
	}
	if err != nil {
		h.kill()
		return nil, display.Fail("DEPENDENCY_MISSING", "The Wayland helper did not start: %v", err)
	}
	h.probe.Raw, h.version = hello.Probe, hello.Version
	return h, nil
}

func (h *Helper) Probe() Probe            { return h.probe }
func (h *Helper) Version() string         { return h.version }
func (h *Helper) Exited() <-chan struct{} { return h.exited }

// Call sends one request and waits for its reply. A helper that has exited or
// broke its framing fails every call; cancellation returns without waiting.
func (h *Helper) Call(ctx context.Context, method string, params, result any) error {
	h.mu.Lock()
	if h.broken != nil {
		err := h.broken
		h.mu.Unlock()
		return err
	}
	h.nextID++
	id := h.nextID
	replies := make(chan reply, 1)
	h.pending[id] = replies
	body, _ := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	_, err := fmt.Fprintf(h.input, "Content-Length: %d\r\n\r\n%s", len(body), body)
	h.mu.Unlock()
	if err != nil {
		h.fail(display.Fail("TARGET_UNAVAILABLE", "The Wayland helper is not running"))
	}
	select {
	case response := <-replies:
		if response.Error != nil {
			return response.Error
		}
		if result != nil {
			return json.Unmarshal(response.Result, result)
		}
		return nil
	case <-ctx.Done():
		h.mu.Lock()
		delete(h.pending, id)
		h.mu.Unlock()
		return display.Fail("CANCELLED", "Wayland helper request cancelled")
	}
}

func (h *Helper) read(output *bufio.Reader) {
	for {
		body, err := readFrame(output)
		if err != nil {
			h.fail(display.Fail("TARGET_UNAVAILABLE", "The Wayland helper exited"))
			return
		}
		var response struct {
			ID uint64 `json:"id"`
			reply
		}
		if json.Unmarshal(body, &response) != nil {
			h.fail(display.Fail("TARGET_UNAVAILABLE", "The Wayland helper sent an invalid reply"))
			h.kill()
			return
		}
		h.mu.Lock()
		replies := h.pending[response.ID]
		delete(h.pending, response.ID)
		h.mu.Unlock()
		if replies != nil {
			replies <- response.reply
		}
	}
}

// fail answers every waiting call with err and refuses later calls.
func (h *Helper) fail(err *display.Error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.broken == nil {
		h.broken = err
	}
	for id, replies := range h.pending {
		replies <- reply{Error: err}
		delete(h.pending, id)
	}
}

// Close ends the helper's input so it releases its state and exits, then waits
// briefly before killing it.
func (h *Helper) Close() {
	_ = h.input.Close()
	select {
	case <-h.exited:
	case <-time.After(2 * time.Second):
		h.kill()
	}
	h.fail(display.Fail("TARGET_UNAVAILABLE", "The Wayland helper was closed"))
}

func (h *Helper) kill() {
	_ = h.input.Close()
	_ = h.command.Process.Kill()
	<-h.exited
}

func readFrame(reader *bufio.Reader) ([]byte, error) {
	length := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if !strings.HasSuffix(line, "\r\n") {
			return nil, errors.New("invalid frame header")
		}
		if line == "\r\n" {
			break
		}
		if key, value, ok := strings.Cut(strings.TrimSuffix(line, "\r\n"), ":"); ok && strings.EqualFold(key, "Content-Length") {
			if length, err = strconv.Atoi(strings.TrimSpace(value)); err != nil || length <= 0 || length > 64<<20 {
				return nil, errors.New("invalid Content-Length")
			}
		}
	}
	if length < 0 {
		return nil, errors.New("missing Content-Length")
	}
	body := make([]byte, length)
	_, err := io.ReadFull(reader, body)
	return body, err
}
