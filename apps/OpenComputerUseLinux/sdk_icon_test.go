package main

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSDKAppIcon(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux icon libraries required")
	}
	dir := t.TempDir()
	icon := filepath.Join(dir, "app.png")
	pixels := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			pixels.SetNRGBA(x, y, color.NRGBA{R: 220, A: 255})
		}
	}
	var source bytes.Buffer
	_ = png.Encode(&source, pixels)
	if err := os.WriteFile(icon, source.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(dir, "app.desktop")
	if err := os.WriteFile(launcher, []byte("[Desktop Entry]\nType=Application\nName=Icon Fixture\nExec=/must-not-be-launched\nIcon="+icon+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	d := &linuxDesktop{}
	data, err := d.AppIcon(context.Background(), "linux:desktop:"+launcher)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("GTK 3 icon query returned no image")
	}
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil || decoded.Bounds() != image.Rect(0, 0, 64, 64) {
		t.Fatalf("invalid PNG: %v", err)
	}
	r, g, b, a := decoded.At(32, 32).RGBA()
	if r == 0 || g != 0 || b != 0 || a != 65535 {
		t.Fatal("wrong application icon")
	}
	for _, identity := range []string{"invalid", "linux:desktop:" + filepath.Join(dir, "missing.desktop")} {
		data, err := d.AppIcon(context.Background(), identity)
		if err != nil || len(data) != 0 {
			t.Fatalf("missing identity: %v %v", data, err)
		}
	}
}

func TestSDKAppIconSVG(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux icon libraries required")
	}
	dir := t.TempDir()
	svg := filepath.Join(dir, "app.svg")
	if err := os.WriteFile(svg, []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="48" height="48"><rect width="48" height="48" fill="#00ff00"/></svg>`), 0600); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(dir, "svg.desktop")
	if err := os.WriteFile(launcher, []byte("[Desktop Entry]\nType=Application\nName=SVG\nExec=/must-not-be-launched\nIcon="+svg+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := (&linuxDesktop{}).AppIcon(context.Background(), "linux:desktop:"+launcher)
	if err != nil {
		t.Fatal(err)
	}
	icon, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, a := icon.At(32, 32).RGBA()
	if r != 0 || g != 65535 || b != 0 || a != 65535 || icon.Bounds() != image.Rect(0, 0, 64, 64) {
		t.Fatal("SVG icon did not render correctly")
	}
}

func TestSDKAppIconTheme(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux icon libraries required")
	}
	if os.Getenv("OPEN_COMPUTER_USE_ICON_THEME_TEST") == "" {
		t.Skip("set OPEN_COMPUTER_USE_ICON_THEME_TEST to an installed icon name")
	}
	launcher := filepath.Join(t.TempDir(), "theme.desktop")
	if err := os.WriteFile(launcher, []byte("[Desktop Entry]\nType=Application\nName=Theme\nExec=/must-not-be-launched\nIcon="+os.Getenv("OPEN_COMPUTER_USE_ICON_THEME_TEST")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := (&linuxDesktop{}).AppIcon(context.Background(), "linux:desktop:"+launcher)
	if err != nil {
		t.Fatal(err)
	}
	icon, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || icon.Width != 64 || icon.Height != 64 {
		t.Fatalf("theme lookup failed: %v", err)
	}
}
