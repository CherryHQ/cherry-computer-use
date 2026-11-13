param([Parameter(Mandatory = $true)][string]$RuntimePath)
$ErrorActionPreference = 'Stop'
$tokens = $null; $errors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile($RuntimePath, [ref]$tokens, [ref]$errors)
if ($errors.Count) { throw ($errors | Out-String) }
$names = @('Initialize-PhysicalCoordinates', 'Assert-InputWindowBounds', 'Send-MouseClick', 'Send-Scroll', 'ConvertTo-LParam', 'ConvertTo-WheelWParam')
foreach ($function in $ast.FindAll({ param($node) $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -in $names }, $true)) { Invoke-Expression $function.Extent.Text }
# Execute the production C# hit-test and normalization against a small Win32 model.
$source = Get-Content -LiteralPath $RuntimePath -Raw
function Read-NativeMethod([string]$signature) {
    $start = $source.IndexOf($signature, [StringComparison]::Ordinal)
    if ($start -lt 0) { throw "Missing native method $signature" }
    $opening = $source.IndexOf('{', $start); $depth = 0
    for ($i = $opening; $i -lt $source.Length; $i++) {
        if ($source[$i] -eq '{') { $depth++ }
        if ($source[$i] -eq '}') { $depth--; if ($depth -eq 0) { return $source.Substring($start, $i - $start + 1) } }
    }
    throw 'Unterminated native method'
}
$methods = (Read-NativeMethod 'public static IntPtr ClickTarget(') + (Read-NativeMethod 'public static bool SendPointerClick(')
Add-Type -TypeDefinition (@'
using System;
using System.Collections.Generic;
public static class OCUWin32 {
 public struct POINT { public int X, Y; }
 public struct RECT { public int Left, Top, Right, Bottom; }
 public struct PointerInput { public uint type, flags; public int dx, dy; }
 public class Window { public long Parent; public uint Pid = 42; public int X, Y, Width, Height; public bool Enabled = true, Visible = true; }
 public static Dictionary<long, Window> windows = new Dictionary<long, Window>();
 public static List<long[]> events = new List<long[]>();
 public static PointerInput[] inputs;
 public static bool threadOK = true, moveAfterClick = false;
 public static int awareness = 2;
 public static uint failMessage;
 const uint MOUSE_MOVE = 1, MOUSE_ABSOLUTE = 0x8000, MOUSE_VIRTUALDESK = 0x4000;
 const uint MOUSE_LEFT_DOWN = 2, MOUSE_LEFT_UP = 4, MOUSE_RIGHT_DOWN = 8, MOUSE_RIGHT_UP = 16, MOUSE_MIDDLE_DOWN = 32, MOUSE_MIDDLE_UP = 64;
 public static bool SetProcessDpiAwarenessContext(IntPtr c) { return false; }
 public static IntPtr SetThreadDpiAwarenessContext(IntPtr c) { return threadOK ? (IntPtr)(-1) : IntPtr.Zero; }
 public static IntPtr GetThreadDpiAwarenessContext() { return (IntPtr)(-4); }
 public static int GetAwarenessFromDpiAwarenessContext(IntPtr c) { return awareness; }
 public static uint GetWindowThreadProcessId(IntPtr h, out uint p) { Window w; p = windows.TryGetValue((long)h, out w) ? w.Pid : 0; return p == 0 ? 0U : 1U; }
 public static bool IsWindowEnabled(IntPtr h) { return windows[(long)h].Enabled; }
 public static bool IsChild(IntPtr root, IntPtr child) {
   long id = (long)child;
   for (int i = 0; i < 64 && windows.ContainsKey(id); i++) { id = windows[id].Parent; if (id == (long)root) return true; }
   return false;
 }
 public static bool ScreenToClient(IntPtr h, ref POINT p) { Window w; if (!windows.TryGetValue((long)h, out w)) return false; p.X -= w.X; p.Y -= w.Y; return true; }
 public static bool GetClientRect(IntPtr h, out RECT r) { Window w = windows[(long)h]; r = new RECT { Right = w.Width, Bottom = w.Height }; return true; }
 public static IntPtr ChildWindowFromPointEx(IntPtr h, POINT p, uint flags) {
   Window parent = windows[(long)h]; int x = p.X + parent.X, y = p.Y + parent.Y;
   foreach (var item in windows) { Window w = item.Value; if (w.Parent == (long)h && (w.Visible || (flags & 1) == 0) && x >= w.X && y >= w.Y && x < w.X + w.Width && y < w.Y + w.Height) return (IntPtr)item.Key; }
   return h;
 }
 public static bool PostMessage(IntPtr h, uint m, IntPtr w, IntPtr l) {
   events.Add(new long[] {(long)h, m, (long)w, (long)l});
   if (moveAfterClick && m == 0x202) windows[3].X++;
   return m != failMessage;
 }
 public static int GetSystemMetrics(int i) { switch(i) { case 76: return -1920; case 77: return -1080; case 78: return 3840; case 79: return 2160; default: throw new Exception("unexpected metric"); } }
 public static uint SendInput(uint count, PointerInput[] values, int size) { inputs = values; return count; }
'@ + $methods + '}')
function Assert($condition, $message) { if (-not $condition) { throw $message } }
function Expect-Failure([scriptblock]$body, [string]$message) {
    $failed = $false
    try { & $body } catch { $failed = $true }
    Assert $failed $message
}
function Assert-OperationActive {}
function Get-InputWindowHandle($hwnd) { return $hwnd }
function Start-Sleep { param($Milliseconds) }
$script:bounds = [pscustomobject]@{ x = -1920; y = -1080; width = 1920; height = 2160 }
function Get-WindowRectFrame($hwnd) { return $script:bounds }
$WM_MOUSEMOVE = 0x200; $WM_LBUTTONDOWN = 0x201; $WM_LBUTTONUP = 0x202
$WM_RBUTTONDOWN = 0x204; $WM_RBUTTONUP = 0x205; $WM_MBUTTONDOWN = 0x207; $WM_MBUTTONUP = 0x208
$WM_MOUSEWHEEL = 0x20A; $WM_MOUSEHWHEEL = 0x20E

Initialize-PhysicalCoordinates
[OCUWin32]::threadOK = $false
Expect-Failure { Initialize-PhysicalCoordinates } 'DPI setup silently accepted a failed thread override'
[OCUWin32]::threadOK = $true; [OCUWin32]::awareness = 1
Expect-Failure { Initialize-PhysicalCoordinates } 'DPI setup silently accepted virtualized coordinates'
[OCUWin32]::awareness = 2

[OCUWin32]::windows.Add(1, [OCUWin32+Window]@{ X = -1920; Y = -1080; Width = 1920; Height = 2160 })
[OCUWin32]::windows.Add(2, [OCUWin32+Window]@{ Parent = 1; X = -1600; Y = 100; Width = 800; Height = 700 })
[OCUWin32]::windows.Add(3, [OCUWin32+Window]@{ Parent = 2; X = -1500; Y = 200; Width = 200; Height = 200 })
Assert ([OCUWin32]::ClickTarget([IntPtr]1, -1400, 250) -eq [IntPtr]3) 'Nested control was not selected'
[OCUWin32]::windows[3].Enabled = $false
Expect-Failure { [OCUWin32]::ClickTarget([IntPtr]1, -1400, 250) } 'Disabled child redirected a click to its parent'
[OCUWin32]::windows[3].Enabled = $true; [OCUWin32]::windows[3].Pid = 999
Expect-Failure { [OCUWin32]::ClickTarget([IntPtr]1, -1400, 250) } 'Hit-test escaped the target process'
[OCUWin32]::windows[3].Pid = 42
Expect-Failure { [OCUWin32]::ClickTarget([IntPtr]1, -1921, 250) } 'Window chrome/outside click was accepted'
[OCUWin32]::windows[3].Visible = $false
Assert ([OCUWin32]::ClickTarget([IntPtr]1, -1400, 250) -eq [IntPtr]2) 'Hidden child was selected'
[OCUWin32]::windows[3].Visible = $true
Send-MouseClick ([IntPtr]1) -1400 250 left 1 $script:bounds
Assert ([OCUWin32]::events.Count -eq 3) 'Click did not deliver move/down/up exactly once'
foreach ($event in [OCUWin32]::events) { Assert ($event[0] -eq 3 -and $event[3] -eq (100 -bor (50 -shl 16))) 'Click used the root HWND or incorrect child coordinates' }
[OCUWin32]::events.Clear(); [OCUWin32]::moveAfterClick = $true
Expect-Failure { Send-MouseClick ([IntPtr]1) -1400 250 left 2 $script:bounds } 'Moved child retained a stale second click'
Assert ([OCUWin32]::events.Count -eq 3) 'Input was sent after the child moved'
[OCUWin32]::moveAfterClick = $false; [OCUWin32]::windows[3].X = -1500
[OCUWin32]::events.Clear(); [OCUWin32]::failMessage = $WM_LBUTTONDOWN
Expect-Failure { Send-MouseClick ([IntPtr]1) -1400 250 left 1 $script:bounds } 'Failed mouse post was accepted'
Assert ([OCUWin32]::events[2][1] -eq $WM_LBUTTONUP) 'Mouse up was not attempted after failed mouse down'
[OCUWin32]::failMessage = 0

foreach ($case in @(@('up', $WM_MOUSEWHEEL, 120), @('down', $WM_MOUSEWHEEL, -120), @('left', $WM_MOUSEHWHEEL, -120), @('right', $WM_MOUSEHWHEEL, 120))) {
    [OCUWin32]::events.Clear()
    Send-Scroll ([IntPtr]1) -1400 -200 $case[0] 1 $script:bounds
    $event = [OCUWin32]::events[0]
    Assert ($event[0] -eq 1 -and $event[1] -eq $case[1]) 'Wrong wheel target/message'
    $delta = [int]($event[2] -shr 16) -band 0xffff
    if ($delta -ge 32768) { $delta -= 65536 }
    Assert ($delta -eq $case[2]) 'Wheel direction was inverted'
    $x = [int]($event[3] -band 0xffff); if ($x -ge 32768) { $x -= 65536 }
    $y = [int](($event[3] -shr 16) -band 0xffff); if ($y -ge 32768) { $y -= 65536 }
    Assert ($x -eq -1400 -and $y -eq -200) 'Wheel coordinates were converted to client coordinates or lost their sign'
}
[OCUWin32]::events.Clear(); [OCUWin32]::failMessage = $WM_MOUSEWHEEL
Expect-Failure { Send-Scroll ([IntPtr]1) -1400 250 up 1 $script:bounds } 'Wheel delivery failure was ignored'
[OCUWin32]::failMessage = 0; [OCUWin32]::events.Clear()
$oldBounds = $script:bounds; $script:bounds = [pscustomobject]@{ x = -1919; y = -1080; width = 1920; height = 2160 }
Expect-Failure { Send-Scroll ([IntPtr]1) -1400 250 up 1 $oldBounds } 'Moved root accepted stale wheel input'
Assert ([OCUWin32]::events.Count -eq 0) 'Stale geometry produced input'
$script:bounds = $oldBounds

Assert ([OCUWin32]::SendPointerClick(-1920, -1080, 'left', 1)) 'Virtual desktop origin was rejected'
Assert ([OCUWin32]::inputs[0].dx -eq 0 -and [OCUWin32]::inputs[0].dy -eq 0 -and ([OCUWin32]::inputs[0].flags -band 0x4000) -ne 0) 'Pointer coordinates omitted the virtual desktop origin'
Assert ([OCUWin32]::SendPointerClick(1919, 1079, 'left', 1)) 'Virtual desktop far corner was rejected'
Assert ([OCUWin32]::inputs[0].dx -eq 65535 -and [OCUWin32]::inputs[0].dy -eq 65535) 'Pointer coordinates used only the primary display'
Assert (-not [OCUWin32]::SendPointerClick(-1921, 0, 'left', 1)) 'Pointer input outside the display bounds was accepted'
Write-Output 'Production pointer routing passed: DPI refusal, nested children, geometry, release, signed wheel coordinates/directions and virtual desktop normalization (modeled Win32).'
