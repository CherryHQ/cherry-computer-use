package x11

import (
	"bytes"
	"context"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

func iconProperty(width, height uint32, pixel uint32) []byte {
	data := make([]byte, 8+int(width*height)*4)
	xgb.Put32(data, width)
	xgb.Put32(data[4:], height)
	for offset := 8; offset < len(data); offset += 4 {
		xgb.Put32(data[offset:], pixel)
	}
	return data
}

func TestWindowIconDecodesARGBAndSelects64Pixels(t *testing.T) {
	data := append(iconProperty(16, 16, 0xffff0000), iconProperty(64, 64, 0x8000ff00)...)
	data = append(data, iconProperty(128, 128, 0xff0000ff)...)
	decoded, err := png.Decode(bytes.NewReader(iconPNG(data)))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Dx() != 64 || decoded.Bounds().Dy() != 64 {
		t.Fatal("icon was not bounded to 64 pixels")
	}
	if got := color.NRGBAModel.Convert(decoded.At(32, 32)).(color.NRGBA); got != (color.NRGBA{G: 255, A: 128}) {
		t.Fatalf("lost selected icon color/alpha: %+v", got)
	}
}

func TestWindowIconRejectsMalformedProperties(t *testing.T) {
	for _, data := range [][]byte{nil, make([]byte, 8), {255, 255, 255, 255, 255, 255, 255, 255}, iconProperty(2, 2, 0xffffffff)[:20], append(iconProperty(2, 2, 0xffffffff), 1)} {
		if len(iconPNG(data)) != 0 {
			t.Fatal("malformed icon was accepted")
		}
	}
}

func TestExecutableWindowIcon(t *testing.T) {
	if os.Getenv("OPEN_COMPUTER_USE_X11_ICON_TEST") != "1" {
		t.Skip("requires an X11 or Xwayland display")
	}
	conn, err := xgb.NewConn()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	window, err := xproto.NewWindowId(conn)
	if err != nil {
		t.Fatal(err)
	}
	screen := xproto.Setup(conn).DefaultScreen(conn)
	if err := xproto.CreateWindowChecked(conn, screen.RootDepth, window, screen.Root, 0, 0, 1, 1, 0, xproto.WindowClassInputOutput, screen.RootVisual, 0, nil).Check(); err != nil {
		t.Fatal(err)
	}
	defer xproto.DestroyWindow(conn, window)
	set := func(name string, data []byte) {
		atom, err := xproto.InternAtom(conn, false, uint16(len(name)), name).Reply()
		if err != nil {
			t.Fatal(err)
		}
		if err := xproto.ChangePropertyChecked(conn, xproto.PropModeReplace, window, atom.Atom, xproto.AtomCardinal, 32, uint32(len(data)/4), data).Check(); err != nil {
			t.Fatal(err)
		}
	}
	pid := make([]byte, 4)
	xgb.Put32(pid, uint32(os.Getpid()))
	set("_NET_WM_PID", pid)
	set("_NET_WM_ICON", iconProperty(32, 32, 0xffff0080))
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(ExecutableIcon(context.Background(), executable)))
	if err != nil {
		t.Fatal(err)
	}
	if got := color.NRGBAModel.Convert(decoded.At(32, 32)).(color.NRGBA); got != (color.NRGBA{R: 255, B: 128, A: 255}) {
		t.Fatalf("wrong process icon: %+v", got)
	}
	if data := ExecutableIcon(context.Background(), filepath.Join(t.TempDir(), "other")); len(data) != 0 {
		t.Fatal("another executable acquired fixture icon")
	}
}
