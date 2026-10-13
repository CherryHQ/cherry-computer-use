# Linux Wayland probe (W0)

A read-only Rust prototype for the [Linux display backend design](../../docs/design-docs/linux-display-backends.md).
It opens an xdg-desktop-portal RemoteDesktop session with a ScreenCast source,
connects to EIS through `ConnectToEIS`, binds the advertised seat capabilities and
prints streams, devices and absolute-pointer regions. It never starts emulating,
so it sends no input. Findings are recorded in the design document.

```sh
cargo build --release
target/release/linux-wayland-probe portal window   # or: portal monitor
```

Run it in a signed-in Wayland session. The portal shows a "Remote Desktop" dialog;
without a parent window GNOME may not raise it, so look for it in the overview,
choose a window and select Share. The session closes when the probe exits.
