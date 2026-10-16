---
"@cherrystudio/computer-use": patch
---

Support identity-authorized installed application launch on Windows and Linux.
Windows resolves AppsFolder applications and canonical executables; Linux resolves
XDG desktop entries and launches them through D-Bus activation or Exec, without GIO tools. Preserve running-only rebinding,
revalidate identity before effects, and wait for the matching accessible window.
