<#
.SYNOPSIS
    Pre-uninstall script for LocalWEB MSI
.DESCRIPTION
    Runs before uninstallation to stop services and clean up
#>

$ErrorActionPreference = "Continue"

Write-Host "LocalWEB Pre-Uninstall"

# Stop and disable service
if (Get-Service -Name "LocalWEB" -ErrorAction SilentlyContinue) {
    Write-Host "Stopping LocalWEB service..."
    Stop-Service -Name "LocalWEB" -Force -ErrorAction SilentlyContinue
    Start-Sleep -Seconds 2
    sc.exe delete "LocalWEB" 2>$null
    Write-Host "LocalWEB service removed"
}

# Remove firewall rules
Remove-NetFirewallRule -DisplayName "LocalWEB" -ErrorAction SilentlyContinue
Write-Host "Firewall rules removed"

# Stop Wintun if no other services use it
# (We don't remove Wintun as other apps might use it)

Write-Host "Pre-uninstall completed"
exit 0