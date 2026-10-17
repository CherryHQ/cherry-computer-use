package main

import (
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	sdk "github.com/CherryHQ/cherry-computer-use/packages/runtime-go"
	"golang.org/x/sys/windows"
)

func launchWindowsApp(identity sdk.AppIdentity) error {
	var err error
	if strings.HasPrefix(identity.ID, "win32:aumid:") {
		err = activateWindowsApp(identity.Query)
	} else {
		file, conversionErr := windows.UTF16PtrFromString(identity.Query)
		if conversionErr != nil {
			return sdk.Error("INVALID_ARGUMENT", "Invalid executable path")
		}
		err = windows.ShellExecute(0, nil, file, nil, nil, 4)
	}
	if err != nil {
		return &sdk.DomainError{Code: "TARGET_UNAVAILABLE", Message: "Native application launch failed: " + err.Error(), Effect: "possible"}
	}
	return nil
}

func activateWindowsApp(appID string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ole32 := windows.NewLazySystemDLL("ole32.dll")
	result, _, _ := ole32.NewProc("CoInitializeEx").Call(0, 2)
	if int32(result) < 0 {
		return fmt.Errorf("CoInitializeEx: HRESULT %#x", uint32(result))
	}
	defer ole32.NewProc("CoUninitialize").Call()
	classID, _ := windows.GUIDFromString("{45BA127D-10A8-46EA-8AB7-56EA9078943C}")
	interfaceID, _ := windows.GUIDFromString("{2E941141-7F97-4756-BA1D-9DECDE894A3D}")
	var manager *struct{ vtable *[6]uintptr }
	result, _, _ = ole32.NewProc("CoCreateInstance").Call(uintptr(unsafe.Pointer(&classID)), 0, 5, uintptr(unsafe.Pointer(&interfaceID)), uintptr(unsafe.Pointer(&manager)))
	if int32(result) < 0 {
		return fmt.Errorf("CoCreateInstance: HRESULT %#x", uint32(result))
	}
	defer syscall.SyscallN(manager.vtable[2], uintptr(unsafe.Pointer(manager)))
	name, err := windows.UTF16PtrFromString(appID)
	if err != nil {
		return err
	}
	var pid uint32
	result, _, _ = syscall.SyscallN(manager.vtable[3], uintptr(unsafe.Pointer(manager)), uintptr(unsafe.Pointer(name)), 0, 2, uintptr(unsafe.Pointer(&pid)))
	runtime.KeepAlive(name)
	if int32(result) < 0 {
		return fmt.Errorf("ActivateApplication: HRESULT %#x", uint32(result))
	}
	return nil
}
