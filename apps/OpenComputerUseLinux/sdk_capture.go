package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"time"

	sdk "github.com/CherryHQ/cherry-computer-use/packages/runtime-go"
	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

func captureLinuxWindow(ctx context.Context, pid uint32, title string) sdk.Capture {
	failed := func(message string) sdk.Capture {
		return sdk.Capture{Status: "unavailable", Reason: &sdk.Reason{Code: "CAPTURE_FAILED", Message: message}}
	}
	if os.Getenv("WAYLAND_DISPLAY") != "" || strings.EqualFold(os.Getenv("XDG_SESSION_TYPE"), "wayland") {
		return sdk.Capture{Status: "unavailable", Reason: &sdk.Reason{Code: "UNSUPPORTED_CAPABILITY", Message: "Wayland capture is not connected"}}
	}
	if os.Getenv("DISPLAY") == "" {
		return failed("No X11 display is configured")
	}
	connection, err := xgb.NewConn()
	if err != nil {
		return failed("Cannot connect to the X11 display")
	}
	defer connection.Close()
	captureCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	stop := context.AfterFunc(captureCtx, connection.Close)
	defer stop()
	window, err := findX11Window(connection, pid, title)
	if err != nil {
		return failed(err.Error())
	}
	geometry, err := xproto.GetGeometry(connection, xproto.Drawable(window)).Reply()
	if err != nil || geometry == nil || geometry.Width == 0 || geometry.Height == 0 || int(geometry.Width)*int(geometry.Height) > 16_000_000 {
		return failed("Window geometry is unavailable or exceeds capture budget")
	}
	pixels, err := xproto.GetImage(connection, xproto.ImageFormatZPixmap, xproto.Drawable(window), 0, 0, geometry.Width, geometry.Height, 0xffffffff).Reply()
	if err != nil || pixels == nil {
		return failed("X11 window capture failed")
	}
	setup := xproto.Setup(connection)
	if setup.ImageByteOrder != xproto.ImageOrderLSBFirst || geometry.Depth != 24 || len(pixels.Data) != int(geometry.Width)*int(geometry.Height)*4 {
		return failed("X11 pixel format is unsupported")
	}
	valid := false
	for _, screen := range setup.Roots {
		for _, depth := range screen.AllowedDepths {
			for _, visual := range depth.Visuals {
				if visual.VisualId == pixels.Visual && visual.RedMask == 0xff0000 && visual.GreenMask == 0xff00 && visual.BlueMask == 0xff {
					valid = true
				}
			}
		}
	}
	if !valid {
		return failed("X11 visual is unsupported")
	}
	bitmap := image.NewRGBA(image.Rect(0, 0, int(geometry.Width), int(geometry.Height)))
	for y := 0; y < bitmap.Bounds().Dy(); y++ {
		if captureCtx.Err() != nil {
			return failed("Capture cancelled")
		}
		for x := 0; x < bitmap.Bounds().Dx(); x++ {
			pixel := binary.LittleEndian.Uint32(pixels.Data[(y*bitmap.Bounds().Dx()+x)*4:])
			bitmap.SetRGBA(x, y, color.RGBA{R: byte(pixel >> 16), G: byte(pixel >> 8), B: byte(pixel), A: 255})
		}
	}
	var output bytes.Buffer
	if png.Encode(&output, bitmap) != nil {
		return failed("PNG encoding failed")
	}
	return sdk.Capture{Status: "available", Image: &sdk.Image{MimeType: "image/png", Width: bitmap.Bounds().Dx(), Height: bitmap.Bounds().Dy(), DataBase64: base64.StdEncoding.EncodeToString(output.Bytes())}}
}

func findX11Window(connection *xgb.Conn, pid uint32, title string) (xproto.Window, error) {
	atom := func(name string) xproto.Atom {
		result, err := xproto.InternAtom(connection, true, uint16(len(name)), name).Reply()
		if err != nil || result == nil {
			return 0
		}
		return result.Atom
	}
	pidAtom, nameAtom := atom("_NET_WM_PID"), atom("_NET_WM_NAME")
	if pidAtom == 0 {
		return 0, errors.New("X11 application identity is unavailable")
	}
	queue := []xproto.Window{}
	for _, screen := range xproto.Setup(connection).Roots {
		queue = append(queue, screen.Root)
	}
	matches := []xproto.Window{}
	for visited := 0; len(queue) > 0 && visited < 4096; visited++ {
		window := queue[0]
		queue = queue[1:]
		identity, err := xproto.GetProperty(connection, false, window, pidAtom, xproto.AtomCardinal, 0, 1).Reply()
		if err == nil && identity != nil && identity.Format == 32 && len(identity.Value) == 4 && xgb.Get32(identity.Value) == pid {
			name, _ := xproto.GetProperty(connection, false, window, nameAtom, xproto.GetPropertyTypeAny, 0, 16384).Reply()
			if name == nil || len(name.Value) == 0 {
				name, _ = xproto.GetProperty(connection, false, window, xproto.AtomWmName, xproto.GetPropertyTypeAny, 0, 16384).Reply()
			}
			if name != nil && string(name.Value) == title {
				attributes, _ := xproto.GetWindowAttributes(connection, window).Reply()
				if attributes != nil && attributes.MapState == xproto.MapStateViewable {
					matches = append(matches, window)
				}
			}
		}
		children, err := xproto.QueryTree(connection, window).Reply()
		if err == nil && children != nil {
			queue = append(queue, children.Children...)
		}
	}
	if len(queue) > 0 || len(matches) != 1 {
		return 0, errors.New("Cannot uniquely match the accessible window to an X11 window")
	}
	return matches[0], nil
}
