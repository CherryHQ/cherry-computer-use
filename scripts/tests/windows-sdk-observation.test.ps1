param([Parameter(Mandatory = $true)][string]$RuntimePath)
$ErrorActionPreference = 'Stop'

# Parse every embedded bridge script without loading Windows-only dependencies.
$directory = Split-Path (Resolve-Path $RuntimePath).Path
foreach ($name in @('runtime.ps1', 'sdk.ps1', 'sdk_apps.ps1')) {
    $tokens = $null; $errors = $null
    $ast = [System.Management.Automation.Language.Parser]::ParseFile((Join-Path $directory $name), [ref]$tokens, [ref]$errors)
    if ($errors.Count) { throw ($errors | Out-String) }
    if ($name -eq 'sdk.ps1') { $sdk = $ast }
}
$functions = $sdk.FindAll({ param($node)
    $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and
    $node.Name -in @('Observe-SDK', 'Get-SDKActions', 'Get-SDKSecondaryActions')
}, $true)
if ($functions.Count -ne 3) { throw 'Production SDK observation functions missing' }
foreach ($function in $functions) { Invoke-Expression $function.Extent.Text }

Add-Type @'
public static class OCUWin32 {
    public static string WindowTextOf(System.IntPtr hwnd) { return "Fixture " + hwnd.ToInt64(); }
}
'@
function Assert($condition, $message) { if (-not $condition) { throw $message } }
function Resolve-SDKTarget($target) { $script:TargetHwnd = $target.hwnd; [pscustomobject]@{ Id = 42 } }
function Get-SDKTarget($process, $hwnd) { @{ pid = $process.Id; hwnd = $hwnd } }
function Get-MainElement($process, $hwnd) { Assert ($hwnd -eq 123) 'Wrong observation window'; 'root' }
function Get-WindowBounds($process, $root, $hwnd) { @{ x = 0; y = 0; width = 300; height = 200 } }
function Resolve-TextLimit($limit) { 500 }
function Render-Tree($root, $bounds, $limit, $nodes, $depth) {
    $script:treeCalls++
    @{ records = @([pscustomobject]@{
        index = 7; parentIndex = -1; controlType = 'Button'; name = 'Target'; value = ''
        actions = @('Invoke'); frame = @{ x = 10; y = 20; width = 30; height = 40 }
    }); truncated = @('maxNodes') }
}
function Get-SDKCapture($process) { $script:captureCalls++; @{ status = 'available'; image = @{ dataBase64 = 'cG5n' } } }

foreach ($payload in @('{}', '{"includeAX":true,"includeScreenshot":true}',
                       '{"includeAX":false,"includeScreenshot":true}', '{"includeAX":true,"includeScreenshot":false}')) {
    $options = $payload | ConvertFrom-Json
    $script:treeCalls = 0; $script:captureCalls = 0
    $snapshot = Observe-SDK (@{ hwnd = 123 }) $options
    $wantTree = $options.includeAX -ne $false
    $wantCapture = $options.includeScreenshot -ne $false
    Assert ($script:treeCalls -eq [int]$wantTree -and $script:captureCalls -eq [int]$wantCapture) "Disabled SDK collector executed: $payload"
    Assert ($snapshot.window.title -eq 'Fixture 123' -and $snapshot.target.hwnd -eq 123 -and $snapshot.bounds.width -eq 300) 'SDK window metadata changed'
    if ($wantTree) {
        Assert ($snapshot.tree.status -eq 'available' -and $snapshot.tree.elements.Count -eq 1) 'SDK tree missing'
        $element = $snapshot.tree.elements[0]
        Assert ($element.id -eq '7' -and $element.name -eq 'Target' -and 'click' -in $element.actions) 'SDK element actions or identity missing'
        Assert ($snapshot.references['7'].index -eq 7 -and $snapshot.tree.truncated[0] -eq 'maxNodes') 'SDK references or truncation missing'
    } else {
        Assert ($snapshot.tree.status -eq 'unavailable' -and $snapshot.tree.reason.code -eq 'OBSERVATION_NOT_REQUESTED') 'SDK omitted-tree reason changed'
        Assert ($snapshot.references.Count -eq 0 -and $null -eq $snapshot.tree.elements) 'SDK retained omitted tree references'
    }
    if ($wantCapture) {
        Assert ($snapshot.screenshot.status -eq 'available' -and $snapshot.screenshot.image.dataBase64 -eq 'cG5n') 'SDK capture missing'
    } else {
        Assert ($snapshot.screenshot.status -eq 'unavailable' -and $snapshot.screenshot.reason.code -eq 'CAPTURE_NOT_REQUESTED') 'SDK omitted-capture reason changed'
    }
}
Write-Output 'Embedded bridge syntax and production SDK observation modes passed (instrumented dependencies).'
