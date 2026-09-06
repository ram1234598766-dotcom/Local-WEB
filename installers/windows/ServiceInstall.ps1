<# 
.SYNOPSIS
    LocalWEB Windows Service Installation Script
.DESCRIPTION
    Installs/uninstalls LocalWEB as a Windows Service for auto-start at boot.
    Handles Wintun driver, service creation, and firewall rules.
#>

param(
    [switch]$Install,
    [switch]$Uninstall,
    [string]$InstallDir = "C:\Program Files\LocalWEB",
    [string]$ServiceName = "LocalWEB",
    [string]$ServiceDisplayName = "LocalWEB Mesh Network",
    [string]$ServiceDescription = "Local-first encrypted mesh network daemon"
)

$ErrorActionPreference = "Stop"
$VerbosePreference = "Continue"

function Write-Log {
    param([string]$Message)
    $timestamp = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
    Write-Host "[$timestamp] $Message"
}

function Test-Admin {
    $currentUser = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($currentUser)
    return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

if (-not (Test-Admin)) {
    Write-Error "This script must be run as Administrator"
    exit 1
}

if ($Install) {
    Write-Log "Installing LocalWEB Windows Service..."

    # Ensure Wintun driver is installed
    if (-not (Get-Service -Name "wintun" -ErrorAction SilentlyContinue)) {
        Write-Log "Installing Wintun driver..."
        $wintunDll = Join-Path $InstallDir "wintun\wintun.dll"
        if (Test-Path $wintunDll) {
            Copy-Item $wintunDll -Destination "C:\Windows\System32\drivers\wintun.dll" -Force
            Write-Log "Wintun driver copied to System32\drivers"
        } else {
            Write-Warning "Wintun DLL not found at $wintunDll"
        }
        
        # Register Wintun as a kernel driver
        $wintunPath = "C:\Windows\System32\drivers\wintun.dll"
        if (Test-Path $wintunPath) {
            sc.exe create wintun binPath= "$wintunPath" type= kernel start= demand 2>$null
            sc.exe start wintun
            Write-Log "Wintun driver service started"
        }
    }

    # Create the LocalWEB service
    $servicePath = Join-Path $InstallDir "localweb.exe"
    if (-not (Test-Path $servicePath)) {
        throw "LocalWEB executable not found at $servicePath"
    }

    $serviceArgs = "node --data-dir C:\ProgramData\LocalWEB"
    
    # Create service
    $result = sc.exe create "LocalWEB" binPath= "`"$servicePath`" $serviceArgs" `
        DisplayName= "LocalWEB Mesh Network" `
        start= auto `
        obj= "LocalSystem" `
        type= own

    if ($LASTEXITCODE -ne 0) {
        throw "Failed to create service: $result"
    }

    # Set service description
    sc.exe description "LocalWEB" "Local-first encrypted mesh network daemon"

    # Configure recovery actions
    sc.exe failure "LocalWEB" reset= 86400 actions= restart/5000/restart/10000/restart/60000

    # Set service to start automatically with delayed start
    Set-ItemProperty -Path "HKLM:\SYSTEM\CurrentControlSet\Services\LocalWEB" -Name "DelayedAutoStart" -Value 1 -Type DWord

    # Start the service
    Start-Service -Name "LocalWEB" -ErrorAction SilentlyContinue
    Write-Log "LocalWEB service installed and started"

    # Configure firewall
    $firewallRuleName = "LocalWEB"
    if (-not (Get-NetFirewallRule -DisplayName $firewallRuleName -ErrorAction SilentlyContinue)) {
        New-NetFirewallRule -DisplayName "LocalWEB" -Direction Inbound -Action Allow `
            -Program "C:\Program Files\LocalWEB\localweb.exe" -Profile Domain,Private -Enabled True
        New-NetFirewallRule -DisplayName "LocalWEB" -Direction Outbound -Action Allow `
            -Program "C:\Program Files\LocalWEB\localweb.exe" -Profile Domain,Private -Enabled True
        Write-Log "Firewall rules created"
    }

    Write-Log "LocalWEB Windows Service installed successfully"
}

if ($Uninstall) {
    Write-Log "Uninstalling LocalWEB Windows Service..."

    # Stop and remove service
    if (Get-Service -Name "LocalWEB" -ErrorAction SilentlyContinue) {
        Stop-Service -Name "LocalWEB" -Force -ErrorAction SilentlyContinue
        Start-Sleep -Seconds 2
        sc.exe delete "LocalWEB"
        Write-Log "LocalWEB service removed"
    }

    # Remove firewall rules
    Remove-NetFirewallRule -DisplayName "LocalWEB" -ErrorAction SilentlyContinue
    Write-Log "Firewall rules removed"

    Write-Log "LocalWEB Windows Service uninstalled successfully"
}