$ErrorActionPreference = 'Stop'
$OutputEncoding = [System.Text.Encoding]::UTF8
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
# The Go owner joins this process to its private Job Object before releasing stdin.
$line = [Console]::ReadLine()
if ([string]::IsNullOrEmpty($line)) { exit 1 }
$operation = $line | ConvertFrom-Json
. "$PSScriptRoot/runtime.ps1" -DefinitionsOnly

$script:sdkCode = 'TARGET_UNAVAILABLE'
$script:sdkEffect = 'none'

function Get-SDKTarget($process) {
    [pscustomobject]@{ pid = [int]$process.Id; started = $process.StartTime.ToUniversalTime().Ticks.ToString(); hwnd = [long]$process.MainWindowHandle; name = $process.ProcessName }
}
function Resolve-SDKTarget($target) {
    $process = Get-Process -Id ([int]$target.pid) -ErrorAction Stop
    if ($process.StartTime.ToUniversalTime().Ticks.ToString() -ne $target.started -or [long]$process.MainWindowHandle -ne [long]$target.hwnd) {
        $script:sdkCode = 'STALE_SNAPSHOT'
        throw 'Application process or window changed'
    }
    if ($process.MainWindowHandle -eq 0) { throw 'Application has no window' }
    return $process
}
function Get-SDKClick($element) {
    foreach ($name in @('Invoke', 'SelectionItem', 'Toggle')) {
        $pattern = switch ($name) {
            'Invoke' { [Windows.Automation.InvokePattern]::Pattern }
            'SelectionItem' { [Windows.Automation.SelectionItemPattern]::Pattern }
            'Toggle' { [Windows.Automation.TogglePattern]::Pattern }
        }
        if ($null -ne (Get-CurrentPatternOrNull $element $pattern)) { return $name }
    }
    return ''
}
function Limit-SDKText([string]$value, $options, $truncated) {
    if ($options.textLimit -eq 'max') { return $value }
    $limit = 500
    if ($null -ne $options.textLimit) { $limit = [int]$options.textLimit }
    if ($value.Length -gt $limit) {
        if (-not $truncated.Contains('text')) { $truncated.Add('text') }
        return $value.Substring(0, $limit)
    }
    return $value
}
function Get-SDKCapture($process) {
    try {
        Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
public static class SDKWindowCapture {
    [DllImport("user32.dll")] public static extern bool PrintWindow(IntPtr hwnd, IntPtr hdc, uint flags);
}
'@
        $bounds = Get-WindowRectFrame ([IntPtr]$process.MainWindowHandle)
        if ($null -eq $bounds -or $bounds.width -le 0 -or $bounds.height -le 0 -or $bounds.width * $bounds.height -gt 16000000) { throw 'Invalid window capture dimensions' }
        $bitmap = New-Object System.Drawing.Bitmap ([int]$bounds.width), ([int]$bounds.height)
        try {
            $graphics = [System.Drawing.Graphics]::FromImage($bitmap)
            try {
                $hdc = $graphics.GetHdc()
                try { if (-not [SDKWindowCapture]::PrintWindow([IntPtr]$process.MainWindowHandle, $hdc, 2)) { throw 'Window did not provide an image' } }
                finally { $graphics.ReleaseHdc($hdc) }
            } finally { $graphics.Dispose() }
            $stream = New-Object System.IO.MemoryStream
            try {
                $bitmap.Save($stream, [System.Drawing.Imaging.ImageFormat]::Png)
                return @{ status = 'available'; image = @{ mimeType = 'image/png'; width = $bitmap.Width; height = $bitmap.Height; dataBase64 = [Convert]::ToBase64String($stream.ToArray()) } }
            } finally { $stream.Dispose() }
        } finally { $bitmap.Dispose() }
    } catch { return @{ status = 'unavailable'; reason = @{ code = 'CAPTURE_FAILED'; message = $_.Exception.Message } } }
}
function Observe-SDK($target, $options) {
    $process = Resolve-SDKTarget $target
    $root = Get-MainElement $process
    $walker = [Windows.Automation.TreeWalker]::RawViewWalker
    $queue = New-Object System.Collections.Generic.Queue[object]
    $queue.Enqueue(@{ element = $root; parent = ''; path = @(); depth = 0 })
    $elements = New-Object System.Collections.Generic.List[object]
    $truncated = New-Object System.Collections.Generic.List[string]
    $references = @{}
    while ($queue.Count -gt 0) {
        if ($elements.Count -ge $options.maxTreeNodes) { $truncated.Add('nodes'); break }
        $node = $queue.Dequeue()
        $element = $node.element
        $id = $elements.Count.ToString()
        $name = [string]$element.Current.Name
        $role = $element.Current.ControlType.ProgrammaticName
        $click = Get-SDKClick $element
        $actions = @()
        if ($click) { $actions = @('click') }
        $wire = @{ id = $id; role = $role; name = (Limit-SDKText $name $options $truncated); actions = $actions; secondaryActions = @() }
        if ($node.parent -ne '') { $wire.parentId = $node.parent }
        $elements.Add($wire)
        $references[$id] = @{ path = @($node.path); runtimeId = @($element.GetRuntimeId()); name = $name; role = $role; click = $click }
        $child = $walker.GetFirstChild($element)
        if ($node.depth -ge $options.maxTreeDepth) {
            if ($null -ne $child -and -not $truncated.Contains('depth')) { $truncated.Add('depth') }
            continue
        }
        $index = 0
        while ($null -ne $child) {
            $queue.Enqueue(@{ element = $child; parent = $id; path = @($node.path) + @($index); depth = $node.depth + 1 })
            $child = $walker.GetNextSibling($child)
            $index++
        }
    }
    return @{ window = @{ id = 'native'; title = $process.MainWindowTitle }; tree = @{ status = 'available'; elements = @($elements.ToArray()); truncated = @($truncated.ToArray()) }; screenshot = (Get-SDKCapture $process); target = (Get-SDKTarget $process); references = $references }
}
function Invoke-SDKClick($target, $reference) {
    $process = Resolve-SDKTarget $target
    $element = Get-MainElement $process
    $walker = [Windows.Automation.TreeWalker]::RawViewWalker
    foreach ($index in $reference.path) {
        $element = $walker.GetFirstChild($element)
        for ($i = 0; $i -lt $index -and $null -ne $element; $i++) { $element = $walker.GetNextSibling($element) }
        if ($null -eq $element) { break }
    }
    $script:sdkCode = 'STALE_SNAPSHOT'
    if ($null -eq $element -or -not (Same-RuntimeId @($element.GetRuntimeId()) @($reference.runtimeId)) -or
        $element.Current.Name -cne $reference.name -or $element.Current.ControlType.ProgrammaticName -ne $reference.role -or
        (Get-SDKClick $element) -ne $reference.click) { throw 'Element changed since observation' }
    if (-not $element.Current.IsEnabled) { $script:sdkCode = 'TARGET_UNAVAILABLE'; throw 'Element is not enabled' }
    $cancel = [System.Threading.EventWaitHandle]::OpenExisting($operation.cancelEvent)
    try {
        if ($cancel.WaitOne(0)) { $script:sdkCode = 'CANCELLED'; throw 'Click cancelled before dispatch' }
        $script:sdkCode = 'TARGET_UNAVAILABLE'
        $script:sdkEffect = 'possible'
        return (Invoke-PreferredClick $element)
    } finally { $cancel.Dispose() }
}
try {
    switch ($operation.method) {
        'apps' {
            $result = @(Get-Process | Where-Object { $_.MainWindowHandle -ne 0 } | ForEach-Object { try { Get-SDKTarget $_ } catch {} })
        }
        'observe' { $result = Observe-SDK $operation.target $operation.options }
        'click' { $result = Invoke-SDKClick $operation.target $operation.element }
        default { $script:sdkCode = 'INVALID_ARGUMENT'; throw 'Unknown SDK bridge method' }
    }
    $response = @{ ok = $true; result = $result }
} catch { $response = @{ ok = $false; code = $script:sdkCode; message = $_.Exception.Message; effect = $script:sdkEffect } }
$response | ConvertTo-Json -Depth 50 -Compress
