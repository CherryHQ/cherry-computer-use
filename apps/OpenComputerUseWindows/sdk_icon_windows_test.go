package main

import (
	"bytes"
	"context"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSDKAppIcons(t *testing.T) {
	d := &windowsDesktop{}
	t.Cleanup(func() { _ = d.Close(context.Background()) })
	executable, err := exec.LookPath("powershell.exe")
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{executable, "Calculator"} {
		t.Run(query, func(t *testing.T) {
			identity, err := d.ResolveAppIdentity(context.Background(), query)
			if err != nil {
				if query == "Calculator" {
					t.Skipf("Calculator is not installed: %v", err)
				}
				t.Fatal(err)
			}
			requireWindowsAppIcon(t, d, identity.ID)
		})
	}

	t.Run("AppsFolder-only entry", func(t *testing.T) {
		ctx := context.Background()
		const query = "Microsoft.Windows.ControlPanel"
		identity, err := d.ResolvePresentationIdentity(ctx, query)
		if err != nil || identity.ID != "win32:shell:"+query || identity.Query != query {
			t.Fatalf("resolve Control Panel: %+v %v", identity, err)
		}
		requireWindowsAppIcon(t, d, identity.ID)
		if _, err := d.ResolveAppIdentity(ctx, query); err == nil {
			t.Fatal("Control Panel resolved as a launch target")
		}
	})

	t.Run("TextInputHost", func(t *testing.T) {
		apps, err := d.Apps(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, app := range apps {
			if strings.EqualFold(app.Name, "TextInputHost") && strings.HasPrefix(app.Identity, "win32:aumid:") {
				requireWindowsAppIcon(t, d, app.Identity)
				return
			}
		}
		t.Skip("TextInputHost has no discoverable window with a packaged identity")
	})

	for _, identity := range []string{
		"win32:aumid:nonexistent_abcdefghijklm!app",
		"win32:shell:Microsoft.Windows.DoesNotExist",
	} {
		data, err := d.AppIcon(context.Background(), identity)
		if err != nil || len(data) != 0 {
			t.Fatalf("missing icon for %s: %d bytes, %v", identity, len(data), err)
		}
	}
}

func requireWindowsAppIcon(t *testing.T, d *windowsDesktop, identity string) {
	t.Helper()
	data, err := d.AppIcon(context.Background(), identity)
	if err != nil {
		t.Fatal(err)
	}
	icon, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode icon for %s: %v", identity, err)
	}
	bounds := icon.Bounds()
	if bounds.Dx() != 64 || bounds.Dy() != 64 || len(data) > 49152 {
		t.Fatalf("invalid icon for %s: %v, %d bytes", identity, bounds, len(data))
	}
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if _, _, _, alpha := icon.At(x, y).RGBA(); alpha != 0 {
				return
			}
		}
	}
	t.Fatalf("icon for %s is fully transparent", identity)
}

func TestSDKShellTargetDoesNotAbortIdentityResolution(t *testing.T) {
	script := filepath.Join(t.TempDir(), "identity.ps1")
	source := `$ErrorActionPreference = 'Stop'
` + sdkAppsScript + `
function Get-SDKAppsFolder {
    $folder = New-Object PSObject
    $folder | Add-Member ScriptMethod Items {
        $broken = [pscustomobject]@{ Name = 'Virtual target'; Path = 'Virtual.Target' }
        $broken | Add-Member ScriptMethod ExtendedProperty { param($key) 'shell:virtual' + [char]0 }
        $control = [pscustomobject]@{ Name = 'Control Panel'; Path = 'Microsoft.Windows.ControlPanel' }
        $control | Add-Member ScriptMethod ExtendedProperty { param($key) 'shell:control' + [char]0 }
        @($broken, $control)
    }
    $folder
}
$identity = Resolve-SDKAppIdentity 'Microsoft.Windows.ControlPanel' -AllowShellItems
if ($identity.id -cne 'win32:shell:Microsoft.Windows.ControlPanel') { throw 'Control Panel was not resolved' }
try {
    $null = Resolve-SDKAppIdentity 'Microsoft.Windows.ControlPanel'
} catch {
    if ($_.Exception.Message -like '*uniquely resolved*') { exit 0 }
    throw
}
throw 'Presentation identity became launchable'
`
	if err := os.WriteFile(script, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-File", script)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("Shell identity regression: %v\n%s", err, output)
	}
}
