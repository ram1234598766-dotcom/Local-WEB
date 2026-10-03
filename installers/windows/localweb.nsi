; LocalWEB NSIS Installer Script
; Creates a Windows installer with Wintun driver and optional Windows Service
; Requires NSIS 3.0+ with nsProcess plugin

!include "MUI2.nsh"
!include "LogicLib.nsh"
!include "x64.nsh"
!include "FileFunc.nsh"
!include "WinVer.nsh"

; Application info
;
; APP_VERSION is overridable so scripts/build-nsis.sh can stamp the release
; version with -DAPP_VERSION=. A bare !define here would silently win over that
; flag and the build would ship whatever is written on this line, which is
; exactly the bug this file already had: it reported 1.0.1 while the release was
; being cut as 1.1.0. localweb.wxs:5-11 documents the same trap on the MSI side.
!ifndef APP_VERSION
  !define APP_VERSION "1.0.1"
!endif
!define APP_NAME "LocalWEB"
!define APP_PUBLISHER "LocalWEB Project"
!define APP_WEBSITE "https://github.com/ram1234598766-dotcom/Local-WEB"
!define APP_EXECUTABLE "localweb.exe"
!define CLI_EXECUTABLE "localweb-cli.exe"
; The real install directory is resolved in .onInit instead. $PROGRAMFILES here
; would be "C:\Program Files (x86)" because this is a 32-bit installer, and
; $PROGRAMFILES64 does not expand to anything usable in this NSIS build.
!define INSTALL_DIR "$PROGRAMFILES\LocalWEB"
!define SERVICE_NAME "LocalWEB"
!define SERVICE_DISPLAY_NAME "LocalWEB Mesh Network"
!define SERVICE_DESCRIPTION "Local-first encrypted mesh network daemon"
!define UNINSTALLER_NAME "uninstall.exe"
!define REG_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\LocalWEB"
!define REG_APP_PATH "Software\LocalWEB"

; Modern UI
!define MUI_ICON "${NSISDIR}\Contrib\Graphics\Icons\modern-install.ico"
!define MUI_UNICON "${NSISDIR}\Contrib\Graphics\Icons\modern-uninstall.ico"
!define MUI_WELCOMEPAGE_TITLE "Welcome to the LocalWEB Setup Wizard"
!define MUI_WELCOMEPAGE_TEXT "This wizard will guide you through the installation of LocalWEB, a local-first encrypted mesh network."
!define MUI_COMPONENTSPAGE_TEXT_TOP "Choose which features to install:"
!define MUI_DIRECTORYPAGE_TEXT_TOP "Choose the installation directory:"
!define MUI_FINISHPAGE_TITLE "Setup Complete"
!define MUI_FINISHPAGE_TEXT "LocalWEB has been installed on your computer."
!define MUI_FINISHPAGE_SHOWREADME ""
!define MUI_FINISHPAGE_RUN "$INSTDIR\localweb.exe"
!define MUI_FINISHPAGE_RUN_TEXT "Launch LocalWEB now"

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_LICENSE "LICENSE"
!insertmacro MUI_PAGE_COMPONENTS
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_WELCOME
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_UNPAGE_FINISH

; Language
!insertmacro MUI_LANGUAGE "English"

; Request admin rights
RequestExecutionLevel admin

; 64-bit
InstallDir "${INSTALL_DIR}"
InstallDirRegKey HKLM "${REG_KEY}" "InstallLocation"

; Output is declared here rather than passed as makensis /OutFile, because
; not every makensis build accepts that switch. Declaring it in the script
; makes the build reproducible with any NSIS 3.x.
Name "${APP_NAME} ${APP_VERSION} Setup"
OutFile "localweb-${APP_VERSION}-setup.exe"

; Pages
; There is deliberately no 'Page custom PreComponentPage' any more. It read
; HKLM\SOFTWARE\Wintun and, when that key was missing -- which it always is,
; because Wintun registers a kernel driver and does not create that key -- popped
; a modal telling the user Wintun ships with the installer. That is true of every
; install, so the dialog only ever added a click. Verified on this host: neither
; HKLM\SOFTWARE\Wintun nor its WOW6432Node counterpart exists.
Page components
Page directory
Page instfiles

