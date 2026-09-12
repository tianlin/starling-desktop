param([string]$Binary = (Join-Path (Split-Path -Parent $PSScriptRoot) "build\Starling.exe"))
$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
if ($env:OS -ne "Windows_NT") { throw "Windows only." }
if (-not (Test-Path -LiteralPath $Binary -PathType Leaf)) { throw "Build Starling.exe first." }
if (Get-Process -Name 'Starling', 'Starling-candidate*' -ErrorAction SilentlyContinue) { throw "Exit Starling and its candidates before installing. The installer will not terminate them." }
. (Join-Path $PSScriptRoot 'install-paths.ps1')
$Target = Get-StarlingChildPath $env:LOCALAPPDATA 'Programs\Starling'
$Menu = Get-StarlingChildPath $env:APPDATA 'Microsoft\Windows\Start Menu\Programs\Starling.lnk'
Assert-StarlingTree $Target
New-Item -ItemType Directory -Force $Target | Out-Null
$Destination = Join-Path $Target 'Starling.exe'
$Staged = Join-Path $Target ('Starling-' + [guid]::NewGuid().ToString('N') + '.tmp')
try {
    Copy-Item -LiteralPath $Binary -Destination $Staged
    if (Test-Path -LiteralPath $Destination) { [IO.File]::Replace($Staged, $Destination, [NullString]::Value) }
    else { [IO.File]::Move($Staged, $Destination) }
} finally {
    if (Test-Path -LiteralPath $Staged) { Remove-Item -LiteralPath $Staged -Force }
}
New-Item -ItemType Directory -Force (Split-Path -Parent $Menu) | Out-Null
$Shell = New-Object -ComObject WScript.Shell
$Shortcut = $Shell.CreateShortcut($Menu)
$Shortcut.TargetPath = Join-Path $Target "Starling.exe"
$Shortcut.WorkingDirectory = $Target
$Shortcut.Description = "Starling - experimental unofficial desktop podcast client"
$Shortcut.Save()
Write-Host "Installed for the current user. Account features remain experimental and disabled by default."
