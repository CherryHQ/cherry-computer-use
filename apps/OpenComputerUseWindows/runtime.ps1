param(
    [string]$OperationPath,
    [switch]$DefinitionsOnly
)

$ErrorActionPreference = "Stop"
$DefaultTextLimit = 500
$AccessibilityTreeMaxNodeCount = 1200
$AccessibilityTreeMaxDepth = 64

# Set output encoding to UTF-8 to properly handle non-ASCII characters
$OutputEncoding = [System.Text.Encoding]::UTF8
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes
Add-Type -AssemblyName System.Drawing

Add-Type -ReferencedAssemblies UIAutomationClient -TypeDefinition @"
using System;
using System.Collections.Generic;
using System.Runtime.CompilerServices;
using System.Runtime.InteropServices;
using System.Text;
using System.Windows.Automation;

public static class OCUWin32 {
    // UIA's default-provider stack walk cannot inspect PowerShell dynamic frames.
    // Keep a managed frame present, including on ARM64 tail-call paths.
    [MethodImpl(MethodImplOptions.NoInlining)]
    public static void InitializeUIAutomation() {
        try { ClientSettings.RegisterClientSideProviders(new ClientSideProviderDescription[0]); }
        finally { GC.KeepAlive(typeof(OCUWin32)); }
    }

    [StructLayout(LayoutKind.Sequential)]
    public struct RECT {
        public int Left;
        public int Top;
        public int Right;
        public int Bottom;
    }

    [StructLayout(LayoutKind.Sequential)]
    public struct POINT {
        public int X;
        public int Y;
    }

    [DllImport("user32.dll")]
    public static extern bool GetWindowRect(IntPtr hWnd, out RECT rect);

    [DllImport("user32.dll", SetLastError = true)]
    public static extern bool PrintWindow(IntPtr hwnd, IntPtr hdc, uint flags);

    [DllImport("user32.dll")]
    public static extern bool IsIconic(IntPtr hwnd);

    [DllImport("user32.dll")]
    public static extern bool ScreenToClient(IntPtr hWnd, ref POINT point);

    [DllImport("user32.dll", CharSet = CharSet.Unicode)]
    public static extern bool PostMessage(IntPtr hWnd, UInt32 msg, IntPtr wParam, IntPtr lParam);

    [DllImport("user32.dll", CharSet = CharSet.Unicode)]
    public static extern IntPtr SendMessage(IntPtr hWnd, UInt32 msg, IntPtr wParam, IntPtr lParam);

    [DllImport("user32.dll", CharSet = CharSet.Unicode)]
    public static extern IntPtr SendMessage(IntPtr hWnd, UInt32 msg, IntPtr wParam, string lParam);

    [StructLayout(LayoutKind.Sequential)]
    private struct GUITHREADINFO {
        public int cbSize, flags;
        public IntPtr hwndActive, hwndFocus, hwndCapture, hwndMenuOwner, hwndMoveSize, hwndCaret;
        public RECT rcCaret;
    }

    [DllImport("user32.dll")]
    private static extern bool GetGUIThreadInfo(uint threadId, ref GUITHREADINFO info);

    // The window with keyboard focus on the target window's own GUI thread, or zero when that thread has none.
    public static IntPtr FocusedWindow(IntPtr window) {
        uint pid;
        uint thread = GetWindowThreadProcessId(window, out pid);
        GUITHREADINFO info = new GUITHREADINFO();
        info.cbSize = Marshal.SizeOf(typeof(GUITHREADINFO));
        if (thread == 0 || !GetGUIThreadInfo(thread, ref info)) return IntPtr.Zero;
        uint focusPid;
        if (info.hwndFocus == IntPtr.Zero || GetWindowThreadProcessId(info.hwndFocus, out focusPid) == 0 || focusPid != pid)
            return IntPtr.Zero;
        return info.hwndFocus;
    }

    [DllImport("user32.dll")]
    public static extern bool IsChild(IntPtr parent, IntPtr child);

    public delegate bool EnumWindowsProc(IntPtr hWnd, IntPtr lParam);

    [DllImport("user32.dll")]
    public static extern bool EnumWindows(EnumWindowsProc callback, IntPtr lParam);

    [DllImport("user32.dll")]
    public static extern uint GetWindowThreadProcessId(IntPtr hWnd, out uint processId);

    [DllImport("user32.dll")]
    public static extern bool IsWindow(IntPtr hWnd);

    [DllImport("user32.dll")]
    public static extern bool IsWindowVisible(IntPtr hWnd);

    [DllImport("user32.dll", CharSet = CharSet.Unicode)]
    public static extern int GetClassName(IntPtr hWnd, StringBuilder name, int count);

    [DllImport("user32.dll", CharSet = CharSet.Unicode)]
    public static extern int GetWindowText(IntPtr hWnd, StringBuilder text, int count);

    // Process.MainWindowHandle reports one window per process and flips between the windows of a
    // shared host (ApplicationFrameHost owns every UWP frame). Enumerate the real top-level windows.
    public static long[] TopLevelWindows(uint processId) {
        var found = new List<long>();
        EnumWindows(delegate(IntPtr hWnd, IntPtr lParam) {
            uint owner;
            GetWindowThreadProcessId(hWnd, out owner);
            if (owner == processId && IsWindowVisible(hWnd)) { found.Add(hWnd.ToInt64()); }
            return true;
        }, IntPtr.Zero);
        return found.ToArray();
    }

    public static long[] AllTopLevelWindows() {
        var found = new List<long>();
        EnumWindows(delegate(IntPtr hWnd, IntPtr lParam) {
            if (IsWindowVisible(hWnd)) { found.Add(hWnd.ToInt64()); }
            return true;
        }, IntPtr.Zero);
        return found.ToArray();
    }

    public static string ClassNameOf(IntPtr hWnd) {
        var name = new StringBuilder(256);
        return GetClassName(hWnd, name, name.Capacity) > 0 ? name.ToString() : "";
    }

    public static string WindowTextOf(IntPtr hWnd) {
        var text = new StringBuilder(1024);
        return GetWindowText(hWnd, text, text.Capacity) > 0 ? text.ToString() : "";
    }

    public static uint OwnerProcessOf(IntPtr hWnd) {
        uint processId;
        GetWindowThreadProcessId(hWnd, out processId);
        return processId;
    }

    public static bool IsAlive(IntPtr hWnd) { return hWnd != IntPtr.Zero && IsWindow(hWnd); }

    [DllImport("user32.dll")]
    public static extern IntPtr GetForegroundWindow();

    [DllImport("user32.dll")]
    public static extern IntPtr GetAncestor(IntPtr hWnd, uint flags);

    [DllImport("user32.dll")]
    public static extern bool EnumChildWindows(IntPtr parent, EnumWindowsProc callback, IntPtr lParam);

    [DllImport("user32.dll")]
    public static extern int GetSystemMetrics(int index);

    // The frame window of a hosted app (UWP ApplicationFrameWindow, Chromium widget) never sees keyboard
    // or mouse messages; the hosted input window does.
    public static IntPtr FindChildByClass(IntPtr parent, string[] classes) {
        IntPtr found = IntPtr.Zero;
        EnumChildWindows(parent, delegate(IntPtr hWnd, IntPtr lParam) {
            string name = ClassNameOf(hWnd);
            for (int i = 0; i < classes.Length; i++) {
                if (name == classes[i]) { found = hWnd; return false; }
            }
            return true;
        }, IntPtr.Zero);
        return found;
    }

    [StructLayout(LayoutKind.Explicit, Size = 40)]
    public struct KeyInput {
        [FieldOffset(0)] public uint type;
        [FieldOffset(8)] public ushort virtualKey;
        [FieldOffset(10)] public ushort scanCode;
        [FieldOffset(12)] public uint flags;
        [FieldOffset(16)] public uint time;
        [FieldOffset(24)] public IntPtr extraInfo;
    }

    [StructLayout(LayoutKind.Explicit, Size = 40)]
    public struct PointerInput {
        [FieldOffset(0)] public uint type;
        [FieldOffset(8)] public int dx;
        [FieldOffset(12)] public int dy;
        [FieldOffset(16)] public uint mouseData;
        [FieldOffset(20)] public uint flags;
        [FieldOffset(24)] public uint time;
        [FieldOffset(32)] public IntPtr extraInfo;
    }

    [DllImport("user32.dll", SetLastError = true)]
    public static extern uint SendInput(uint count, KeyInput[] inputs, int size);

    [DllImport("user32.dll", SetLastError = true)]
    public static extern uint SendInput(uint count, PointerInput[] inputs, int size);

    const uint KEY_UP = 0x0002;
    const uint MOUSE_MOVE = 0x0001;
    const uint MOUSE_LEFT_DOWN = 0x0002;
    const uint MOUSE_LEFT_UP = 0x0004;
    const uint MOUSE_RIGHT_DOWN = 0x0008;
    const uint MOUSE_RIGHT_UP = 0x0010;
    const uint MOUSE_MIDDLE_DOWN = 0x0020;
    const uint MOUSE_MIDDLE_UP = 0x0040;
    const uint MOUSE_ABSOLUTE = 0x8000;

    // Real keyboard input: the only way to deliver modifier and navigation keys to hosted apps.
    public static bool SendKeyChord(ushort virtualKey, bool shift, bool control, bool alt) {
        var inputs = new List<KeyInput>();
        if (control) { inputs.Add(KeyEvent(0x11, false)); }
        if (shift) { inputs.Add(KeyEvent(0x10, false)); }
        if (alt) { inputs.Add(KeyEvent(0x12, false)); }
        inputs.Add(KeyEvent(virtualKey, false));
        inputs.Add(KeyEvent(virtualKey, true));
        if (alt) { inputs.Add(KeyEvent(0x12, true)); }
        if (shift) { inputs.Add(KeyEvent(0x10, true)); }
        if (control) { inputs.Add(KeyEvent(0x11, true)); }
        return SendInput((uint)inputs.Count, inputs.ToArray(), 40) == (uint)inputs.Count;
    }

    static KeyInput KeyEvent(ushort virtualKey, bool up) {
        var input = new KeyInput();
        input.type = 1;
        input.virtualKey = virtualKey;
        if (up) { input.flags = KEY_UP; }
        return input;
    }

