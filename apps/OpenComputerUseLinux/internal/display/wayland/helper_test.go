package wayland

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/iFurySt/open-codex-computer-use/apps/opencomputeruselinux/internal/display"
)

// The test binary doubles as a scripted helper selected by OCU_FAKE_HELPER.
func TestMain(m *testing.M) {
	if role := os.Getenv("OCU_FAKE_HELPER"); role != "" {
		fakeHelper(role)
		return
	}
	os.Exit(m.Run())
}

func fakeHelper(role string) {
	if file := os.Getenv("OCU_FAKE_PID"); file != "" {
		_ = os.WriteFile(file, []byte(strconv.Itoa(os.Getpid())), 0o600)
	}
	reader := bufio.NewReader(os.Stdin)
	write := func(value any) {
		body, _ := json.Marshal(value)
		fmt.Printf("Content-Length: %d\r\n\r\n%s", len(body), body)
	}
	for calls := 0; ; calls++ {
		body, err := readFrame(reader)
		if err != nil {
			// End of input: a real helper releases its state here.
			if file := os.Getenv("OCU_FAKE_EXIT"); file != "" {
				_ = os.WriteFile(file, []byte("clean"), 0o600)
			}
			os.Exit(0)
		}
		var request struct {
			ID     uint64 `json:"id"`
			Method string `json:"method"`
		}
		_ = json.Unmarshal(body, &request)
		switch {
		case role == "silent":
			continue
		case role == "crash" && calls > 0:
			os.Exit(3)
		case role == "slow" && calls > 0:
			continue
		case request.Method == "hello":
			protocol := Protocol
			if role == "old" {
				protocol = 0
			}
			write(map[string]any{"id": request.ID, "result": map[string]any{
				"protocol": protocol, "version": "test",
				"probe": map[string]any{"wayland": map[string]any{"connected": true, "globals": []any{map[string]any{"interface": "wl_seat", "version": 9}}},
					"portal": map[string]any{"remote_desktop": map[string]any{"version": 2, "types": 3}}},
			}})
		default:
			write(map[string]any{"id": request.ID, "error": map[string]any{"code": "UNSUPPORTED_CAPABILITY", "message": "no", "effect": "none"}})
		}
	}
}

func start(t *testing.T, role string, timeout time.Duration) (*Helper, error, string) {
	t.Helper()
	dir := t.TempDir()
	env := append(os.Environ(), "OCU_FAKE_HELPER="+role, "OCU_FAKE_PID="+filepath.Join(dir, "pid"), "OCU_FAKE_EXIT="+filepath.Join(dir, "exit"))
	h, err := Start(context.Background(), os.Args[0], env, timeout)
	if h != nil {
		t.Cleanup(h.Close)
	}
	return h, err, dir
}

func code(err error) string {
	var native *display.Error
	if errors.As(err, &native) {
		return native.Code
	}
	return fmt.Sprint(err)
}

func alive(t *testing.T, dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(string(data))
	return syscall.Kill(pid, 0) == nil
}

func TestHandshakeProbeAndCleanClose(t *testing.T) {
	h, err, dir := start(t, "ok", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	probe := h.Probe()
	if !probe.Wayland.Connected || !probe.HasGlobal("wl_seat") || probe.Portal.RemoteDesktop.Version != 2 || len(probe.Raw) == 0 {
		t.Fatalf("probe = %+v", probe)
	}
	if c := code(h.Call(context.Background(), "teleport", nil, nil)); c != "UNSUPPORTED_CAPABILITY" {
		t.Fatalf("helper errors must keep their code: %s", c)
	}
	h.Close()
	if data, _ := os.ReadFile(filepath.Join(dir, "exit")); string(data) != "clean" {
		t.Fatal("closing must end the helper's input so it can clean up")
	}
	if c := code(h.Call(context.Background(), "probe", nil, nil)); c != "TARGET_UNAVAILABLE" {
		t.Fatalf("calls after close: %s", c)
	}
}

func TestSilentHelperIsStoppedAtStart(t *testing.T) {
	_, err, dir := start(t, "silent", 200*time.Millisecond)
	if code(err) != "DEPENDENCY_MISSING" {
		t.Fatalf("start = %v", err)
	}
	if alive(t, dir) {
		t.Fatal("a helper that failed its handshake must not keep running")
	}
}

func TestProtocolMismatchIsRefused(t *testing.T) {
	if _, err, _ := start(t, "old", 5*time.Second); code(err) != "DEPENDENCY_MISSING" {
		t.Fatalf("start = %v", err)
	}
}

func TestMissingHelperIsADependencyError(t *testing.T) {
	if _, err := Start(context.Background(), "/nonexistent/open-computer-use-wayland", nil, time.Second); code(err) != "DEPENDENCY_MISSING" {
		t.Fatalf("start = %v", err)
	}
}

func TestCrashFailsCallsAndDoesNotRestart(t *testing.T) {
	h, err, dir := start(t, "crash", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if c := code(h.Call(context.Background(), "probe", nil, nil)); c != "TARGET_UNAVAILABLE" {
			t.Fatalf("call %d after crash = %s", attempt, c)
		}
	}
	<-h.Exited()
	if alive(t, dir) {
		t.Fatal("crashed helper still running")
	}
}

func TestCancelledCallReturnsWithoutWaiting(t *testing.T) {
	h, err, _ := start(t, "slow", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	began := time.Now()
	if c := code(h.Call(ctx, "probe", nil, nil)); c != "CANCELLED" || time.Since(began) > 2*time.Second {
		t.Fatalf("cancelled call = %s after %s", c, time.Since(began))
	}
}
