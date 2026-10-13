package wayland

import (
	"encoding/base64"
	"image/color"
	"testing"
)

func TestCapturedFramesBecomeOpaqueRGBA(t *testing.T) {
	// Two rows of one pixel each, with stride padding: XRGB8888 is B, G, R, X in memory.
	data := []byte{0x30, 0x20, 0x10, 0x00, 0xee, 0xee, 0xee, 0xee, 0x60, 0x50, 0x40, 0x00, 0xee, 0xee, 0xee, 0xee}
	frame := capturedFrame{Data: base64.StdEncoding.EncodeToString(data), Format: formatXRGB8888, Width: 1, Height: 2, Stride: 8}
	pixels, err := frame.image()
	if err != nil {
		t.Fatal(err)
	}
	if got := pixels.RGBAAt(0, 0); got != (color.RGBA{0x10, 0x20, 0x30, 0xff}) {
		t.Fatalf("XRGB8888 top pixel = %v", got)
	}
	frame.YInvert, frame.Format = true, formatXBGR8888
	pixels, _ = frame.image()
	if got := pixels.RGBAAt(0, 0); got != (color.RGBA{0x60, 0x50, 0x40, 0xff}) {
		t.Fatalf("y-inverted XBGR8888 top pixel = %v", got)
	}
	frame.Format = 0x30335258 // XRGB2101010
	if _, err := frame.image(); err == nil {
		t.Fatal("ten-bit formats are refused, not misread")
	}
	frame.Format, frame.Height = formatXRGB8888, 3
	if _, err := frame.image(); err == nil {
		t.Fatal("a frame shorter than its size is refused")
	}
}

func TestDownscaleAveragesToLogicalSize(t *testing.T) {
	data := make([]byte, 4*2*4)
	for i := 0; i < 4*2; i++ {
		if i%2 == 0 {
			data[i*4], data[i*4+1], data[i*4+2] = 200, 200, 200
		}
	}
	frame := capturedFrame{Data: base64.StdEncoding.EncodeToString(data), Format: formatARGB8888, Width: 4, Height: 2, Stride: 16}
	pixels, err := frame.image()
	if err != nil {
		t.Fatal(err)
	}
	small := downscale(pixels, 2, 1)
	if small.Bounds().Dx() != 2 || small.Bounds().Dy() != 1 {
		t.Fatalf("downscaled to %v", small.Bounds())
	}
	if got := small.RGBAAt(1, 0); got != (color.RGBA{100, 100, 100, 0xff}) {
		t.Fatalf("averaged pixel = %v", got)
	}
}
