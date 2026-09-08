<#
.SYNOPSIS
    LocalWEB Windows Installer Script
    Downloads and installs the latest LocalWEB MSI from GitHub Releases

.DESCRIPTION
    This script downloads the latest LocalWEB MSI installer from GitHub Releases,
    installs it silently with optional components, and configures the service.

.REQUIREMENTS
    - Windows 10/11 (64-bit)
    - Administrator privileges
    - PowerShell 5.1+

.EXAMPLE
    # Run as Administrator in PowerShell:
    iwr -useb https://raw.githubusercontent.com/ram1234598766-dotcom/Local-WEB/main/installers/windows/install.ps1 | iex
#>

#Requires -RunAsAdministrator

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$REPO = "ram1234598766-dotcom/Local-WEB"
$APP_NAME = "LocalWEB"
$MSI_NAME = "LocalWEB-Setup.msi"
$INSTALL_DIR = "C:\Program Files\LocalWEB"
$SERVICE_NAME = "LocalWEB"
$WINTUN_URL = "https://github.com/wintun/wintun/releases/download/v0.14.1/wintun-0.14.1.zip"

function Write-Log {
    param([string]$Message, [string]$Level = "INFO")
    $timestamp = Get-Date -Format "HH:mm:ss"
    $colors = @{
        INFO = "Cyan"
        SUCCESS = "Green"
        WARN = "Yellow"
        ERROR = "Red"
    }
    $color = $colors[$Level] ?? "White"
    Write-Host "[$timestamp] [$Level] $Message" -ForegroundColor $color
}

function Check-Admin {
    $currentUser = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($currentUser)
    if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        Write-Log "This script must be run as Administrator" -Level "ERROR"
        exit 1
    }
}

function Check-WindowsVersion {
    $osVersion = [System.Environment]::OSVersion.Version
    if ($osVersion.Major -lt 10) {
        Write-Log "LocalWEB requires Windows 10 or later" -Level "ERROR"
        exit 1
    }
    if (-not [Environment]::Is64BitOperatingSystem) {
        Write-Log "LocalWEB requires 64-bit Windows" -Level "ERROR"
        exit 1
    }
    Write-Log "Windows version: $($osVersion.Major).$($osVersion.Minor).$($osVersion.Build)" -Level "INFO"
}

function Get-LatestRelease {
    Write-Log "Fetching latest release from GitHub..." -Level "INFO"
    try {
        $apiUrl = "https://api.github.com/repos/ram1234598766-dotcom/Local-WEB/releases/latest"
        $response = Invoke-RestMethod -Uri $apiUrl -Method Get -Headers @{Accept = "application/vnd.github.v3+json"}
        
        $script:VERSION = $response.tag_name
        $script:MSI_URL = $response.assets | Where-Object { $_.name -like "*.msi" } | Select-Object -First 1 -ExpandProperty browser_download_url
        $script:WINTUN_URL = $response.assets | Where-Object { $_.name -like "*wintun*" } | Select-Object -First 1 -ExpandProperty browser_download_url
        
        if (-not $script:VERSION -or -not $script:MSI_URL) {
            Write-Log "Failed to find MSI asset in latest release" -Level "ERROR"
            exit 1
        }
        
        Write-Log "Latest version: $VERSION" -Level "INFO"
        Write-Log "MSI URL: $MSI_URL" -Level "INFO"
    } catch {
        Write-Log "Failed to fetch release info: $_" -Level "ERROR"
        exit 1
    }
}

function Download-File {
    param(
        [string]$Url,
        [string]$OutFile,
        [string]$Description = "File"
    )
    
    Write-Log "Downloading $Description..." -Level "INFO"
    try {
        $progressPreference = 'silentlyContinue'
        Invoke-WebRequest -Uri $Url -OutFile $OutFile -UseBasicParsing
        $size = [math]::Round((Get-Item $OutFile).Length / 1MB, 2)
        Write-Log "Downloaded $Description ($size MB)" -Level "SUCCESS"
    } catch {
        Write-Log "Failed to download $Description: $_" -Level "ERROR"
        exit 1
    }
}

