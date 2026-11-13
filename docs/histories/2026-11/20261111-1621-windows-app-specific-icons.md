## [2026-11-11 16:21] | Task: Recover application-specific Windows icons

### User Query

Inspect the 17 Windows Shell defaults found in PR #35, recover the application
icons where available, rerun the full scan and update the previews.

### Changes

- Use the Shell app ID of a detached window owned by the system
  ApplicationFrameHost when UI Automation cannot identify the hosted process.
  When discovery returns both the matching frame and detached CoreWindow, keep
  the application-owned window and use the frame's display name.
- Look for a unique AppsFolder shortcut when an executable has no embedded icon,
  which recovers the Windows Speech Recognition microphone.
- Read the icon from the Windows Application Verifier UI module for the native
  and WOW launchers. The extraction uses the module as an icon resource.
- Added Windows regressions for hosted Settings, shortcut fallback, the two
  Application Verifier launchers and the default icon for an unrelated executable.

### Verification

- The Windows 11 x64 scan returned valid 64×64 PNGs for 184 Start menu entries
  and 10 running apps. Windows listed the Settings frame and detached CoreWindow
  as one app. The SDK scan kept zero app control sessions.
- Compared decoded PNGs with local Windows Shell defaults. The generic count
  fell from 17 to 13. Settings, Windows Speech Recognition and both Application
  Verifier entries now return their own icons.
- Checked the Start menu shortcuts, configured icon files and installed resources
  for the entries that displayed a default icon. Seven shortcuts have missing
  targets or icon resources, and six are SDK web shortcuts with no custom icon.
- Windows native regression tests, the cold-launch test, SDK checks and shared
  Go tests passed. An additional native coverage pass rechecked the entries
  interrupted during the second SDK scan.
