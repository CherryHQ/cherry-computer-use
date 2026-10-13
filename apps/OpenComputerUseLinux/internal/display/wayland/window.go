package wayland

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"

	"github.com/iFurySt/open-codex-computer-use/apps/opencomputeruselinux/internal/display"
)

// WindowsAvailable reports whether the compositor's IPC can locate windows by
// PID and title, which window capture and the virtual pointer rely on.
func (p Probe) WindowsAvailable() bool { return p.Compositor.Sway || p.Compositor.Hyprland }

// CaptureAvailable reports whether the helper can capture one window: Sway
// copies the window's region of its output, Hyprland exports the toplevel.
func (p Probe) CaptureAvailable() display.Availability {
	switch {
	case p.Compositor.Sway && p.HasGlobal("zwlr_screencopy_manager_v1"):
		return display.Available()
	case p.Compositor.Hyprland && p.HasGlobal("hyprland_toplevel_export_manager_v1"):
		return display.Available()
	case p.Compositor.Sway || p.Compositor.Hyprland:
		return display.Unavailable("DEPENDENCY_MISSING", "The compositor offers no screen copy protocol")
	}
	return display.Unsupported("Wayland window capture needs Sway or Hyprland; this compositor is not supported yet")
}

// VirtualPointerAvailable reports whether the helper can move and press the
// pointer: the wlroots virtual pointer, output positions, and IPC naming the
// focused window so a click can be bound to its target.
func (p Probe) VirtualPointerAvailable() display.Availability {
	if !p.WindowsAvailable() {
		return display.Unsupported("Wayland pointer input needs Sway or Hyprland; this compositor is not supported yet")
	}
	if !p.HasGlobal("zwlr_virtual_pointer_manager_v1") || !p.HasGlobal("zxdg_output_manager_v1") || !p.HasGlobal("wl_seat") {
		return display.Unavailable("DEPENDENCY_MISSING", "The compositor offers no virtual pointer")
	}
	return display.Available()
}

// Window is a compositor window found by PID and title; Rect is in logical
// layout coordinates.
type Window struct {
	PID     uint32    `json:"pid"`
	Title   string    `json:"title"`
	Rect    []float64 `json:"rect"`
	Focused bool      `json:"focused"`
	Visible bool      `json:"visible"`
}

func (w Window) Bounds() display.Rect {
	if len(w.Rect) != 4 {
		return display.Rect{}
	}
	return display.Rect{X: w.Rect[0], Y: w.Rect[1], Width: w.Rect[2], Height: w.Rect[3]}
}

// FindWindow locates the one window of target's PID with target's title; the
// helper refuses when none or several match.
func (h *Helper) FindWindow(ctx context.Context, target display.Target) (Window, error) {
	var window Window
	err := h.Call(ctx, "window.find", map[string]any{"pid": target.PID, "title": target.Title}, &window)
	return window, err
}

// wl_shm formats the helper may return. 0 and 1 are ARGB8888 and XRGB8888;
// the others are DRM fourcc codes.
const (
	formatARGB8888 = 0
	formatXRGB8888 = 1
	formatABGR8888 = 0x34324241
	formatXBGR8888 = 0x34324258
)

