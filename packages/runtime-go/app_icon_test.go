package sdkruntime

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/png"
	"testing"
)

type iconFixture struct {
	desktopFixture
	data []byte
}

func (f *iconFixture) AppIcon(context.Context, string) ([]byte, error) { return f.data, nil }

func TestAppIconDoesNotAcquireControlState(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 64, 64))); err != nil {
		t.Fatal(err)
	}
	d := NewDesktop(&iconFixture{data: encoded.Bytes()})
	value, err := desktopCall(t, d, "getAppIcon", map[string]any{"identity": "installed-app"})
	if err != nil {
		t.Fatal(err)
	}
	result := value.(map[string]any)
	if result["status"] != "available" {
		t.Fatalf("missing icon: %v", result)
	}
	icon := result["icon"].(map[string]string)
	data, err := base64.StdEncoding.DecodeString(icon["dataBase64"])
	if err != nil || !bytes.Equal(data, encoded.Bytes()) || icon["mimeType"] != "image/png" {
		t.Fatal("icon bytes changed")
	}
	if len(d.apps) != 0 || len(d.sessions) != 0 {
		t.Fatal("presentation query acquired control state")
	}
}

func TestAppIconRejectsUnusableImages(t *testing.T) {
	var wrongSize bytes.Buffer
	_ = png.Encode(&wrongSize, image.NewNRGBA(image.Rect(0, 0, 128, 128)))
	for _, data := range [][]byte{nil, []byte("not a PNG"), make([]byte, 49153), wrongSize.Bytes()} {
		d := NewDesktop(&iconFixture{data: data})
		value, err := desktopCall(t, d, "getAppIcon", map[string]any{"identity": "installed-app"})
		if err != nil || value.(map[string]any)["status"] != "unavailable" {
			t.Fatalf("unusable image escaped: %v %v", value, err)
		}
	}
}
