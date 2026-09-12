param([switch]$RemoveData)
$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
if ($env:OS -ne "Windows_NT") { throw "Windows only." }
if (Get-Process -Name "Starling" -ErrorAction SilentlyContinue) { throw "Exit Starling first. The uninstaller will not terminate it." }
$Target = Join-Path $env:LOCALAPPDATA "Programs\Starling"
$Menu = Join-Path $env:APPDATA "Microsoft\Windows\Start Menu\Programs\Starling.lnk"
if (Test-Path -LiteralPath $Menu) { Remove-Item -LiteralPath $Menu -Force }
if (Test-Path -LiteralPath $Target) { Remove-Item -LiteralPath $Target -Recurse -Force }
if ($RemoveData) {
    $Data = Join-Path $env:APPDATA "Starling"
    if (Test-Path -LiteralPath $Data) { Remove-Item -LiteralPath $Data -Recurse -Force }
    Write-Host "Program and local user data removed. Forensic secure erasure is not guaranteed."
} else { Write-Host "Program removed; local data retained. Use -RemoveData to delete it explicitly." }
