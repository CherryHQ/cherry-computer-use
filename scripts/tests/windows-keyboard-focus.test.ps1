param([Parameter(Mandatory = $true)][string]$RuntimePath)
$ErrorActionPreference = 'Stop'
$tokens = $null; $errors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile($RuntimePath, [ref]$tokens, [ref]$errors)
if ($errors.Count) { throw ($errors | Out-String) }
$names = @('Send-Key', 'Get-VirtualKey', 'Get-PostedKeyTarget', 'Split-KeyChord', 'Test-CharacterKey')
foreach ($function in $ast.FindAll({ param($node) $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -in $names }, $true)) { Invoke-Expression $function.Extent.Text }
Add-Type -TypeDefinition @'
using System;
using System.Collections.Generic;
public static class OCUWin32 {
 public static IntPtr focus = (IntPtr)44;
 public static bool postWorks = true;
 public static List<string> events = new List<string>();
 public static IntPtr FocusedWindow(IntPtr w) { return focus; }
 public static bool IsChild(IntPtr parent, IntPtr child) { return parent == (IntPtr)42 && child == (IntPtr)44; }
 public static bool PostMessage(IntPtr h, uint m, IntPtr w, IntPtr l) { events.Add(h + ":" + m + ":" + w); return postWorks; }
 public static bool SendKeyChord(ushort key, bool shift, bool control, bool alt) { throw new Exception("unexpected global input"); }
}
'@
$WM_CHAR = 0x102; $WM_KEYDOWN = 0x100; $WM_KEYUP = 0x101
function Assert-OperationActive {}
function Get-InputWindowHandle($hwnd) { return $hwnd }
function Test-WindowForeground($hwnd) { return $false }
function Start-Sleep { param($Milliseconds) }
function Assert($condition, $message) { if (-not $condition) { throw $message } }
Send-Key ([IntPtr]42) 'Return'
Assert (([OCUWin32]::events -join ',') -eq '44:256:13,44:257:13') 'Enter did not reach the focused child and release'
[OCUWin32]::events.Clear()
try { Send-Key ([IntPtr]42) 'ctrl+a'; throw 'accepted invalid chord' } catch { if ($_.Exception.Message -eq 'accepted invalid chord') { throw } }
Assert ([OCUWin32]::events.Count -eq 0) 'Unrepresentable modifier chord sent partial input'
Send-Key ([IntPtr]42) 'shift+a'
Assert (([OCUWin32]::events -join ',') -eq '44:258:65') 'Shifted character was not delivered as character input'
[OCUWin32]::events.Clear(); [OCUWin32]::focus = [IntPtr]999
Send-Key ([IntPtr]42) 'Return'
Assert (([OCUWin32]::events -join ',') -eq '42:256:13,42:257:13') 'Input escaped the target window'
[OCUWin32]::postWorks = $false
try { Send-Key ([IntPtr]42) 'a'; throw 'accepted failed post' } catch { if ($_.Exception.Message -eq 'accepted failed post') { throw } }
Write-Output 'Keyboard routing, unrepresentable chords, release and delivery failures passed.'