function Install-Wintun {
    Write-Log "Installing Wintun driver..." -Level "INFO"
    
    $wintunZip = "$env:TEMP\wintun.zip"
    Download-File -Url "https://github.com/wintun/wintun/releases/download/v0.14.1/wintun-0.14.1.zip" -OutFile $wintunZip -Description "Wintun driver"
    
    $wintunDir = "$env:TEMP\wintun"
    if (Test-Path $wintunDir) { Remove-Item $wintunDir -Recurse -Force }
    New-Item -ItemType Directory -Path $wintunDir -Force | Out-Null
    
    Expand-Archive -Path $wintunZip -DestinationPath $wintunDir -Force
    $wintunDll = Join-Path $wintunDir "wintun\x64\wintun.dll"
    
    if (Test-Path $wintunDll) {
        Copy-Item $wintunDll -Destination "C:\Windows\System32\drivers\wintun.dll" -Force
        Write-Log "Wintun driver installed to System32\drivers" -Level "SUCCESS"
        
        # Register Wintun as kernel driver if not already registered
        if (-not (Get-Service -Name "wintun" -ErrorAction SilentlyContinue)) {
            sc.exe create wintun binPath= "C:\Windows\System32\drivers\wintun.dll" type= kernel start= demand 2>$null
            sc.exe start wintun 2>$null
            Write-Log "Wintun service registered and started" -Level "SUCCESS"
        }
    } else {
        Write-Log "Wintun DLL not found in archive" -Level "WARN"
    }
    
    Remove-Item $wintunZip -Force -ErrorAction SilentlyContinue
    if (Test-Path $wintunDir) { Remove-Item $wintunDir -Recurse -Force }
}

function Install-MSI {
    Write-Log "Installing LocalWEB MSI..." -Level "INFO"
    
    $msiPath = "$env:TEMP\$MSI_NAME"
    Download-File -Url $MSI_URL -OutFile $msiPath -Description "LocalWEB MSI"
    
    # Silent install with all components
    $arguments = @(
        "/i", "`"$msiPath`"",
        "/qn",                    # Quiet, no UI
        "/norestart",             # Don't restart
        "ADDLOCAL=ALL",           # Install all components
        "INSTALLDIR=`"$INSTALL_DIR`""
    )
    
    Write-Log "Running MSI installer..." -Level "INFO"
    $process = Start-Process msiexec.exe -ArgumentList $arguments -Wait -PassThru
    
    if ($process.ExitCode -eq 0) {
        Write-Log "MSI installation completed successfully" -Level "SUCCESS"
    } else {
        Write-Log "MSI installation failed with exit code $($process.ExitCode)" -Level "ERROR"
        exit 1
    }
    
    Remove-Item $msiPath -Force -ErrorAction SilentlyContinue
}