    public static bool SendPointerClick(int screenX, int screenY, string button, int count) {
        int width = GetSystemMetrics(0);
        int height = GetSystemMetrics(1);
        if (width < 2 || height < 2) { return false; }
        var move = new PointerInput();
        move.type = 0;
        move.dx = (int)Math.Round(screenX * 65535.0 / (width - 1));
        move.dy = (int)Math.Round(screenY * 65535.0 / (height - 1));
        move.flags = MOUSE_MOVE | MOUSE_ABSOLUTE;
        uint downFlag = MOUSE_LEFT_DOWN, upFlag = MOUSE_LEFT_UP;
        if (button == "right") { downFlag = MOUSE_RIGHT_DOWN; upFlag = MOUSE_RIGHT_UP; }
        else if (button == "middle") { downFlag = MOUSE_MIDDLE_DOWN; upFlag = MOUSE_MIDDLE_UP; }
        var events = new List<PointerInput>();
        events.Add(move);
        int repeat = Math.Max(1, count);
        for (int i = 0; i < repeat; i++) {
            var down = new PointerInput(); down.type = 0; down.flags = downFlag; events.Add(down);
            var up = new PointerInput(); up.type = 0; up.flags = upFlag; events.Add(up);
        }
        return SendInput((uint)events.Count, events.ToArray(), 40) == (uint)events.Count;
    }
}
"@

[OCUWin32]::InitializeUIAutomation()

$WM_SETTEXT = 0x000C
$WM_MOUSEMOVE = 0x0200
$WM_LBUTTONDOWN = 0x0201
$WM_LBUTTONUP = 0x0202
$WM_RBUTTONDOWN = 0x0204
$WM_RBUTTONUP = 0x0205
$WM_MBUTTONDOWN = 0x0207
$WM_MBUTTONUP = 0x0208
$WM_MOUSEWHEEL = 0x020A
$WM_MOUSEHWHEEL = 0x020E
$WM_KEYDOWN = 0x0100
$WM_KEYUP = 0x0101
$WM_CHAR = 0x0102
$EM_SETSEL = 0x00B1
$EM_REPLACESEL = 0x00C2

function Test-EnvFlagEnabled([string]$name) {
    $value = [Environment]::GetEnvironmentVariable($name)
    if ([string]::IsNullOrWhiteSpace($value)) {
        return $false
    }
    $normalized = $value.Trim().ToLowerInvariant()
    return @("1", "true", "yes", "on") -contains $normalized
}

function New-Frame($x, $y, $width, $height) {
    if ($width -lt 0 -or $height -lt 0) {
        return $null
    }
    [pscustomobject]@{
        x = [double]$x
        y = [double]$y
        width = [double]$width
        height = [double]$height
    }
}

function ConvertTo-LParam([int]$x, [int]$y) {
    $packed = (($y -band 0xffff) -shl 16) -bor ($x -band 0xffff)
    [IntPtr]$packed
}

function ConvertTo-WheelWParam([int]$delta) {
    $packed = (($delta -band 0xffff) -shl 16)
    [IntPtr]$packed
}

function Get-WindowRectFrame([IntPtr]$hwnd) {
    $rect = New-Object OCUWin32+RECT
    if ([OCUWin32]::GetWindowRect($hwnd, [ref]$rect)) {
        return New-Frame $rect.Left $rect.Top ($rect.Right - $rect.Left) ($rect.Bottom - $rect.Top)
    }
    return $null
}

function Get-ElementFrame($element, $windowBounds) {
    try {
        $rect = $element.Current.BoundingRectangle
        if ($rect.IsEmpty -or $rect.Width -le 0 -or $rect.Height -le 0) {
            return $null
        }
        if ($null -ne $windowBounds) {
            return New-Frame ($rect.X - $windowBounds.x) ($rect.Y - $windowBounds.y) $rect.Width $rect.Height
        }
        return New-Frame $rect.X $rect.Y $rect.Width $rect.Height
    } catch {
        return $null
    }
}

function Get-ScreenPoint($localFrame, $windowBounds) {
    if ($null -eq $localFrame -or $null -eq $windowBounds) {
        return $null
    }
    [pscustomobject]@{
        x = [int][math]::Round($windowBounds.x + $localFrame.x + ($localFrame.width / 2))
        y = [int][math]::Round($windowBounds.y + $localFrame.y + ($localFrame.height / 2))
    }
}

function Assert-OperationActive {
    if ($null -ne $script:OperationCancelled -and (& $script:OperationCancelled)) {
        $script:sdkCode = 'CANCELLED'
        throw 'Action cancelled during execution'
    }
}

# ApplicationFrameHost hosts the frame window of every UWP app and Chromium-based apps host their
# content in a render widget child. Messages posted to the frame window are discarded, so messages
# have to be posted to the window that actually owns the keyboard/mouse input queue.
function Get-InputWindowHandle([IntPtr]$hwnd) {
    if ($hwnd -eq [IntPtr]::Zero) {
        return $hwnd
    }
    $class = [OCUWin32]::ClassNameOf($hwnd)
    if ($class -eq 'ApplicationFrameWindow') {
        $child = [OCUWin32]::FindChildByClass($hwnd, @('Windows.UI.Core.CoreWindow', 'ApplicationFrameInputSinkWindow'))
        if ($child -ne [IntPtr]::Zero) {
            return $child
        }
    } elseif ($class -like 'Chrome_WidgetWin*') {
        $child = [OCUWin32]::FindChildByClass($hwnd, @('Chrome_RenderWidgetHostHWND'))
        if ($child -ne [IntPtr]::Zero) {
            return $child
        }
    }
    return $hwnd
}

function Test-WindowForeground([IntPtr]$hwnd) {
    $foreground = [OCUWin32]::GetForegroundWindow()
    if ($foreground -eq [IntPtr]::Zero -or $hwnd -eq [IntPtr]::Zero) {
        return $false
    }
    if ($foreground -eq $hwnd) {
        return $true
    }
    return ([OCUWin32]::GetAncestor($foreground, 2) -eq [OCUWin32]::GetAncestor($hwnd, 2))
}

# Hosted content (UWP/Chromium) never receives posted mouse messages, and real pointer input needs the
# target to own the pointer. Hit-test through UI Automation and use the element's own pattern instead: that is
# what the click means semantically, and it works in the background.
# The smallest element of the target's own tree that contains the point. Unlike
# AutomationElement.FromPoint this ignores which window is on top, so it also works for covered windows.
function Get-ElementAtScreenPoint([IntPtr]$hwnd, [int]$x, [int]$y) {
    try {
        $root = Get-MainElement $null $hwnd
        $all = $root.FindAll([Windows.Automation.TreeScope]::Descendants, [Windows.Automation.Condition]::TrueCondition)
        $best = $null
        $bestArea = [double]::MaxValue
        $limit = [math]::Min($all.Count, 2000)
        for ($i = 0; $i -lt $limit; $i++) {
            $element = $all.Item($i)
            try {
                $rect = $element.Current.BoundingRectangle
                if ($rect.IsEmpty -or $rect.Width -le 0 -or $rect.Height -le 0) { continue }
                if ($x -lt $rect.X -or $x -gt ($rect.X + $rect.Width) -or $y -lt $rect.Y -or $y -gt ($rect.Y + $rect.Height)) { continue }
                $area = $rect.Width * $rect.Height
                if ($area -lt $bestArea) {
                    $best = $element
                    $bestArea = $area
                }
            } catch {
            }
        }
        return $best
    } catch {
        return $null
    }
}

function Get-ElementParent($element) {
    try {
        return [Windows.Automation.TreeWalker]::ControlViewWalker.GetParent($element)
    } catch {
        return $null
    }
}

function Send-SemanticClick([IntPtr]$hwnd, [int]$screenX, [int]$screenY, [int]$depth = 2) {
    $element = Get-ElementAtScreenPoint $hwnd $screenX $screenY
    if ($null -eq $element) {
        return $false
    }
    $hostPids = @([OCUWin32]::OwnerProcessOf($hwnd), 0)
    $hosted = Get-WindowHostedProcessId $hwnd ([int][OCUWin32]::OwnerProcessOf($hwnd))
    if ($hosted -gt 0) {
        $hostPids += $hosted
    }
    if ([int]$element.Current.ProcessId -notin $hostPids) {
        return $false
    }
    $node = $element
    for ($level = 0; $level -le $depth -and $null -ne $node; $level++) {
        $processId = [int]$node.Current.ProcessId
        if ($processId -notin $hostPids) {
            break
        }
        if (Invoke-PreferredClick $node) {
            return $true
        }
        $node = Get-ElementParent $node
    }
    return $false
}

function Get-ScrollableAncestor($element, [int]$depth = 8) {
    $node = $element
    for ($level = 0; $level -le $depth -and $null -ne $node; $level++) {
        $pattern = Get-CurrentPatternOrNull $node ([Windows.Automation.ScrollPattern]::Pattern)
        if ($null -ne $pattern) {
            return $node
        }
        $node = Get-ElementParent $node
    }
    return $null
}

function Send-MouseClick([IntPtr]$hwnd, [int]$screenX, [int]$screenY, [string]$button, [int]$count) {
    $inputWindow = Get-InputWindowHandle $hwnd
    # Hosted content ignores posted mouse messages: use real pointer input when the target already owns the
    # pointer, otherwise the element action under the point. Classic windows keep posted messages.
    if ($inputWindow -ne $hwnd) {
        if ((Test-WindowForeground $hwnd) -and [OCUWin32]::SendPointerClick($screenX, $screenY, $button, $count)) {
            return
        }
        if ($button -ne 'right' -and $button -ne 'middle' -and (Send-SemanticClick $hwnd $screenX $screenY)) {
            return
        }
        $script:sdkCode = 'UNSUPPORTED_CAPABILITY'
        throw "Coordinate clicks cannot be delivered to this hosted application while it is in the background; click the element instead, or bring the window to the foreground and retry"
    }
    $clickWindow = $hwnd
    $point = New-Object OCUWin32+POINT
    $point.X = $screenX
    $point.Y = $screenY
    [void][OCUWin32]::ScreenToClient($clickWindow, [ref]$point)
    $lParam = ConvertTo-LParam $point.X $point.Y

    $down = $WM_LBUTTONDOWN
    $up = $WM_LBUTTONUP
    $downFlag = 0x0001
    if ($button -eq "right") {
        $down = $WM_RBUTTONDOWN
        $up = $WM_RBUTTONUP
        $downFlag = 0x0002
    } elseif ($button -eq "middle") {
        $down = $WM_MBUTTONDOWN
        $up = $WM_MBUTTONUP
        $downFlag = 0x0010
    }

    $repeat = [math]::Max(1, $count)
    for ($i = 0; $i -lt $repeat; $i++) {
        Assert-OperationActive
        [void][OCUWin32]::PostMessage($clickWindow, $WM_MOUSEMOVE, [IntPtr]::Zero, $lParam)
        try {
            [void][OCUWin32]::PostMessage($clickWindow, $down, [IntPtr]$downFlag, $lParam)
            Start-Sleep -Milliseconds 35
        } finally { [void][OCUWin32]::PostMessage($clickWindow, $up, [IntPtr]::Zero, $lParam) }
        Start-Sleep -Milliseconds 50
    }
}

