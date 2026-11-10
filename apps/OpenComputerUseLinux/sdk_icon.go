package main

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/iFurySt/open-codex-computer-use/apps/opencomputeruselinux/internal/desktopentry"
)

type iconRequest struct {
	name   string
	result chan []byte
}

var iconWorker = sync.OnceValue(func() chan iconRequest {
	requests := make(chan iconRequest)
	go func() {
		// GTK initialization and every subsequent query belong to one OS thread.
		runtime.LockOSThread()
		load := loadIconLibrary()
		for request := range requests {
			var data []byte
			if load != nil {
				data = load(request.name)
			}
			request.result <- data
		}
	}()
	return requests
})

func (d *linuxDesktop) AppIcon(ctx context.Context, identity string) ([]byte, error) {
	path, ok := strings.CutPrefix(identity, "linux:desktop:")
	if !ok || !filepath.IsAbs(path) || !strings.HasSuffix(path, ".desktop") {
		return nil, nil
	}
	entry, err := desktopentry.Read(path)
	if err != nil || entry.Identity() != identity || entry.Icon == "" {
		return nil, nil
	}
	request := iconRequest{entry.Icon, make(chan []byte, 1)}
	select {
	case iconWorker() <- request:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case data := <-request.result:
		return data, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
