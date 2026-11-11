package x11

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"path/filepath"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// ExecutableIcon reads a running application's EWMH icon, including Xwayland clients.
func ExecutableIcon(ctx context.Context, executable string) []byte {
	conn, err := xgb.NewConn()
	if err != nil {
		return nil
	}
	defer conn.Close()
	atom := func(name string) xproto.Atom {
		reply, err := xproto.InternAtom(conn, true, uint16(len(name)), name).Reply()
		if err != nil || reply == nil {
			return 0
		}
		return reply.Atom
	}
	pidAtom, iconAtom := atom("_NET_WM_PID"), atom("_NET_WM_ICON")
	if pidAtom == 0 || iconAtom == 0 {
		return nil
	}
	queue := []xproto.Window{}
	for _, screen := range xproto.Setup(conn).Roots {
		queue = append(queue, screen.Root)
	}
	for visited := 0; len(queue) > 0 && visited < 4096 && ctx.Err() == nil; visited++ {
		window := queue[0]
		queue = queue[1:]
		pid, err := xproto.GetProperty(conn, false, window, pidAtom, xproto.AtomCardinal, 0, 1).Reply()
		if err == nil && pid != nil && pid.Format == 32 && len(pid.Value) == 4 {
			file, err := filepath.EvalSymlinks(filepath.Join("/proc", fmt.Sprint(xgb.Get32(pid.Value)), "exe"))
			if err == nil && file == executable {
				icon, err := xproto.GetProperty(conn, false, window, iconAtom, xproto.AtomCardinal, 0, 1024*1024).Reply()
				if err == nil && icon != nil && icon.Format == 32 && icon.BytesAfter == 0 {
					if data := iconPNG(icon.Value); len(data) > 0 {
						return data
					}
				}
			}
		}
		children, err := xproto.QueryTree(conn, window).Reply()
		if err == nil && children != nil {
			queue = append(queue, children.Children...)
		}
	}
	return nil
}

func iconPNG(data []byte) []byte {
	var best []byte
	var width, height uint32
	for len(data) >= 8 {
		w, h := xgb.Get32(data), xgb.Get32(data[4:])
		data = data[8:]
		if w == 0 || h == 0 || uint64(w)*uint64(h) > uint64(len(data)/4) {
			return nil
		}
		count := int(w) * int(h) * 4
		if best == nil || (width < 64 && w > width) || (w >= 64 && w < width) {
			width, height, best = w, h, data[:count]
		}
		data = data[count:]
	}
	if len(data) != 0 || best == nil {
		return nil
	}
	pixels := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			offset := (uint64(y)*uint64(height)/64*uint64(width) + uint64(x)*uint64(width)/64) * 4
			pixel := xgb.Get32(best[offset:])
			pixels.SetNRGBA(x, y, color.NRGBA{R: byte(pixel >> 16), G: byte(pixel >> 8), B: byte(pixel), A: byte(pixel >> 24)})
		}
	}
	var buffer bytes.Buffer
	if png.Encode(&buffer, pixels) != nil || buffer.Len() > 49152 {
		return nil
	}
	return buffer.Bytes()
}
