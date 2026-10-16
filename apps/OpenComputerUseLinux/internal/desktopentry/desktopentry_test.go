package desktopentry

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func writeEntry(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestDesktopOverridesAndLocalizedNames(t *testing.T) {
	root := t.TempDir()
	user, system := filepath.Join(root, "user"), filepath.Join(root, "system")
	writeEntry(t, filepath.Join(system, "org.example.Calculator.desktop"), "[Desktop Entry]\nType=Application\nName=Calculator\nExec=does-not-run\n")
	writeEntry(t, filepath.Join(user, "org.example.Calculator.desktop"), "[Desktop Entry]\nType=Application\nName=Calculator\nHidden=true\n")
	writeEntry(t, filepath.Join(system, "nested", "editor.desktop"), "[Desktop Entry]\nType=Application\nName=Text\\sEditor\nName[zh_CN]=文本编辑器\nDBusActivatable=true\n[Desktop Action new]\nName=Wrong name\n")
	entries, err := Load(context.Background(), []string{user, system})
	if err != nil || len(entries) != 1 {
		t.Fatalf("override failed: %+v %v", entries, err)
	}
	for _, query := range []string{"文本编辑器", "Text Editor", "nested-editor.desktop"} {
		entry, err := Resolve(entries, query)
		if err != nil || entry.Name != "Text Editor" {
			t.Fatalf("localized/id lookup failed: %+v %v", entry, err)
		}
	}
	if _, err := Resolve(entries, "Calculator"); err == nil {
		t.Fatal("hidden user override exposed system launcher")
	}
}

func TestAmbiguousDesktopNamesRequireExplicitIdentity(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"one.desktop", "two.desktop"} {
		writeEntry(t, filepath.Join(root, id), "[Desktop Entry]\nType=Application\nName=Calculator\nExec=no-effect\n")
	}
	entries, err := Load(context.Background(), []string{root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(entries, "Calculator"); err == nil {
		t.Fatal("ambiguous name selected a launcher")
	}
	if entry, err := Resolve(entries, "one.desktop"); err != nil || entry.ID != "one.desktop" {
		t.Fatalf("explicit selection failed: %+v %v", entry, err)
	}
}

func TestDesktopPathAliasesKeepCanonicalIdentity(t *testing.T) {
	root := t.TempDir()
	path, alias := filepath.Join(root, "editor.desktop"), filepath.Join(root, "alias.desktop")
	writeEntry(t, path, "[Desktop Entry]\nType=Application\nName=Editor\nDBusActivatable=true\n")
	if err := os.Symlink(path, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	first, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Resolve(nil, alias)
	if err != nil || first.Identity() != second.Identity() {
		t.Fatalf("symlink bypassed identity: %+v %v", second, err)
	}
}

func TestProcessAssociationDoesNotConfuseInheritedLaunchEnvironment(t *testing.T) {
	root := t.TempDir()
	launcher := filepath.Join(root, "editor.desktop")
	writeEntry(t, launcher, "[Desktop Entry]\nType=Application\nName=Editor\nDBusActivatable=true\n")
	writeEntry(t, filepath.Join(root, "42", "environ"), "GIO_LAUNCHED_DESKTOP_FILE="+launcher+"\x00GIO_LAUNCHED_DESKTOP_FILE_PID=42\x00")
	entry, _ := Read(launcher)
	if got := ProcessIdentity([]Entry{entry}, root, 42); got != entry.Identity() {
		t.Fatalf("launched process lost identity: %q", got)
	}
	writeEntry(t, filepath.Join(root, "43", "environ"), "GIO_LAUNCHED_DESKTOP_FILE="+launcher+"\x00GIO_LAUNCHED_DESKTOP_FILE_PID=42\x00")
	if got := ProcessIdentity([]Entry{entry}, root, 43); got != "" {
		t.Fatalf("child inherited another application's identity: %q", got)
	}
	writeEntry(t, filepath.Join(root, "44", "root", ".flatpak-info"), "[Application]\nname=editor\n")
	if got := ProcessIdentity([]Entry{entry}, root, 44); got != entry.Identity() {
		t.Fatalf("Flatpak lost desktop identity: %q", got)
	}
}

func TestNonApplicationEntriesCannotBeResolved(t *testing.T) {
	for _, extra := range []string{"Type=Link\nURL=https://example.com", "Type=Application\nHidden=true\nExec=no-effect", "Type=Application"} {
		path := filepath.Join(t.TempDir(), "invalid.desktop")
		writeEntry(t, path, "[Desktop Entry]\nName=Invalid\n"+extra+"\n")
		if _, err := Read(path); err == nil {
			t.Fatalf("invalid launcher accepted: %q", extra)
		}
	}
}

func TestProcessExecutableMatchingChecksLauncherArguments(t *testing.T) {
	root := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(executable)
	if err != nil {
		t.Fatal(err)
	}
	writeEntry(t, filepath.Join(root, "42", "cmdline"), executable+"\x00other-script.py\x00")
	if err := os.Symlink(executable, filepath.Join(root, "42", "exe")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	entry := Entry{Path: "/installed/editor.desktop", Executable: canonical, Arguments: []string{"editor.py"}, MatchExecutable: true}
	if got := ProcessIdentity([]Entry{entry}, root, 42); got != "" {
		t.Fatalf("unrelated interpreter application was selected: %q", got)
	}
	writeEntry(t, filepath.Join(root, "42", "cmdline"), executable+"\x00editor.py\x00")
	if got := ProcessIdentity([]Entry{entry}, root, 42); got != entry.Identity() {
		t.Fatalf("matching application lost identity: %q", got)
	}
}
