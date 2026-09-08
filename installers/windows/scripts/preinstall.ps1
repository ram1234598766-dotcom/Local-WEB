<#
.SYNOPSIS
    Pre-installation script for LocalWEB MSI
.DESCRIPTION
    Runs before installation to check prerequisites and prepare system
#>

$ErrorActionPreference = "Stop"

Write-Host "LocalWEB Pre-Installation Check"

# Check Windows version
$osVersion = [System.Environment]::OSVersion.Version
if ($osVersion.Major -lt 10) {
    Write-Error "LocalWEB requires Windows 10 or later"
    exit 1
}

# Check architecture
if (-not [Environment]::Is64BitOperatingSystem) {
    Write-Error "LocalWEB requires 64-bit Windows"
    exit 1
}

# Check for existing installation
$installPath = "C:\Program Files\LocalWEB"
if (Test-Path $installPath) {
    Write-Host "Existing installation found at $installPath"
    # Stop service if running
    if (Get-Service -Name "LocalWEB" -ErrorAction SilentlyContinue) {
        Stop-Service -Name "LocalWEB" -Force -ErrorAction SilentlyContinue
        Start-Sleep -Seconds 2
    }
}

# Check for Wintun driver
if (-not (Get-Service -Name "wintun" -ErrorAction SilentlyContinue)) {
    Write-Host "Wintun driver not found - will be installed"
}

Write-Host "Pre-installation checks passed"
exit 0