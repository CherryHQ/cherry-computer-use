//go:build !windows && !linux && !darwin

package webcdp

import "errors"

// commandLine is not implemented for this platform, so discovery always reports ErrUnavailable.
func commandLine(pid int) (string, error) {
	return "", errors.New("webcdp: command line discovery is unsupported on this platform")
}