function Send-Drag([IntPtr]$hwnd, [int]$fromX, [int]$fromY, [int]$toX, [int]$toY) {
    $inputWindow = Get-InputWindowHandle $hwnd
    if ($inputWindow -ne $hwnd) {
        $script:sdkCode = 'UNSUPPORTED_CAPABILITY'
        throw "Dragging cannot be delivered to this hosted application while it is in the background; bring the window to the foreground and retry"
    }
    $dragWindow = $hwnd
    $start = New-Object OCUWin32+POINT
    $start.X = $fromX
    $start.Y = $fromY
    [void][OCUWin32]::ScreenToClient($dragWindow, [ref]$start)
    $end = New-Object OCUWin32+POINT
    $end.X = $toX
    $end.Y = $toY
    [void][OCUWin32]::ScreenToClient($dragWindow, [ref]$end)

    $steps = 12
    $startParam = ConvertTo-LParam $start.X $start.Y
    [void][OCUWin32]::PostMessage($dragWindow, $WM_MOUSEMOVE, [IntPtr]::Zero, $startParam)
    Assert-OperationActive
    $lastParam = $startParam
    try {
        [void][OCUWin32]::PostMessage($dragWindow, $WM_LBUTTONDOWN, [IntPtr]1, $startParam)
        for ($i = 1; $i -le $steps; $i++) {
            Assert-OperationActive
            $x = [int][math]::Round($start.X + (($end.X - $start.X) * $i / $steps))
            $y = [int][math]::Round($start.Y + (($end.Y - $start.Y) * $i / $steps))
            $lastParam = ConvertTo-LParam $x $y
            [void][OCUWin32]::PostMessage($dragWindow, $WM_MOUSEMOVE, [IntPtr]1, $lastParam)
            Start-Sleep -Milliseconds 20
        }
    } finally { [void][OCUWin32]::PostMessage($dragWindow, $WM_LBUTTONUP, [IntPtr]::Zero, $lastParam) }
}

function Send-Scroll([IntPtr]$hwnd, [int]$screenX, [int]$screenY, [string]$direction, [double]$pages) {
    $point = New-Object OCUWin32+POINT
    $point.X = $screenX
    $point.Y = $screenY
    [void][OCUWin32]::ScreenToClient($hwnd, [ref]$point)
    $lParam = ConvertTo-LParam $point.X $point.Y
    $delta = [int][math]::Round(120 * $pages)
    $message = $WM_MOUSEWHEEL
    if ($direction -eq "down" -or $direction -eq "right") {
        $delta = -1 * $delta
    }
    if ($direction -eq "left" -or $direction -eq "right") {
        $message = $WM_MOUSEHWHEEL
    }
    $wheelWindow = $hwnd
    [void][OCUWin32]::PostMessage($wheelWindow, $message, (ConvertTo-WheelWParam $delta), $lParam)
}

function Get-PostedKeyTarget([IntPtr]$hwnd) {
    $focus = [OCUWin32]::FocusedWindow($hwnd)
    if ($focus -ne [IntPtr]::Zero -and ($focus -eq $hwnd -or [OCUWin32]::IsChild($hwnd, $focus))) {
        return $focus
    }
    return $hwnd
}

# Edit and RichEdit controls, including WinForms' superclasses (WindowsForms10.EDIT..., WindowsForms10.RichEdit20W...).

function Send-Text([IntPtr]$hwnd, [string]$text) {
    $target = Get-InputWindowHandle $hwnd
    foreach ($char in $text.ToCharArray()) {
        Assert-OperationActive
        if ($char -eq "`r" -or $char -eq "`n") {
            Send-Key $hwnd 'Return'
            continue
        }
        if (-not [OCUWin32]::PostMessage($target, $WM_CHAR, [IntPtr][int][char]$char, [IntPtr]::Zero)) { throw 'PostMessage character delivery failed' }
        Start-Sleep -Milliseconds 8
    }
}

function Send-TextToEditHandle([IntPtr]$hwnd, [string]$text, $element) {
    if ($hwnd -eq [IntPtr]::Zero) {
        return $false
    }

    try {
        [void][OCUWin32]::SendMessage($hwnd, $EM_SETSEL, [IntPtr](-1), [IntPtr](-1))
        [void][OCUWin32]::SendMessage($hwnd, $EM_REPLACESEL, [IntPtr]1, $text)
        return $true
    } catch {
    }

    try {
        $current = ""
        if ($null -ne $element) {
            $current = Get-ElementValue $element
        }
        [void][OCUWin32]::SendMessage($hwnd, $WM_SETTEXT, [IntPtr]::Zero, ($current + $text))
        return $true
    } catch {
        return $false
    }
}

function Get-VirtualKey([string]$key) {
    $normalized = $key.ToLowerInvariant()
    $map = @{
        "return" = 0x0D; "enter" = 0x0D; "tab" = 0x09; "escape" = 0x1B; "esc" = 0x1B
        "backspace" = 0x08; "back_space" = 0x08; "delete" = 0x2E; "space" = 0x20
        "left" = 0x25; "up" = 0x26; "right" = 0x27; "down" = 0x28
        "home" = 0x24; "end" = 0x23; "page_up" = 0x21; "prior" = 0x21; "page_down" = 0x22; "next" = 0x22
        "insert" = 0x2D; "printscreen" = 0x2C
    }
    if ($map.ContainsKey($normalized)) {
        return $map[$normalized]
    }
    if ($normalized -match "^f([1-9]|1[0-2])$") {
        return 0x70 + [int]$Matches[1] - 1
    }
    if ($normalized -match "^kp_([0-9])$") {
        return 0x60 + [int]$Matches[1]
    }
    if ($normalized.Length -eq 1) {
        $code = [int][char]$normalized.ToUpperInvariant()[0]
        if (($code -ge 0x30 -and $code -le 0x39) -or ($code -ge 0x41 -and $code -le 0x5A)) {
            return $code
        }
        $symbols = @{
            ' ' = 0x20; ';' = 0xBA; '=' = 0xBB; ',' = 0xBC; '-' = 0xBD; '.' = 0xBE; '/' = 0xBF; '`' = 0xC0
            '[' = 0xDB; '\' = 0xDC; ']' = 0xDD; "'" = 0xDE; "+" = 0xBB; "*" = 0x38; "%" = 0x35; "(" = 0x39
            ")" = 0x30; "!" = 0x31; "@" = 0x32; "#" = 0x33; "$" = 0x34; "^" = 0x36; "&" = 0x37
            "_" = 0xBD; ":" = 0xBA; '"' = 0xDE; "<" = 0xBC; ">" = 0xBE; "?" = 0xBF; "{" = 0xDB; "}" = 0xDD
            "|" = 0xDC; "~" = 0xC0
        }
        if ($symbols.ContainsKey($normalized)) {
            return $symbols[$normalized]
        }
    }
    throw "Unsupported key: $key"
}

# Characters an application can receive as plain WM_CHAR input (no modifier state needed).
function Test-CharacterKey([string]$key) {
    if ($key.Length -ne 1) { return $false }
    $code = [int][char]$key
    return ($code -ge 0x20 -and $code -ne 0x7F)
}

# Splits 'ctrl+shift+s' into a main key and its modifiers. A bare '+' or a trailing '+' is the key
# itself, so 'ctrl++' works too.
function Split-KeyChord([string]$key) {
    $chord = @{ main = $key; shift = $false; control = $false; alt = $false; super = $false }
    while ($true) {
        $match = [regex]::Match($chord.main, '^(ctrl|control|shift|alt|super|win|cmd)\+(?<rest>.+)$', [System.Text.RegularExpressions.RegexOptions]::IgnoreCase)
        if (-not $match.Success) { break }
        switch ($match.Groups[1].Value.ToLowerInvariant()) {
            "ctrl" { $chord.control = $true }
            "control" { $chord.control = $true }
            "shift" { $chord.shift = $true }
            "alt" { $chord.alt = $true }
            "super" { $chord.super = $true }
            "win" { $chord.super = $true }
            "cmd" { $chord.super = $true }
        }
        $chord.main = $match.Groups['rest'].Value
    }
    return $chord
}

function Test-KeySupported([string]$key) {
    $chord = Split-KeyChord $key
    if ((Test-CharacterKey $chord.main) -and -not $chord.control -and -not $chord.alt -and -not $chord.super) {
        return
    }
    $null = Get-VirtualKey $chord.main
}

# Keys that need modifier state, navigation semantics or a real input queue.
function Send-Key([IntPtr]$hwnd, [string]$key) {
    $chord = Split-KeyChord $key
    $main = $chord.main
    $shift = $chord.shift
    $control = $chord.control
    $alt = $chord.alt
    $super = $chord.super
    Assert-OperationActive
    $inputWindow = Get-InputWindowHandle $hwnd
    $target = if ($inputWindow -ne $hwnd) { $inputWindow } else { Get-PostedKeyTarget $hwnd }
    if (-not $control -and -not $alt -and -not $super -and (Test-CharacterKey $main)) {
        # Plain characters reach hosted apps as character input; this is also what fixes '+' '*'
        # '=' and friends, which previously had no virtual-key mapping at all.
        $char = if ($shift) { $main.ToUpperInvariant() } else { $main }
        if (-not [OCUWin32]::PostMessage($target, $WM_CHAR, [IntPtr][int][char]$char, [IntPtr]::Zero)) { throw 'PostMessage character delivery failed' }
        Start-Sleep -Milliseconds 20
        return
    }
    $vk = Get-VirtualKey $main
    # Hosted content ignores posted key messages, so it needs real input; classic windows keep the
    # historical posted-message path (apps may count keys on the top-level window).
    if ($inputWindow -ne $hwnd) {
        if (-not $super -and (Test-WindowForeground $hwnd) -and [OCUWin32]::SendKeyChord($vk, $shift, $control, $alt)) {
            return
        }
        $script:sdkCode = 'UNSUPPORTED_CAPABILITY'
        throw "Key '$key' cannot be delivered to this hosted application in the background; bring the window to the foreground and retry, or use an element action instead"
    }
    if ($shift -or $control -or $alt -or $super) {
        $script:sdkCode = 'UNSUPPORTED_CAPABILITY'
        throw "Window messages cannot carry modifier state for '$key'; use a semantic control action or a page input channel"
    }
    if (-not [OCUWin32]::PostMessage($target, $WM_KEYDOWN, [IntPtr]$vk, [IntPtr]::Zero)) { throw 'PostMessage key delivery failed' }
    try { Start-Sleep -Milliseconds 25 }
    finally {
        if (-not [OCUWin32]::PostMessage($target, $WM_KEYUP, [IntPtr]$vk, [IntPtr]::Zero)) { throw 'PostMessage key release failed' }
    }
}

