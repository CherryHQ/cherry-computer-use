module github.com/iFurySt/open-codex-computer-use/apps/opencomputeruselinux

go 1.22

require (
	github.com/CherryHQ/cherry-computer-use/packages/runtime-go v0.0.0
	github.com/godbus/dbus/v5 v5.2.2
	github.com/jezek/xgb v1.1.1
)

require golang.org/x/sys v0.27.0 // indirect

replace github.com/CherryHQ/cherry-computer-use/packages/runtime-go => ../../packages/runtime-go
