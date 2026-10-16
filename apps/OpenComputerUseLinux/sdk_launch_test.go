//go:build linux

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/CherryHQ/cherry-computer-use/packages/runtime-go"
	"github.com/iFurySt/open-codex-computer-use/apps/opencomputeruselinux/internal/desktop"
)

func TestSDKColdLaunchDesktopEntry(t *testing.T) {
	if os.Getenv("COMPUTER_USE_LINUX_LAUNCH_TEST") != "1" {
		t.Skip("requires an isolated GTK/AT-SPI desktop")
	}
	working, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(t.TempDir(), "启动 fixture.desktop")
	contents := "[Desktop Entry]\nType=Application\nName=SDK Cold Launch\nExec=/usr/bin/python3 \"" + filepath.Join(working, "testdata", "gtk_fixture.py") + "\" \"SDK Cold Launch\"\n"
	if err := os.WriteFile(launcher, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	driver := newLinuxDesktop()
	t.Cleanup(func() { _ = driver.Close(context.Background()) })
	identity, err := driver.ResolveAppIdentity(context.Background(), launcher)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		targets, _ := driver.Apps(context.Background())
		for _, target := range targets {
			if target.Identity == identity.ID {
				process, _ := os.FindProcess(int(target.Native.(desktop.App).PID))
				if process != nil {
					_ = process.Kill()
				}
			}
		}
	})
	runtime := sdk.NewDesktop(driver)
	params, _ := json.Marshal(map[string]any{"query": identity.Query, "expectedIdentity": "different.application"})
	if _, err := runtime.Call(context.Background(), "resolveApp", params); err == nil {
		t.Fatal("mismatched installed identity was accepted")
	}
	params, _ = json.Marshal(map[string]any{"query": identity.Query, "expectedIdentity": identity.ID})
	value, err := runtime.Call(context.Background(), "resolveApp", params)
	if err != nil {
		targets, listingErr := driver.Apps(context.Background())
		for _, target := range targets {
			process := target.Native.(desktop.App)
			environment, _ := os.ReadFile(filepath.Join("/proc", fmt.Sprint(process.PID), "environ"))
			for _, item := range strings.Split(string(environment), "\x00") {
				if strings.HasPrefix(item, "GIO_LAUNCHED_") {
					t.Log(item)
				}
			}
		}
		t.Fatalf("%v; targets: %+v; listing: %v", err, targets, listingErr)
	}
	app := value.(sdk.App)
	if app.Identity != identity.ID {
		t.Fatal("launched a different desktop application")
	}
	params, _ = json.Marshal(map[string]any{"appId": app.ID})
	value, err = runtime.Call(context.Background(), "openAppSession", params)
	if err != nil {
		t.Fatal(err)
	}
	params, _ = json.Marshal(map[string]any{"appSessionId": value.(sdk.AppSession).ID, "includeScreenshot": false})
	value, err = runtime.Call(context.Background(), "getAppState", params)
	if err != nil {
		t.Fatal(err)
	}
	for _, element := range value.(sdk.Snapshot).Tree.Elements {
		if element.Name == "Count: 0" {
			return
		}
	}
	t.Fatal("launched fixture has no accessible counter")
}