function Configure-Service {
    Write-Log "Configuring Windows Service..." -Level "INFO"
    
    $servicePath = Join-Path $INSTALL_DIR "localweb.exe"
    if (-not (Test-Path $servicePath)) {
        Write-Log "LocalWEB executable not found at $servicePath" -Level "WARN"
        return
    }
    
    $serviceArgs = "node --data-dir C:\ProgramData\LocalWEB"
    
    # Create service if not exists
    if (-not (Get-Service -Name $SERVICE_NAME -ErrorAction SilentlyContinue)) {
        sc.exe create $SERVICE_NAME binPath= "`"$servicePath`" $serviceArgs" `
            DisplayName= "LocalWEB Mesh Network" `
            start= delayed-auto `
            obj= "LocalSystem" `
            type= own
        Write-Log "Service $SERVICE_NAME created" -Level "SUCCESS"
    }
    
    # Set description
    sc.exe description $SERVICE_NAME "Local-first encrypted mesh network daemon"
    
    # Configure recovery actions
    sc.exe failure $SERVICE_NAME reset= 86400 actions= restart/5000/restart/10000/restart/60000
    
    # Set delayed auto start
    Set-ItemProperty -Path "HKLM:\SYSTEM\CurrentControlSet\Services\$SERVICE_NAME" -Name "DelayedAutoStart" -Value 1 -Type DWord -Force
    
    # Start service
    try {
        Start-Service -Name $SERVICE_NAME -ErrorAction Stop
        Write-Log "Service $SERVICE_NAME started" -Level "SUCCESS"
    } catch {
        Write-Log "Service may not have started automatically. Try: Start-Service $SERVICE_NAME" -Level "WARN"
    }
}

function Configure-Firewall {
    Write-Log "Configuring Windows Firewall..." -Level "INFO"
    
    $firewallRuleName = "LocalWEB"
    if (-not (Get-NetFirewallRule -DisplayName $firewallRuleName -ErrorAction SilentlyContinue)) {
        New-NetFirewallRule -DisplayName "LocalWEB" -Direction Inbound -Action Allow `
            -Program "C:\Program Files\LocalWEB\localweb.exe" -Profile Domain,Private -Enabled True
        New-NetFirewallRule -DisplayName "LocalWEB" -Direction Outbound -Action Allow `
            -Program "C:\Program Files\LocalWEB\localweb.exe" -Profile Domain,Private -Enabled True
        Write-Log "Firewall rules created" -Level "SUCCESS"
    } else {
        Write-Log "Firewall rules already exist" -Level "INFO"
    }
}

function Create-Shortcuts {
    Write-Log "Creating Start Menu and Desktop shortcuts..." -Level "INFO"
    
    $startMenuDir = Join-Path $env:ProgramData "Microsoft\Windows\Start Menu\Programs\LocalWEB"
    if (-not (Test-Path $startMenuDir)) {
        New-Item -ItemType Directory -Path $startMenuDir -Force | Out-Null
    }
    
    $shell = New-Object -ComObject WScript.Shell
    
    $shortcuts = @(
        @{ Name = "LocalWEB"; Target = "C:\Program Files\LocalWEB\localweb.exe"; Args = "node"; Desc = "Start LocalWEB node" },
        @{ Name = "LocalWEB CLI"; Target = "C:\Program Files\LocalWEB\localweb-cli.exe"; Args = ""; Desc = "LocalWEB command line interface" },
        @{ Name = "Uninstall LocalWEB"; Target = "C:\Program Files\LocalWEB\uninstall.exe"; Args = ""; Desc = "Uninstall LocalWEB" }
    )
    
    foreach ($sc in $shortcuts) {
        $shortcutPath = Join-Path $startMenuDir "$($sc.Name).lnk"
        $shortcut = $shell.CreateShortcut($shortcutPath)
        $shortcut.TargetPath = $sc.Target
        $shortcut.Arguments = $sc.Args
        $shortcut.WorkingDirectory = "C:\Program Files\LocalWEB"
        $shortcut.Description = $sc.Desc
        $shortcut.Save()
    }
    
    # Desktop shortcut
    $desktopPath = [Environment]::GetFolderPath("Desktop")
    $shortcutPath = Join-Path $desktopPath "LocalWEB.lnk"
    $shortcut = $shell.CreateShortcut($shortcutPath)
    $shortcut.TargetPath = "C:\Program Files\LocalWEB\localweb.exe"
    $shortcut.Arguments = "node"
    $shortcut.WorkingDirectory = "C:\Program Files\LocalWEB"
    $shortcut.Description = "Start LocalWEB node"
    $shortcut.Save()
    
    Write-Log "Shortcuts created" -Level "SUCCESS"
}

function Print-Summary {
    Write-Log "Waiting for node to generate identity..." -Level "INFO"
    Start-Sleep -Seconds 3
    
    $dataDir = "C:\ProgramData\LocalWEB"
    $identityFile = Join-Path $dataDir "identity.json"
    
    if (Test-Path $identityFile) {
        $content = Get-Content $identityFile -Raw
        if ($content -match '"node_id":"([^"]+)"') {
            $nodeId = $matches[1]
            Write-Log "`n============================================" -Level "SUCCESS"
            Write-Log "Installation complete!" -Level "SUCCESS"
            Write-Log "============================================" -Level "SUCCESS"
            Write-Log "Your Node ID: $nodeId" -Level "SUCCESS"
            Write-Log ""
            Write-Log "Next steps:" -Level "INFO"
            Write-Log "  - Check service: Get-Service $SERVICE_NAME" -Level "INFO"
            Write-Log "  - View logs: Get-WinEvent -LogName Application -FilterXPath \"*[System[Provider[@Name='$SERVICE_NAME']]]\" -MaxEvents 20" -Level "INFO"
            Write-Log "  - CLI: localweb-cli peers" -Level "INFO"
            Write-Log "  - On another machine: localweb-cli peers" -Level "INFO"
        }
    } else {
        Write-Log "Identity file not found yet. Node may still be starting." -Level "WARN"
        Write-Log "Check service status: Get-Service $SERVICE_NAME" -Level "INFO"
        Write-Log "View logs: Get-WinEvent -LogName Application -FilterXPath \"*[System[Provider[@Name='$SERVICE_NAME']]]\" -MaxEvents 20" -Level "INFO"
    }
}

# Main
Write-Log "============================================" -Level "INFO"
Write-Log "  LocalWEB Windows Installer" -Level "INFO"
Write-Log "============================================" -Level "INFO"
Write-Log ""

Check-Admin
Check-WindowsVersion
Get-LatestRelease

$msiPath = "$env:TEMP\LocalWEB-Setup.msi"
Download-File -Url $MSI_URL -OutFile $msiPath -Description "LocalWEB MSI"

Install-Wintun
Install-MSI
Configure-Service
Configure-Firewall
Create-Shortcuts

Print-Summary

Write-Log "`nInstallation complete! Your Node ID will appear above once generated." -Level "SUCCESS"
Write-Log "Run 'localweb-cli peers' on another machine to connect." -Level "INFO"