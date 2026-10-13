package wayland

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/draw"
	"image/png"

	"github.com/iFurySt/open-codex-computer-use/apps/opencomputeruselinux/internal/display"
)

// WindowsAvailable reports whether the compositor's IPC can locate windows by
// PID and title, which window capture and the virtual pointer rely on.
func (p Probe) WindowsAvailable() bool { return p.Compositor.Sway || p.Compositor.Hyprland }

// CaptureAvailable reports whether the helper can capture one window: Sway
// copies the window's region of its output, Hyprland exports the toplevel, and
// the GNOME Shell extension paints the window itself.
func (p Probe) CaptureAvailable() display.Availability {
	// A compositor's own IPC says more than XDG_CURRENT_DESKTOP, which a
	// nested or misconfigured session can inherit from another desktop.
	switch {
	case p.Compositor.Sway && p.HasGlobal("zwlr_screencopy_manager_v1"):
		return display.Available()
	case p.Compositor.Hyprland && p.HasGlobal("hyprland_toplevel_export_manager_v1"):
		return display.Available()
	case p.Compositor.Sway || p.Compositor.Hyprland:
		return display.Unavailable("DEPENDENCY_MISSING", "The compositor offers no screen copy protocol")
	case p.ShellAvailable(), p.KWinCapture():
		return display.Available()
	case p.GNOME():
		return p.ShellMissing()
	case p.KDE():
		return p.KDEMissing()
	}
	return display.Unsupported("Wayland window capture needs Sway or Hyprland; this compositor is not supported yet")
}

// PointerAvailable reports whether pointer input can be bound to a target: the
// GNOME Shell extension and KWin check the window at the point through the
// helper; elsewhere the wlroots virtual pointer needs output positions and IPC
// naming the focused window.
func (p Probe) PointerAvailable() display.Availability {
	if p.AssistedRoute() != "" {
		return display.Available()
	}
	if !p.WindowsAvailable() {
		switch {
		case p.GNOME():
			return p.ShellMissing()
		case p.KDE():
			return p.KDEMissing()
		}
		return display.Unsupported("Wayland pointer input needs Sway or Hyprland; this compositor is not supported yet")
	}
	if !p.HasGlobal("zwlr_virtual_pointer_manager_v1") || !p.HasGlobal("zxdg_output_manager_v1") || !p.HasGlobal("wl_seat") {
		return display.Unavailable("DEPENDENCY_MISSING", "The compositor offers no virtual pointer")
	}
	return display.Available()
}

// Window is a compositor window found by PID and title; Rect is in logical
// layout coordinates. Client is where toolkit window coordinates start: the client
// surface, which on GNOME includes client-side shadows.
type Window struct {
	PID     uint32    `json:"pid"`
	Title   string    `json:"title"`
	Rect    []float64 `json:"rect"`
	Client  []float64 `json:"client"`
	Focused bool      `json:"focused"`
	Visible bool      `json:"visible"`
}

func (w Window) Bounds() display.Rect { return rect(w.Rect) }

// ClientBounds is the client surface; it equals Bounds where clients draw no shadows.
func (w Window) ClientBounds() display.Rect {
	if len(w.Client) != 4 {
		return w.Bounds()
	}
	return rect(w.Client)
}

func rect(values []float64) display.Rect {
	if len(values) != 4 {
		return display.Rect{}
	}
	return display.Rect{X: values[0], Y: values[1], Width: values[2], Height: values[3]}
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
	// PNG is set when the GNOME Shell extension encoded the image; the raw
	// fields describe a shared-memory copy otherwise.
	PNG     string `json:"png"`
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
	bounds := window.Bounds()
	if frame.PNG != "" {
		return scaledPNG(frame, bounds)
	}
	pixels, err := frame.image()
	if err != nil {
		return display.Image{}, err
	}
	if width, height := int(bounds.Width+0.5), int(bounds.Height+0.5); width > 0 && height > 0 && (width != frame.Width || height != frame.Height) {
		pixels = downscale(pixels, width, height)
	}
	var out bytes.Buffer
	if err := png.Encode(&out, pixels); err != nil {
		return display.Image{}, display.Fail("CAPTURE_FAILED", "Could not encode the window image: %v", err)
	}
	return display.Image{PNG: out.Bytes(), Width: pixels.Bounds().Dx(), Height: pixels.Bounds().Dy()}, nil
}

// scaledPNG passes an encoded image through, or rescales it to the window's
// logical size when the output is scaled.
func scaledPNG(frame capturedFrame, bounds display.Rect) (display.Image, error) {
	data, err := base64.StdEncoding.DecodeString(frame.PNG)
	if err != nil {
		return display.Image{}, display.Fail("CAPTURE_FAILED", "The helper returned a malformed window image")
	}
	width, height := int(bounds.Width+0.5), int(bounds.Height+0.5)
	if width <= 0 || height <= 0 || (width == frame.Width && height == frame.Height) {
		return display.Image{PNG: data, Width: frame.Width, Height: frame.Height}, nil
	}
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return display.Image{}, display.Fail("CAPTURE_FAILED", "The helper returned an unreadable window image: %v", err)
	}
	pixels := image.NewRGBA(decoded.Bounds())
	draw.Draw(pixels, pixels.Bounds(), decoded, decoded.Bounds().Min, draw.Src)
	pixels = downscale(pixels, width, height)
	var out bytes.Buffer
	if err := png.Encode(&out, pixels); err != nil {
		return display.Image{}, display.Fail("CAPTURE_FAILED", "Could not encode the window image: %v", err)
	}
	return display.Image{PNG: out.Bytes(), Width: width, Height: height}, nil
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
