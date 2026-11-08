//go:build linux

package webcdp

import (
	"fmt"
	"os"
	"strings"
)

// commandLine reads /proc/<pid>/cmdline, which stores the arguments separated by NUL bytes.
func commandLine(pid int) (string, error) {
	content, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return "", err
	}
	arguments := strings.Split(strings.TrimRight(string(content), "\x00"), "\x00")
	return strings.Join(arguments, " "), nil
}
