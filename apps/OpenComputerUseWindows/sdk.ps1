$ErrorActionPreference = 'Stop'
$OutputEncoding = [System.Text.Encoding]::UTF8
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
[Console]::InputEncoding = [System.Text.Encoding]::UTF8
$script:sdkCode = 'INVALID_ARGUMENT'
$script:sdkEffect = 'none'

function Get-SDKTarget($process, [long]$windowHandle = 0) {
    if ($windowHandle -eq 0 -and $null -ne $script:TargetHwnd) { $windowHandle = [long]$script:TargetHwnd }
    if ($windowHandle -eq 0) { $windowHandle = [long](Resolve-AppWindowHandle $process $null) }
    $identityPID = Get-WindowHostedProcessId ([IntPtr][long]$windowHandle) ([int]$process.Id)
    if ($identityPID -le 0) { $identityPID = [int]$process.Id }
    $title = [OCUWin32]::WindowTextOf([IntPtr][long]$windowHandle)
    # ApplicationFrameHost is a shared host with no useful name of its own; the hosted app is the app.
    $name = if ($process.ProcessName -eq 'ApplicationFrameHost' -and -not [string]::IsNullOrWhiteSpace($title)) { $title } else { $process.ProcessName }
    [pscustomobject]@{ pid = [int]$process.Id; started = $process.StartTime.ToUniversalTime().Ticks.ToString(); hwnd = [long]$windowHandle; name = $name; title = $title; identity = [SDKApplicationIdentity]::ForProcess($identityPID) }
}
function Resolve-SDKTarget($target) {
    $process = Get-Process -Id ([int]$target.pid) -ErrorAction Stop
    $hwnd = [IntPtr][long]$target.hwnd
    if ($process.StartTime.ToUniversalTime().Ticks.ToString() -ne $target.started -or [long]$target.hwnd -eq 0 -or -not [OCUWin32]::IsAlive($hwnd) -or [OCUWin32]::OwnerProcessOf($hwnd) -ne [uint32]$process.Id) {
        $script:sdkCode = 'STALE_SNAPSHOT'
        throw 'Application process or window changed'
    }
    $script:TargetHwnd = [long]$target.hwnd
    if ($target.identity -and (Get-SDKTarget $process ([long]$target.hwnd)).identity -cne $target.identity) {
        $script:sdkCode = 'STALE_SNAPSHOT'
        throw 'Hosted application identity changed'
    }
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
        $bounds = Get-WindowRectFrame ([IntPtr][long]$script:TargetHwnd)
        if ($null -eq $bounds -or $bounds.width -le 0 -or $bounds.height -le 0 -or $bounds.width * $bounds.height -gt 16000000) { throw 'Invalid window capture dimensions' }
        $bitmap = New-Object System.Drawing.Bitmap ([int]$bounds.width), ([int]$bounds.height)
        try {
            $graphics = [System.Drawing.Graphics]::FromImage($bitmap)
            try {
                $hdc = $graphics.GetHdc()
                try { if (-not [SDKWindowCapture]::PrintWindow([IntPtr][long]$script:TargetHwnd, $hdc, 2)) { throw 'Window did not provide an image' } }
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
function Get-SDKRunningTargets {
    $targets = @()
    $seen = @{}
    $byProcess = @{}
    foreach ($raw in ([OCUWin32]::AllTopLevelWindows())) {
        $handle = [IntPtr][long]$raw
        $owner = [int][OCUWin32]::OwnerProcessOf($handle)
        if ($owner -le 0) { continue }
        if (-not $byProcess.ContainsKey($owner)) { $byProcess[$owner] = New-Object System.Collections.Generic.List[IntPtr] }
        $byProcess[$owner].Add($handle)
    }
    foreach ($processId in $byProcess.Keys) {
        $process = Get-Process -Id $processId -ErrorAction SilentlyContinue
        if ($null -eq $process) { continue }
        $handles = @($byProcess[$processId])
        $frames = @($handles | Where-Object { [OCUWin32]::ClassNameOf($_) -eq 'ApplicationFrameWindow' })
        if ($frames.Count -gt 0) {
            $pool = $frames
        } elseif ([long]$process.MainWindowHandle -ne 0) {
            $pool = @([IntPtr][long]$process.MainWindowHandle)
        } else {
            continue
        }
        foreach ($handle in $pool) {
            try {
                $target = Get-SDKTarget $process ([long]$handle)
                $key = "$($target.pid)|$($target.identity)"
                if ($seen.ContainsKey($key)) { continue }
                $seen[$key] = $true
                $targets += $target
            } catch {
            }
        }
    }
    return $targets
}
function Observe-SDK($target, $options) {
    $process = Resolve-SDKTarget $target
    $hwnd = [long]$script:TargetHwnd
    $root = Get-MainElement $process $hwnd
    $bounds = Get-WindowBounds $process $root $hwnd
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
    $capture = if ($options.includeScreenshot -eq $false) { @{ status = 'unavailable'; reason = @{ code = 'CAPTURE_NOT_REQUESTED'; message = 'Screenshot was not requested' } } } else { Get-SDKCapture $process }
    return @{ window = @{ id = 'native'; title = [OCUWin32]::WindowTextOf([IntPtr][long]$script:TargetHwnd) }; tree = @{ status = 'available'; elements = $elements; truncated = @($rendered.truncated) }; screenshot = $capture; target = (Get-SDKTarget $process $hwnd); bounds = $bounds; references = $references }
}
function Invoke-SDKAction($operation) {
    $process = Resolve-SDKTarget $operation.target
    $hwnd = [long]$script:TargetHwnd
    $bounds = Get-WindowBounds $process (Get-MainElement $process $hwnd) $hwnd
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
        if ($operation.action.type -notin @(Get-SDKActions $current)) {
            if ($operation.action.type -notin @(Get-SDKActions $reference)) {
                $script:sdkCode = 'UNSUPPORTED_CAPABILITY'
                throw "Element does not support $($operation.action.type)"
            }
            $script:sdkCode = 'STALE_SNAPSHOT'
            throw 'Element action changed since observation'
        }
        if (-not $element.Current.IsEnabled) { $script:sdkCode = 'TARGET_UNAVAILABLE'; throw 'Element is not enabled' }
    }
    $action = $operation.action
    $script:sdkCode = 'INVALID_ARGUMENT'
    # Text fields of hosted apps expose no native edit handle; UIA ValuePattern is the only channel that
    # reaches them. It stays behind OPEN_COMPUTER_USE_WINDOWS_ALLOW_UIA_TEXT_FALLBACK because it can activate the app.
    $engine = @{ windowBounds = $bounds; windowHandle = $hwnd; element = $reference; allowFocusFallback = $true }
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
        'pressKey' { $engine.tool = 'press_key'; $engine.key = $action.key; Test-KeySupported $action.key }
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
    $cancel = $null
    if (-not [string]::IsNullOrEmpty($operation.cancelEvent)) { $cancel = [System.Threading.EventWaitHandle]::OpenExisting($operation.cancelEvent) }
    try {
        if ($null -ne $cancel -and $cancel.WaitOne(0)) { $script:sdkCode = 'CANCELLED'; throw 'Action cancelled before dispatch' }
        if ($null -ne $cancel) { $script:OperationCancelled = { $cancel.WaitOne(0) }.GetNewClosure() }
        $script:sdkCode = 'TARGET_UNAVAILABLE'
        $script:sdkEffect = 'possible'
        Invoke-Operation $process $engine $element
        return $true
    } finally { $script:OperationCancelled = $null; if ($null -ne $cancel) { $cancel.Dispose() } }
}
try {
    # The owner attaches this worker to its private Job Object before releasing stdin.
    $line = [Console]::ReadLine()
    if ([string]::IsNullOrEmpty($line)) { throw 'Empty command payload' }
    $operation = $line | ConvertFrom-Json
    if ($null -eq $operation -or $operation -isnot [pscustomobject]) { throw 'Expected a command object' }
    $script:sdkCode = 'TARGET_UNAVAILABLE'
    . "$PSScriptRoot/runtime.ps1" -DefinitionsOnly
    . "$PSScriptRoot/sdk_apps.ps1"
    switch ($operation.method) {
        'appIcon' { $result = Get-SDKAppIcon $operation.identity }
        'resolveIdentity' { $result = Resolve-SDKAppIdentity $operation.query }
        'apps' { $result = @(Get-SDKRunningTargets) }
        'observe' { $result = Observe-SDK $operation.target $operation.options }
        'act' { $result = Invoke-SDKAction $operation }
        default { $script:sdkCode = 'INVALID_ARGUMENT'; throw 'Unknown SDK bridge method' }
    }
    $response = @{ ok = $true; result = $result }
} catch { $response = @{ ok = $false; code = $script:sdkCode; message = $_.Exception.Message; effect = $script:sdkEffect } }
$response | ConvertTo-Json -Depth 50 -Compress
