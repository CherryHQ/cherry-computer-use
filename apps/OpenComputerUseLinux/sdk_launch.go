package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"

	sdk "github.com/CherryHQ/cherry-computer-use/packages/runtime-go"
	"github.com/godbus/dbus/v5"
	"github.com/iFurySt/open-codex-computer-use/apps/opencomputeruselinux/internal/desktopentry"
)

func (d *linuxDesktop) ResolveAppIdentity(ctx context.Context, query string) (sdk.AppIdentity, error) {
	entries, err := desktopentry.Load(ctx, desktopentry.Roots())
	if err != nil {
		return sdk.AppIdentity{}, sdk.Error("TARGET_UNAVAILABLE", err.Error())
	}
	entry, err := desktopentry.Resolve(entries, query)
	if err != nil {
		return sdk.AppIdentity{}, sdk.Error("TARGET_UNAVAILABLE", err.Error())
	}
	return sdk.AppIdentity{ID: entry.Identity(), Name: entry.Name, Query: entry.Path}, nil
}

// LaunchApp follows the Desktop Entry Specification on every desktop: a
// DBusActivatable entry is activated over the session bus and Exec is the
// fallback when no service is installed; other entries run Exec directly.
func (d *linuxDesktop) LaunchApp(ctx context.Context, identity sdk.AppIdentity) error {
	current, err := d.ResolveAppIdentity(ctx, identity.Query)
	if err != nil {
		return err
	}
	if current.ID != identity.ID {
		return sdk.Error("TARGET_UNAVAILABLE", "Application identity changed before launch")
	}
	entry, err := desktopentry.Read(current.Query)
	if err != nil {
		return sdk.Error("TARGET_UNAVAILABLE", err.Error())
	}
	if ctx.Err() != nil {
		return sdk.Error("CANCELLED", "Application launch cancelled before dispatch")
	}
	if name, ok := entry.BusName(); ok && entry.DBusActivatable {
		if err := activate(ctx, name); !errors.Is(err, errNoService) {
			return err
		}
		if entry.Exec == "" {
			return sdk.Error("TARGET_UNAVAILABLE", "Application has neither a D-Bus service nor an Exec command")
		}
	}
	return launchExec(entry)
}

// errNoService means the bus had nothing to activate, so nothing was dispatched.
var errNoService = errors.New("no D-Bus service")

// activate calls org.freedesktop.Application.Activate. The reply means the
// application received the request, so the connection may close afterwards.
func activate(ctx context.Context, name string) error {
	session, err := sessionBus(ctx)
	if err != nil {
		return err
	}
	defer session.Close()
	call := session.Object(name, dbus.ObjectPath(desktopentry.BusPath(name))).
		CallWithContext(ctx, "org.freedesktop.Application.Activate", 0, map[string]dbus.Variant{})
	var reply dbus.Error
	if errors.As(call.Err, &reply) && reply.Name == "org.freedesktop.DBus.Error.ServiceUnknown" {
		return errNoService
	}
	if call.Err != nil {
		return &sdk.DomainError{Code: "TARGET_UNAVAILABLE", Message: "D-Bus application activation failed: " + call.Err.Error(), Effect: "possible"}
	}
	return nil
}

func sessionBus(ctx context.Context) (*dbus.Conn, error) {
	var session *dbus.Conn
	var err error
	if address := os.Getenv("DBUS_SESSION_BUS_ADDRESS"); address != "" {
		session, err = dbus.Dial(address, dbus.WithContext(ctx))
	} else {
		session, err = dbus.SessionBusPrivateNoAutoStartup(dbus.WithContext(ctx))
	}
	if err == nil {
		if err = session.Auth(nil); err == nil {
			err = session.Hello()
		}
		if err != nil {
			session.Close()
		}
	}
	if err != nil {
		return nil, sdk.Error("DEPENDENCY_MISSING", "A desktop D-Bus session is required to launch applications")
	}
	return session, nil
}

// launchExec starts the expanded Exec in its own session so the application
// outlives the runtime and never holds its pipes. The GIO launch variables let
// the runtime and desktop shells associate the new process with its entry; the
// POSIX shell only records its own PID before replacing itself with the program.
func launchExec(entry desktopentry.Entry) error {
	if entry.Terminal {
		return sdk.Error("UNSUPPORTED_CAPABILITY", "Terminal applications have no accessible window to launch")
	}
	args, err := entry.Command()
	if err != nil {
		return sdk.Error("TARGET_UNAVAILABLE", err.Error())
	}
	program, err := exec.LookPath(args[0])
	if err != nil {
		return sdk.Error("TARGET_UNAVAILABLE", "Application program is not installed: "+args[0])
	}
	shim := `GIO_LAUNCHED_DESKTOP_FILE_PID=$$; export GIO_LAUNCHED_DESKTOP_FILE_PID; exec "$@"`
	command := exec.Command("/bin/sh", append([]string{"-c", shim, "sh", program}, args[1:]...)...)
	command.Dir = entry.WorkingDirectory
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	for _, variable := range os.Environ() {
		key, _, _ := strings.Cut(variable, "=")
		// The runtime's own launch metadata and startup tokens belong to its host.
		if !strings.HasPrefix(key, "GIO_LAUNCHED_DESKTOP_FILE") && key != "DESKTOP_STARTUP_ID" && key != "XDG_ACTIVATION_TOKEN" {
			command.Env = append(command.Env, variable)
		}
	}
	command.Env = append(command.Env, "GIO_LAUNCHED_DESKTOP_FILE="+entry.Path)
	// The application is launched to be controlled through AT-SPI. Qt, Gecko and
	// Chromium expose their tree only once assistive technology is announced
	// session-wide; these opt-ins announce it to this process alone.
	command.Env = append(command.Env, "QT_LINUX_ACCESSIBILITY_ALWAYS_ON=1", "GNOME_ACCESSIBILITY=1", "ACCESSIBILITY_ENABLED=1")
	if err := command.Start(); err != nil {
		return sdk.Error("TARGET_UNAVAILABLE", "Application launch failed: "+err.Error())
	}
	go command.Wait()
	return nil
}

// busOwners maps the PIDs owning DBusActivatable applications' names to their
// identities. D-Bus activation runs a service command line that differs from
// Exec and carries no launch environment, so the owned name is the reliable
// signal on every desktop. A PID owning several application names maps to "".
func busOwners(ctx context.Context, entries []desktopentry.Entry) map[uint32]string {
	owners := map[uint32]string{}
	session, err := sessionBus(ctx)
	if err != nil {
		return owners
	}
	defer session.Close()
	var names []string
	if session.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.ListNames", 0).Store(&names) != nil {
		return owners
	}
	owned := map[string]bool{}
	for _, name := range names {
		owned[name] = true
	}
	for _, entry := range entries {
		name, ok := entry.BusName()
		if !ok || !entry.DBusActivatable || !owned[name] {
			continue
		}
		var pid uint32
		if session.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetConnectionUnixProcessID", 0, name).Store(&pid) != nil {
			continue
		}
		if previous, seen := owners[pid]; seen && previous != entry.Identity() {
			owners[pid] = ""
		} else {
			owners[pid] = entry.Identity()
		}
	}
	return owners
}
