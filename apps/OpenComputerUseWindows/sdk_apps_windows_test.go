package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/CherryHQ/cherry-computer-use/packages/runtime-go"
)

func TestSDKExecutableIdentityAndMismatchBeforeLaunch(t *testing.T) {
	d := &windowsDesktop{}
	t.Cleanup(func() { _ = d.Close(context.Background()) })
	executable, err := exec.LookPath("powershell.exe")
	if err != nil {
		t.Fatal(err)
	}
	identity, err := d.ResolveAppIdentity(context.Background(), executable)
	if err != nil {
		t.Fatal(err)
	}
	byName, err := d.ResolveAppIdentity(context.Background(), strings.ToUpper(executable))
	if err != nil || byName.ID != identity.ID {
		t.Fatalf("executable aliases disagree: %+v %v", byName, err)
	}
	identity.ID = "different.application"
	err = d.LaunchApp(context.Background(), identity)
	if domain, ok := err.(*sdk.DomainError); !ok || domain.Code != "TARGET_UNAVAILABLE" || domain.Effect != "none" {
		t.Fatalf("identity mismatch reached dispatch: %v", err)
	}
}

func TestSDKPackagedAppIdentity(t *testing.T) {
	d := &windowsDesktop{}
	t.Cleanup(func() { _ = d.Close(context.Background()) })
	identity, err := d.ResolveAppIdentity(context.Background(), "Calculator")
	if err != nil {
		t.Skipf("Calculator is not installed: %v", err)
	}
	byID, err := d.ResolveAppIdentity(context.Background(), identity.Query)
	if err != nil || identity.ID != byID.ID || !strings.HasPrefix(identity.ID, "win32:aumid:") {
		t.Fatalf("packaged aliases disagree: %+v %v", byID, err)
	}
}

func TestSDKExecutableSymlinkIdentity(t *testing.T) {
	executable, _ := exec.LookPath("powershell.exe")
	alias := filepath.Join(t.TempDir(), "alias.exe")
	if err := os.Symlink(executable, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	d := &windowsDesktop{}
	t.Cleanup(func() { _ = d.Close(context.Background()) })
	first, err := d.ResolveAppIdentity(context.Background(), executable)
	if err != nil {
		t.Fatal(err)
	}
	second, err := d.ResolveAppIdentity(context.Background(), alias)
	if err != nil || first.ID != second.ID {
		t.Fatalf("symlink bypassed identity: %+v %v", second, err)
	}
}

func TestSDKColdLaunchExecutable(t *testing.T) {
	if os.Getenv("COMPUTER_USE_WINDOWS_LAUNCH_TEST") != "1" {
		t.Skip("requires an interactive Windows desktop")
	}
	root := t.TempDir()
	source := filepath.Join(root, "fixture.cs")
	executable := filepath.Join(root, "启动 fixture.exe")
	code := `using System.Windows.Forms;
public class LaunchFixture { [System.STAThread] public static void Main() {
    var form = new Form { Text = "SDK Cold Launch", Width = 320, Height = 200 };
    form.Controls.Add(new Button { Text = "Launch ready", Dock = DockStyle.Fill });
    Application.Run(form);
} }`
	if err := os.WriteFile(source, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	compiler := filepath.Join(os.Getenv("WINDIR"), "Microsoft.NET", "Framework64", "v4.0.30319", "csc.exe")
	if output, err := exec.Command(compiler, "/nologo", "/target:winexe", "/reference:System.Windows.Forms.dll", "/reference:System.Drawing.dll", "/out:"+executable, source).CombinedOutput(); err != nil {
		t.Fatalf("fixture compilation: %v %s", err, output)
	}
	driver := &windowsDesktop{}
	t.Cleanup(func() { _ = driver.Close(context.Background()) })
	identity, err := driver.ResolveAppIdentity(context.Background(), executable)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		targets, _ := driver.Apps(context.Background())
		for _, target := range targets {
			if target.Identity == identity.ID {
				process, _ := os.FindProcess(target.Native.(windowsTarget).PID)
				if process != nil {
					_ = process.Kill()
				}
			}
		}
	})
	desktop := sdk.NewDesktop(driver)
	params, _ := json.Marshal(map[string]any{"query": identity.Query, "expectedIdentity": identity.ID})
	value, err := desktop.Call(context.Background(), "resolveApp", params)
	if err != nil {
		t.Fatal(err)
	}
	app := value.(sdk.App)
	if app.Identity != identity.ID {
		t.Fatal("launched the wrong executable")
	}
	params, _ = json.Marshal(map[string]any{"appId": app.ID})
	value, err = desktop.Call(context.Background(), "openAppSession", params)
	if err != nil {
		t.Fatal(err)
	}
	params, _ = json.Marshal(map[string]any{"appSessionId": value.(sdk.AppSession).ID, "includeScreenshot": false})
	value, err = desktop.Call(context.Background(), "getAppState", params)
	if err != nil {
		t.Fatal(err)
	}
	for _, element := range value.(sdk.Snapshot).Tree.Elements {
		if element.Name == "Launch ready" {
			return
		}
	}
	t.Fatal("launched fixture has no accessible ready button")
}
