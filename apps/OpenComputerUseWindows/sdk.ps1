$ErrorActionPreference = 'Stop'
$OutputEncoding = [System.Text.Encoding]::UTF8
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
[Console]::InputEncoding = [System.Text.Encoding]::UTF8
$script:sdkCode = 'INVALID_ARGUMENT'
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
function Get-SDKActions($record) {
    $actions = @()
    if ($null -ne $record.frame -or @($record.actions | Where-Object { $_ -in @('Invoke', 'Select', 'Toggle') }).Count -gt 0) { $actions += 'click' }
    if ($null -ne $record.frame -or 'Scroll' -in $record.actions) { $actions += 'scroll' }
    if ('SetValue' -in $record.actions) { $actions += 'setValue' }
    if (@(Get-SDKSecondaryActions $record).Count -gt 0) { $actions += 'performSecondaryAction' }
    return $actions
}
function Get-SDKSecondaryActions($record) {
    foreach ($name in $record.actions) {
        if ($name -in @('Invoke', 'Toggle', 'Select', 'Expand', 'Collapse', 'ScrollIntoView')) { @{ id = $name; label = $name } }
    }
}
function Observe-SDK($target, $options) {
    $process = Resolve-SDKTarget $target
    $root = Get-MainElement $process
    $bounds = Get-WindowBounds $process $root
    $rendered = Render-Tree $root $bounds (Resolve-TextLimit $options.textLimit) $options.maxTreeNodes $options.maxTreeDepth
    $elements = @()
    $references = @{}
    foreach ($record in $rendered.records) {
        $id = $record.index.ToString()
        $wire = @{ id = $id; role = $record.controlType; name = $record.name; value = $record.value; actions = @(Get-SDKActions $record); secondaryActions = @(Get-SDKSecondaryActions $record) }
        if ($record.parentIndex -ge 0) { $wire.parentId = $record.parentIndex.ToString() }
        if ($null -ne $record.frame) { $wire.bounds = $record.frame }
        $elements += $wire
        $references[$id] = $record
    }
    return @{ window = @{ id = 'native'; title = $process.MainWindowTitle }; tree = @{ status = 'available'; elements = $elements; truncated = @($rendered.truncated) }; screenshot = (Get-SDKCapture $process); target = (Get-SDKTarget $process); bounds = $bounds; references = $references }
}
function Invoke-SDKAction($operation) {
    $process = Resolve-SDKTarget $operation.target
    $bounds = Get-WindowBounds $process (Get-MainElement $process)
    $script:sdkCode = 'STALE_SNAPSHOT'
    foreach ($key in @('x', 'y', 'width', 'height')) {
        if ($null -eq $bounds -or $bounds.$key -ne $operation.bounds.$key) { throw 'Window bounds changed since observation' }
    }
    $element = $null
    $reference = $operation.element
    if ($null -ne $reference) {
        $element = Find-Element $process $reference
        if ($null -eq $element -or $reference.runtimeId.Count -eq 0 -or
            -not (Same-RuntimeId @($element.GetRuntimeId()) @($reference.runtimeId)) -or
            $element.Current.Name -cne $reference.identityName -or $element.Current.ControlType.ProgrammaticName -ne $reference.controlType) { throw 'Element changed since observation' }
        $current = Get-ElementRecord $element $reference.index $bounds $null
        foreach ($key in @('x', 'y', 'width', 'height')) {
            if ($current.frame.$key -ne $reference.frame.$key) { throw 'Element moved since observation' }
        }
        if ($operation.action.type -notin @(Get-SDKActions $current)) { throw 'Element action changed since observation' }
        if (-not $element.Current.IsEnabled) { $script:sdkCode = 'TARGET_UNAVAILABLE'; throw 'Element is not enabled' }
    }
    $action = $operation.action
    $script:sdkCode = 'INVALID_ARGUMENT'
    $engine = @{ windowBounds = $bounds; element = $reference; allowFocusFallback = $false }
    switch ($action.type) {
        'click' {
            $engine.tool = 'click'; $engine.click_method = 'auto'; $engine.click_count = $action.count; $engine.mouse_button = $action.button
            $engine.x = $action.x; $engine.y = $action.y
            if ($null -eq $reference -and ($action.x -ge $bounds.width -or $action.y -ge $bounds.height)) { throw 'Click is outside the observed window' }
        }
        'scroll' { $engine.tool = 'scroll'; $engine.direction = $action.direction; $engine.pages = $action.pages }
        'drag' {
            $engine.tool = 'drag'; $engine.from_x = $action.from.x; $engine.from_y = $action.from.y; $engine.to_x = $action.to.x; $engine.to_y = $action.to.y
            foreach ($point in @($action.from, $action.to)) {
                if ($point.x -ge $bounds.width -or $point.y -ge $bounds.height) { throw 'Drag is outside the observed window' }
            }
        }
        'typeText' { $engine.tool = 'type_text'; $engine.text = $action.text }
        'pressKey' { $engine.tool = 'press_key'; $engine.key = $action.key; $null = Get-VirtualKey (($action.key -split '\+')[-1]) }
        'setValue' {
            $pattern = Get-CurrentPatternOrNull $element ([Windows.Automation.ValuePattern]::Pattern)
            if ($null -eq $pattern -or $pattern.Current.IsReadOnly) { $script:sdkCode = 'UNSUPPORTED_CAPABILITY'; throw 'Element value is not writable' }
            $engine.tool = 'set_value'; $engine.value = $action.value
        }
        'performSecondaryAction' {
            if ($action.actionId -notin @((Get-SDKSecondaryActions $current) | ForEach-Object { $_.id })) { $script:sdkCode = 'STALE_SNAPSHOT'; throw 'Secondary action changed since observation' }
            $engine.tool = 'perform_secondary_action'; $engine.action = $action.actionId
        }
        default { throw 'Unknown SDK action' }
    }
    $cancel = [System.Threading.EventWaitHandle]::OpenExisting($operation.cancelEvent)
    try {
        if ($cancel.WaitOne(0)) { $script:sdkCode = 'CANCELLED'; throw 'Action cancelled before dispatch' }
        $script:OperationCancelled = { $cancel.WaitOne(0) }.GetNewClosure()
        $script:sdkCode = 'TARGET_UNAVAILABLE'
        $script:sdkEffect = 'possible'
        Invoke-Operation $process $engine $element
        return $true
    } finally { $script:OperationCancelled = $null; $cancel.Dispose() }
}
try {
    # The owner attaches this worker to its private Job Object before releasing stdin.
    $line = [Console]::ReadLine()
    if ([string]::IsNullOrEmpty($line)) { throw 'Empty command payload' }
    $operation = $line | ConvertFrom-Json
    if ($null -eq $operation -or $operation -isnot [pscustomobject]) { throw 'Expected a command object' }
    $script:sdkCode = 'TARGET_UNAVAILABLE'
    . "$PSScriptRoot/runtime.ps1" -DefinitionsOnly
    switch ($operation.method) {
        'apps' { $result = @(Get-Process | Where-Object { $_.MainWindowHandle -ne 0 } | ForEach-Object { try { Get-SDKTarget $_ } catch {} }) }
        'observe' { $result = Observe-SDK $operation.target $operation.options }
        'act' { $result = Invoke-SDKAction $operation }
        default { $script:sdkCode = 'INVALID_ARGUMENT'; throw 'Unknown SDK bridge method' }
    }
    $response = @{ ok = $true; result = $result }
} catch { $response = @{ ok = $false; code = $script:sdkCode; message = $_.Exception.Message; effect = $script:sdkEffect } }
$response | ConvertTo-Json -Depth 50 -Compress