function Resolve-App([string]$query) {
    $normalized = $query.Trim()
    $processQuery = $normalized
    if ($processQuery.EndsWith(".exe", [System.StringComparison]::OrdinalIgnoreCase)) {
        $processQuery = $processQuery.Substring(0, $processQuery.Length - 4)
    }
    $processes = @(Get-Process | Where-Object { $_.MainWindowHandle -ne 0 })
    $pidValue = 0
    if ([int]::TryParse($normalized, [ref]$pidValue)) {
        $match = $processes | Where-Object { $_.Id -eq $pidValue } | Select-Object -First 1
        if ($null -ne $match) {
            return $match
        }
    }

    $match = $processes | Where-Object {
        $_.ProcessName -ieq $processQuery -or
        "$($_.ProcessName).exe" -ieq $normalized -or
        $_.MainWindowTitle -ieq $normalized -or
        $_.MainWindowTitle -ilike "*$normalized*"
    } | Select-Object -First 1
    if ($null -ne $match) {
        return $match
    }

    if (Test-EnvFlagEnabled "OPEN_COMPUTER_USE_WINDOWS_ALLOW_APP_LAUNCH") {
        try {
            $started = Start-Process -FilePath $normalized -PassThru
            for ($i = 0; $i -lt 20; $i++) {
                Start-Sleep -Milliseconds 250
                $candidate = Get-Process -Id $started.Id -ErrorAction SilentlyContinue
                if ($null -ne $candidate -and $candidate.MainWindowHandle -ne 0) {
                    return $candidate
                }
            }
        } catch {
        }
    }

    throw "appNotFound(`"$query`")"
}

function Get-ProcessWindows([int]$processId) {
    return @([OCUWin32]::TopLevelWindows([uint32]$processId) | ForEach-Object { [IntPtr]$_ })
}

function Get-WindowHostedProcessId([IntPtr]$hwnd, [int]$hostProcessId) {
    if ([OCUWin32]::ClassNameOf($hwnd) -ne 'ApplicationFrameWindow') {
        return [int]$hostProcessId
    }
    try {
        $root = [Windows.Automation.AutomationElement]::FromHandle($hwnd)
        $condition = New-Object Windows.Automation.NotCondition (New-Object Windows.Automation.PropertyCondition ([Windows.Automation.AutomationElement]::ProcessIdProperty), $hostProcessId)
        $child = $root.FindFirst([Windows.Automation.TreeScope]::Descendants, $condition)
        if ($null -ne $child) {
            return [int]$child.Current.ProcessId
        }
    } catch {
    }
    return 0
}

function Get-WindowAppIdentity([IntPtr]$hwnd, [int]$hostProcessId) {
    $identityPid = Get-WindowHostedProcessId $hwnd $hostProcessId
    if ($identityPid -gt 0) {
        return [SDKApplicationIdentity]::ForProcess($identityPid)
    }
    $identity = [SDKApplicationIdentity]::ForProcess($hostProcessId)
    $frameHost = Join-Path ([Environment]::SystemDirectory) 'ApplicationFrameHost.exe'
    if ($identity -ceq ('win32:exe:' + $frameHost.ToLowerInvariant())) {
        $appID = [SDKApplicationIdentity]::ForWindow($hwnd)
        if ($appID -match '^[^\\/:]+![^\\/:]+$') {
            return 'win32:aumid:' + $appID.ToLowerInvariant()
        }
    }
    return $identity
}

# A window handle is the identity of a hosted application: ApplicationFrameHost owns the frame
# windows of every running UWP app, so Process.MainWindowHandle flips between them and cannot
# tell Calculator from Settings.
function Resolve-AppWindowHandle($process, [string]$identity, [long]$preferred = 0) {
    if ($preferred -ne 0) {
        $handle = [IntPtr][long]$preferred
        if ([OCUWin32]::IsAlive($handle) -and [OCUWin32]::OwnerProcessOf($handle) -eq [uint32]$process.Id) {
            return $handle
        }
    }
    $candidates = @(Get-ProcessWindows ([int]$process.Id))
    if ($candidates.Count -eq 0) {
        return [IntPtr][long]$process.MainWindowHandle
    }
    $frameWindows = @($candidates | Where-Object { [OCUWin32]::ClassNameOf($_) -eq 'ApplicationFrameWindow' })
    $pool = if ($frameWindows.Count -gt 0) { $frameWindows } else { $candidates }
    if (-not [string]::IsNullOrEmpty($identity)) {
        foreach ($handle in $pool) {
            if ((Get-WindowAppIdentity $handle ([int]$process.Id)) -ceq $identity) {
                return $handle
            }
        }
    }
    foreach ($handle in $pool) {
        if ([OCUWin32]::WindowTextOf($handle) -ceq $process.MainWindowTitle) {
            return $handle
        }
    }
    return $pool[0]
}

function Get-MainElement($process, [long]$windowHandle = 0) {
    if ($windowHandle -eq 0 -and $null -ne $script:TargetHwnd) { $windowHandle = [long]$script:TargetHwnd }
    $hwnd = if ($windowHandle -ne 0) { [IntPtr][long]$windowHandle } else { [IntPtr][long]$process.MainWindowHandle }
    if ($hwnd -ne [IntPtr]::Zero -and [OCUWin32]::IsAlive($hwnd)) {
        return [Windows.Automation.AutomationElement]::FromHandle($hwnd)
    }
    $condition = New-Object Windows.Automation.PropertyCondition ([Windows.Automation.AutomationElement]::ProcessIdProperty), $process.Id
    $children = [Windows.Automation.AutomationElement]::RootElement.FindAll([Windows.Automation.TreeScope]::Children, $condition)
    if ($children.Count -gt 0) {
        return $children.Item(0)
    }
    throw "No top-level UI Automation window is available for $($process.ProcessName). Run the Windows runtime in the signed-in desktop session."
}

function Get-WindowBounds($process, $element, [long]$windowHandle = 0) {
    if ($windowHandle -eq 0 -and $null -ne $script:TargetHwnd) { $windowHandle = [long]$script:TargetHwnd }
    $hwnd = if ($windowHandle -ne 0) { [IntPtr][long]$windowHandle } else { [IntPtr][long]$process.MainWindowHandle }
    if ($hwnd -ne [IntPtr]::Zero) {
        $fromWin32 = Get-WindowRectFrame $hwnd
        if ($null -ne $fromWin32) {
            return $fromWin32
        }
    }
    try {
        $rect = $element.Current.BoundingRectangle
        if (-not $rect.IsEmpty -and $rect.Width -gt 0 -and $rect.Height -gt 0) {
            return New-Frame $rect.X $rect.Y $rect.Width $rect.Height
        }
    } catch {
    }
    return $null
}

function Get-PatternNames($element) {
    $names = New-Object System.Collections.Generic.List[string]
    foreach ($pattern in $element.GetSupportedPatterns()) {
        $programmatic = $pattern.ProgrammaticName
        if ($programmatic -like "InvokePatternIdentifiers.Pattern") { $names.Add("Invoke") }
        elseif ($programmatic -like "TogglePatternIdentifiers.Pattern") { $names.Add("Toggle") }
        elseif ($programmatic -like "SelectionItemPatternIdentifiers.Pattern") { $names.Add("Select") }
        elseif ($programmatic -like "ExpandCollapsePatternIdentifiers.Pattern") {
            try {
                $state = $element.GetCurrentPattern([Windows.Automation.ExpandCollapsePattern]::Pattern).Current.ExpandCollapseState
                if ($state -eq [Windows.Automation.ExpandCollapseState]::Collapsed) { $names.Add("Expand") }
                elseif ($state -eq [Windows.Automation.ExpandCollapseState]::Expanded) { $names.Add("Collapse") }
            } catch {
                $names.Add("Expand")
                $names.Add("Collapse")
            }
        }
        elseif ($programmatic -like "ScrollItemPatternIdentifiers.Pattern") { $names.Add("ScrollIntoView") }
        elseif ($programmatic -like "ScrollPatternIdentifiers.Pattern") { $names.Add("Scroll") }
        elseif ($programmatic -like "ValuePatternIdentifiers.Pattern") { $names.Add("SetValue") }
    }
    if ($names.Count -gt 0) {
        return @($names | Select-Object -Unique)
    }
    return @()
}

function Get-ElementString($element, [string]$propertyName) {
    try {
        $value = $element.Current.$propertyName
        if ($null -eq $value) {
            return ""
        }
        return [string]$value
    } catch {
        return ""
    }
}

function Get-ElementInt64($element, [string]$propertyName) {
    try {
        return [int64]$element.Current.$propertyName
    } catch {
        return 0
    }
}

function Get-ElementControlTypeName($element) {
    try {
        $controlType = $element.Current.ControlType
        if ($null -eq $controlType) {
            return ""
        }
        return [string]$controlType.ProgrammaticName
    } catch {
        return ""
    }
}

function Resolve-TextLimit($Value) {
    if ($null -eq $Value) {
        return $script:DefaultTextLimit
    }
    if ($Value -is [string] -and $Value.Trim().ToLowerInvariant() -eq "max") {
        return $null
    }
    if ($Value -is [bool]) {
        return $script:DefaultTextLimit
    }
    try {
        $integer = [int]$Value
        if ($integer -gt 0) {
            return $integer
        }
    } catch {
    }
    return $script:DefaultTextLimit
}

function Limit-Text([string]$Text, $TextLimit = $script:DefaultTextLimit) {
    if ($null -eq $Text) {
        return ""
    }
    if ($null -eq $TextLimit) {
        return $Text
    }
    $effectiveTextLimit = [int]$TextLimit
    if ($Text.Length -gt $effectiveTextLimit) {
        return $Text.Substring(0, $effectiveTextLimit) + "..."
    }
    return $Text
}

function Test-PasswordElement($element) {
    try { return ($element.Current.IsPassword -ne $false) } catch { return $true }
}

function Get-NameDigest([string]$name) {
    $hash = [Security.Cryptography.SHA256]::Create()
    try { [Convert]::ToBase64String($hash.ComputeHash([Text.Encoding]::UTF8.GetBytes($name))) }
    finally { $hash.Dispose() }
}

function Get-ElementValue($element, $TextLimit = $script:DefaultTextLimit) {
    if (Test-PasswordElement $element) { return "" }
    try {
        $valuePattern = $element.GetCurrentPattern([Windows.Automation.ValuePattern]::Pattern)
        $value = $valuePattern.Current.Value
        if ($null -eq $value) {
            return ""
        }
        $text = [string]$value
        return Limit-Text $text $TextLimit
    } catch {
        return ""
    }
}

function Get-ElementRecord($element, [int]$index, $windowBounds, $TextLimit = $script:DefaultTextLimit) {
    $frame = Get-ElementFrame $element $windowBounds
    $fullName = Get-ElementString $element "Name"
    $runtimeId = @()
    try { $runtimeId = @($element.GetRuntimeId()) } catch {}
    [pscustomobject]@{
        index = $index
        runtimeId = $runtimeId
        automationId = Get-ElementString $element "AutomationId"
        identityName = $fullName
        name = Limit-Text $fullName $TextLimit
        nameDigest = Get-NameDigest $fullName
        controlType = Get-ElementControlTypeName $element
        localizedControlType = Get-ElementString $element "LocalizedControlType"
        className = Get-ElementString $element "ClassName"
        value = Get-ElementValue $element $TextLimit
        nativeWindowHandle = Get-ElementInt64 $element "NativeWindowHandle"
        frame = $frame
        actions = @(Get-PatternNames $element)
    }
}

function Get-ElementTitle($record) {
    if (-not [string]::IsNullOrWhiteSpace($record.name)) {
        return $record.name
    }
    if (-not [string]::IsNullOrWhiteSpace($record.automationId)) {
        return "ID: $($record.automationId)"
    }
    return ""
}

function New-TreeCacheRequest {
    $cache = New-Object Windows.Automation.CacheRequest
    $cache.TreeScope = [Windows.Automation.TreeScope]::Element
    $cache.TreeFilter = [Windows.Automation.Condition]::TrueCondition
    foreach ($name in @('ControlType', 'IsPassword', 'IsOffscreen', 'BoundingRectangle', 'AutomationId', 'Name', 'LocalizedControlType', 'ClassName', 'NativeWindowHandle',
        'IsInvokePatternAvailable', 'IsTogglePatternAvailable', 'IsSelectionItemPatternAvailable', 'IsExpandCollapsePatternAvailable',
        'IsScrollItemPatternAvailable', 'IsScrollPatternAvailable', 'IsValuePatternAvailable')) {
        $cache.Add([Windows.Automation.AutomationElement]::($name + 'Property'))
    }
    $cache.Add([Windows.Automation.ExpandCollapsePattern]::ExpandCollapseStateProperty)
    return $cache
}

function Get-TreeRecord($element, [int]$index, $windowBounds, $TextLimit) {
    try {
        $info = $element.Cached
        $rect = $info.BoundingRectangle
        $frame = $null
        if (-not $rect.IsEmpty -and $rect.Width -gt 0 -and $rect.Height -gt 0) {
            $frame = if ($null -eq $windowBounds) { New-Frame $rect.X $rect.Y $rect.Width $rect.Height }
                     else { New-Frame ($rect.X - $windowBounds.x) ($rect.Y - $windowBounds.y) $rect.Width $rect.Height }
        }
        $actions = New-Object System.Collections.Generic.List[string]
        foreach ($pair in @(@('Invoke', 'Invoke'), @('Toggle', 'Toggle'), @('SelectionItem', 'Select'),
            @('ScrollItem', 'ScrollIntoView'), @('Scroll', 'Scroll'), @('Value', 'SetValue'))) {
            if ($element.GetCachedPropertyValue([Windows.Automation.AutomationElement]::('Is' + $pair[0] + 'PatternAvailableProperty'))) {
                $actions.Add($pair[1])
            }
        }
        if ($element.GetCachedPropertyValue([Windows.Automation.AutomationElement]::IsExpandCollapsePatternAvailableProperty)) {
            $state = $element.GetCachedPropertyValue([Windows.Automation.ExpandCollapsePattern]::ExpandCollapseStateProperty)
            if ($state -eq [Windows.Automation.ExpandCollapseState]::Collapsed) { $actions.Add('Expand') }
            elseif ($state -eq [Windows.Automation.ExpandCollapseState]::Expanded) { $actions.Add('Collapse') }
        }
        $value = ''
        if ($info.IsPassword -eq $false) {
            $value = Get-ElementValue $element $TextLimit
        }
        return [pscustomobject]@{
            index = $index
            runtimeId = @($element.GetCachedPropertyValue([Windows.Automation.AutomationElement]::RuntimeIdProperty))
            automationId = $info.AutomationId
            identityName = $info.Name
            name = Limit-Text $info.Name $TextLimit
            nameDigest = Get-NameDigest $info.Name
            controlType = $info.ControlType.ProgrammaticName
            localizedControlType = $info.LocalizedControlType
            className = $info.ClassName
            value = $value
            nativeWindowHandle = [int64]$info.NativeWindowHandle
            frame = $frame
            actions = $actions.ToArray()
        }
    } catch {
        return Get-ElementRecord $element $index $windowBounds $TextLimit
    }
}

function Get-TreeChildren($node, [int]$limit) {
    if ($limit -le 0) {
        return [pscustomobject]@{ children = @(); truncated = ($null -ne [Windows.Automation.TreeWalker]::RawViewWalker.GetFirstChild($node)) }
    }
    $children = $node.FindAll([Windows.Automation.TreeScope]::Children, [Windows.Automation.Condition]::TrueCondition)
    $result = New-Object System.Collections.Generic.List[object]
    for ($i = 0; $i -lt [math]::Min($children.Count, $limit); $i++) { $result.Add($children.Item($i)) }
    [pscustomobject]@{ children = $result.ToArray(); truncated = ($children.Count -gt $limit) }
}

function Get-PrioritizedTree($root, [int]$MaxNodes, [int]$MaxDepth) {
    $queues = @(); $offsets = @(0, 0, 0, 0, 0)
    for ($i = 0; $i -lt 5; $i++) { $queues += ,(New-Object System.Collections.Generic.List[object]) }
    $seen = New-Object System.Collections.Generic.HashSet[string]
    $selected = New-Object System.Collections.Generic.List[object]
    $state = @{ reasons = (New-Object System.Collections.Generic.HashSet[string]); scanned = 0; truncated = $false; limit = [math]::Max(64, [math]::Min($MaxNodes, 2500) * 4) }
    function Enqueue($node, [int]$depth, [string]$path, [bool]$inCollection) {
        if ($depth -gt $MaxDepth -or $state.scanned -ge $state.limit) { $state.truncated = $true; [void]$state.reasons.Add($(if ($depth -gt $MaxDepth) { 'depth' } else { 'nodes' })); return }
        $state.scanned++
        if ($null -eq $node) { return }
        try { $runtime = (@($node.GetCachedPropertyValue([Windows.Automation.AutomationElement]::RuntimeIdProperty)) -join '.') }
        catch { try { $runtime = (@($node.GetRuntimeId()) -join '.') } catch { $runtime = $path } }
        if ([string]::IsNullOrWhiteSpace($runtime)) { $runtime = $path }
        if (-not $seen.Add($runtime)) { return }
        try { $role = $node.Cached.ControlType.ProgrammaticName } catch { $role = Get-ElementControlTypeName $node }
        if ([string]::IsNullOrWhiteSpace($role)) { $role = Get-ElementControlTypeName $node }
        $collection = $inCollection -or $role -in @('ControlType.List', 'ControlType.DataGrid', 'ControlType.Tree', 'ControlType.Table')
        $hidden = $false
        try {
            $info = $node.Cached
            if ($null -eq $info) { $info = $node.Current }
            $hidden = [bool]$info.IsOffscreen
        } catch { try { $hidden = [bool]$node.Current.IsOffscreen } catch { } }
        $control = $role -in @('ControlType.Button', 'ControlType.CheckBox', 'ControlType.RadioButton', 'ControlType.Edit', 'ControlType.ComboBox', 'ControlType.Slider', 'ControlType.Spinner', 'ControlType.Hyperlink')
        $rank = if ($hidden) { 4 } elseif ($collection) { if ($control) { 2 } else { 3 } } elseif ($control) { 0 } else { 1 }
        $queues[$rank].Add([pscustomobject]@{ node = $node; depth = $depth; path = $path; collection = $collection })
    }
    Enqueue $root 0 '' $false
    while ($selected.Count -lt $MaxNodes) {
        $rank = -1
        for ($i = 0; $i -lt 5; $i++) { if ($offsets[$i] -lt $queues[$i].Count) { $rank = $i; break } }
        if ($rank -lt 0) { break }
        $work = $queues[$rank][$offsets[$rank]]; $offsets[$rank]++
        $selected.Add($work)
        try {
            $remaining = if ($work.depth -ge $MaxDepth) { 0 } else { [math]::Max(0, $state.limit - $state.scanned) }
            $result = Get-TreeChildren $work.node $remaining
            if ($result.truncated) { $state.truncated = $true; [void]$state.reasons.Add($(if ($work.depth -ge $MaxDepth) { 'depth' } else { 'nodes' })) }
            for ($i = 0; $i -lt $result.children.Count; $i++) {
                Enqueue $result.children[$i] ($work.depth + 1) ($work.path + '/' + $i.ToString('D10')) $work.collection
            }
        } catch { $state.truncated = $true }
    }
    for ($i = 0; $i -lt 5; $i++) { if ($offsets[$i] -lt $queues[$i].Count) { $state.truncated = $true } }
    [pscustomobject]@{ nodes = @($selected | Sort-Object path); truncated = $state.truncated; reasons = @($state.reasons) }
}

function Render-Tree($element, $windowBounds, $TextLimit = $script:DefaultTextLimit, [int]$MaxTreeNodes = $script:AccessibilityTreeMaxNodeCount, [int]$MaxTreeDepth = $script:AccessibilityTreeMaxDepth) {
    $records = New-Object System.Collections.Generic.List[object]
    $lines = New-Object System.Collections.Generic.List[string]
    $effectiveMaxTreeNodes = if ($MaxTreeNodes -gt 0) { $MaxTreeNodes } else { $script:AccessibilityTreeMaxNodeCount }
    $effectiveMaxTreeDepth = if ($MaxTreeDepth -gt 0) { $MaxTreeDepth } else { $script:AccessibilityTreeMaxDepth }
    $cache = New-TreeCacheRequest
    $scope = $cache.Activate()
    try {
        try { $element = $element.GetUpdatedCache($cache) } catch { }
        $plan = Get-PrioritizedTree $element $effectiveMaxTreeNodes $effectiveMaxTreeDepth
    } finally { $scope.Dispose() }
    $parents = @{}
    $truncated = New-Object System.Collections.Generic.HashSet[string]
    foreach ($reason in $plan.reasons) { [void]$truncated.Add($reason) }
    if ($plan.truncated -and $truncated.Count -eq 0) { [void]$truncated.Add('nodes') }
    foreach ($work in $plan.nodes) {
        Assert-OperationActive
        $depth = $work.depth
        $index = $records.Count
        $record = Get-TreeRecord $work.node $index $windowBounds $TextLimit
        $parentPath = if ($work.path.Contains('/')) { $work.path.Substring(0, $work.path.LastIndexOf('/')) } else { $null }
        $parent = if ($null -ne $parentPath -and $parents.ContainsKey($parentPath)) { $parents[$parentPath] } else { -1 }
        $record | Add-Member -NotePropertyName parentIndex -NotePropertyValue $parent
        $parents[$work.path] = $index
        if ($null -ne $TextLimit -and ($record.identityName.Length -gt $TextLimit -or (Get-ElementValue $work.node $null).Length -gt $TextLimit)) { [void]$truncated.Add('text') }
        $records.Add($record)
        $role = $record.localizedControlType
        if ([string]::IsNullOrWhiteSpace($role)) {
            $role = $record.controlType
        }
        $title = Get-ElementTitle $record
        $actionsSegment = ""
        if ($record.actions.Count -gt 0) {
            $actionsSegment = " Secondary Actions: " + ($record.actions -join ", ")
        }
        $valueSegment = ""
        if (-not [string]::IsNullOrWhiteSpace($record.value) -and $record.value -ne $title) {
            $safeValue = (($record.value -replace "`r", "\\r") -replace "`n", "\\n")
            $valueSegment = " Value: $safeValue"
        }
        $frameSegment = ""
        if ($null -ne $record.frame) {
            $frameSegment = " Frame: {{x: {0}, y: {1}, width: {2}, height: {3}}}" -f [int][math]::Round($record.frame.x), [int][math]::Round($record.frame.y), [int][math]::Round($record.frame.width), [int][math]::Round($record.frame.height)
        }
        $lines.Add(("`t" * ($depth + 1)) + "$index $role $title$valueSegment$actionsSegment$frameSegment")

    }
    if ($plan.truncated) {
        $lines.Add('[Accessibility tree truncated: prioritized visible controls; some nodes were omitted.]')
    }

    [pscustomobject]@{
        truncated = @($truncated)
        records = $records.ToArray()
        lines = $lines.ToArray()
    }
}

