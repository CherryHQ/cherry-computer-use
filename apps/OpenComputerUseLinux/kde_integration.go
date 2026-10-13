package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/iFurySt/open-codex-computer-use/apps/opencomputeruselinux/internal/display/wayland"
)

// KWin authorizes restricted interfaces for the executable a .desktop file names:
// it matches the canonical first word of Exec= against /proc/<pid>/exe of the
// caller. This file names the Wayland helper, which uses both interfaces.
const kdeDesktopFile = "com.cherryai.computer-use.wayland-helper.desktop"

// kdeHost is what installing touches outside the .desktop file.
type kdeHost struct {
	extensionHost
	// helper is the canonical path of the Wayland helper.
	helper func() (string, error)
	// served reports whether KWin serves the helper both ScreenShot2 and
	// fake_input, and otherwise what is missing.
	served func() (bool, string, error)
	wait   time.Duration
}

func systemKDEHost() kdeHost {
	host := kdeHost{extensionHost: systemExtensionHost(), wait: 15 * time.Second}
	host.helper = func() (string, error) {
		path, err := filepath.Abs(waylandHelperPath())
		if err != nil {
			return "", err
		}
		return filepath.EvalSymlinks(path)
	}
	host.served = func() (bool, string, error) {
		path, err := host.helper()
		if err != nil {
			return false, "", err
		}
		helper, err := wayland.Start(context.Background(), path, os.Environ(), 5*time.Second)
		if err != nil {
			return false, "", err
		}
		defer helper.Close()
		probe := helper.Probe()
		detail := fmt.Sprintf("KWin found: %v, fake_input offered: %v, ScreenShot2 allowed: %v", probe.KWin != nil, probe.KWinInput(), probe.KWinCapture())
		return probe.KWinInput() && probe.KWinCapture(), detail, nil
	}
	return host
}

func (h kdeHost) desktopPath() (string, error) {
	data := h.env("XDG_DATA_HOME")
	if data == "" {
		home := h.env("HOME")
		if home == "" {
			return "", fmt.Errorf("neither XDG_DATA_HOME nor HOME is set")
		}
		data = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(data, "applications", kdeDesktopFile), nil
}

func isKDE(env func(string) string) bool {
	for _, desktop := range strings.Split(env("XDG_CURRENT_DESKTOP"), ":") {
		if strings.EqualFold(desktop, "KDE") {
			return true
		}
	}
	return false
}

// installKDEIntegration authorizes the Wayland helper for KWin's ScreenShot2 and
// fake_input, then waits until KWin honours it. Nothing needs a new login.
func installKDEIntegration(stdout io.Writer, host kdeHost) error {
	helper, err := host.helper()
	if err != nil {
		return fmt.Errorf("cannot find the Wayland helper: %w", err)
	}
	if info, err := os.Stat(helper); err != nil || info.Mode()&0o111 == 0 {
		return fmt.Errorf("the Wayland helper at %s is missing or not executable", helper)
	}
	path, err := host.desktopPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	entry := "[Desktop Entry]\n" +
		"Type=Application\n" +
		"Name=Cherry Computer Use Wayland helper\n" +
		"Comment=Lets Cherry Computer Use capture windows and send input on KDE Plasma\n" +
		"Exec=" + helper + "\n" +
		"NoDisplay=true\n" +
		"X-KDE-DBUS-Restricted-Interfaces=org.kde.KWin.ScreenShot2\n" +
		"X-KDE-Wayland-Interfaces=org_kde_kwin_fake_input\n"
	if err := os.WriteFile(path, []byte(entry), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Authorized %s for KWin window capture and input in %s.\n", helper, path)
	if _, err := host.run("kbuildsycoca6"); err != nil {
		fmt.Fprintln(stdout, "Note: kbuildsycoca6 did not run; KDE picks the file up when it next rebuilds its application cache.")
	}
	if !isKDE(host.env) {
		fmt.Fprintln(stdout, "Note: this session is not KDE Plasma; the authorization takes effect there.")
		return nil
	}
	// KWin reads the application cache lazily; wait until both interfaces serve the helper.
	start := time.Now()
	deadline := start.Add(host.wait)
	for attempt := 1; ; attempt++ {
		served, detail, err := host.served()
		if served {
			fmt.Fprintln(stdout, "KWin now serves the helper. Reinstall after moving or upgrading Cherry Computer Use, since the authorization names the helper's path.")
			return nil
		}
		if time.Now().After(deadline) {
			if err != nil {
				return fmt.Errorf("KWin did not serve the helper yet: %w", err)
			}
			return fmt.Errorf("KWin did not serve the helper within %s (%d checks; %s); check that this is KDE Plasma 6 and try again", time.Since(start).Round(time.Second), attempt, detail)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// uninstallKDEIntegration removes the authorization.
func uninstallKDEIntegration(stdout io.Writer, host kdeHost) error {
	path, err := host.desktopPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	_, _ = host.run("kbuildsycoca6")
	fmt.Fprintf(stdout, "Removed the KDE authorization %s; KWin stops serving the helper once it rereads its application cache.\n", path)
	return nil
}
