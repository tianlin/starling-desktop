param(
    [switch]$SkipTests,
    [switch]$Candidate,
    [ValidatePattern('^Starling-candidate(?:-[A-Za-z0-9]+)?$')]
    [string]$CandidateName = 'Starling-candidate'
)
$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
$Root = Split-Path -Parent $PSScriptRoot
function Run-Checked([string]$Exe, [string[]]$Arguments) {
    & $Exe @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$Exe failed (exit $LASTEXITCODE)" }
}
if ($env:OS -ne "Windows_NT") { throw "Build this Wails host on Windows 11 x64." }
if (-not $Candidate -and (Get-Process -Name "Starling" -ErrorAction SilentlyContinue)) {
    throw "Starling is running. Exit it normally, or use -Candidate to build a separate executable without running binding generation."
}
if (-not $Candidate -and $PSBoundParameters.ContainsKey('CandidateName')) { throw 'Use -Candidate with -CandidateName.' }
$Name = if ($Candidate) { $CandidateName } else { "Starling" }
$HashName = if ($Candidate) { $Name.Replace('Starling-', 'SHA256SUMS-') + '.txt' } else { "SHA256SUMS.txt" }
if (Get-Process -Name $Name -ErrorAction SilentlyContinue) { throw "Exit $Name before replacing its executable." }
$OldCGO = $env:CGO_ENABLED
try {
    $env:CGO_ENABLED = "0"
    Push-Location $Root
    try {
        Run-Checked "go" @("version")
        Run-Checked "node" @("--version")
        & (Join-Path $PSScriptRoot "verify-dependencies.ps1")
        if (-not $SkipTests) { Run-Checked "go" @("test", "./..."); Run-Checked "go" @("vet", "./...") }
        Push-Location "frontend"
        try {
            Run-Checked "npm.cmd" @("ci", "--ignore-scripts")
            if ($SkipTests) { Run-Checked "npm.cmd" @("run", "build") } else { Run-Checked "npm.cmd" @("test") }
        } finally { Pop-Location }
        Push-Location "desktop"
        try {
            if (-not $SkipTests) { Run-Checked "go" @("test", "./..."); Run-Checked "go" @("vet", "./...") }
            $BuildArgs = @("run", "github.com/wailsapp/wails/v2/cmd/wails@v2.11.0", "build", "-platform", "windows/amd64", "-o", "$Name.exe")
            # Candidate builds keep the stable Call bridge and never launch the
            # binding helper, which otherwise executes main and steals focus.
            if ($Candidate) { $BuildArgs += "-skipbindings" }
            Run-Checked "go" $BuildArgs
        } finally { Pop-Location }
        $Binary = Join-Path $Root "desktop\build\bin\$Name.exe"
        if (-not (Test-Path $Binary)) { throw "Expected Wails output not found: $Binary" }
        $Output = Join-Path $Root "build"
        New-Item -ItemType Directory -Force $Output | Out-Null
        Copy-Item $Binary (Join-Path $Output "$Name.exe") -Force
        $Hash = Get-FileHash (Join-Path $Output "$Name.exe") -Algorithm SHA256
        "$($Hash.Hash.ToLower())  $Name.exe" | Set-Content (Join-Path $Output $HashName) -Encoding ASCII
        Write-Host "Build produced build\$Name.exe. This is NOT real-account or Windows runtime acceptance."
    } finally { Pop-Location }
} finally { $env:CGO_ENABLED = $OldCGO }