function Test-WindowCaptureBounds($bounds, [IntPtr]$hwnd) {
    if ($null -eq $bounds -or $hwnd -eq [IntPtr]::Zero) { return $false }
    foreach ($value in @($bounds.x, $bounds.y, $bounds.width, $bounds.height)) {
        if ([double]::IsNaN([double]$value) -or [double]::IsInfinity([double]$value)) { return $false }
    }
    if ($bounds.width -lt 1 -or $bounds.height -lt 1 -or $bounds.width * $bounds.height -gt 16000000) { return $false }
    if (-not [OCUWin32]::IsWindowVisible($hwnd) -or [OCUWin32]::IsIconic($hwnd)) { return $false }
    $current = Get-WindowRectFrame $hwnd
    return $null -ne $current -and $current.x -eq $bounds.x -and $current.y -eq $bounds.y -and
        $current.width -eq $bounds.width -and $current.height -eq $bounds.height
}

function Capture-WindowPngBase64($bounds, [IntPtr]$hwnd) {
    if (-not (Test-WindowCaptureBounds $bounds $hwnd)) {
        return $null
    }
    $bitmap = $null
    $graphics = $null
    $stream = $null
    try {
        $bitmap = New-Object System.Drawing.Bitmap ([int][math]::Round($bounds.width)), ([int][math]::Round($bounds.height))
        $graphics = [System.Drawing.Graphics]::FromImage($bitmap)
        # Capture the requested HWND even when another app covers it.
        $hdc = $graphics.GetHdc()
        try {
            if (-not [OCUWin32]::PrintWindow($hwnd, $hdc, 2)) { return $null }
        } finally { $graphics.ReleaseHdc($hdc) }
        if (-not (Test-WindowCaptureBounds $bounds $hwnd)) { return $null }
        $stream = New-Object System.IO.MemoryStream
        $bitmap.Save($stream, [System.Drawing.Imaging.ImageFormat]::Png)
        $bytes = $stream.ToArray()
        return [Convert]::ToBase64String($bytes)
    } catch {
        return $null
    } finally {
        if ($null -ne $graphics) { $graphics.Dispose() }
        if ($null -ne $bitmap) { $bitmap.Dispose() }
        if ($null -ne $stream) { $stream.Dispose() }
    }
}

