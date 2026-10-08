package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// A cancelled read or a killed Go owner must not leave its worker/descendant running.
func TestSDKBridgeWorkerOwnership(t *testing.T) {
	for _, ownerDeath := range []bool{false, true} {
		name := "cancel"
		if ownerDeath {
			name = "owner-death"
		}
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			script := `$operation = [Console]::ReadLine() | ConvertFrom-Json
$child = Start-Process powershell.exe -ArgumentList '-NoProfile -NonInteractive -Command "Start-Sleep -Seconds 120"' -PassThru -WindowStyle Hidden
@($PID, $child.Id) | ConvertTo-Json -Compress | Set-Content -Encoding ASCII $operation.pidPath
Start-Sleep -Seconds 120
`
			if err := os.WriteFile(filepath.Join(directory, "sdk.ps1"), []byte(script), 0600); err != nil {
				t.Fatal(err)
			}
			pidPath := filepath.Join(directory, "workers.json")
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			done := make(chan error, 1)
			var owner *exec.Cmd
			if ownerDeath {
				owner = exec.Command(os.Args[0], "-test.run=^TestSDKBridgeOwnerProcess$")
				owner.Env = append(os.Environ(), "CHERRY_SDK_TEST_OWNER="+directory)
				if err := owner.Start(); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { owner.Process.Kill() })
				go func() { done <- owner.Wait() }()
			} else {
				go func() {
					_, err := (&powershellSDKBridge{directory}).Run(ctx, map[string]any{"method": "observe", "pidPath": pidPath})
					done <- err
				}()
			}
			var pids []uint32
			deadline := time.Now().Add(10 * time.Second)
			for time.Now().Before(deadline) {
				data, err := os.ReadFile(pidPath)
				if err == nil && json.Unmarshal(data, &pids) == nil && len(pids) == 2 {
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
			if len(pids) != 2 {
				t.Fatal("owned worker did not start its descendant")
			}
			handles := make([]windows.Handle, 0, len(pids))
			for _, pid := range pids {
				handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { windows.CloseHandle(handle) })
				handles = append(handles, handle)
			}
			if ownerDeath {
				if err := owner.Process.Kill(); err != nil {
					t.Fatal(err)
				}
			} else {
				cancel()
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("interrupted operation reported success")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("owner did not settle")
			}
			for i, handle := range handles {
				status, err := windows.WaitForSingleObject(handle, 3000)
				if err != nil || status != windows.WAIT_OBJECT_0 {
					t.Fatalf("worker %d survived owner cleanup: status=%d err=%v", pids[i], status, err)
				}
			}
		})
	}
}

func TestSDKBridgeOwnerProcess(t *testing.T) {
	directory := os.Getenv("CHERRY_SDK_TEST_OWNER")
	if directory == "" {
		return
	}
	_, err := (&powershellSDKBridge{directory}).Run(context.Background(), map[string]any{"method": "observe", "pidPath": filepath.Join(directory, "workers.json")})
	t.Fatalf("owned bridge returned before test owner was killed: %v", err)
}
