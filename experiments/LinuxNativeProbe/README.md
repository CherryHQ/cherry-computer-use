# Linux native backend probe

An isolated experiment for the [runtime decision](../../docs/design-docs/computer-use-runtime.md).
It uses Go D-Bus and X11 clients directly, without Python or cgo. A GTK fixture
exposes a counter button; the probe discovers it over AT-SPI, invokes its action,
checks the changed accessible name, and captures its X11 window as PNG.

The standalone probe remains an experiment, with a fixture-supplied X11 window ID.
The container now also runs the real SDK backend and
[desktop contract test](../../protocol/desktop.test.mjs), which discover the app
through AT-SPI and map capture by PID/title without a supplied window ID.
Neither test establishes Wayland support or cancellation of synthesized input.

From the repository root, with Docker and Go installed:

```sh
npm ci --ignore-scripts
npm run sdk:build
./scripts/build-open-computer-use-linux.sh --arch arm64
(cd experiments/LinuxNativeProbe && GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o ../../dist/linux/arm64/native-probe .)
docker build -t cherry-native-probe experiments/LinuxNativeProbe
docker run --rm -v "$PWD:/workspace:ro" -w /workspace cherry-native-probe
```

Use `amd64` for both Go builds and set `PROBE_ARCH=amd64` in the container on x64
hosts. The image builds the GTK fixture in a separate stage; its runtime stage
contains desktop services but no Python. The probe runs with an empty executable search path;
no external interpreter or command can supply its desktop operations.
