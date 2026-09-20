package main

import (
	"context"
	"errors"
	"net"
	"testing"

	sdk "github.com/CherryHQ/cherry-computer-use/packages/runtime-go"
	"github.com/godbus/dbus/v5"
	"github.com/jezek/xgb/xproto"
)

func TestCaptureDecodesDepth32Windows(t *testing.T) {
	cases := []struct {
		name   string
		order  byte
		depth  byte
		width  uint16
		height uint16
		data   []byte
		want   bool
	}{
		{"plain depth 24 window", xproto.ImageOrderLSBFirst, 24, 320, 160, make([]byte, 320*160*4), true},
		{"ARGB depth 32 window", xproto.ImageOrderLSBFirst, 32, 320, 160, make([]byte, 320*160*4), true},
		{"depth 16 window", xproto.ImageOrderLSBFirst, 16, 320, 160, make([]byte, 320*160*2), false},
		{"big endian server", xproto.ImageOrderMSBFirst, 24, 320, 160, make([]byte, 320*160*4), false},
		{"short reply", xproto.ImageOrderLSBFirst, 24, 320, 160, make([]byte, 320*160*4-4), false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := decodablePixelFormat(testCase.order, testCase.depth, testCase.width, testCase.height, testCase.data); got != testCase.want {
				t.Fatalf("decodablePixelFormat(depth=%d, order=%v) = %v, want %v", testCase.depth, testCase.order, got, testCase.want)
			}
		})
	}
}

func TestUnreadableDesktopIsNotReportedAsEmpty(t *testing.T) {
	reasons := []error{errors.New(":1.25: the accessible name is unavailable")}
	var domain *sdk.DomainError
	if err := emptyDesktopError(3, 0, reasons); !errors.As(err, &domain) || domain.Code != "TARGET_UNAVAILABLE" {
		t.Fatalf("registered but unreadable applications must not look like an empty desktop: %v", err)
	}
	if err := emptyDesktopError(3, 1, reasons); err != nil {
		t.Fatalf("a partially readable desktop must still list what it could read: %v", err)
	}
	if err := emptyDesktopError(0, 0, nil); err != nil {
		t.Fatalf("a desktop with no registered applications is legitimately empty: %v", err)
	}
}

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