function Get-FocusedSummary($processId, $TextLimit = $script:DefaultTextLimit) {
    try {
        $focused = [Windows.Automation.AutomationElement]::FocusedElement
        if ($null -ne $focused -and $focused.Current.ProcessId -eq $processId) {
            $role = $focused.Current.LocalizedControlType
            $name = Limit-Text $focused.Current.Name $TextLimit
            if ([string]::IsNullOrWhiteSpace($name)) {
                return $role
            }
            return "$role $name"
        }
    } catch {
    }
    return $null
}

function Test-ChromiumWindow($processId) {
    try {
        $process = Get-Process -Id $processId -ErrorAction Stop
        $className = [OCUWin32]::ClassNameOf($(if ($script:TargetHwnd) { [IntPtr][long]$script:TargetHwnd } else { $process.MainWindowHandle }))
        if ([string]::IsNullOrEmpty($className)) { return $null }
        return $className -like "Chrome_*"
    } catch { return $null }
}

function Read-SafeSelectedText($focused, $TextLimit) {
    if (Test-PasswordElement $focused) { return $null }
    # Embedded Chromium controls can live under a non-Chromium host HWND.
    if ($focused.Current.FrameworkId -eq 'Chrome' -or $focused.Current.ClassName -like 'Chrome_*') { return $null }
    $textPattern = $focused.GetCurrentPattern([Windows.Automation.TextPattern]::Pattern)
    $selection = $textPattern.GetSelection()
    if ($selection.Count -gt 0) {
        $maxLength = if ($null -eq $TextLimit) { -1 } else { [int]$TextLimit + 1 }
        return Limit-Text ($selection.Item(0).GetText($maxLength)) $TextLimit
    }
    return $null
}

function Get-SelectedText($processId, $TextLimit = $script:DefaultTextLimit) {
    # Chromium and Electron UIA providers raise an uncatchable AccessViolationException
    # inside TextPatternRange.GetText, which terminates the PowerShell runspace and
    # fails every observation/action for that app. Skip the selection read for them.
    if ((Test-ChromiumWindow $processId) -ne $false) {
        return $null
    }
    try {
        $focused = [Windows.Automation.AutomationElement]::FocusedElement
        if ($null -eq $focused -or $focused.Current.ProcessId -ne $processId) {
            return $null
        }
        return Read-SafeSelectedText $focused $TextLimit
    } catch {
    }
    return $null
}

function Resolve-ObservationOptions($operation) {
    $values = @{ include_ax = $true; include_screenshot = $true }
    foreach ($key in @("include_ax", "include_screenshot")) {
        $property = $operation.PSObject.Properties[$key]
        if ($null -ne $property) {
            if ($property.Value -isnot [bool]) { throw "$key must be a boolean" }
            $values[$key] = $property.Value
        }
    }
    if (-not $values.include_ax -and -not $values.include_screenshot) {
        throw "At least one of include_ax or include_screenshot must be true"
    }
    return [pscustomobject]$values
}

function Resolve-OperationWindow($process, $operation) {
    if ($operation.expected_pid -and [int]$operation.expected_pid -ne [int]$process.Id) {
        throw 'staleSnapshot: the observed application has restarted; get_app_state is required'
    }
    $hwnd = if ($operation.windowHandle) { [IntPtr][long]$operation.windowHandle } else { [IntPtr][long]$process.MainWindowHandle }
    if ($operation.windowHandle -and (-not [OCUWin32]::IsAlive($hwnd) -or [OCUWin32]::OwnerProcessOf($hwnd) -ne [uint32]$process.Id)) {
        throw 'staleSnapshot: the observed window is no longer owned by the application; get_app_state is required'
    }
    return $hwnd
}

function Build-Snapshot([string]$query, $TextLimit = $script:DefaultTextLimit, [int]$MaxTreeNodes = $script:AccessibilityTreeMaxNodeCount, [int]$MaxTreeDepth = $script:AccessibilityTreeMaxDepth, [bool]$IncludeAX = $true, [bool]$IncludeScreenshot = $true) {
    if (-not $IncludeAX -and -not $IncludeScreenshot) { throw "At least one observation channel is required" }
    $process = Resolve-App $query
    $hwnd = Resolve-OperationWindow $process ([pscustomobject]@{ windowHandle = $script:TargetHwnd })
    $element = Get-MainElement $process $hwnd
    $bounds = Get-WindowBounds $process $element $hwnd
    $rendered = if ($IncludeAX) { Render-Tree $element $bounds $TextLimit $MaxTreeNodes $MaxTreeDepth } else { @{ records = @(); lines = @(); truncated = @() } }
    $image = if ($IncludeScreenshot) { Capture-WindowPngBase64 $bounds $hwnd } else { $null }
    $focus = if ($IncludeAX) { Get-FocusedSummary $process.Id $TextLimit } else { $null }
    $selection = if ($IncludeAX) { Get-SelectedText $process.Id $TextLimit } else { $null }
    [pscustomobject]@{
        app = [pscustomobject]@{
            name = $process.ProcessName
            bundleIdentifier = $process.ProcessName
            pid = [int]$process.Id
        }
        windowHandle = [long]$hwnd
        windowTitle = Limit-Text $process.MainWindowTitle $TextLimit
        windowBounds = $bounds
        screenshotPngBase64 = $image
        treeLines = @($rendered.lines)
        focusedSummary = $focus
        selectedText = $selection
        elements = @($rendered.records)
    }
}

