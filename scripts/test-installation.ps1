$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$Repo = Split-Path -Parent $PSScriptRoot
$TestRoot = Join-Path ([IO.Path]::GetTempPath()) ('starling-install-test-' + [guid]::NewGuid().ToString('N'))
$OldLocal = $env:LOCALAPPDATA
$OldRoaming = $env:APPDATA
$Checks = 0
function Assert-Test([bool]$Condition, [string]$Message) { if (-not $Condition) { throw $Message } }
function Expect-Failure([scriptblock]$Action, [string]$Pattern) {
    $Failure = $null
    try { & $Action } catch { $Failure = $_.Exception.Message }
    Assert-Test ($null -ne $Failure -and $Failure -match $Pattern) "Expected failure matching '$Pattern', got '$Failure'."
}
# Process detection is simulated; no executable or desktop window is launched.
function Get-Process {
    param($Name, $ErrorAction)
    Assert-Test ($Name -contains 'Starling' -and $Name -contains 'Starling-candidate*') 'Candidate processes must also be guarded.'
    if ($SimulateRunning) { [pscustomobject]@{ ProcessName = 'Starling-candidate-r2' } }
}
$SimulateRunning = $false
try {
    New-Item -ItemType Directory -Path $TestRoot | Out-Null
    Push-Location $TestRoot
    $env:LOCALAPPDATA = Join-Path $TestRoot 'Local 中文'
    $env:APPDATA = Join-Path $TestRoot 'Roaming 中文'
    $MenuDir = Join-Path $env:APPDATA 'Microsoft\Windows\Start Menu\Programs'
    New-Item -ItemType Directory -Path $MenuDir -Force | Out-Null
    $Binary = Join-Path $TestRoot 'fixture.exe'
    [IO.File]::WriteAllText($Binary, 'synthetic version one')
    $env:LOCALAPPDATA = '.'
    Expect-Failure { & (Join-Path $Repo 'scripts/install.ps1') -Binary $Binary } 'absolute'
    $env:LOCALAPPDATA = Join-Path $TestRoot 'Local 中文'
    $Checks++
    & (Join-Path $Repo 'scripts/install.ps1') -Binary $Binary
    $Target = Join-Path $env:LOCALAPPDATA 'Programs\Starling'
    $Installed = Join-Path $Target 'Starling.exe'
    $Menu = Join-Path $MenuDir 'Starling.lnk'
    Assert-Test ((Get-FileHash $Installed).Hash -eq (Get-FileHash $Binary).Hash) 'Installed bytes differ.'
    Assert-Test ([Starling.Installer.ShellLinks]::ReadTarget($Menu) -eq $Installed) 'Shortcut points outside isolated install.'
    $Checks++
    $SimulateRunning = $true
    Expect-Failure { & (Join-Path $Repo 'scripts/install.ps1') -Binary $Binary } 'Exit Starling'
    Expect-Failure { & (Join-Path $Repo 'scripts/uninstall.ps1') } 'Exit Starling'
    $SimulateRunning = $false
    $Checks++
    [IO.File]::WriteAllText($Binary, 'synthetic version two')
    $OldHash = (Get-FileHash $Installed).Hash
    $Locked = [IO.File]::Open($Installed, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::Read)
    try { Expect-Failure { & (Join-Path $Repo 'scripts/install.ps1') -Binary $Binary } '.+' }
    finally { $Locked.Dispose() }
    Assert-Test ((Get-FileHash $Installed).Hash -eq $OldHash) 'Failed upgrade damaged previous binary.'
    Assert-Test (@(Get-ChildItem -LiteralPath $Target -Filter '*.tmp').Count -eq 0) 'Failed upgrade left staged binary.'
    $Checks++
    & (Join-Path $Repo 'scripts/install.ps1') -Binary $Binary
    Assert-Test ((Get-FileHash $Installed).Hash -eq (Get-FileHash $Binary).Hash) 'Upgrade did not replace binary.'
    $Data = Join-Path $env:APPDATA 'Starling'
    New-Item -ItemType Directory -Path $Data -Force | Out-Null
    $Fixture = Join-Path $Data 'test-data.txt'
    [IO.File]::WriteAllText($Fixture, 'synthetic data')
    # A junction stays entirely inside this test root but outside the owned
    # install directory. Rejection must occur before shortcut/program deletion.
    $Outside = Join-Path $TestRoot 'unrelated'
    New-Item -ItemType Directory -Path $Outside | Out-Null
    [IO.File]::WriteAllText((Join-Path $Outside 'keep.txt'), 'preserve')
    $Junction = Join-Path $Target 'unexpected-link'
    New-Item -ItemType Junction -Path $Junction -Target $Outside | Out-Null
    try {
        Expect-Failure { & (Join-Path $Repo 'scripts/uninstall.ps1') -RemoveData } 'link'
        Expect-Failure { & (Join-Path $Repo 'scripts/install.ps1') -Binary $Binary } 'link'
        Assert-Test ((Test-Path -LiteralPath $Installed) -and (Test-Path -LiteralPath $Menu) -and (Test-Path -LiteralPath $Fixture) -and (Test-Path -LiteralPath (Join-Path $Outside 'keep.txt'))) 'Link rejection modified existing files.'
    } finally {
        # Nonrecursive deletion removes only the junction, never its target.
        [IO.Directory]::Delete($Junction)
    }
    $Checks++
    & (Join-Path $Repo 'scripts/uninstall.ps1')
    Assert-Test ((Test-Path -LiteralPath $Fixture) -and -not (Test-Path -LiteralPath $Target) -and -not (Test-Path -LiteralPath $Menu)) 'Default uninstall did not preserve only data.'
    $Checks++
    & (Join-Path $Repo 'scripts/install.ps1') -Binary $Binary
    & (Join-Path $Repo 'scripts/uninstall.ps1') -RemoveData
    Assert-Test (-not (Test-Path -LiteralPath $Data)) 'Explicit data removal failed.'
    $Checks++
    Write-Host "PASS: $Checks isolated installation checks. No real installation or application process was touched."
} catch {
    $Original = $_
    # Diagnose Windows-host differences without saving or launching a shortcut.
    $ProbeResults = @()
    foreach ($ProbePath in @($Binary, (Join-Path $env:LOCALAPPDATA 'Programs\Starling\Starling.exe'), (Join-Path $env:SystemRoot 'System32\where.exe'))) {
        try {
            $ProbeShell = New-Object -ComObject WScript.Shell
            $ProbeLink = $ProbeShell.CreateShortcut((Join-Path $TestRoot 'probe.lnk'))
            $ProbeLink.TargetPath = $ProbePath
            $ProbeResults += 'accepted'
        } catch { $ProbeResults += $_.Exception.Message }
    }
    throw ($Original.Exception.Message + ' at ' + $Original.ScriptStackTrace + '; target probes [ASCII fixture, Unicode fixture, system binary]: ' + ($ProbeResults -join ' | '))
} finally {
    $env:LOCALAPPDATA = $OldLocal
    $env:APPDATA = $OldRoaming
    Pop-Location
    $Resolved = [IO.Path]::GetFullPath($TestRoot)
    $TempBase = [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd('\') + '\'
    if (-not $Resolved.StartsWith($TempBase, [StringComparison]::OrdinalIgnoreCase) -or [IO.Path]::GetFileName($Resolved) -notmatch '^starling-install-test-[a-f0-9]{32}$') { throw 'Unsafe test cleanup path.' }
    if (Test-Path -LiteralPath $Resolved) { Remove-Item -LiteralPath $Resolved -Recurse -Force }
}
