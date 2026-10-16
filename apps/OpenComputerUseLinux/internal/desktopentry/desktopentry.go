// Package desktopentry discovers installed launchers; GIO owns their launch semantics.
package desktopentry

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Entry struct {
	ID, Path, Name, Executable string
	Aliases                    []string
	Arguments                  []string
	MatchExecutable            bool
}

func Roots() []string {
	home := os.Getenv("XDG_DATA_HOME")
	if !filepath.IsAbs(home) {
		user, _ := os.UserHomeDir()
		home = filepath.Join(user, ".local", "share")
	}
	dirs := os.Getenv("XDG_DATA_DIRS")
	if dirs == "" {
		dirs = "/usr/local/share:/usr/share"
	}
	roots := []string{}
	if filepath.IsAbs(home) {
		roots = append(roots, filepath.Join(home, "applications"))
	}
	for _, dir := range filepath.SplitList(dirs) {
		if filepath.IsAbs(dir) {
			roots = append(roots, filepath.Join(dir, "applications"))
		}
	}
	return roots
}

func Load(ctx context.Context, roots []string) ([]Entry, error) {
	seen := map[string]bool{}
	entries := []Entry{}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, file fs.DirEntry, err error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				if os.IsNotExist(err) || os.IsPermission(err) {
					return nil
				}
				return err
			}
			if file.IsDir() || !strings.HasSuffix(path, ".desktop") {
				return nil
			}
			relative, _ := filepath.Rel(root, path)
			id := strings.ReplaceAll(relative, string(filepath.Separator), "-")
			if seen[id] {
				return nil
			}
			seen[id] = true
			entry, err := Read(path)
			if err == nil {
				entry.ID = id
				entries = append(entries, entry)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return entries, nil
}

func Read(path string) (Entry, error) {
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return Entry{}, err
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return Entry{}, err
	}
	file, err := os.Open(canonical)
	if err != nil {
		return Entry{}, err
	}
	defer file.Close()
	values := map[string]string{}
	group := false
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") {
			group = line == "[Desktop Entry]"
			continue
		}
		if !group || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if ok {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	if err := scanner.Err(); err != nil {
		return Entry{}, err
	}
	if values["Type"] != "Application" || values["Name"] == "" || values["Hidden"] == "true" || (values["Exec"] == "" && values["DBusActivatable"] != "true") {
		return Entry{}, fmt.Errorf("not a desktop application launcher")
	}
	if binary := values["TryExec"]; binary != "" {
		if _, err := exec.LookPath(binary); err != nil {
			return Entry{}, err
		}
	}
	unescape := strings.NewReplacer(`\s`, " ", `\n`, "\n", `\t`, "\t", `\r`, "\r", `\\`, `\`)
	entry := Entry{ID: filepath.Base(path), Path: canonical, Name: unescape.Replace(values["Name"])}
	for key, value := range values {
		if key == "Name" || strings.HasPrefix(key, "Name[") {
			entry.Aliases = append(entry.Aliases, unescape.Replace(value))
		}
	}
	// Only use a plain executable token for process association; never interpret Exec.
	if fields := strings.Fields(values["Exec"]); len(fields) > 0 && !strings.ContainsAny(fields[0], `"'\%=`) {
		if binary, err := exec.LookPath(fields[0]); err == nil && (filepath.IsAbs(fields[0]) || !strings.Contains(fields[0], "/")) {
			entry.Executable, _ = filepath.EvalSymlinks(binary)
			entry.MatchExecutable = values["Terminal"] != "true"
			for index, argument := range fields[1:] {
				if index == len(fields)-2 && (argument == "%u" || argument == "%U" || argument == "%f" || argument == "%F") {
					break
				}
				if strings.ContainsAny(argument, `"'\%`) {
					entry.MatchExecutable = false
					break
				}
				entry.Arguments = append(entry.Arguments, argument)
			}
		}
	}
	return entry, nil
}

func (e Entry) Identity() string { return "linux:desktop:" + e.Path }

func Resolve(entries []Entry, query string) (Entry, error) {
	if filepath.IsAbs(query) && strings.HasSuffix(query, ".desktop") {
		return Read(query)
	}
	var matches []Entry
	seen := map[string]bool{}
	for _, entry := range entries {
		base := strings.TrimSuffix(entry.ID, ".desktop")
		aliases := append([]string{entry.ID, base, entry.Path, entry.Executable}, entry.Aliases...)
		if dot := strings.LastIndex(base, "."); dot >= 0 {
			aliases = append(aliases, base[dot+1:])
		}
		if entry.Executable != "" {
			aliases = append(aliases, filepath.Base(entry.Executable))
		}
		for _, alias := range aliases {
			if strings.EqualFold(alias, query) && !seen[entry.Identity()] {
				matches = append(matches, entry)
				seen[entry.Identity()] = true
			}
		}
	}
	if len(matches) != 1 {
		return Entry{}, fmt.Errorf("application could not be uniquely resolved; use a desktop file ID or path")
	}
	return matches[0], nil
}

func ProcessIdentity(entries []Entry, procRoot string, pid uint32) string {
	process := filepath.Join(procRoot, fmt.Sprint(pid))
	environment, _ := os.ReadFile(filepath.Join(process, "environ"))
	for _, variable := range strings.Split(string(environment), "\x00") {
		key, value, _ := strings.Cut(variable, "=")
		if key != "GIO_LAUNCHED_DESKTOP_FILE" && key != "BAMF_DESKTOP_FILE_HINT" {
			continue
		}
		// GIO metadata is inherited by children; only the designated launcher PID owns it.
		if key == "GIO_LAUNCHED_DESKTOP_FILE" {
			prefix := "GIO_LAUNCHED_DESKTOP_FILE_PID="
			for _, item := range strings.Split(string(environment), "\x00") {
				if strings.HasPrefix(item, prefix) && strings.TrimPrefix(item, prefix) != fmt.Sprint(pid) {
					value = ""
				}
			}
		}
		if value != "" {
			if entry, err := Read(value); err == nil {
				return entry.Identity()
			}
		}
	}
	// Flatpak applications may be D-Bus activated and have no GIO launch environment.
	if info, err := os.ReadFile(filepath.Join(process, "root", ".flatpak-info")); err == nil {
		group := false
		for _, line := range strings.Split(string(info), "\n") {
			if strings.HasPrefix(line, "[") {
				group = line == "[Application]"
			}
			if group && strings.HasPrefix(line, "name=") {
				if entry, err := Resolve(entries, strings.TrimPrefix(line, "name=")+".desktop"); err == nil {
					return entry.Identity()
				}
			}
		}
	}
	executable, err := filepath.EvalSymlinks(filepath.Join(process, "exe"))
	if err != nil {
		return ""
	}
	identity := ""
	command, _ := os.ReadFile(filepath.Join(process, "cmdline"))
	arguments := strings.Split(strings.TrimSuffix(string(command), "\x00"), "\x00")
	for _, entry := range entries {
		matches := entry.MatchExecutable && entry.Executable == executable && len(arguments) > len(entry.Arguments)
		for index, argument := range entry.Arguments {
			if index+1 >= len(arguments) || arguments[index+1] != argument {
				matches = false
				break
			}
		}
		if matches {
			if identity != "" && identity != entry.Identity() {
				return ""
			}
			identity = entry.Identity()
		}
	}
	return identity
}
