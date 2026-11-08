//go:build windows

package webcdp

import (
	"encoding/binary"
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

const processCommandLineInformation = 60

var (
	ntdll                         = syscall.NewLazyDLL("ntdll.dll")
	procNtQueryInformationProcess = ntdll.NewProc("NtQueryInformationProcess")
	kernel32                      = syscall.NewLazyDLL("kernel32.dll")
	procOpenProcess               = kernel32.NewProc("OpenProcess")
	procCloseHandle               = kernel32.NewProc("CloseHandle")
)

// unicodeString mirrors the Windows UNICODE_STRING that ProcessCommandLineInformation returns.
type unicodeString struct {
	Length        uint16
	MaximumLength uint16
	_             uint32
	Buffer        uintptr
}

// commandLine reads another process's command line without WMI, so discovery stays fast and does not
// depend on PowerShell.
func commandLine(pid int) (string, error) {
	const processQueryLimitedInformation = 0x1000
	handle, _, callErr := procOpenProcess.Call(processQueryLimitedInformation, 0, uintptr(uint32(pid)))
	if handle == 0 {
		return "", fmt.Errorf("webcdp: open process %d: %v", pid, callErr)
	}
	defer procCloseHandle.Call(handle)

	var needed uint32
	status, _, _ := procNtQueryInformationProcess.Call(
		handle,
		processCommandLineInformation,
		0,
		0,
		uintptr(unsafe.Pointer(&needed)),
	)
	if needed == 0 {
		return "", fmt.Errorf("webcdp: command line of %d unavailable (status 0x%x)", pid, status)
	}
	buffer := make([]byte, needed)
	status, _, _ = procNtQueryInformationProcess.Call(
		handle,
		processCommandLineInformation,
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(needed),
		uintptr(unsafe.Pointer(&needed)),
	)
	if status != 0 {
		return "", fmt.Errorf("webcdp: command line of %d failed (status 0x%x)", pid, status)
	}
	value := (*unicodeString)(unsafe.Pointer(&buffer[0]))
	if value.Buffer == 0 || value.Length == 0 {
		return "", nil
	}
	// The command line lives inside the same buffer; index it instead of following the raw pointer.
	base := uintptr(unsafe.Pointer(&buffer[0]))
	if value.Buffer < base || value.Buffer-base+uintptr(value.Length) > uintptr(len(buffer)) {
		return "", errors.New("webcdp: command line buffer out of range")
	}
	section := buffer[value.Buffer-base : value.Buffer-base+uintptr(value.Length)]
	chars := make([]uint16, len(section)/2)
	for index := range chars {
		chars[index] = binary.LittleEndian.Uint16(section[index*2:])
	}
	return syscall.UTF16ToString(chars), nil
}
