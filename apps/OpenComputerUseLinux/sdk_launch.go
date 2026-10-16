package main

import (
	"context"
	"os"
	"os/exec"

	sdk "github.com/CherryHQ/cherry-computer-use/packages/runtime-go"
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

func (d *linuxDesktop) LaunchApp(ctx context.Context, identity sdk.AppIdentity) error {
	current, err := d.ResolveAppIdentity(ctx, identity.Query)
	if err != nil {
		return err
	}
	if current.ID != identity.ID {
		return sdk.Error("TARGET_UNAVAILABLE", "Application identity changed before launch")
	}
	gio, err := exec.LookPath("gio")
	if err != nil {
		return sdk.Error("DEPENDENCY_MISSING", "GIO (gio launch) is required to launch desktop applications")
	}
	if ctx.Err() != nil {
		return sdk.Error("CANCELLED", "Application launch cancelled before dispatch")
	}
	// GIO handles field codes, quoting, working directory and D-Bus activation.
	// Unset stdio prevents a launched application's lifetime from owning SDK pipes.
	command := exec.CommandContext(ctx, gio, "launch", current.Query)
	command.Env = append(os.Environ(), "GIO_LAUNCHED_DESKTOP_FILE="+current.Query)
	if err := command.Run(); err != nil {
		return &sdk.DomainError{Code: "TARGET_UNAVAILABLE", Message: "GIO application launch failed: " + err.Error(), Effect: "possible"}
	}
	return nil
}