; Components
Section "LocalWEB Core" SEC_CORE
    SectionIn RO
    SetOutPath "$INSTDIR"
    File "localweb.exe"
    File "localweb-cli.exe"
    File "README.md"
    File "LICENSE"
    File "CHANGELOG.md"

    ; Each subdirectory gets its own SetOutPath, and the File references only the
    ; name to install. Relying on the directory part of 'File "wintun\wintun.dll"'
    ; does not work: NSIS installed wintun.dll, config.json and every .ps1 flat
    ; into $INSTDIR, so $INSTDIR\scripts\ServiceInstall.ps1 -- the path
    ; .onInstSuccess and un.onInit both invoke -- did not exist and the whole
    ; configure/uninstall step was skipped. Verified by installing this package
    ; and inspecting the result.
    SetOutPath "$INSTDIR\wintun"
    File "wintun\wintun.dll"
    File "wintun\wintun.dll.sig"
    File "wintun\LICENSE"

    ; The .ps1 scripts live once, in installers/windows/. The nsi installs them to
    ; $INSTDIR\scripts\, so stage them into a scripts/ subdirectory to satisfy
    ; 'File "scripts\*.ps1"'.
    SetOutPath "$INSTDIR\scripts"
    File "scripts\*.ps1"

    SetOutPath "$INSTDIR\config"
    File "config\*.json"
SectionEnd

Section "Windows Service (auto-start at boot)" SEC_SERVICE
    ; Optional Windows Service for auto-start
SectionEnd

Section "Start Menu Shortcuts" SEC_SHORTCUTS
    CreateDirectory "$SMPROGRAMS\LocalWEB"
    CreateShortcut "$SMPROGRAMS\LocalWEB\LocalWEB.lnk" "$INSTDIR\localweb.exe" "node" "$INSTDIR\localweb.exe" 0 SW_SHOWNORMAL "" "Start LocalWEB node"
    CreateShortcut "$SMPROGRAMS\LocalWEB\LocalWEB CLI.lnk" "$INSTDIR\localweb-cli.exe" "" "$INSTDIR\localweb-cli.exe" 0 SW_SHOWNORMAL "" "LocalWEB command line interface"
    CreateShortcut "$SMPROGRAMS\LocalWEB\Uninstall.lnk" "$INSTDIR\${UNINSTALLER_NAME}" "" "$INSTDIR\${UNINSTALLER_NAME}" 0 SW_SHOWNORMAL "" "Uninstall LocalWEB"
    CreateShortcut "$DESKTOP\LocalWEB.lnk" "$INSTDIR\localweb.exe" "node" "$INSTDIR\localweb.exe" 0 SW_SHOWNORMAL "" "Start LocalWEB node"
SectionEnd

Section "Wintun Driver" SEC_WINTUN
    SectionIn RO
    ; Wintun driver files already copied in SEC_CORE. Stage the DLL where
    ; pkg/services/vpn/tun_windows.go:80 looks for it.
    ;
    ; CopyFiles rather than a PowerShell one-liner: Expand-Archive only
    ; accepts .zip archives and so never worked on a .dll, and a nested
    ; "-Command \"... $env:... \"" string is fragile here because NSIS
    ; tries to expand the $ itself. CopyFiles does it natively.
    ;
    ; There is deliberately no "sc create wintun type= kernel" here, and the copy
    ; below is not guarded by "sc query wintun".
    ;
    ; Wintun owns the "wintun" kernel-driver service itself. "sc qc wintun"
    ; reports TYPE 1 KERNEL_DRIVER, DISPLAY_NAME "Wintun", bound to
    ; \SystemRoot\System32\drivers\wintun.sys -- a driver image this installer
    ; never writes. Creating that name here pointed it at wintun.dll, which is
    ; not a driver image, so it either left an entry the kernel refused to load
    ; or collided with the driver Wintun had already registered and failed with
    ; 1073.
    ;
    ; Gating the DLL copy on that service check also tied an unrelated file copy
    ; to whether Wintun happened to be loaded, which is not what "is the driver
    ; staged" means. CopyFiles is idempotent, so the copy is unconditional and
    ; reports failure without aborting the install.
    ClearErrors
    CopyFiles "$INSTDIR\wintun\wintun.dll" "$WINDIR\System32\drivers\wintun.dll"
    ${If} ${Errors}
        DetailPrint "Failed to copy wintun.dll to $WINDIR\System32\drivers"
    ${EndIf}
SectionEnd

