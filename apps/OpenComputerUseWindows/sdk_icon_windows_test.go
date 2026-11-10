package main

import (
	"bytes"
	"context"
	"image/png"
	"os/exec"
	"testing"
)

func TestSDKAppIcons(t *testing.T) {
	d := &windowsDesktop{}
	t.Cleanup(func() { _ = d.Close(context.Background()) })
	executable, err := exec.LookPath("powershell.exe")
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{executable, "Calculator"} {
		t.Run(query, func(t *testing.T) {
			identity, err := d.ResolveAppIdentity(context.Background(), query)
			if err != nil {
				if query == "Calculator" {
					t.Skipf("Calculator is not installed: %v", err)
				}
				t.Fatal(err)
			}
			data, err := d.AppIcon(context.Background(), identity.ID)
			if err != nil {
				t.Fatal(err)
			}
			icon, err := png.DecodeConfig(bytes.NewReader(data))
			if err != nil || icon.Width != 64 || icon.Height != 64 || len(data) > 49152 {
				t.Fatalf("invalid app icon: %+v %v", icon, err)
			}
		})
	}
	data, err := d.AppIcon(context.Background(), "win32:aumid:not.installed!app")
	if err != nil || len(data) != 0 {
		t.Fatalf("missing app: %v %v", data, err)
	}
}
