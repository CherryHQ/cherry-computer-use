package main

import (
	"context"
	"time"

	sdk "github.com/CherryHQ/cherry-computer-use/packages/runtime-go"
	"github.com/CherryHQ/cherry-computer-use/packages/runtime-go/webcdp"
)

// deliverWebInput sends text and key input to Chromium and Electron pages through the DevTools
// protocol before the PowerShell bridge is asked. It reports whether the page confirmed the input;
// every failure keeps the existing accessibility path.
func deliverWebInput(ctx context.Context, target windowsTarget, action sdk.Action) bool {
	if !webcdp.Enabled() || (action.Type != "typeText" && action.Type != "pressKey") {
		return false
	}
	timeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	input := webcdp.Input{PID: target.PID, WindowTitle: target.Title}
	if action.Type == "typeText" {
		return input.InsertText(timeout, action.Text) == nil
	}
	return input.PressKey(timeout, action.Key) == nil
}
