param([switch]$SkipTests)
$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
$Root = Split-Path -Parent $PSScriptRoot
function Run-Checked([string]$Exe, [string[]]$Arguments) {
    & $Exe @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$Exe failed (exit $LASTEXITCODE)" }
}
if ($env:OS -ne "Windows_NT") { throw "Build this Wails host on Windows 11 x64." }
$OldCGO = $env:CGO_ENABLED
try {
    $env:CGO_ENABLED = "0"
    Push-Location $Root
    try {
        Run-Checked "go" @("version")
        Run-Checked "node" @("--version")
        if (-not $SkipTests) { Run-Checked "go" @("test", "./..."); Run-Checked "go" @("vet", "./...") }
        Push-Location "frontend"
        try {
            Run-Checked "npm.cmd" @("ci", "--ignore-scripts")
            if ($SkipTests) { Run-Checked "npm.cmd" @("run", "build") } else { Run-Checked "npm.cmd" @("test") }
        } finally { Pop-Location }
        Push-Location "desktop"
        try {
            # Initial dependency resolution is deliberately explicit; do not disable GOSUMDB or TLS.
            Run-Checked "go" @("mod", "tidy")
            Run-Checked "go" @("mod", "verify")
            Run-Checked "go" @("run", "github.com/wailsapp/wails/v2/cmd/wails@v2.11.0", "build", "-platform", "windows/amd64", "-clean")
        } finally { Pop-Location }
        $Binary = Join-Path $Root "desktop\build\bin\Starling.exe"
        if (-not (Test-Path $Binary)) { throw "Expected Wails output not found: $Binary" }
        $Output = Join-Path $Root "build"
        New-Item -ItemType Directory -Force $Output | Out-Null
        Copy-Item $Binary (Join-Path $Output "Starling.exe") -Force
        $Hash = Get-FileHash (Join-Path $Output "Starling.exe") -Algorithm SHA256
        "$($Hash.Hash.ToLower())  Starling.exe" | Set-Content (Join-Path $Output "SHA256SUMS.txt") -Encoding ASCII
        Write-Host "Build produced build\Starling.exe. This is NOT real-account or Windows runtime acceptance."
    } finally { Pop-Location }
} finally { $env:CGO_ENABLED = $OldCGO }
