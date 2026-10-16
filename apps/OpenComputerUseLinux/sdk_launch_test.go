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
	"github.com/iFurySt/open-codex-computer-use/apps/opencomputeruselinux/internal/desktopentry"
)

func TestSDKColdLaunchDesktopEntry(t *testing.T) {
	if os.Getenv("COMPUTER_USE_LINUX_LAUNCH_TEST") != "1" {
		t.Skip("requires an isolated GTK/AT-SPI desktop")
	}
	working, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(working, "testdata", "gtk_fixture.py")
	t.Run("Exec", func(t *testing.T) {
		launcher := filepath.Join(t.TempDir(), "启动 fixture.desktop")
		writeLaunchFile(t, launcher, "[Desktop Entry]\nType=Application\nName=SDK Cold Launch\nExec=/usr/bin/python3 \""+fixture+"\" \"SDK Cold Launch\" %U\n")
		coldLaunch(t, launcher)
	})
	// Like GNOME applications, the service command line differs from Exec and has
	// no launch environment; a launcher that exits before activation completes
	// leaves the service to quit without a window.
	t.Run("DBusActivatable", func(t *testing.T) {
		data := os.Getenv("XDG_DATA_HOME")
		if !filepath.IsAbs(data) {
			t.Skip("requires the isolated session's private XDG_DATA_HOME")
		}
		const id = "org.cherry.SDKDBusLaunch"
		command := "/usr/bin/python3 \"" + fixture + "\" --application-id " + id + " \"SDK D-Bus Launch\""
		writeLaunchFile(t, filepath.Join(data, "dbus-1", "services", id+".service"), "[D-BUS Service]\nName="+id+"\nExec="+command+" --gapplication-service\n")
		launcher := filepath.Join(data, "applications", id+".desktop")
		writeLaunchFile(t, launcher, "[Desktop Entry]\nType=Application\nName=SDK D-Bus Launch\nDBusActivatable=true\nExec="+command+" --new-window\n")
		coldLaunch(t, launcher)
	})
}

func writeLaunchFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
}

func coldLaunch(t *testing.T, launcher string) {
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

func TestLaunchRecordsForgetReusedProcessIDs(t *testing.T) {
	var records launches
	entry := desktopentry.Entry{Path: "/apps/editor.desktop"}
	records.remember(os.Getpid(), entry)
	if identity, ok := records.identity(uint32(os.Getpid())); !ok || identity != entry.Identity() {
		t.Fatalf("launched process lost identity: %q %v", identity, ok)
	}
	if len(records.entries()) != 1 {
		t.Fatal("launched entry is not offered to discovery")
	}
	// Another process with the same PID has a different start time.
	records.processes[uint32(os.Getpid())] = launch{"0", entry}
	if _, ok := records.identity(uint32(os.Getpid())); ok || len(records.entries()) != 0 {
		t.Fatal("a reused PID inherited the launched identity")
	}
}
