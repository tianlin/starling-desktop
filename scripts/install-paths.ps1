# Shared by the current-user installer and uninstaller. Never follow directory
# links while installing or recursively deleting application-owned paths.
function Get-StarlingChildPath([string]$Base, [string]$Relative) {
    if ([string]::IsNullOrWhiteSpace($Base) -or $Base -notmatch '^(?:[A-Za-z]:[\\/]|\\\\[^\\]+\\[^\\]+\\)') {
        throw 'Application data directories must be absolute paths.'
    }
    $Root = [IO.Path]::GetFullPath($Base).TrimEnd('\') + '\'
    $Path = [IO.Path]::GetFullPath((Join-Path $Root $Relative))
    if (-not $Path.StartsWith($Root, [StringComparison]::OrdinalIgnoreCase)) { throw 'Application path escapes its data directory.' }
    $Cursor = $Path
    while ($Cursor) {
        if (Test-Path -LiteralPath $Cursor) {
            $Item = Get-Item -LiteralPath $Cursor -Force
            if ($Item.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Application path contains a directory link or reparse point.' }
        }
        $Parent = [IO.Directory]::GetParent($Cursor)
        $Cursor = if ($Parent) { $Parent.FullName } else { $null }
    }
    return $Path
}
function Assert-StarlingTree([string]$Path) {
    if (-not (Test-Path -LiteralPath $Path)) { return }
    $Item = Get-Item -LiteralPath $Path -Force
    if ($Item.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Application directory contains a link; no files were removed.' }
    if ($Item.PSIsContainer) {
        foreach ($Child in Get-ChildItem -LiteralPath $Path -Force) { Assert-StarlingTree $Child.FullName }
    }
}
