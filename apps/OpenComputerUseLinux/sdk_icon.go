package main

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/iFurySt/open-codex-computer-use/apps/opencomputeruselinux/internal/desktopentry"
	"github.com/iFurySt/open-codex-computer-use/apps/opencomputeruselinux/internal/display/x11"
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
	if executable, ok := strings.CutPrefix(identity, "linux:exe:"); ok {
		canonical, err := filepath.EvalSymlinks(executable)
		if err != nil || !filepath.IsAbs(executable) || canonical != executable {
			return nil, nil
		}
		if data := x11.ExecutableIcon(ctx, executable); len(data) > 0 {
			return data, nil
		}
		return loadAppIcon(ctx, strings.ToLower(filepath.Base(executable)))
	}
	path, ok := strings.CutPrefix(identity, "linux:desktop:")
	if !ok || !filepath.IsAbs(path) || !strings.HasSuffix(path, ".desktop") {
		return nil, nil
	}
	entry, err := desktopentry.Read(path)
	if err != nil || entry.Identity() != identity || entry.Icon == "" {
		return nil, nil
	}
	return loadAppIcon(ctx, entry.Icon)
}

func loadAppIcon(ctx context.Context, name string) ([]byte, error) {
	request := iconRequest{name, make(chan []byte, 1)}
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

func executableIdentity(procRoot string, pid uint32) string {
	executable, err := filepath.EvalSymlinks(filepath.Join(procRoot, fmt.Sprint(pid), "exe"))
	if err != nil || !filepath.IsAbs(executable) {
		return ""
	}
	return "linux:exe:" + executable
}