function List-Apps {
    $lines = New-Object System.Collections.Generic.List[string]
    foreach ($process in (Get-Process | Where-Object { $_.MainWindowHandle -ne 0 } | Sort-Object ProcessName, Id)) {
        $title = $process.MainWindowTitle
        if ([string]::IsNullOrWhiteSpace($title)) {
            $title = "untitled"
        }
        $lines.Add(("{0} -- {1} [running, pid={2}, window={3}]" -f $process.ProcessName, $process.ProcessName, $process.Id, $title))
    }
    return ($lines -join "`n")
}

function Same-RuntimeId($left, $right) {
    if ($null -eq $left -or $null -eq $right -or $left.Count -ne $right.Count) {
        return $false
    }
    for ($i = 0; $i -lt $left.Count; $i++) {
        if ([int]$left[$i] -ne [int]$right[$i]) {
            return $false
        }
    }
    return $true
}

function Get-AllElements($root) {
    $items = New-Object System.Collections.Generic.List[object]
    $items.Add($root)
    try {
        $descendants = $root.FindAll([Windows.Automation.TreeScope]::Descendants, [Windows.Automation.Condition]::TrueCondition)
        for ($i = 0; $i -lt $descendants.Count; $i++) {
            $items.Add($descendants.Item($i))
        }
    } catch {
    }
    return $items.ToArray()
}

function Find-Element($process, $record) {
    if ($null -eq $record) { return $null }
    foreach ($element in (Get-AllElements (Get-MainElement $process))) {
        try {
            if (-not (Same-RuntimeId @($element.GetRuntimeId()) @($record.runtimeId))) { continue }
            $current = $element.Current
            if ($current.ControlType.ProgrammaticName -ceq $record.controlType -and
                ([string]$current.AutomationId) -ceq ([string]$record.automationId) -and
                -not [string]::IsNullOrEmpty($record.nameDigest) -and
                (Get-NameDigest $current.Name) -ceq $record.nameDigest) {
                return $element
            }
        } catch { }
    }
    throw "Element $($record.index) changed or is no longer available. Run get_app_state again before acting; nothing was sent."
}

function Invoke-VerifiedSetValue($pattern, [string]$value) {
    if ($pattern.Current.IsReadOnly) { throw "Cannot set a value for an element that is not settable" }
    $pattern.SetValue($value)
    $clock = [Diagnostics.Stopwatch]::StartNew()
    while ($true) {
        $matched = $false
        try { $matched = [string]::Equals($pattern.Current.Value, $value, [StringComparison]::Ordinal) } catch { }
        if ($matched) { return }
        if ($clock.Elapsed.TotalSeconds -ge 0.6) {
            throw "set_value could not verify the requested value. Read the current value before retrying; the write may have changed the control."
        }
        Start-Sleep -Milliseconds 50
    }
}

function Get-CurrentPatternOrNull($element, $pattern) {
    try {
        return $element.GetCurrentPattern($pattern)
    } catch {
        return $null
    }
}

function Invoke-PreferredClick($element) {
    $invoke = Get-CurrentPatternOrNull $element ([Windows.Automation.InvokePattern]::Pattern)
    if ($null -ne $invoke) {
        $invoke.Invoke()
        return $true
    }
    $selection = Get-CurrentPatternOrNull $element ([Windows.Automation.SelectionItemPattern]::Pattern)
    if ($null -ne $selection) {
        $selection.Select()
        return $true
    }
    $toggle = Get-CurrentPatternOrNull $element ([Windows.Automation.TogglePattern]::Pattern)
    if ($null -ne $toggle) {
        $toggle.Toggle()
        return $true
    }
    return $false
}

function Invoke-SecondaryAction($element, [string]$action) {
    switch ($action.ToLowerInvariant()) {
        "invoke" {
            $pattern = Get-CurrentPatternOrNull $element ([Windows.Automation.InvokePattern]::Pattern)
            if ($null -ne $pattern) { $pattern.Invoke(); return }
        }
        "toggle" {
            $pattern = Get-CurrentPatternOrNull $element ([Windows.Automation.TogglePattern]::Pattern)
            if ($null -ne $pattern) { $pattern.Toggle(); return }
        }
        "select" {
            $pattern = Get-CurrentPatternOrNull $element ([Windows.Automation.SelectionItemPattern]::Pattern)
            if ($null -ne $pattern) { $pattern.Select(); return }
        }
        "expand" {
            $pattern = Get-CurrentPatternOrNull $element ([Windows.Automation.ExpandCollapsePattern]::Pattern)
            if ($null -ne $pattern) { $pattern.Expand(); return }
        }
        "collapse" {
            $pattern = Get-CurrentPatternOrNull $element ([Windows.Automation.ExpandCollapsePattern]::Pattern)
            if ($null -ne $pattern) { $pattern.Collapse(); return }
        }
        "scrollintoview" {
            $pattern = Get-CurrentPatternOrNull $element ([Windows.Automation.ScrollItemPattern]::Pattern)
            if ($null -ne $pattern) { $pattern.ScrollIntoView(); return }
        }
        "setfocus" {
            if (-not (Test-EnvFlagEnabled "OPEN_COMPUTER_USE_WINDOWS_ALLOW_FOCUS_ACTIONS")) {
                throw "SetFocus is disabled by default to avoid stealing user focus; set OPEN_COMPUTER_USE_WINDOWS_ALLOW_FOCUS_ACTIONS=1 to enable it."
            }
            $element.SetFocus()
            return
        }
    }
    throw "$action is not a valid secondary action for $($operation.element.index)"
}

function Invoke-Scroll($element, [string]$direction, [double]$pages) {
    $scroll = Get-CurrentPatternOrNull $element ([Windows.Automation.ScrollPattern]::Pattern)
    if ($null -eq $scroll) {
        return $false
    }
    $horizontal = [Windows.Automation.ScrollAmount]::NoAmount
    $vertical = [Windows.Automation.ScrollAmount]::NoAmount
    if ($direction -eq "up") { $vertical = [Windows.Automation.ScrollAmount]::LargeDecrement }
    elseif ($direction -eq "down") { $vertical = [Windows.Automation.ScrollAmount]::LargeIncrement }
    elseif ($direction -eq "left") { $horizontal = [Windows.Automation.ScrollAmount]::LargeDecrement }
    elseif ($direction -eq "right") { $horizontal = [Windows.Automation.ScrollAmount]::LargeIncrement }
    # A scroll pattern that currently has nothing to scroll is a no-op for a real wheel too, not a failure.
    try {
        if ($vertical -ne [Windows.Automation.ScrollAmount]::NoAmount -and -not $scroll.Current.VerticallyScrollable) { return $true }
        if ($horizontal -ne [Windows.Automation.ScrollAmount]::NoAmount -and -not $scroll.Current.HorizontallyScrollable) { return $true }
    } catch {
    }
    $repeat = [math]::Max(1, [int][math]::Ceiling($pages))
    for ($i = 0; $i -lt $repeat; $i++) {
        Assert-OperationActive
        try {
            $scroll.Scroll($horizontal, $vertical)
        } catch {
            return $false
        }
        Start-Sleep -Milliseconds 40
    }
    return $true
}

function Find-TextEntryElement($process) {
    try {
        $focused = [Windows.Automation.AutomationElement]::FocusedElement
        if ($null -ne $focused -and $focused.Current.ProcessId -eq $process.Id) {
            $focusedValue = Get-CurrentPatternOrNull $focused ([Windows.Automation.ValuePattern]::Pattern)
            if ($null -ne $focusedValue -and -not $focusedValue.Current.IsReadOnly) {
                return $focused
            }
        }
    } catch {
    }

    $root = Get-MainElement $process
    foreach ($element in (Get-AllElements $root)) {
        $valuePattern = Get-CurrentPatternOrNull $element ([Windows.Automation.ValuePattern]::Pattern)
        if ($null -eq $valuePattern -or $valuePattern.Current.IsReadOnly) {
            continue
        }
        $controlType = Get-ElementControlTypeName $element
        if ($controlType -like "*Edit*" -or $controlType -like "*Document*") {
            return $element
        }
    }

    foreach ($element in (Get-AllElements $root)) {
        $valuePattern = Get-CurrentPatternOrNull $element ([Windows.Automation.ValuePattern]::Pattern)
        if ($null -ne $valuePattern -and -not $valuePattern.Current.IsReadOnly) {
            return $element
        }
    }

    return $null
}

function Get-NativeWindowHandle($element) {
    $handle = Get-ElementInt64 $element "NativeWindowHandle"
    if ($handle -le 0) {
        return [IntPtr]::Zero
    }
    return [IntPtr]$handle
}

function Test-TextWindowHandleCandidate($process, $element) {
    if ($null -eq $element) {
        return $false
    }
    $handle = Get-NativeWindowHandle $element
    if ($handle -eq [IntPtr]::Zero -or $handle -eq [IntPtr]$process.MainWindowHandle) {
        return $false
    }
    $controlType = Get-ElementControlTypeName $element
    $className = Get-ElementString $element "ClassName"
    return (
        $controlType -like "*Edit*" -or
        $controlType -like "*Document*" -or
        $className -like "*Edit*" -or
        $className -like "*Rich*" -or
        $className -like "*Text*"
    )
}

