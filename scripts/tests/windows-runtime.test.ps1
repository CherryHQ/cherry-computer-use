param([Parameter(Mandatory = $true)][string]$RuntimePath)
$ErrorActionPreference = 'Stop'
$engine = (Get-Process -Id $PID).Path

# Add-Type definitions cannot be unloaded between scripts in one PowerShell
# process. Keep each suite's Win32/UIA model in its own process.
foreach ($name in @('windows-set-value', 'windows-capture-bounds', 'windows-observation')) {
    & $engine -NoProfile -File (Join-Path $PSScriptRoot "$name.test.ps1") -RuntimePath $RuntimePath
    if ($LASTEXITCODE -ne 0) { throw "$name failed with exit code $LASTEXITCODE" }
}