Function .onInit
    ; Resolve the install directory here rather than relying on a shell constant.
    ; This installer is built as a 32-bit PE, so $PROGRAMFILES is
    ; "C:\Program Files (x86)" and the package landed in a different directory
    ; than the MSI, which installs to System64Folder. Two copies of the same
    ; product in two directories would leave two services and two firewall rule
    ; sets. Verified on this host, with a 32-bit NSIS installer:
    ;   $PROGRAMFILES        -> C:\Program Files (x86)
    ;   $PROGRAMFILES64      -> "C:\Program Filesiles"   (garbled, unusable)
    ;   $PROGRAMW6432        -> the literal string, not a constant in this build
    ;   HKLM\...\CurrentVersion\ProgramFilesDir -> C:\Program Files (x86)
    ;   SetShellVarContext all -> $PROGRAMFILES still C:\Program Files (x86)
    ;
    ; ProgramW6432 in the environment is what Windows itself sets for a 32-bit
    ; process on 64-bit Windows, so it yields the native Program Files and is
    ; localised the same way the MSI's System64Folder is. ${AtLeastWin} has
    ; already rejected 32-bit Windows by the time this matters, but the
    ; $PROGRAMFILES fallback keeps the value sane if that check is ever moved.
    System::Call 'kernel32::GetEnvironmentVariableW(w "ProgramW6432", w .r0, i 1024)i.r1'
    ${If} $1 > 0
        StrCpy $INSTDIR "$0\LocalWEB"
    ${Else}
        StrCpy $INSTDIR "$PROGRAMFILES\LocalWEB"
    ${EndIf}

    ; Check if running on Windows 10/11
    ${IfNot} ${AtLeastWin10}
        MessageBox MB_ICONSTOP "LocalWEB requires Windows 10 or later." /SD IDOK
        Abort
    ${EndIf}

    ; Check for Wintun driver
    ${IfNot} ${RunningX64}
        MessageBox MB_ICONSTOP "LocalWEB requires a 64-bit version of Windows." /SD IDOK
        Abort
    ${EndIf}
FunctionEnd

