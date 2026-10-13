package main

import (
	"embed"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// The GNOME Shell extension ships inside the runtime so installation needs no
// other files. See gnome-shell-extension/ and docs/design-docs/linux-display-backends.md.
//
//go:embed gnome-shell-extension/computer-use@cherryai
var gnomeExtensionFiles embed.FS

const gnomeExtensionUUID = "computer-use@cherryai"

// extensionHost is what installing touches outside the extension directory.
type extensionHost struct {
	env func(string) string
	// run executes a command and returns its trimmed standard output.
	run func(name string, args ...string) (string, error)
}

func systemExtensionHost() extensionHost {
	return extensionHost{env: os.Getenv, run: func(name string, args ...string) (string, error) {
		out, err := exec.Command(name, args...).Output()
		return strings.TrimSpace(string(out)), err
	}}
}

func (h extensionHost) extensionDir() (string, error) {
	data := h.env("XDG_DATA_HOME")
	if data == "" {
		home := h.env("HOME")
		if home == "" {
			return "", fmt.Errorf("neither XDG_DATA_HOME nor HOME is set")
		}
		data = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(data, "gnome-shell", "extensions", gnomeExtensionUUID), nil
}

// enabledExtensions reads org.gnome.shell enabled-extensions, an "as" GVariant
// printed as "['a', 'b']" or "@as []".
func (h extensionHost) enabledExtensions() ([]string, error) {
	out, err := h.run("gsettings", "get", "org.gnome.shell", "enabled-extensions")
	if err != nil {
		return nil, fmt.Errorf("cannot read enabled GNOME extensions: %w", err)
	}
	list := strings.TrimSpace(strings.TrimPrefix(out, "@as"))
	list = strings.TrimSuffix(strings.TrimPrefix(list, "["), "]")
	var uuids []string
	for _, item := range strings.Split(list, ",") {
		if item = strings.Trim(strings.TrimSpace(item), `'"`); item != "" {
			uuids = append(uuids, item)
		}
	}
	return uuids, nil
}

func (h extensionHost) setEnabledExtensions(uuids []string) error {
	quoted := make([]string, len(uuids))
	for i, uuid := range uuids {
		quoted[i] = "'" + strings.ReplaceAll(uuid, "'", `\'`) + "'"
	}
	_, err := h.run("gsettings", "set", "org.gnome.shell", "enabled-extensions", "["+strings.Join(quoted, ", ")+"]")
	return err
}

func isGNOME(env func(string) string) bool {
	for _, desktop := range strings.Split(env("XDG_CURRENT_DESKTOP"), ":") {
		if strings.EqualFold(desktop, "GNOME") {
			return true
		}
	}
	return false
}

// installGNOMEExtension writes the extension for this user and enables it. GNOME
// loads new extensions on Wayland only at the next login.
func installGNOMEExtension(stdout io.Writer, host extensionHost) error {
	dir, err := host.extensionDir()
	if err != nil {
		return err
	}
	if !isGNOME(host.env) {
		fmt.Fprintln(stdout, "Note: this session is not GNOME; the extension only takes effect in GNOME Shell.")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	root := "gnome-shell-extension/" + gnomeExtensionUUID
	err = fs.WalkDir(gnomeExtensionFiles, root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := gnomeExtensionFiles.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, strings.TrimPrefix(path, root+"/")), data, 0o644)
	})
	if err != nil {
		return fmt.Errorf("cannot write the extension to %s: %w", dir, err)
	}
	fmt.Fprintf(stdout, "Installed the Cherry Computer Use GNOME Shell extension to %s.\n", dir)

	// A running shell that already knows the extension enables it directly;
	// otherwise the setting makes the next login load it.
	if _, err := host.run("gnome-extensions", "enable", gnomeExtensionUUID); err != nil {
		uuids, err := host.enabledExtensions()
		if err != nil {
			return err
		}
		if !contains(uuids, gnomeExtensionUUID) {
			if err := host.setEnabledExtensions(append(uuids, gnomeExtensionUUID)); err != nil {
				return fmt.Errorf("cannot enable the extension: %w", err)
			}
		}
	}
	if disabled, _ := host.run("gsettings", "get", "org.gnome.shell", "disable-user-extensions"); disabled == "true" {
		fmt.Fprintln(stdout, "User extensions are turned off in GNOME; turn them on in the Extensions app (or ask your administrator) before it can load.")
	}
	fmt.Fprintln(stdout, "Log out and back in to load it. The first time Cherry Computer Use needs your windows, GNOME asks you to allow desktop control; stop it from the top bar at any time.")
	return nil
}

// uninstallGNOMEExtension disables the extension and removes its files.
func uninstallGNOMEExtension(stdout io.Writer, host extensionHost) error {
	dir, err := host.extensionDir()
	if err != nil {
		return err
	}
	_, _ = host.run("gnome-extensions", "disable", gnomeExtensionUUID)
	if uuids, err := host.enabledExtensions(); err == nil && contains(uuids, gnomeExtensionUUID) {
		kept := uuids[:0]
		for _, uuid := range uuids {
			if uuid != gnomeExtensionUUID {
				kept = append(kept, uuid)
			}
		}
		if err := host.setEnabledExtensions(kept); err != nil {
			return fmt.Errorf("cannot disable the extension: %w", err)
		}
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Removed the Cherry Computer Use GNOME Shell extension from %s. It stops after you log out.\n", dir)
	return nil
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
