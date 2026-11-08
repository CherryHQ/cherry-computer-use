package main

import (
	"context"
	"time"

	sdk "github.com/CherryHQ/cherry-computer-use/packages/runtime-go"
	"github.com/CherryHQ/cherry-computer-use/packages/runtime-go/webcdp"
)

// resolveWebInput resolves the DevTools page that belongs to this window while observing it. Doing it
// here keeps the action path deterministic: the channel is known before input is sent, instead of
// being inferred from whether typing appeared to work.
func (d *windowsDesktop) resolveWebInput(target sdk.Target, windowTitle string) {
	timeout, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	endpoint, err := webcdp.Resolve(timeout, target.Native.(windowsTarget).PID, windowTitle)
	d.cdpLock.Lock()
	defer d.cdpLock.Unlock()
	if d.cdpEndpoints == nil {
		d.cdpEndpoints = map[string]*webcdp.Endpoint{}
	}
	if err != nil {
		delete(d.cdpEndpoints, target.Key)
		return
	}
	d.cdpEndpoints[target.Key] = endpoint
}

// InputChannel reports the resolved page channel, so observations say where text and keys will go.
func (d *windowsDesktop) InputChannel(_ context.Context, target sdk.Target, _ sdk.Observation) (sdk.InputChannel, error) {
	if endpoint := d.cdpEndpoint(target.Key); endpoint != nil {
		return sdk.PageInputChannel(endpoint.Target.Title), nil
	}
	return sdk.AccessibleInputChannel(), nil
}

func (d *windowsDesktop) cdpEndpoint(key string) *webcdp.Endpoint {
	d.cdpLock.Lock()
	defer d.cdpLock.Unlock()
	return d.cdpEndpoints[key]
}

func (d *windowsDesktop) forgetCdpEndpoint(key string) {
	d.cdpLock.Lock()
	defer d.cdpLock.Unlock()
	delete(d.cdpEndpoints, key)
}

// deliverWebInput sends text and key input to a Chromium or Electron page. A nil error means the page
// took the input; ErrNotDelivered means the page ignored it and the caller must not retry elsewhere.
func deliverWebInput(ctx context.Context, endpoint *webcdp.Endpoint, action sdk.Action) error {
	if !webcdp.Enabled() {
		return webcdp.ErrUnavailable
	}
	timeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	input := webcdp.Input{Endpoint: endpoint}
	switch action.Type {
	case "typeText":
		return input.InsertText(timeout, action.Text)
	case "pressKey":
		return input.PressKey(timeout, action.Key)
	}
	return webcdp.ErrUnavailable
}
