<# 
.SYNOPSIS
    LocalWEB Windows Service Installation Script
.DESCRIPTION
    Stages the Wintun driver and writes the firewall rules for an installed
    LocalWEB node. Those two jobs are all the MSI needs, because the MSI's native
    ServiceInstall element in localweb.wxs owns the service lifecycle itself.

    The NSIS installer has no such element, so it passes -CreateService to have
    this script create and start the service. Ownership is therefore explicit per
    caller rather than implied, which is what keeps the two installers from both
    trying to own the same service. Its uninstaller passes -RemoveService for the
    same reason in reverse.
#>

param(
    [switch]$Install,
    [switch]$Uninstall,
    [switch]$CreateService,
    [switch]$RemoveService,
    [string]$InstallDir = "C:\Program Files\LocalWEB",
    [string]$ServiceName = "LocalWEB",
    [string]$ServiceDisplayName = "LocalWEB Mesh Network",
    [string]$ServiceDescription = "Local-first encrypted mesh network daemon"
)

$ErrorActionPreference = "Stop"
$VerbosePreference = "Continue"

# Normalise the install directory before anything joins onto it.
#
# The MSI passes -InstallDir "[INSTALLDIR]", and INSTALLDIR carries a trailing
# backslash. On a command line a backslash immediately before the closing quote
# escapes that quote, so the value arrived here as C:\Program Files\LocalWEB"  and
# every Join-Path built a nonsense path ending in a stray quote. The install then
# failed with 1603 and, because the package rolled back, the script's own error
# was nowhere in the MSI log.
#
# Trailing separators and a stray quote are stripped here rather than in the caller,
# so the MSI, the PowerShell installer and anyone running this by hand all behave the
# same. The quote is stripped because that is what the escaping above leaves behind:
# the value arrives as C:\Program Files\LocalWEB" and every Join-Path then builds a
# path containing a quote character.
$InstallDir = $InstallDir.Trim().Trim('"').Trim().TrimEnd('\', '/')
if ([string]::IsNullOrWhiteSpace($InstallDir)) {
    throw "InstallDir must not be empty"
}

# A separate log file, because the MSI log cannot explain its own failure. When a
# custom action exits non-zero the package rolls back and takes its log with it,
# so the reason the action failed is gone by the time anyone reads the log. This
# file lives in the temp directory rather than under InstallDir precisely so it
# survives the rollback that removes the install directory.
$ScriptLogPath = Join-Path $env:TEMP "LocalWEB-installer.log"

function Write-Log {
    param([string]$Message)
    $timestamp = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
    Write-Host "[$timestamp] $Message"
    try {
        Add-Content -Path $ScriptLogPath -Value "[$timestamp] $Message" -ErrorAction Stop
    } catch {
        # Failing to write the log must never fail the install. Write-Host above
        # already reached the console and, under msiexec, the MSI log.
    }
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
    Write-Log "Configuring LocalWEB..."

    # Stage the Wintun DLL into System32\drivers, which is the first place
    # pkg/services/vpn/tun_windows.go:80 looks for it.
    #
    # Copying the file is all this installer does for Wintun. The "wintun"
    # kernel-driver service belongs to Wintun itself: once the node calls
    # WintunCreateAdapter, Wintun registers and starts that driver against its
    # own wintun.sys. Verified on this host with "sc qc wintun", which reports
    # TYPE 1 KERNEL_DRIVER, DISPLAY_NAME "Wintun", START_TYPE 3 DEMAND_START and
    # BINARY_PATH_NAME \SystemRoot\System32\drivers\wintun.sys -- a driver image
    # this installer never writes. LocalWEB-installer.log recorded "no wintun
    # service" during the MSI runs, and the entry exists only once the node has
    # been left running, which is consistent with that.
    #
    # So this script must not create that service. The "sc.exe create wintun
    # type= kernel" it used to run bound that name to wintun.dll, which is not a
    # driver image, so it either left behind an entry the kernel refused to load
    # or, once Wintun had registered the real driver, failed outright with 1073.
    #
    # A failed copy stays a warning rather than fatal: the node also loads the
    # DLL from the wintun\ directory next to its own executable
    # (tun_windows.go:85), so the install still works without the System32 copy.
    $wintunDll = Join-Path $InstallDir "wintun\wintun.dll"
    if (-not (Test-Path $wintunDll)) {
        throw "Wintun DLL not found at $wintunDll"
    }
    try {
        Copy-Item $wintunDll -Destination "$env:SystemRoot\System32\drivers\wintun.dll" -Force
        Write-Log "Wintun driver staged in System32\drivers"
    } catch {
        Write-Log "WARNING: could not stage wintun.dll into System32\drivers: $($_.Exception.Message)"
    }

    # The executable must exist because the firewall rules below point at it.
    $servicePath = Join-Path $InstallDir "localweb.exe"
    if (-not (Test-Path $servicePath)) {
        throw "LocalWEB executable not found at $servicePath"
    }

    # Service lifecycle. The MSI's ServiceInstall element in localweb.wxs owns the
    # service and creates it later, in InstallServices, so this block is skipped for
    # that caller. Creating it unconditionally is what broke the MSI: sc.exe create
    # returns 1073 when a previous install left the service registered, the throw
    # turned that into a non-zero exit, and the package reported 1722 then 1603.
    #
    # The NSIS installer has no ServiceInstall element, so it passes -CreateService
    # and lands here. The delete-before-create below is what makes that path survive
    # a reinstall over an existing service instead of failing with 1073.
    if ($CreateService) {
        $existing = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
        if ($existing) {
            Write-Log "Service $ServiceName already exists; replacing it"
            if ($existing.Status -ne "Stopped") {
                Stop-Service -Name $ServiceName -Force -ErrorAction SilentlyContinue
                $existing.WaitForStatus("Stopped", [TimeSpan]::FromSeconds(30))
            }
            sc.exe delete $ServiceName | Out-Null
            # sc.exe delete only marks the service for removal; the SCM frees the
            # name asynchronously, so creating it again immediately still hits 1073.
            $deadline = (Get-Date).AddSeconds(30)
            while ((Get-Service -Name $ServiceName -ErrorAction SilentlyContinue) -and
                   ((Get-Date) -lt $deadline)) {
                Start-Sleep -Milliseconds 500
            }
        }

        # "node" is a compatibility verb that cmd/node/main.go strips, so the
        # installed binPath matches the MSI's.
        $serviceArgs = "node --data-dir C:\ProgramData\LocalWEB"

        # New-Service, not sc.exe create. sc.exe cannot accept a binPath that
        # carries arguments when it is invoked from PowerShell: PowerShell
        # rebuilds the command line from argv and the nested quoting around the
        # executable plus the trailing arguments does not survive, so sc.exe
        # printed its usage text and exited 1639 without creating anything.
        # Verified by isolating it -- 'binPath= "<exe>"' succeeded while
        # 'binPath= "<exe>" node --data-dir C:\ProgramData\LocalWEB' failed, and
        # the failure only ever appeared on this line.
        #
        # New-Service writes the same result the MSI's WiX ServiceInstall does:
        #   BINARY_PATH_NAME  "C:\Program Files\LocalWEB\localweb.exe" node --data-dir C:\ProgramData\LocalWEB
        #   TYPE              10  WIN32_OWN_PROCESS
        #   START_TYPE        2   AUTO_START
        #   SERVICE_START_NAME LocalSystem
        New-Service -Name $ServiceName `
            -BinaryPathName "`"$servicePath`" $serviceArgs" `
            -DisplayName $ServiceDisplayName `
            -StartupType Automatic `
            -Description $ServiceDescription `
            -ErrorAction Stop | Out-Null

        # Recovery actions are not exposed by New-Service, so sc.exe still sets
        # them. This one takes no arguments that need quoting, which is why it
        # works where the create above did not.
        sc.exe failure $ServiceName reset= 86400 actions= restart/5000/restart/10000/restart/60000 | Out-Null

        Start-Service -Name $ServiceName -ErrorAction SilentlyContinue
        Write-Log "Service $ServiceName created and started"
    } else {
        Write-Log "Leaving service lifecycle to the MSI ServiceInstall element"
    }

    # Firewall rules are the one job no WiX element can express. The program
    # path is taken from InstallDir rather than repeated literally, which is
    # what pointed the rules at C:\Program Files\LocalWEB\localweb.exe even when
    # the package installed elsewhere.
    if (-not (Get-NetFirewallRule -DisplayName $ServiceName -ErrorAction SilentlyContinue)) {
        New-NetFirewallRule -DisplayName $ServiceName -Direction Inbound -Action Allow `
            -Program $servicePath -Profile Domain,Private -Enabled True
        New-NetFirewallRule -DisplayName $ServiceName -Direction Outbound -Action Allow `
            -Program $servicePath -Profile Domain,Private -Enabled True
        Write-Log "Firewall rules created for $servicePath"
    } else {
        Write-Log "Firewall rules for $ServiceName already exist; leaving them unchanged"
    }

    Write-Log "LocalWEB configuration complete"
}

if ($Uninstall) {
    Write-Log "Removing LocalWEB configuration..."

    # The service is only removed when the caller owns it. The MSI's
    # ServiceInstall element has Remove="uninstall" and does that itself, and
    # deleting it from here as well would race with that step.
    #
    # The NSIS installer has no such element and created the service itself, so
    # its uninstaller passes -RemoveService and the removal happens here. It has
    # to be here rather than in the NSIS script because the stop has to be waited
    # out: the uninstaller used to 'sc stop' then sleep two seconds, which is not
    # long enough, and deleting a still-running service's binary left
    # localweb.exe behind in the install directory. Verified by uninstalling.
    if ($RemoveService) {
        $existing = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
        if ($existing) {
            if ($existing.Status -ne "Stopped") {
                Write-Log "Stopping service $ServiceName before removal"
                Stop-Service -Name $ServiceName -Force -ErrorAction SilentlyContinue
                # Wait for the process to actually exit, otherwise its binary is
                # still locked and cannot be deleted.
                try {
                    $existing.WaitForStatus("Stopped", [TimeSpan]::FromSeconds(30))
                } catch {
                    Write-Log "WARNING: service $ServiceName did not report Stopped within 30s: $($_.Exception.Message)"
                }
            }
            sc.exe delete $ServiceName | Out-Null
            # sc.exe delete only marks the service for removal; the SCM frees the
            # name asynchronously.
            $deadline = (Get-Date).AddSeconds(30)
            while ((Get-Service -Name $ServiceName -ErrorAction SilentlyContinue) -and
                   ((Get-Date) -lt $deadline)) {
                Start-Sleep -Milliseconds 500
            }
            if (Get-Service -Name $ServiceName -ErrorAction SilentlyContinue) {
                Write-Log "WARNING: service $ServiceName still registered after delete"
            } else {
                Write-Log "Service $ServiceName removed"
            }
        } else {
            Write-Log "Service $ServiceName is not registered; nothing to remove"
        }
    } else {
        Write-Log "Leaving service removal to the MSI ServiceInstall element"
    }

    Remove-NetFirewallRule -DisplayName $ServiceName -ErrorAction SilentlyContinue
    Write-Log "Firewall rules removed for $ServiceName"

    Write-Log "LocalWEB configuration removed"
}