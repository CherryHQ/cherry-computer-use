package sdkruntime

import (
	"bytes"
	"context"
	"encoding/base64"
	"image/png"
)

// AppIconDriver reads presentation data without resolving a control target.
type AppIconDriver interface {
	AppIcon(context.Context, string) ([]byte, error)
}

// PresentationIdentityDriver also resolves entries available only for presentation.
type PresentationIdentityDriver interface {
	ResolvePresentationIdentity(context.Context, string) (AppIdentity, error)
}

func (d *Desktop) appIcon(ctx context.Context, identity string) (any, error) {
	unavailable := map[string]any{"status": "unavailable"}
	driver, ok := d.driver.(AppIconDriver)
	if !ok {
		return unavailable, nil
	}
	data, err := driver.AppIcon(ctx, identity)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || base64.StdEncoding.EncodedLen(len(data)) > 65536 {
		return unavailable, nil
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width != 64 || config.Height != 64 {
		return unavailable, nil
	}
	return map[string]any{"status": "available", "icon": map[string]string{
		"mimeType": "image/png", "dataBase64": base64.StdEncoding.EncodeToString(data),
	}}, nil
}
