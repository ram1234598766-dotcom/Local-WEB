; LocalWEB NSIS Installer Script
; Creates a Windows installer with Wintun driver and optional Windows Service
; Requires NSIS 3.0+ with nsProcess plugin

!include "MUI2.nsh"
!include "LogicLib.nsh"
!include "x64.nsh"
!include "FileFunc.nsh"

; Application info
!define APP_NAME "LocalWEB"
!define APP_VERSION "1.0.0"
!define APP_PUBLISHER "LocalWEB Project"
!define APP_WEBSITE "https://github.com/ram1234598766-dotcom/Local-WEB"
!define APP_EXECUTABLE "localweb.exe"
!define CLI_EXECUTABLE "localweb-cli.exe"
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

; Pages
Page custom PreComponentPage
Page components
Page directory
Page instfiles

Function PreComponentPage
    ; Check if Wintun is already installed
    ReadRegStr $0 HKLM "SOFTWARE\Wintun" ""
    ${If} $0 == ""
        ; Wintun not found, show notice
        MessageBox MB_ICONINFORMATION "LocalWEB requires the Wintun driver for the VPN service.$\n$\nThe installer will download and install Wintun automatically.$\n$\nThis requires an internet connection." IDOK
    ${EndIf}
FunctionEnd

; Components
Section "LocalWEB Core" SEC_CORE
    SectionIn RO
    SetOutPath "$INSTDIR"
    File "localweb.exe"
    File "localweb-cli.exe"
    File "README.md"
    File "LICENSE"
    File "CHANGELOG.md"
    File "wintun\wintun.dll"
    File "wintun\wintun.dll.sig"
    File "wintun\LICENSE"
    File "scripts\*.ps1"
    File "config\*.json"
SectionEnd

Section "Windows Service (auto-start at boot)" SEC_SERVICE
    ; Optional Windows Service for auto-start
SectionEnd

Section "Start Menu Shortcuts" SEC_SHORTCUTS
    CreateDirectory "$SMPROGRAMS\LocalWEB"
    CreateShortcut "$SMPROGRAMS\LocalWEB\LocalWEB.lnk" "$INSTDIR\localweb.exe" "node" "" "$INSTDIR" "Start LocalWEB node"
    CreateShortcut "$SMPROGRAMS\LocalWEB\LocalWEB CLI.lnk" "$INSTDIR\localweb-cli.exe" "" "" "$INSTDIR" "LocalWEB command line interface"
    CreateShortcut "$SMPROGRAMS\LocalWEB\Uninstall.lnk" "$INSTDIR\${UNINSTALLER_NAME}" "" "" "$INSTDIR" "Uninstall LocalWEB"
    CreateShortcut "$DESKTOP\LocalWEB.lnk" "$INSTDIR\localweb.exe" "node" "" "$INSTDIR" "Start LocalWEB node"
SectionEnd

Section "Wintun Driver" SEC_WINTUN
    SectionIn RO
    ; Wintun driver files already copied in SEC_CORE
    ; Install Wintun service if not present
    nsExec::ExecToLog 'sc query wintun'
    Pop $0
    ${If} $0 != "0"
        ; Wintun not installed, install it
        nsExec::ExecToLog 'powershell -Command "Expand-Archive -Path $INSTDIR\wintun\wintun.dll -DestinationPath $env:SYSTEMROOT\System32\drivers -Force"'
        nsExec::ExecToLog 'sc create wintun binPath= "C:\Windows\System32\drivers\wintun.dll" type= kernel start= demand'
        nsExec::ExecToLog 'sc start wintun'
    ${EndIf}
SectionEnd

Function .onInit
    ; Check if running on Windows 10/11
    ${If} ${AtLeastWin10} == 0
        MessageBox MB_ICONSTOP "LocalWEB requires Windows 10 or later." IDOK
        Abort
    ${EndIf}

    ; Check for Wintun driver
    ${IfNot} ${RunningX64}
        MessageBox MB_ICONSTOP "LocalWEB requires a 64-bit version of Windows." IDOK
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

    ; Install Windows Service if selected
    ${If} ${SectionIsSelected} ${SEC_SERVICE}
        ExecWait '"$INSTDIR\scripts\ServiceInstall.ps1" -Install'
    ${EndIf}

    ; Add to Windows Firewall
    ExecWait 'netsh advfirewall firewall add rule name="LocalWEB" dir=in action=allow program="$INSTDIR\localweb.exe" enable=yes profile=private,domain'
    ExecWait 'netsh advfirewall firewall add rule name="LocalWEB" dir=out action=allow program="$INSTDIR\localweb.exe" enable=yes profile=private,domain'
FunctionEnd

Section -Post
    WriteUninstaller "$INSTDIR\${UNINSTALLER_NAME}"
    WriteRegStr HKLM "${REG_KEY}" "UninstallString" "$INSTDIR\${UNINSTALLER_NAME}"
SectionEnd

; Uninstaller
Function un.onInit
    ; Check if service is running, stop it
    nsExec::ExecToLog 'sc query "${SERVICE_NAME}" | findstr "RUNNING"'
    Pop $0
    ${If} $0 == "0"
        ExecWait 'sc stop "${SERVICE_NAME}"'
        Sleep 2000
    ${EndIf}

    ; Remove firewall rules
    ExecWait 'netsh advfirewall firewall delete rule name="LocalWEB"'
FunctionEnd

Section Uninstall
    ; Stop and remove service
    nsExec::ExecToLog 'sc stop "${SERVICE_NAME}"'
    Sleep 2000
    nsExec::ExecToLog 'sc delete "${SERVICE_NAME}"'

    ; Remove files
    Delete "$INSTDIR\localweb.exe"
    Delete "$INSTDIR\localweb-cli.exe"
    Delete "$INSTDIR\wintun.dll"
    Delete "$INSTDIR\README.md"
    Delete "$INSTDIR\LICENSE"
    Delete "$INSTDIR\CHANGELOG.md"
    Delete "$INSTDIR\${UNINSTALLER_NAME}"
    Delete "$INSTDIR\scripts\*.ps1"

    RMDir /r "$INSTDIR\scripts"
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

    ; Remove firewall rules
    ExecWait 'netsh advfirewall firewall delete rule name="LocalWEB"'
SectionEnd

Function un.onUninstSuccess
    HideWindow
    MessageBox MB_ICONINFORMATION "LocalWEB has been successfully uninstalled." IDOK
FunctionEnd