package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	sdk "github.com/CherryHQ/cherry-computer-use/packages/runtime-go"
	"golang.org/x/sys/windows"
)

type powershellSDKBridge struct{ directory string }

func newSDKBridge() (sdkBridge, error) {
	directory, err := os.MkdirTemp("", "cherry-cua-sdk-")
	if err != nil {
		return nil, err
	}
	for name, content := range map[string]string{"runtime.ps1": windowsRuntimeScript, "sdk.ps1": sdkWindowsScript} {
		if err = os.WriteFile(filepath.Join(directory, name), []byte(content), 0600); err != nil {
			os.RemoveAll(directory)
			return nil, err
		}
	}
	return &powershellSDKBridge{directory}, nil
}
func (b *powershellSDKBridge) Close(context.Context) error { return os.RemoveAll(b.directory) }
func (b *powershellSDKBridge) Run(ctx context.Context, operation any) (json.RawMessage, error) {
	action := operation.(map[string]any)["method"] == "click"
	if ctx.Err() != nil {
		return nil, sdk.Error("CANCELLED", "Request cancelled before dispatch")
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(job)
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		return nil, err
	}
	command := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", filepath.Join(b.directory, "sdk.ps1"))
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	input, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	defer input.Close()
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err = command.Start(); err != nil {
		return nil, err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(command.Process.Pid))
	if err == nil {
		err = windows.AssignProcessToJobObject(job, process)
		windows.CloseHandle(process)
	}
	if err != nil {
		command.Process.Kill()
		command.Wait()
		return nil, fmt.Errorf("cannot own SDK worker: %w", err)
	}
	eventName := "Local\\CherryComputerUse-" + sdk.NewID()
	eventNameUTF16, _ := windows.UTF16PtrFromString(eventName)
	event, eventErr := windows.CreateEvent(nil, 1, 0, eventNameUTF16)
	if eventErr != nil {
		windows.TerminateJobObject(job, 1)
		command.Wait()
		return nil, eventErr
	}
	defer windows.CloseHandle(event)
	operation.(map[string]any)["cancelEvent"] = eventName
	cancelled := make(chan struct{})
	stopCancellation := context.AfterFunc(ctx, func() { windows.SetEvent(event); close(cancelled) })
	defer func() {
		if !stopCancellation() {
			<-cancelled
		}
	}()
	payload, err := json.Marshal(operation)
	if err == nil {
		_, err = input.Write(append(payload, '\n'))
	}
	input.Close()
	if err != nil {
		windows.TerminateJobObject(job, 1)
		command.Wait()
		return nil, err
	}
	// Once a semantic call is dispatched, cancellation cannot claim that UIA stopped it.
	waitCtx := ctx
	if action {
		waitCtx = context.WithoutCancel(ctx)
	}
	waitCtx, cancel := context.WithTimeout(waitCtx, 20*time.Second)
	defer cancel()
	terminated := make(chan struct{})
	stop := context.AfterFunc(waitCtx, func() { windows.TerminateJobObject(job, 1); close(terminated) })
	err = command.Wait()
	if !stop() {
		<-terminated
	}
	if err != nil {
		effect := "none"
		if action {
			effect = "possible"
		}
		code := "TARGET_UNAVAILABLE"
		if ctx.Err() != nil {
			code = "CANCELLED"
		}
		return nil, &sdk.DomainError{Code: code, Message: "UI Automation worker did not finish: " + stderr.String(), Effect: effect}
	}
	var response struct {
		OK      bool            `json:"ok"`
		Result  json.RawMessage `json:"result"`
		Code    string          `json:"code"`
		Message string          `json:"message"`
		Effect  string          `json:"effect"`
	}
	if err = json.Unmarshal([]byte(strings.TrimPrefix(stdout.String(), "\ufeff")), &response); err != nil {
		effect := "none"
		if action {
			effect = "possible"
		}
		return nil, &sdk.DomainError{Code: "PROTOCOL_ERROR", Message: "Invalid UI Automation response", Effect: effect}
	}
	if !response.OK {
		return nil, &sdk.DomainError{Code: response.Code, Message: response.Message, Effect: response.Effect}
	}
	return response.Result, nil
}