function Find-TextEntryWindowHandle($process, $preferredElement) {
    if (Test-TextWindowHandleCandidate $process $preferredElement) {
        return Get-NativeWindowHandle $preferredElement
    }

    $root = Get-MainElement $process
    foreach ($element in (Get-AllElements $root)) {
        if (-not (Test-TextWindowHandleCandidate $process $element)) {
            continue
        }
        $valuePattern = Get-CurrentPatternOrNull $element ([Windows.Automation.ValuePattern]::Pattern)
        if ($null -ne $valuePattern -and -not $valuePattern.Current.IsReadOnly) {
            return Get-NativeWindowHandle $element
        }
    }

    foreach ($element in (Get-AllElements $root)) {
        if (Test-TextWindowHandleCandidate $process $element) {
            return Get-NativeWindowHandle $element
        }
    }

    return [IntPtr]::Zero
}

function Invoke-TypeText($process, [string]$text, [bool]$AllowFocusFallback = $true) {
    $element = Find-TextEntryElement $process
    $targetHwnd = Find-TextEntryWindowHandle $process $element
    if ($targetHwnd -ne [IntPtr]::Zero -and (Send-TextToEditHandle $targetHwnd $text $element)) {
        return $true
    }

    if ($null -ne $element) {
        $valuePattern = Get-CurrentPatternOrNull $element ([Windows.Automation.ValuePattern]::Pattern)
        if ($null -ne $valuePattern -and -not $valuePattern.Current.IsReadOnly) {
            # Writing into the element that already owns the caret does not move focus, so it stays
            # enabled without opting in. Reaching an unfocused control can still activate the app,
            # so that path keeps requiring OPEN_COMPUTER_USE_WINDOWS_ALLOW_UIA_TEXT_FALLBACK=1.
            $alreadyFocused = $false
            try {
                $focused = [Windows.Automation.AutomationElement]::FocusedElement
                $alreadyFocused = $null -ne $focused -and $focused.Equals($element)
            } catch {
            }
            if (-not $alreadyFocused) {
                if (-not $AllowFocusFallback -or -not (Test-EnvFlagEnabled "OPEN_COMPUTER_USE_WINDOWS_ALLOW_UIA_TEXT_FALLBACK")) {
                    # Nothing has been typed yet: reporting an uncertain ("possible") effect here would force the
                    # caller to discard the whole session even though the target never received any input.
                    $script:sdkCode = 'UNSUPPORTED_CAPABILITY'
                    $script:sdkEffect = 'none'
                    throw "UIA ValuePattern text fallback is disabled by default because it may bring the target app to the foreground; set OPEN_COMPUTER_USE_WINDOWS_ALLOW_UIA_TEXT_FALLBACK=1 to enable it."
                }
            }
            $current = ""
            try { $current = [string]$valuePattern.Current.Value } catch {}
            $valuePattern.SetValue($current + $text)
            return $true
        }
    }
    return $false
}

function Invoke-Operation($process, $operation, $element) {
    $hwnd = if ($null -ne $operation.windowHandle -and [long]$operation.windowHandle -ne 0) { [IntPtr][long]$operation.windowHandle } else { [IntPtr][long]$process.MainWindowHandle }
    $windowBounds = $operation.windowBounds
    switch ($operation.tool) {
        "click" {
            $clickMethod = [string]$operation.click_method
            if ([string]::IsNullOrWhiteSpace($clickMethod)) { $clickMethod = "auto" }

            if ($clickMethod -eq "accessibility") {
                if ($null -eq $element) {
                    $script:sdkCode = 'INVALID_ARGUMENT'
                    $script:sdkEffect = 'none'
                    throw "click_method 'accessibility' requires element_index"
                }
                if ($operation.mouse_button -eq "right" -or $operation.mouse_button -eq "middle") {
                    $script:sdkCode = 'UNSUPPORTED_CAPABILITY'
                    $script:sdkEffect = 'none'
                    throw "click_method 'accessibility' does not support mouse_button '$($operation.mouse_button)'"
                }
                if (-not (Invoke-PreferredClick $element)) {
                    $script:sdkCode = 'UNSUPPORTED_CAPABILITY'
                    $script:sdkEffect = 'none'
                    throw "click_method 'accessibility' could not click the requested element"
                }
            } elseif ($clickMethod -eq "app_post") {
                if ($null -ne $operation.element -and $null -ne $operation.element.frame) {
                    $point = Get-ScreenPoint $operation.element.frame $windowBounds
                } else {
                    $point = [pscustomobject]@{
                        x = [int][math]::Round($windowBounds.x + [double]$operation.x)
                        y = [int][math]::Round($windowBounds.y + [double]$operation.y)
                    }
                }
                Send-MouseClick $hwnd $point.x $point.y $operation.mouse_button ([int]$operation.click_count)
            } elseif ($clickMethod -eq "global") {
                $script:sdkCode = 'UNSUPPORTED_CAPABILITY'
                $script:sdkEffect = 'none'
                throw "click_method 'global' is not supported on Windows"
            } elseif ($clickMethod -eq "sky_click") {
                $script:sdkCode = 'UNSUPPORTED_CAPABILITY'
                $script:sdkEffect = 'none'
                throw "click_method 'sky_click' is not supported on Windows"
            } elseif ($clickMethod -eq "auto") {
                $handled = $false
                if ($null -ne $element -and $operation.mouse_button -ne "right" -and $operation.mouse_button -ne "middle") {
                    # A double click is a semantic activation for hosted content: invoking twice would toggle or
                    # repeat the control's action instead of activating it.
                    $hostedContent = (Get-InputWindowHandle $hwnd) -ne $hwnd
                    $handled = ($operation.click_count -eq 1 -or $hostedContent) -and (Invoke-PreferredClick $element)
                }
                if (-not $handled) {
                    if ($null -ne $operation.element -and $null -ne $operation.element.frame) {
                        $point = Get-ScreenPoint $operation.element.frame $windowBounds
                    } else {
                        $point = [pscustomobject]@{
                            x = [int][math]::Round($windowBounds.x + [double]$operation.x)
                            y = [int][math]::Round($windowBounds.y + [double]$operation.y)
                        }
                    }
                    Send-MouseClick $hwnd $point.x $point.y $operation.mouse_button ([int]$operation.click_count)
                }
            } else {
                $script:sdkCode = 'INVALID_ARGUMENT'
                $script:sdkEffect = 'none'
                throw "Invalid click_method '$clickMethod'"
            }
        }
        "perform_secondary_action" {
            if ($null -eq $element) {
                $script:sdkCode = 'INVALID_ARGUMENT'
                $script:sdkEffect = 'none'
                throw "unknown element_index '$($operation.element.index)'"
            }
            Invoke-SecondaryAction $element $operation.action
        }
        "scroll" {
            $handled = $false
            if ($null -ne $element) {
                $handled = Invoke-Scroll $element $operation.direction ([double]$operation.pages)
                if (-not $handled) {
                    $scrollable = Get-ScrollableAncestor $element
                    if ($null -ne $scrollable) {
                        $handled = Invoke-Scroll $scrollable $operation.direction ([double]$operation.pages)
                    }
                }
            }
            if (-not $handled) {
                if ((Get-InputWindowHandle $hwnd) -ne $hwnd) {
                    $script:sdkCode = 'UNSUPPORTED_CAPABILITY'
                    $script:sdkEffect = 'none'
                    throw "Scrolling cannot be delivered to this hosted application while it is in the background and the target exposes no ScrollPattern; observe a scrollable element or bring the window to the foreground"
                }
                $frame = if ($null -ne $operation.element) { $operation.element.frame } else { New-Frame 0 0 $windowBounds.width $windowBounds.height }
                $point = Get-ScreenPoint $frame $windowBounds
                Send-Scroll $hwnd $point.x $point.y $operation.direction ([double]$operation.pages)
            }
        }
        "drag" {
            Send-Drag $hwnd ([int][math]::Round($windowBounds.x + [double]$operation.from_x)) ([int][math]::Round($windowBounds.y + [double]$operation.from_y)) ([int][math]::Round($windowBounds.x + [double]$operation.to_x)) ([int][math]::Round($windowBounds.y + [double]$operation.to_y))
        }
        "type_text" {
            if (-not (Invoke-TypeText $process $operation.text ($operation.allowFocusFallback -ne $false))) {
                Send-Text $hwnd $operation.text
            }
        }
        "press_key" {
            Send-Key $hwnd $operation.key
        }
        "set_value" {
            if ($null -eq $element) { throw "unknown element_index '$($operation.element.index)'" }
            $valuePattern = Get-CurrentPatternOrNull $element ([Windows.Automation.ValuePattern]::Pattern)
            if ($null -eq $valuePattern) {
                throw "Cannot set a value for an element that is not settable"
            }
            Invoke-VerifiedSetValue $valuePattern ([string]$operation.value)
        }
        default {
            throw "unsupportedTool(`"$($operation.tool)`")"
        }
    }
}

# Read the operation file as UTF-8 explicitly. Windows PowerShell 5.1's
# Get-Content defaults to the system ANSI code page (e.g. GBK on Chinese
# systems) for files without a BOM, which corrupts non-ASCII input such as
# Chinese text passed to set_value/type_text.
if ($DefinitionsOnly) { return }
$operationJson = [System.IO.File]::ReadAllText($OperationPath, [System.Text.Encoding]::UTF8)
$operation = $operationJson | ConvertFrom-Json

try {
    if ($operation.tool -eq "list_apps") {
        $response = [pscustomobject]@{ ok = $true; text = (List-Apps) }
    } elseif ($operation.tool -eq "get_app_state") {
        $observe = Resolve-ObservationOptions $operation
        $response = [pscustomobject]@{ ok = $true; snapshot = (Build-Snapshot $operation.app (Resolve-TextLimit $operation.text_limit) ([int]$operation.max_tree_nodes) ([int]$operation.max_tree_depth) $observe.include_ax $observe.include_screenshot) }
    } else {
        $process = Resolve-App $operation.app
        $hwnd = Resolve-OperationWindow $process $operation
        $script:TargetHwnd = $hwnd
        $windowBounds = $operation.windowBounds
        $element = Find-Element $process $operation.element

        Invoke-Operation $process $operation $element

        Start-Sleep -Milliseconds 120
        try {
            $response = [pscustomobject]@{ ok = $true; snapshot = (Build-Snapshot $operation.app) }
        } catch {
            $response = [pscustomobject]@{ ok = $true; observationError = $_.Exception.Message }
        }
    }
} catch {
    $message = $_.Exception.Message
    if (-not [string]::IsNullOrWhiteSpace($_.ScriptStackTrace)) {
        $message = "$message at $($_.ScriptStackTrace)"
    }
    $response = [pscustomobject]@{ ok = $false; error = $message }
}

$response | ConvertTo-Json -Depth 50 -Compress