Function .onInstSuccess
    ; Register uninstaller
    WriteRegStr HKLM "${REG_KEY}" "DisplayName" "${APP_NAME}"
    WriteRegStr HKLM "${REG_KEY}" "DisplayVersion" "${APP_VERSION}"
    WriteRegStr HKLM "${REG_KEY}" "Publisher" "${APP_PUBLISHER}"
    WriteRegStr HKLM "${REG_KEY}" "URLInfoAbout" "${APP_WEBSITE}"
    WriteRegStr HKLM "${REG_KEY}" "InstallLocation" "$INSTDIR"
    WriteRegStr HKLM "${REG_KEY}" "UninstallString" "$INSTDIR\${UNINSTALLER_NAME}"
    WriteRegStr HKLM "${REG_KEY}" "DisplayIcon" "$INSTDIR\localweb.exe"
    WriteRegStr HKLM "${REG_KEY}" "NoModify" "1"
    WriteRegStr HKLM "${REG_KEY}" "NoRepair" "1"
    WriteRegStr HKLM "${REG_KEY}" "EstimatedSize" "51200"

    ; Register app paths
    WriteRegStr HKLM "${REG_APP_PATH}" "InstallDir" "$INSTDIR"
    WriteRegStr HKLM "${REG_APP_PATH}" "Version" "${APP_VERSION}"

    ; Wintun staging and the firewall rules are the script's job, and -InstallDir is
    ; passed so the rules point at the directory this install actually chose. The old
    ; netsh one-liners here could not do that: they hardcoded name="LocalWEB" against
    ; $INSTDIR, so an install to any other directory produced rules pointing nowhere.
    ;
    ; NSIS ExecWait goes through CreateProcess, which cannot launch a .ps1
    ; directly: it fails with ERROR_BAD_EXE_FORMAT ("not a valid application for
    ; this OS platform"), and these call sites ignored the failure. Verified by
    ; launching a .ps1 the same way, which threw that exact error, while the same
    ; script through powershell.exe -File ran and printed its output. So the
    ; service, the firewall rules and the Wintun staging were all silently
    ; skipped by this installer. PowerShell has to be invoked explicitly.
    ;
    ; $SYSDIR\WindowsPowerShell\v1.0\powershell.exe rather than a bare
    ; powershell.exe so the path is not subject to 32/64-bit System32
    ; redirection.
    ;
    ; -CreateService is what hands the service lifecycle to this installer. The MSI
    ; deliberately does not pass it, because its ServiceInstall element owns the
    ; service; see ServiceInstall.ps1.
    ${If} ${SectionIsSelected} ${SEC_SERVICE}
        ExecWait '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -ExecutionPolicy Bypass -File "$INSTDIR\scripts\ServiceInstall.ps1" -Install -CreateService -InstallDir "$INSTDIR"' $0
    ${Else}
        ExecWait '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -ExecutionPolicy Bypass -File "$INSTDIR\scripts\ServiceInstall.ps1" -Install -InstallDir "$INSTDIR"' $0
    ${EndIf}
    ; Report the failure instead of leaving a half-configured install that looks
    ; successful. /SD IDOK keeps the silent install from blocking on the dialog.
    ; DetailPrint only, because this makensis is built without NSIS_CONFIG_LOG, so
    ; there is no install log for LogSet to write to.
    ${If} $0 != 0
        DetailPrint "ERROR: ServiceInstall.ps1 failed with exit code $0"
        MessageBox MB_ICONSTOP "LocalWEB could not finish configuring Windows (exit $0). The Windows service, the firewall rules or the Wintun driver may be missing." /SD IDOK
        Abort
    ${EndIf}
FunctionEnd

Section -Post
    WriteUninstaller "$INSTDIR\${UNINSTALLER_NAME}"
    WriteRegStr HKLM "${REG_KEY}" "UninstallString" "$INSTDIR\${UNINSTALLER_NAME}"
SectionEnd

; Uninstaller
Function un.onInit
    ; The firewall rules were created by ServiceInstall.ps1 with
    ; New-NetFirewallRule -DisplayName, so they have to be removed the same way.
    ; "netsh advfirewall firewall delete rule name=..." matches a rule's Name,
    ; not its DisplayName, and New-NetFirewallRule does not set Name from
    ; -DisplayName, so that netsh form silently removed nothing.
    ;
    ; -RemoveService because this installer created the service itself (see
    ; -CreateService on the install side), unlike the MSI whose ServiceInstall
    ; element owns it. The script stops the service and waits for it to actually
    ; exit before deleting, which this installer cannot do on its own: the previous
    ; 'sc stop' here followed by a fixed two second sleep was not enough, and
    ; deleting the binary of a still-running service left localweb.exe behind.
    ; Verified by uninstalling and finding localweb.exe left in the directory.
    ;
    ; This runs here rather than in Section Uninstall because the script lives under
    ; $INSTDIR\scripts, which that section deletes.
    ;
    ; PowerShell has to be named explicitly: ExecWait uses CreateProcess, which
    ; cannot launch a .ps1, so this call used to fail silently and leave the
    ; firewall rules behind after every uninstall.
    ExecWait '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -ExecutionPolicy Bypass -File "$INSTDIR\scripts\ServiceInstall.ps1" -Uninstall -RemoveService -InstallDir "$INSTDIR"' $0
    ; A failure here leaves stale firewall rules and possibly the service, but it
    ; must not block the uninstall the user asked for, so report it and carry on.
    ${If} $0 != 0
        DetailPrint "WARNING: ServiceInstall.ps1 -Uninstall failed with exit code $0; firewall rules or the service may remain"
    ${EndIf}
FunctionEnd

Section Uninstall
    ; Fallback only. ServiceInstall.ps1 -RemoveService in un.onInit stops the
    ; service (waiting for the process to exit) and deletes it. If that failed the
    ; service would be left registered pointing at files this section is about to
    ; delete, so it is caught here rather than silently orphaned.
    nsExec::ExecToLog 'sc query "${SERVICE_NAME}" >NUL 2>&1'
    Pop $0
    ${If} $0 == "0"
        nsExec::ExecToLog 'sc stop "${SERVICE_NAME}"'
        Sleep 3000
        nsExec::ExecToLog 'sc delete "${SERVICE_NAME}"'
    ${EndIf}

    ; Remove files
    Delete "$INSTDIR\localweb.exe"
    Delete "$INSTDIR\localweb-cli.exe"
    Delete "$INSTDIR\README.md"
    Delete "$INSTDIR\LICENSE"
    Delete "$INSTDIR\CHANGELOG.md"
    Delete "$INSTDIR\${UNINSTALLER_NAME}"

    ; wintun.dll is installed under $INSTDIR\wintun\, so the old
    ; 'Delete "$INSTDIR\wintun.dll"' matched nothing and left it behind. Same
    ; reason config\ has to be removed explicitly.
    RMDir /r "$INSTDIR\wintun"
    RMDir /r "$INSTDIR\scripts"
    RMDir /r "$INSTDIR\config"
    RMDir /r "$INSTDIR"

    ; Remove shortcuts
    Delete "$SMPROGRAMS\LocalWEB\LocalWEB.lnk"
    Delete "$SMPROGRAMS\LocalWEB\LocalWEB CLI.lnk"
    Delete "$SMPROGRAMS\LocalWEB\Uninstall.lnk"
    RMDir "$SMPROGRAMS\LocalWEB"
    Delete "$DESKTOP\LocalWEB.lnk"

    ; Remove registry keys
    DeleteRegKey HKLM "${REG_KEY}"
    DeleteRegKey HKLM "${REG_APP_PATH}"
    ; The firewall rules were already removed in un.onInit, before this section
    ; deleted the script that owns them.
SectionEnd

Function un.onUninstSuccess
    HideWindow
    ; /SD IDOK so a silent uninstall takes the default and returns instead of
    ; waiting on a dialog nobody can see.
    MessageBox MB_ICONINFORMATION "LocalWEB has been successfully uninstalled." /SD IDOK
FunctionEnd