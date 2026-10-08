# Native desktop fixtures

These small counter windows let the public SDK prove an actual semantic action,
capture, identity checks and independent session cleanup. They do not use the
user's applications or external services. Build the SDK before running Node tests;
`sdk:test` rebuilds its output and must not run concurrently with native tests.

## Windows (interactive desktop)

With Node 24 and Go 1.23.4 or later installed, run Windows PowerShell (`powershell.exe`) from the repository root in a logged-in desktop session:

```powershell
npm ci --ignore-scripts
npm run sdk:test
New-Item -ItemType Directory -Force dist/native | Out-Null
go -C packages/runtime-go test ./...
go -C apps/OpenComputerUseWindows test ./...
go -C apps/OpenComputerUseWindows build -trimpath -o ../../dist/native/open-computer-use.exe .
$env:COMPUTER_USE_RUNTIME_PATH = Join-Path $PWD 'dist/native/open-computer-use.exe'
node --test protocol/native.test.mjs
```

Stop if any command fails. Go builds for the current Windows architecture. Then run the real desktop test:

```powershell
$ErrorActionPreference = 'Stop'
$fixtureDirectory = New-Item -ItemType Directory -Path (Join-Path $env:TEMP ([Guid]::NewGuid().ToString()))
$fixture = Join-Path $fixtureDirectory.FullName 'CherrySDKFixture.exe'
& ./protocol/fixtures/windows.ps1 -OutputPath $fixture
$process = Start-Process $fixture -PassThru
try {
  $env:COMPUTER_USE_RUNTIME_PATH = Join-Path $PWD 'dist/native/open-computer-use.exe'
  $env:COMPUTER_USE_FIXTURE_APP = 'CherrySDKFixture'
  $env:COMPUTER_USE_REQUIRE_SCREENSHOT = '1'
  node --test protocol/desktop.test.mjs
  if ($LASTEXITCODE -ne 0) { throw 'Desktop contract failed' }
} finally {
  Stop-Process -Id $process.Id -ErrorAction SilentlyContinue
  Remove-Item -LiteralPath $fixtureDirectory.FullName -Recurse -Force
}
```

Adjust the runtime path to the build output. The fixture uses WinForms; its real
InvokePattern also guards against default UIA providers failing to initialize from
PowerShell dynamic frames. `go test ./...` in `apps/OpenComputerUseWindows` tests
worker and descendant exit after read cancellation and forced Go-owner death.

Parallels users can run these commands with `prlctl exec <vm> --current-user` so
the fixture and runtime share the logged-in user's interactive session. Session 0
is not a valid replacement for a GUI test. Portable Node and a cross-built Go
executable suffice; no Go installation is needed inside the VM.

## Linux X11

Follow the [Python-free Docker instructions](../../experiments/LinuxNativeProbe/README.md).
That runner builds a GTK fixture and executes both native lifecycle and desktop
tests before the standalone probe. SDK capture discovers the window by PID/title;
it does not receive the probe's fixture window ID. Wayland capture is unsupported.

## macOS (manual permissions)

Build the complete development app with a stable certificate signing identity, and explicitly grant that app
Accessibility and Screen Recording in System Settings. Queries do not prompt or
grant permission. From the repository root, with the SDK already built:

```sh
swiftc protocol/fixtures/macos.swift -o /tmp/CherrySDKFixture
/tmp/CherrySDKFixture &
fixture_pid=$!
trap 'kill "$fixture_pid" 2>/dev/null || true' EXIT
COMPUTER_USE_RUNTIME_PATH="$PWD/dist/Open Computer Use (Dev).app" \
COMPUTER_USE_FIXTURE_APP=CherrySDKFixture COMPUTER_USE_REQUIRE_SCREENSHOT=1 \
node --test protocol/desktop.test.mjs
```

Avoid forcing ad-hoc signing during repeated local permission validation; use the
build script’s `identity` mode and a fixed available signing identity.

The fixture has compiled locally; the SDK GUI test remains pending OS grants.
The macOS CI job checks lifecycle and Swift contracts only and does not claim a
permission-granted desktop test.
