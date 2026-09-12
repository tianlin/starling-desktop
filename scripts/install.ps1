param([string]$Binary = (Join-Path (Split-Path -Parent $PSScriptRoot) "build\Starling.exe"))
$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
if ($env:OS -ne "Windows_NT") { throw "Windows only." }
if (-not (Test-Path -LiteralPath $Binary -PathType Leaf)) { throw "Build Starling.exe first." }
if (Get-Process -Name "Starling" -ErrorAction SilentlyContinue) { throw "Exit Starling before installing. The installer will not terminate it." }
$Target = Join-Path $env:LOCALAPPDATA "Programs\Starling"
New-Item -ItemType Directory -Force $Target | Out-Null
Copy-Item -LiteralPath $Binary -Destination (Join-Path $Target "Starling.exe") -Force
$Menu = Join-Path $env:APPDATA "Microsoft\Windows\Start Menu\Programs\Starling.lnk"
$Shell = New-Object -ComObject WScript.Shell
$Shortcut = $Shell.CreateShortcut($Menu)
$Shortcut.TargetPath = Join-Path $Target "Starling.exe"
$Shortcut.WorkingDirectory = $Target
$Shortcut.Description = "Starling - experimental unofficial desktop podcast client"
$Shortcut.Save()
Write-Host "Installed for the current user. Account features remain experimental and disabled by default."
