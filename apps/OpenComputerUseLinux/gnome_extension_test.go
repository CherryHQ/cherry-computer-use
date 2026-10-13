package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeShellHost records gsettings changes; gnome-extensions fails, as for an
// extension the running shell has not loaded yet.
type fakeShellHost struct {
	enabled  string
	disabled string
	commands []string
}

func (f *fakeShellHost) host(env map[string]string) extensionHost {
	return extensionHost{env: func(key string) string { return env[key] }, run: func(name string, args ...string) (string, error) {
		f.commands = append(f.commands, name+" "+strings.Join(args, " "))
		switch {
		case name == "gnome-extensions":
			return "", errors.New("extension does not exist")
		case len(args) == 3 && args[0] == "get" && args[2] == "enabled-extensions":
			return f.enabled, nil
		case len(args) == 3 && args[0] == "get" && args[2] == "disable-user-extensions":
			return f.disabled, nil
		case len(args) == 4 && args[0] == "set" && args[2] == "enabled-extensions":
			f.enabled = args[3]
			return "", nil
		}
		return "", errors.New("unexpected command")
	}}
}

func TestGNOMEExtensionInstallsEnablesAndRemoves(t *testing.T) {
	data := t.TempDir()
	fake := &fakeShellHost{enabled: "['ding@rastersoft.com']", disabled: "false"}
	host := fake.host(map[string]string{"XDG_DATA_HOME": data, "XDG_CURRENT_DESKTOP": "ubuntu:GNOME"})
	var out bytes.Buffer
	if err := installGNOMEExtension(&out, host); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(data, "gnome-shell", "extensions", gnomeExtensionUUID)
	for _, name := range []string{"metadata.json", "extension.js"} {
		if info, err := os.Stat(filepath.Join(dir, name)); err != nil || info.Size() == 0 {
			t.Fatalf("%s not installed: %v", name, err)
		}
	}
	if fake.enabled != "['ding@rastersoft.com', 'computer-use@cherryai']" {
		t.Fatalf("enabled-extensions = %s", fake.enabled)
	}
	if !strings.Contains(out.String(), "Log out and back in") || strings.Contains(out.String(), "not GNOME") {
		t.Fatalf("install output: %s", out.String())
	}
	// Installing again keeps one entry.
	if err := installGNOMEExtension(&out, host); err != nil || strings.Count(fake.enabled, gnomeExtensionUUID) != 1 {
		t.Fatalf("reinstall: %s, %v", fake.enabled, err)
	}
	out.Reset()
	if err := uninstallGNOMEExtension(&out, host); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("extension directory left behind: %v", err)
	}
	if fake.enabled != "['ding@rastersoft.com']" {
		t.Fatalf("enabled-extensions after removal = %s", fake.enabled)
	}
}

func TestGNOMEExtensionReportsDisabledUserExtensions(t *testing.T) {
	fake := &fakeShellHost{enabled: "@as []", disabled: "true"}
	var out bytes.Buffer
	if err := installGNOMEExtension(&out, fake.host(map[string]string{"XDG_DATA_HOME": t.TempDir(), "XDG_CURRENT_DESKTOP": "KDE"})); err != nil {
		t.Fatal(err)
	}
	if fake.enabled != "['computer-use@cherryai']" {
		t.Fatalf("enabled-extensions = %s", fake.enabled)
	}
	for _, want := range []string{"not GNOME", "User extensions are turned off"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output lacks %q: %s", want, out.String())
		}
	}
	for _, command := range fake.commands {
		if strings.Contains(command, "set org.gnome.shell disable-user-extensions") {
			t.Fatal("the installer must not change disable-user-extensions")
		}
	}
}
