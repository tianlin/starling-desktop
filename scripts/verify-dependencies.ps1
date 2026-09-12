$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
$Root = Split-Path -Parent $PSScriptRoot
$SavedGoWork = $env:GOWORK
$TempBase = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
$WorkDir = Join-Path $TempBase ("starling-verify-" + [guid]::NewGuid().ToString("N"))
# Treat both local modules as workspace mains so go mod verify checks downloaded
# dependencies, rather than looking for a nonexistent starling v0.0.0 ziphash.
try {
    New-Item -ItemType Directory -Path $WorkDir | Out-Null
    $env:GOWORK = Join-Path $WorkDir "go.work"
    Push-Location $Root
    try {
        & go work init $Root (Join-Path $Root "desktop")
        if ($LASTEXITCODE -ne 0) { throw "go work init failed" }
        & go mod download
        if ($LASTEXITCODE -ne 0) { throw "go mod download failed" }
        & go mod verify
        if ($LASTEXITCODE -ne 0) { throw "go mod verify failed" }
    } finally { Pop-Location }
} finally {
    $env:GOWORK = $SavedGoWork
    $ResolvedWorkDir = [IO.Path]::GetFullPath($WorkDir)
    $TempPrefix = $TempBase.TrimEnd('\', '/') + [IO.Path]::DirectorySeparatorChar
    if (-not $ResolvedWorkDir.StartsWith($TempPrefix, [StringComparison]::OrdinalIgnoreCase)) {
        throw "Refusing to clean a verification directory outside the temporary folder"
    }
    if (Test-Path -LiteralPath $ResolvedWorkDir) { Remove-Item -LiteralPath $ResolvedWorkDir -Recurse -Force }
}
