//go:build !windows

package main

import sdk "github.com/CherryHQ/cherry-computer-use/packages/runtime-go"

func newSDKBridge() (sdkBridge, error) {
	return nil, sdk.Error("UNSUPPORTED_PLATFORM", "UI Automation requires Windows")
}
