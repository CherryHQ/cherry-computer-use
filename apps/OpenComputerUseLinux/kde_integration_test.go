package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestKDEIntegrationWritesTheAuthorizationAndWaitsForKWin(t *testing.T) {
	data := t.TempDir()
	helper := filepath.Join(t.TempDir(), "open-computer-use-wayland")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	var commands []string
	checks := 0
	host := kdeHost{
		extensionHost: extensionHost{
			env: func(key string) string {
				return map[string]string{"XDG_DATA_HOME": data, "XDG_CURRENT_DESKTOP": "KDE"}[key]
			},
			run: func(name string, args ...string) (string, error) {
				commands = append(commands, name)
				return "", nil
			},
		},
		helper: func() (string, error) { return helper, nil },
		// KWin rereads its application cache lazily: the second check succeeds.
		served: func() (bool, string, error) { checks++; return checks > 1, "", nil },
		wait:   5 * time.Second,
	}
	var out bytes.Buffer
	if err := installKDEIntegration(&out, host); err != nil {
		t.Fatal(err)
	}
	entry, err := os.ReadFile(filepath.Join(data, "applications", kdeDesktopFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Exec=" + helper + "\n", "X-KDE-DBUS-Restricted-Interfaces=org.kde.KWin.ScreenShot2\n",
		"X-KDE-Wayland-Interfaces=org_kde_kwin_fake_input\n", "NoDisplay=true\n"} {
		if !strings.Contains(string(entry), want) {
			t.Fatalf("desktop file lacks %q:\n%s", want, entry)
		}
	}
	if checks != 2 || len(commands) != 1 || commands[0] != "kbuildsycoca6" || !strings.Contains(out.String(), "KWin now serves the helper") {
		t.Fatalf("checks %d, commands %v, output %s", checks, commands, out.String())
	}
	out.Reset()
	if err := uninstallKDEIntegration(&out, host); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(data, "applications", kdeDesktopFile)); !os.IsNotExist(err) {
		t.Fatalf("authorization left behind: %v", err)
	}
}

func TestKDEIntegrationReportsWhenKWinNeverServes(t *testing.T) {
	helper := filepath.Join(t.TempDir(), "open-computer-use-wayland")
	_ = os.WriteFile(helper, []byte("#!/bin/sh\n"), 0o755)
	host := kdeHost{
		extensionHost: extensionHost{
			env: func(key string) string {
				return map[string]string{"XDG_DATA_HOME": t.TempDir(), "XDG_CURRENT_DESKTOP": "KDE"}[key]
			},
			run: func(string, ...string) (string, error) { return "", errors.New("missing") },
		},
		helper: func() (string, error) { return helper, nil },
		served: func() (bool, string, error) { return false, "fake_input offered: false", nil },
		wait:   0,
	}
	var out bytes.Buffer
	err := installKDEIntegration(&out, host)
	if err == nil || !strings.Contains(err.Error(), "did not serve") || !strings.Contains(err.Error(), "fake_input offered: false") || !strings.Contains(out.String(), "kbuildsycoca6 did not run") {
		t.Fatalf("err %v, output %s", err, out.String())
	}
	// Without the application menu, KDE's cache lists no applications: say so.
	config := t.TempDir()
	host.env = func(key string) string {
		return map[string]string{"XDG_DATA_HOME": t.TempDir(), "XDG_CURRENT_DESKTOP": "KDE", "XDG_CONFIG_DIRS": config, "XDG_MENU_PREFIX": "plasma-"}[key]
	}
	if err := installKDEIntegration(&out, host); err == nil || !strings.Contains(err.Error(), "plasma-applications.menu is missing") {
		t.Fatalf("missing menu: %v", err)
	}
	_ = os.MkdirAll(filepath.Join(config, "menus"), 0o755)
	_ = os.WriteFile(filepath.Join(config, "menus", "plasma-applications.menu"), nil, 0o644)
	if err := installKDEIntegration(&out, host); err == nil || strings.Contains(err.Error(), "menu") {
		t.Fatalf("present menu: %v", err)
	}
	// Outside KDE nothing is waited for.
	host.env = func(key string) string { return map[string]string{"XDG_DATA_HOME": t.TempDir()}[key] }
	out.Reset()
	if err := installKDEIntegration(&out, host); err != nil || !strings.Contains(out.String(), "not KDE Plasma") {
		t.Fatalf("outside KDE: %v %s", err, out.String())
	}
}
