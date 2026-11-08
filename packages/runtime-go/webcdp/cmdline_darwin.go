//go:build darwin

package webcdp

import (
	"os/exec"
	"strconv"
	"strings"
)

// commandLine asks ps, because macOS does not expose another process's argv through a file.
func commandLine(pid int) (string, error) {
	output, err := exec.Command("ps", "-o", "command=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
