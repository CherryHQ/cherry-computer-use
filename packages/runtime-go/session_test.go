package sdkruntime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type testBackend struct {
	started    chan struct{}
	cancelled  chan struct{}
	release    chan struct{}
	closed     atomic.Bool
	closeError error
}

func (backend *testBackend) Permissions(ctx context.Context) ([]Permission, error) {
	select {
	case backend.started <- struct{}{}:
	default:
	}
	select {
	case <-ctx.Done():
		if backend.cancelled != nil {
			backend.cancelled <- struct{}{}
		}
		<-backend.release
		return nil, ctx.Err()
	case <-backend.release:
		return []Permission{}, nil
	}
}
func (backend *testBackend) Close(context.Context) error {
	backend.closed.Store(true)
	return backend.closeError
}

type harness struct {
	input  *io.PipeWriter
	output *bufio.Reader
	done   chan error
}

func start(t *testing.T, backend Backend) *harness {
	t.Helper()
	in, input := io.Pipe()
	output, out := io.Pipe()
	h := &harness{input: input, output: bufio.NewReader(output), done: make(chan error, 1)}
	go func() {
		h.done <- Serve(in, out, Config{SessionID: "test-session", Version: "test", Platform: "linux", Backend: backend})
	}()
	t.Cleanup(func() { input.Close(); output.Close() })
	return h
}
func (h *harness) send(t *testing.T, id, method string, params any) {
	t.Helper()
	m := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
	if id != "" {
		m["id"] = id
	}
	if err := writeFrame(h.input, m); err != nil {
		t.Fatal(err)
	}
}
func (h *harness) receive(t *testing.T) response {
	t.Helper()
	data, err := readFrame(h.output)
	if err != nil {
		t.Fatal(err)
	}
	var resp response
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatal(err)
	}
	return resp
}
func (h *harness) initialize(t *testing.T) {
	h.send(t, "init", "initialize", map[string]any{"sessionId": "test-session", "protocolVersion": 2})
	resp := h.receive(t)
	if resp.Error != nil || resp.Result.(map[string]any)["ownership"] != "private" {
		t.Fatalf("invalid handshake: %+v", resp)
	}
}
func finish(t *testing.T, h *harness, wantError bool) {
	t.Helper()
	select {
	case err := <-h.done:
		if (err != nil) != wantError {
			t.Fatalf("unexpected close result: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("runtime did not exit")
	}
}

func TestHandshakeOwnershipAndUnsupportedActions(t *testing.T) {
	h := start(t, LifecycleBackend{})
	h.send(t, "early", "getCapabilities", map[string]any{})
	if h.receive(t).Error.Data["code"] != "PROTOCOL_ERROR" {
		t.Fatal("request before initialization accepted")
	}
	h.send(t, "wrong", "initialize", map[string]any{"sessionId": "another-session", "protocolVersion": 2})
	if h.receive(t).Error.Data["code"] != "PROTOCOL_MISMATCH" {
		t.Fatal("foreign session accepted")
	}
	h.initialize(t)
	h.send(t, "act", "act", map[string]any{"type": "click", "snapshotId": "stale", "x": 0, "y": 0})
	if resp := h.receive(t); resp.Error.Data["code"] != "UNSUPPORTED_CAPABILITY" || resp.Error.Data["effect"] != "none" {
		t.Fatalf("action not rejected: %+v", resp)
	}
	h.send(t, "foreign-close", "shutdown", map[string]any{"sessionId": "another-session"})
	if h.receive(t).Error.Data["code"] != "INVALID_ARGUMENT" {
		t.Fatal("foreign shutdown accepted")
	}
	h.send(t, "close", "shutdown", map[string]any{"sessionId": "test-session"})
	if h.receive(t).Result.(map[string]any)["cleanup"] != "complete" {
		t.Fatal("missing cleanup acknowledgement")
	}
	finish(t, h, false)
}

func TestCancellationWaitsForBackendAndSkipsQueuedWork(t *testing.T) {
	backend := &testBackend{started: make(chan struct{}, 2), cancelled: make(chan struct{}, 1), release: make(chan struct{})}
	h := start(t, backend)
	h.initialize(t)
	h.send(t, "running", "getPermissionStatus", map[string]any{})
	<-backend.started
	h.send(t, "queued", "getPermissionStatus", map[string]any{})
	h.send(t, "", "$/cancelRequest", map[string]any{"id": "queued"})
	h.send(t, "", "$/cancelRequest", map[string]any{"id": "running"})
	select {
	case <-backend.cancelled:
	case <-time.After(time.Second):
		t.Fatal("cancellation did not reach the active request")
	}
	// A capability request cannot finish while the earlier backend still owns execution.
	h.send(t, "close", "shutdown", map[string]any{"sessionId": "test-session"})
	if backend.closed.Load() {
		t.Fatal("backend closed before the active request settled")
	}
	close(backend.release)
	for _, id := range []string{"running", "queued"} {
		resp := h.receive(t)
		if resp.ID != id || resp.Error.Data["code"] != "CANCELLED" {
			t.Fatalf("request was not cancelled: %+v", resp)
		}
	}
	if resp := h.receive(t); resp.ID != "close" || !backend.closed.Load() {
		t.Fatal("shutdown acknowledged before cleanup")
	}
	select {
	case <-backend.started:
		t.Fatal("cancelled queued request executed")
	default:
	}
	finish(t, h, false)
}

func TestEOFClosesBackend(t *testing.T) {
	backend := &testBackend{started: make(chan struct{}, 1), release: make(chan struct{})}
	h := start(t, backend)
	h.initialize(t)
	h.send(t, "running", "getPermissionStatus", map[string]any{})
	<-backend.started
	h.input.Close()
	close(backend.release)
	finish(t, h, false)
	if !backend.closed.Load() {
		t.Fatal("EOF leaked backend resources")
	}
}

func TestCleanupFailureNeverAcknowledgesSuccess(t *testing.T) {
	backend := &testBackend{closeError: errors.New("resource still active")}
	h := start(t, backend)
	h.initialize(t)
	h.send(t, "close", "shutdown", map[string]any{"sessionId": "test-session"})
	if _, err := readFrame(h.output); !errors.Is(err, io.EOF) {
		t.Fatalf("unexpected shutdown acknowledgement: %v", err)
	}
	finish(t, h, true)
}

func TestUnresponsiveBackendCannotClaimCleanShutdown(t *testing.T) {
	backend := &testBackend{started: make(chan struct{}, 1), release: make(chan struct{})}
	defer close(backend.release)
	h := start(t, backend)
	h.initialize(t)
	h.send(t, "running", "getPermissionStatus", map[string]any{})
	<-backend.started
	h.send(t, "close", "shutdown", map[string]any{"sessionId": "test-session"})
	finish(t, h, true)
}

func TestOutputBackpressureDoesNotPreventEOF(t *testing.T) {
	in, input := io.Pipe()
	output, out := io.Pipe()
	defer output.Close()
	backend := &testBackend{}
	done := make(chan error, 1)
	go func() {
		done <- Serve(in, out, Config{SessionID: "s", Version: "v", Platform: "linux", Backend: backend})
	}()
	if err := writeFrame(input, map[string]any{"jsonrpc": "2.0", "id": "init", "method": "initialize", "params": map[string]any{"sessionId": "s", "protocolVersion": 2}}); err != nil {
		t.Fatal(err)
	}
	input.Close()
	select {
	case err := <-done:
		if err != nil || !backend.closed.Load() {
			t.Fatalf("backpressure bypassed cleanup: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("blocked writer prevented cleanup")
	}
}

func TestFramingUsesUTF8BytesAndRejectsAmbiguousLengths(t *testing.T) {
	var wire bytes.Buffer
	for _, value := range []string{"中文🙂", "second"} {
		if err := writeFrame(&wire, map[string]string{"value": value}); err != nil {
			t.Fatal(err)
		}
	}
	reader := bufio.NewReader(&wire)
	for _, want := range []string{"中文🙂", "second"} {
		body, err := readFrame(reader)
		var got map[string]string
		if err != nil || json.Unmarshal(body, &got) != nil || got["value"] != want {
			t.Fatalf("frame corrupted: %s %v", body, err)
		}
	}
	for _, input := range []string{
		"Content-Length: 2\r\nContent-Length: 2\r\n\r\n{}",
		"Content-Length: 67108865\r\n\r\n",
		"Content-Length: -1\r\n\r\n",
		"Content-Length: 2\n\n{}",
		"Content-Length: 2\r\n\r\n",
		"Content-Length: 4\r\n\r\n{}",
	} {
		if _, err := readFrame(bufio.NewReader(strings.NewReader(input))); err == nil || errors.Is(err, io.EOF) {
			t.Fatalf("invalid frame accepted as a message or clean EOF: %q", input)
		}
	}
}
