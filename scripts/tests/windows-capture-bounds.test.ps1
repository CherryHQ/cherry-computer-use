param([Parameter(Mandatory=$true)][string]$RuntimePath)
$ErrorActionPreference = 'Stop'
$tokens = $null
$errors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile($RuntimePath, [ref]$tokens, [ref]$errors)
if ($errors.Count) { throw ($errors | Out-String) }
$function = $ast.Find({ param($node)
    $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'Test-WindowCaptureBounds'
}, $true)
if ($null -eq $function) { throw 'Capture bounds validation is missing' }
Invoke-Expression $function.Extent.Text
Add-Type @'
using System;
public static class OCUWin32 {
    public static bool Visible = true;
    public static bool Minimized = false;
    public static bool IsWindowVisible(IntPtr hwnd) { return Visible; }
    public static bool IsIconic(IntPtr hwnd) { return Minimized; }
}
'@
$script:frame = [pscustomobject]@{ x = 10; y = 20; width = 640; height = 480 }
function Get-WindowRectFrame([IntPtr]$hwnd) { return $script:frame }
function Assert($value, $message) { if (-not $value) { throw $message } }
Assert (Test-WindowCaptureBounds $script:frame ([IntPtr]1)) 'A stable visible window was rejected'
Assert (-not (Test-WindowCaptureBounds $script:frame ([IntPtr]::Zero))) 'A missing window was accepted'
Assert (-not (Test-WindowCaptureBounds $null ([IntPtr]1))) 'Missing bounds were accepted'
foreach ($invalid in @(
    @{ x = 10; y = 20; width = 0; height = 480 },
    @{ x = 10; y = 20; width = 640; height = [double]::NaN },
    @{ x = [double]::PositiveInfinity; y = 20; width = 640; height = 480 },
    @{ x = 10; y = 20; width = 20000; height = 20000 },
    @{ x = 11; y = 20; width = 640; height = 480 },
    @{ x = 10; y = 20; width = 641; height = 480 }
)) {
    Assert (-not (Test-WindowCaptureBounds ([pscustomobject]$invalid) ([IntPtr]1))) 'Invalid or stale capture geometry was accepted'
}
[OCUWin32]::Visible = $false
Assert (-not (Test-WindowCaptureBounds $script:frame ([IntPtr]1))) 'A hidden window was accepted'
[OCUWin32]::Visible = $true
[OCUWin32]::Minimized = $true
Assert (-not (Test-WindowCaptureBounds $script:frame ([IntPtr]1))) 'A minimized window was accepted'
Write-Output 'Capture bounds: stable, invalid, moved, resized, hidden and minimized cases passed.'
