package desktopentry

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

func TestExecExpansionFollowsSpecification(t *testing.T) {
	entry := Entry{Path: "/apps/editor.desktop", Name: "Text Editor", Icon: "editor"}
	for _, test := range []struct{ exec, want string }{
		{`editor %U`, `editor`},
		{`editor --new-window %f`, `editor|--new-window`},
		{`editor --file=%f %d %D %n %N %v %m`, `editor|--file=`},
		{`"/opt/My Apps/editor" "two words" ""`, `/opt/My Apps/editor|two words|`},
		{`sh -c "printf \"%%s\" \$HOME \\\\ \` + "`" + `"`, "sh|-c|printf \"%s\" $HOME \\\\ `"},
		{`editor %i --name=%c %k 100%%`, `editor|--icon|editor|--name=Text Editor|/apps/editor.desktop|100%`},
		{"editor\t--a  --b", `editor|--a|--b`},
		{`/usr/bin/flatpak run --file-forwarding org.example.Editor @@u %U @@`, `/usr/bin/flatpak|run|--file-forwarding|org.example.Editor|@@u|@@`},
	} {
		entry.Exec = test.exec
		args, err := entry.Command()
		if err != nil || strings.Join(args, "|") != test.want {
			t.Errorf("%s: got %q %v, want %q", test.exec, args, err, test.want)
		}
	}
	entry.Exec, entry.Icon = "editor %i", ""
	if args, err := entry.Command(); err != nil || strings.Join(args, "|") != "editor" {
		t.Errorf("%%i without Icon: %q %v", args, err)
	}
	for _, invalid := range []string{`editor %x`, `editor 100%`, `editor "open`, `%U`, `bin/editor`, `editor --icon=%i`} {
		entry.Exec, entry.Icon = invalid, "editor"
		if args, err := entry.Command(); err == nil {
			t.Errorf("%s: accepted as %q", invalid, args)
		}
	}
}

func TestReadKeepsLaunchKeysAfterStringUnescaping(t *testing.T) {
	path := filepath.Join(t.TempDir(), "org.example.Editor.desktop")
	writeEntry(t, path, "[Desktop Entry]\nType=Application\nName=Editor\nExec=sh -c \"echo \\\\\\\\\"\nPath=/work\\sdir\nTerminal=true\nDBusActivatable=true\n")
	entry, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	args, err := entry.Command()
	if err != nil || strings.Join(args, "|") != `sh|-c|echo \` || entry.WorkingDirectory != "/work dir" || !entry.Terminal || !entry.DBusActivatable {
		t.Fatalf("launch keys: %+v %q %v", entry, args, err)
	}
}

func TestBusNameUsesDesktopFileID(t *testing.T) {
	for id, want := range map[string]string{"org.gnome.clocks.desktop": "org.gnome.clocks", "org.example.My-App.desktop": "org.example.My-App"} {
		if name, ok := (Entry{ID: id}).BusName(); !ok || name != want {
			t.Errorf("%s: %q %v", id, name, ok)
		}
	}
	for _, id := range []string{"editor.desktop", "org.9lives.App.desktop", "org..App.desktop", "nested-org.example.App 2.desktop"} {
		if name, ok := (Entry{ID: id}).BusName(); ok {
			t.Errorf("%s: accepted as %q", id, name)
		}
	}
	if got := BusPath("org.example.My-App"); got != "/org/example/My_App" {
		t.Errorf("object path: %s", got)
	}
}

func TestFlatpakIdentitySurvivesUpdates(t *testing.T) {
	root := t.TempDir()
	branch := filepath.Join(root, "app", "org.example.Editor", "x86_64", "stable")
	deploy := func(commit string) {
		writeEntry(t, filepath.Join(branch, commit, "export", "share", "applications", "org.example.Editor.desktop"), "[Desktop Entry]\nType=Application\nName=Editor\nExec=/usr/bin/flatpak run org.example.Editor\n")
		_ = os.Remove(filepath.Join(branch, "active"))
		if err := os.Symlink(commit, filepath.Join(branch, "active")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	export := filepath.Join(root, "exports", "share", "applications", "org.example.Editor.desktop")
	if err := os.MkdirAll(filepath.Dir(export), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(branch, "active", "export", "share", "applications", "org.example.Editor.desktop"), export); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	deploy(strings.Repeat("a", 64))
	before, err := Read(export)
	if err != nil {
		t.Fatal(err)
	}
	deploy(strings.Repeat("b", 64))
	after, err := Read(export)
	if err != nil || after.Identity() != before.Identity() || strings.Contains(before.Path, strings.Repeat("a", 64)) {
		t.Fatalf("update changed identity: %q -> %q %v", before.Identity(), after.Identity(), err)
	}
	// A deployment that is no longer active keeps its own, commit-specific identity.
	stale, err := Read(filepath.Join(branch, strings.Repeat("a", 64), "export", "share", "applications", "org.example.Editor.desktop"))
	if err != nil || stale.Identity() == after.Identity() {
		t.Fatalf("inactive deployment took the active identity: %q %v", stale.Identity(), err)
	}
}
