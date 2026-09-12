param([switch]$RemoveData)
$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
if ($env:OS -ne "Windows_NT") { throw "Windows only." }
if (Get-Process -Name 'Starling', 'Starling-candidate*' -ErrorAction SilentlyContinue) { throw "Exit Starling and its candidates first. The uninstaller will not terminate them." }
. (Join-Path $PSScriptRoot 'install-paths.ps1')
$Target = Get-StarlingChildPath $env:LOCALAPPDATA 'Programs\Starling'
$Menu = Get-StarlingChildPath $env:APPDATA 'Microsoft\Windows\Start Menu\Programs\Starling.lnk'
Assert-StarlingTree $Target
if ($RemoveData) {
    $Data = Get-StarlingChildPath $env:APPDATA 'Starling'
    Assert-StarlingTree $Data
}
if (Test-Path -LiteralPath $Menu) { Remove-Item -LiteralPath $Menu -Force }
if (Test-Path -LiteralPath $Target) { Remove-Item -LiteralPath $Target -Recurse -Force }
if ($RemoveData) {
    if (Test-Path -LiteralPath $Data) { Remove-Item -LiteralPath $Data -Recurse -Force }
    Write-Host "Program and local user data removed. Forensic secure erasure is not guaranteed."
} else { Write-Host "Program removed; local data retained. Use -RemoveData to delete it explicitly." }
