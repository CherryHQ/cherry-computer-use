package main

import (
	"context"
	"time"

	sdkruntime "github.com/CherryHQ/cherry-computer-use/packages/runtime-go"
	"github.com/CherryHQ/cherry-computer-use/packages/runtime-go/webcdp"
)

// deliverWebInput sends text and key input to Chromium and Electron pages through the DevTools
// protocol, before the AT-SPI engine or any global input path is used. It reports whether the page
// confirmed the input; every failure keeps the existing behaviour.
func deliverWebInput(ctx context.Context, pid int, title string, action sdkruntime.Action) bool {
	if !webcdp.Enabled() || (action.Type != "typeText" && action.Type != "pressKey") {
		return false
	}
	timeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	input := webcdp.Input{PID: pid, WindowTitle: title}
	if action.Type == "typeText" {
		return input.InsertText(timeout, action.Text) == nil
	}
	return input.PressKey(timeout, action.Key) == nil
}
