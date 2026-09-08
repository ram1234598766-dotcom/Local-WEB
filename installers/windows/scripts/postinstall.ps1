<#
.SYNOPSIS
    Post-installation script for LocalWEB MSI
.DESCRIPTION
    Runs after files are installed to configure service, firewall, and shortcuts
#>

$ErrorActionPreference = "Stop"

param(
    [string]$InstallDir = "C:\Program Files\LocalWEB"
)

$ErrorActionPreference = "Continue"

Write-Host "LocalWEB Post-Installation Configuration"

# Create data directory
$dataDir = "C:\ProgramData\LocalWEB"
if (-not (Test-Path $dataDir)) {
    New-Item -ItemType Directory -Path $dataDir -Force | Out-Null
    # Set permissions for LocalSystem
    $acl = Get-Acl $dataDir
    $rule = New-Object System.Security.AccessControl.FileSystemAccessRule("LOCAL SYSTEM", "FullControl", "ContainerInherit,ObjectInherit", "None", "Allow")
    $acl.SetAccessRule($rule)
    Set-Acl $dataDir $acl
}

# Create config directory
$configDir = "C:\ProgramData\LocalWEB"
if (-not (Test-Path $configDir)) {
    New-Item -ItemType Directory -Path $configDir -Force | Out-Null
}

# Install Wintun driver if not present
if (-not (Get-Service -Name "wintun" -ErrorAction SilentlyContinue)) {
    Write-Host "Installing Wintun driver..."
    $wintunDll = Join-Path $env:ProgramFiles "LocalWEB\wintun\wintun.dll"
    if (Test-Path $wintunDll) {
        Copy-Item $wintunDll -Destination "C:\Windows\System32\drivers\wintun.dll" -Force
        sc.exe create wintun binPath= "C:\Windows\System32\drivers\wintun.dll" type= kernel start= demand
        sc.exe start wintun
    }
}

# Create LocalWEB service
$servicePath = "C:\Program Files\LocalWEB\localweb.exe"
if (Test-Path $servicePath) {
    $serviceArgs = "node --data-dir C:\ProgramData\LocalWEB"
    sc.exe create "LocalWEB" binPath= "`"$servicePath`" $serviceArgs" `
        DisplayName= "LocalWEB Mesh Network" `
        start= delayed-auto `
        obj= "LocalSystem" `
        type= own

    sc.exe description "LocalWEB" "Local-first encrypted mesh network daemon"
    sc.exe failure "LocalWEB" reset= 86400 actions= restart/5000/restart/10000/restart/60000
    Set-ItemProperty -Path "HKLM:\SYSTEM\CurrentControlSet\Services\LocalWEB" -Name "DelayedAutoStart" -Value 1 -Type DWord

    # Start service
    Start-Service -Name "LocalWEB" -ErrorAction SilentlyContinue
}

# Firewall rules
$firewallRuleName = "LocalWEB"
if (-not (Get-NetFirewallRule -DisplayName "LocalWEB" -ErrorAction SilentlyContinue)) {
    New-NetFirewallRule -DisplayName "LocalWEB" -Direction Inbound -Action Allow `
        -Program "C:\Program Files\LocalWEB\localweb.exe" -Profile Domain,Private -Enabled True
    New-NetFirewallRule -DisplayName "LocalWEB" -Direction Outbound -Action Allow `
        -Program "C:\Program Files\LocalWEB\localweb.exe" -Profile Domain,Private -Enabled True
}

# Create Start Menu shortcuts
$startMenuDir = Join-Path $env:ProgramData "Microsoft\Windows\Start Menu\Programs\LocalWEB"
if (-not (Test-Path $startMenuDir)) {
    New-Item -ItemType Directory -Path $startMenuDir -Force | Out-Null
}

$shortcuts = @(
    @{ Name = "LocalWEB"; Target = "C:\Program Files\LocalWEB\localweb.exe"; Args = "node"; Desc = "Start LocalWEB node" },
    @{ Name = "LocalWEB CLI"; Target = "C:\Program Files\LocalWEB\localweb-cli.exe"; Args = ""; Desc = "LocalWEB command line interface" },
    @{ Name = "Uninstall LocalWEB"; Target = "C:\Program Files\LocalWEB\uninstall.exe"; Args = ""; Desc = "Uninstall LocalWEB" }
)

foreach ($sc in $shortcuts) {
    $shortcutPath = Join-Path $startMenuDir "$($sc.Name).lnk"
    $shell = New-Object -ComObject WScript.Shell
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
$shell = New-Object -ComObject WScript.Shell
$shortcut = $shell.CreateShortcut($shortcutPath)
$shortcut.TargetPath = "C:\Program Files\LocalWEB\localweb.exe"
$shortcut.Arguments = "node"
$shortcut.WorkingDirectory = "C:\Program Files\LocalWEB"
$shortcut.Description = "Start LocalWEB node"
$shortcut.Save()

Write-Host "LocalWEB installation completed successfully"
exit 0