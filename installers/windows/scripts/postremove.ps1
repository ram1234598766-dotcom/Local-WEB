<#
.SYNOPSIS
    Post-uninstall script for LocalWEB MSI
.DESCRIPTION
    Runs after files are removed to clean up registry, shortcuts, and data
#>

$ErrorActionPreference = "Continue"

Write-Host "LocalWEB Post-Uninstall Cleanup"

# Remove Start Menu shortcuts
$startMenuDir = Join-Path $env:ProgramData "Microsoft\Windows\Start Menu\Programs\LocalWEB"
if (Test-Path $startMenuDir) {
    Remove-Item $startMenuDir -Recurse -Force -ErrorAction SilentlyContinue
}

# Desktop shortcut
$desktopPath = [Environment]::GetFolderPath("Desktop")
$desktopShortcut = Join-Path $desktopPath "LocalWEB.lnk"
if (Test-Path $desktopShortcut) {
    Remove-Item $desktopShortcut -Force -ErrorAction SilentlyContinue
}

# Remove registry keys
$regKeys = @(
    "HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\LocalWEB",
    "HKLM:\SOFTWARE\LocalWEB",
    "HKLM:\SYSTEM\CurrentControlSet\Services\LocalWEB"
)

foreach ($key in $regKeys) {
    if (Test-Path $key) {
        Remove-Item $key -Recurse -Force -ErrorAction SilentlyContinue
    }
}

# Remove data directory (optional - keep user data by default)
# Uncomment to remove user data:
# $dataDir = "C:\ProgramData\LocalWEB"
# if (Test-Path $dataDir) {
#     Remove-Item $dataDir -Recurse -Force -ErrorAction SilentlyContinue
# }

# Remove install directory if empty
$installDir = "C:\Program Files\LocalWEB"
if (Test-Path $installDir) {
    $files = Get-ChildItem $installDir -Force -ErrorAction SilentlyContinue
    if ($files.Count -eq 0) {
        Remove-Item $installDir -Force -ErrorAction SilentlyContinue
    }
}

Write-Host "LocalWEB uninstallation completed"
exit 0