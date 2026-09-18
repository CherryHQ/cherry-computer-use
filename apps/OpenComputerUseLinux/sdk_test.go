package main

import (
	"context"
	"errors"
	"net"
	"testing"

	sdk "github.com/CherryHQ/cherry-computer-use/packages/runtime-go"
	"github.com/godbus/dbus/v5"
)

func TestSDKRejectsDisconnectedBusInsteadOfReusingTargetIdentities(t *testing.T) {
	client, peer := net.Pipe()
	t.Cleanup(func() { peer.Close() })
	bus, err := dbus.NewConn(client)
	if err != nil {
		t.Fatal(err)
	}
	bus.Close()
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/nonexistent-cherry-sdk-test-bus")
	_, err = (&linuxDesktop{bus: bus}).Apps(context.Background())
	var domain *sdk.DomainError
	if !errors.As(err, &domain) || domain.Code != "TARGET_UNAVAILABLE" || domain.Effect != "none" {
		t.Fatalf("closed bus must require a new session, not discover another bus: %v", err)
	}
}
