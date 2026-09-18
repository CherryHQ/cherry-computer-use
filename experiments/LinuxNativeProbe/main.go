package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

type reference struct {
	Bus  string
	Path dbus.ObjectPath
}

func main() {
	window := flag.Uint("window", 0, "X11 fixture window ID")
	output := flag.String("screenshot", "", "PNG output")
	flag.Parse()
	if err := run(uint32(*window), *output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(window uint32, output string) error {
	if window == 0 || output == "" {
		return errors.New("fixture window and screenshot path are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := dbus.ConnectSessionBus(dbus.WithContext(ctx))
	if err != nil {
		return err
	}
	defer session.Close()
	var address string
	if err := session.Object("org.a11y.Bus", "/org/a11y/bus").CallWithContext(ctx, "org.a11y.Bus.GetAddress", 0).Store(&address); err != nil {
		return err
	}
	accessibility, err := dbus.Connect(address, dbus.WithContext(ctx))
	if err != nil {
		return err
	}
	defer accessibility.Close()
	var button reference
	for {
		button, err = findButton(ctx, accessibility)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("AT-SPI fixture not discovered: %w", err)
		case <-time.After(100 * time.Millisecond):
		}
	}
	object := accessibility.Object(button.Bus, button.Path)
	var applied bool
	if err := object.CallWithContext(ctx, "org.a11y.atspi.Action.DoAction", 0, int32(0)).Store(&applied); err != nil {
		return err
	}
	if !applied {
		return errors.New("AT-SPI rejected the fixture action")
	}
	for {
		name, err := accessibleName(ctx, object)
		if err != nil {
			return err
		}
		if name == "Count: 1" {
			break
		}
		select {
		case <-ctx.Done():
			return errors.New("action did not change the fixture counter")
		case <-time.After(25 * time.Millisecond):
		}
	}
	x11, err := xgb.NewConn()
	if err != nil {
		return err
	}
	defer x11.Close()
	geometry, err := xproto.GetGeometry(x11, xproto.Drawable(window)).Reply()
	if err != nil {
		return err
	}
	pixels, err := xproto.GetImage(x11, xproto.ImageFormatZPixmap, xproto.Drawable(window), 0, 0, geometry.Width, geometry.Height, 0xffffffff).Reply()
	if err != nil {
		return err
	}
	setup := xproto.Setup(x11)
	// The experiment explicitly targets Xvfb's little-endian, 24-bit TrueColor visual.
	if setup.ImageByteOrder != xproto.ImageOrderLSBFirst || geometry.Depth != 24 || len(pixels.Data) != int(geometry.Width)*int(geometry.Height)*4 {
		return errors.New("unsupported X11 format for this probe")
	}
	screen := setup.DefaultScreen(x11)
	validVisual := false
	for _, depth := range screen.AllowedDepths {
		for _, visual := range depth.Visuals {
			if visual.VisualId == pixels.Visual && visual.RedMask == 0xff0000 && visual.GreenMask == 0xff00 && visual.BlueMask == 0xff {
				validVisual = true
			}
		}
	}
	if !validVisual {
		return errors.New("unsupported X11 visual for this probe")
	}
	bitmap := image.NewRGBA(image.Rect(0, 0, int(geometry.Width), int(geometry.Height)))
	distinct := map[uint32]bool{}
	for y := 0; y < bitmap.Bounds().Dy(); y++ {
		for x := 0; x < bitmap.Bounds().Dx(); x++ {
			pixel := binary.LittleEndian.Uint32(pixels.Data[(y*bitmap.Bounds().Dx()+x)*4:])
			distinct[pixel] = true
			bitmap.SetRGBA(x, y, color.RGBA{R: byte(pixel >> 16), G: byte(pixel >> 8), B: byte(pixel), A: 255})
		}
	}
	if len(distinct) < 2 {
		return errors.New("captured a blank fixture image")
	}
	file, err := os.Create(output)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := png.Encode(file, bitmap); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"backend": "Go D-Bus + X11", "before": "Count: 0", "after": "Count: 1", "width": geometry.Width, "height": geometry.Height, "colors": len(distinct)})
}

func accessibleName(ctx context.Context, object dbus.BusObject) (string, error) {
	var name dbus.Variant
	err := object.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, "org.a11y.atspi.Accessible", "Name").Store(&name)
	if err != nil {
		return "", err
	}
	value, ok := name.Value().(string)
	if !ok {
		return "", errors.New("invalid accessible name")
	}
	return value, nil
}

func findButton(ctx context.Context, connection *dbus.Conn) (reference, error) {
	queue := []reference{{Bus: "org.a11y.atspi.Registry", Path: "/org/a11y/atspi/accessible/root"}}
	for visited := 0; len(queue) > 0 && visited < 200; visited++ {
		current := queue[0]
		queue = queue[1:]
		object := connection.Object(current.Bus, current.Path)
		name, err := accessibleName(ctx, object)
		if err != nil {
			return reference{}, err
		}
		if name == "Count: 0" {
			return current, nil
		}
		var children []reference
		if err := object.CallWithContext(ctx, "org.a11y.atspi.Accessible.GetChildren", 0).Store(&children); err != nil {
			return reference{}, err
		}
		queue = append(queue, children...)
	}
	return reference{}, errors.New("fixture button not found")
}