type capturedFrame struct {
	Data    string `json:"data"`
	Format  uint32 `json:"format"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Stride  int    `json:"stride"`
	YInvert bool   `json:"yInvert"`
}

// Capture captures the target window as a PNG at its logical size, so image
// coordinates are layout offsets from the window's origin on any output scale.
func (h *Helper) Capture(ctx context.Context, target display.Target) (display.Image, error) {
	window, err := h.FindWindow(ctx, target)
	if err != nil {
		return display.Image{}, err
	}
	var frame capturedFrame
	if err := h.Call(ctx, "window.capture", map[string]any{"pid": target.PID, "title": target.Title}, &frame); err != nil {
		return display.Image{}, err
	}
	pixels, err := frame.image()
	if err != nil {
		return display.Image{}, err
	}
	bounds := window.Bounds()
	if width, height := int(bounds.Width+0.5), int(bounds.Height+0.5); width > 0 && height > 0 && (width != frame.Width || height != frame.Height) {
		pixels = downscale(pixels, width, height)
	}
	var out bytes.Buffer
	if err := png.Encode(&out, pixels); err != nil {
		return display.Image{}, display.Fail("CAPTURE_FAILED", "Could not encode the window image: %v", err)
	}
	return display.Image{PNG: out.Bytes(), Width: pixels.Bounds().Dx(), Height: pixels.Bounds().Dy()}, nil
}

// image converts the helper's shared-memory copy to opaque RGBA.
func (f capturedFrame) image() (*image.RGBA, error) {
	data, err := base64.StdEncoding.DecodeString(f.Data)
	if err != nil || f.Width <= 0 || f.Height <= 0 || f.Stride < f.Width*4 || len(data) < f.Stride*f.Height {
		return nil, display.Fail("CAPTURE_FAILED", "The helper returned a malformed window image")
	}
	var r, b int
	switch f.Format {
	case formatARGB8888, formatXRGB8888:
		r, b = 2, 0
	case formatABGR8888, formatXBGR8888:
		r, b = 0, 2
	default:
		return nil, display.Fail("CAPTURE_FAILED", "The compositor copied the window in an unsupported pixel format %#x", f.Format)
	}
	pixels := image.NewRGBA(image.Rect(0, 0, f.Width, f.Height))
	for y := 0; y < f.Height; y++ {
		row := y
		if f.YInvert {
			row = f.Height - 1 - y
		}
		source := data[row*f.Stride:]
		target := pixels.Pix[y*pixels.Stride:]
		for x := 0; x < f.Width; x++ {
			target[x*4], target[x*4+1], target[x*4+2], target[x*4+3] = source[x*4+r], source[x*4+1], source[x*4+b], 0xff
		}
	}
	return pixels, nil
}

// downscale averages source pixels into a width x height image.
func downscale(source *image.RGBA, width, height int) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, width, height))
	sw, sh := source.Bounds().Dx(), source.Bounds().Dy()
	for y := 0; y < height; y++ {
		y0, y1 := y*sh/height, max((y+1)*sh/height, y*sh/height+1)
		for x := 0; x < width; x++ {
			x0, x1 := x*sw/width, max((x+1)*sw/width, x*sw/width+1)
			var sum [3]int
			for sy := y0; sy < y1 && sy < sh; sy++ {
				for sx := x0; sx < x1 && sx < sw; sx++ {
					c := source.RGBAAt(sx, sy)
					sum[0], sum[1], sum[2] = sum[0]+int(c.R), sum[1]+int(c.G), sum[2]+int(c.B)
				}
			}
			n := max((min(y1, sh)-y0)*(min(x1, sw)-x0), 1)
			out.SetRGBA(x, y, color.RGBA{uint8(sum[0] / n), uint8(sum[1] / n), uint8(sum[2] / n), 0xff})
		}
	}
	return out
}

// PointerMove moves the virtual pointer to a point in layout coordinates.
func (h *Helper) PointerMove(ctx context.Context, point display.Point) error {
	return h.Call(ctx, "vpointer.move", map[string]any{"x": point.X, "y": point.Y}, nil)
}

// PointerClick clicks count times where the pointer is.
func (h *Helper) PointerClick(ctx context.Context, button string, count int) error {
	return h.Call(ctx, "vpointer.click", map[string]any{"button": button, "count": count}, nil)
}

func (h *Helper) PointerPress(ctx context.Context, button string) error {
	return h.Call(ctx, "vpointer.press", map[string]any{"button": button}, nil)
}

func (h *Helper) PointerRelease(ctx context.Context, button string) error {
	return h.Call(ctx, "vpointer.release", map[string]any{"button": button}, nil)
}
