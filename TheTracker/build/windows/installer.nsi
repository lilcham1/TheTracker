; TheTracker installer.
;
; Per-user, no admin prompt, into %LOCALAPPDATA%\TheTracker — the same place,
; registry key and shortcut names every earlier release used, so running it
; over any older version upgrades that install in place.
;
; Switches:
;   /S  silent
;   /P  passive (no wizard) — what the in-app updater passes
;   /R  reopen TheTracker when done
;   /M  ...in the tray, not in front (an update during a match)
;
; Built by build.sh, which passes VERSION and the paths.

Unicode true
ManifestDPIAware true
RequestExecutionLevel user
SetCompressor /SOLID lzma

!ifndef VERSION
  !define VERSION "0.0.0"
!endif
!ifndef EXE
  !define EXE "..\bin\thetracker.exe"
!endif
!ifndef OUTFILE
  !define OUTFILE "..\bin\TheTracker_${VERSION}_x64-setup.exe"
!endif

!ifndef PRODUCT
  !define PRODUCT "TheTracker"
!endif
!define UNINSTKEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\${PRODUCT}"
!define WEBVIEW2KEY "SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}"

Name "${PRODUCT}"
OutFile "${OUTFILE}"
InstallDir "$LOCALAPPDATA\${PRODUCT}"
VIProductVersion "${VERSION}.0"
VIAddVersionKey "ProductName" "${PRODUCT}"
VIAddVersionKey "FileDescription" "${PRODUCT} Setup"
VIAddVersionKey "FileVersion" "${VERSION}"
VIAddVersionKey "ProductVersion" "${VERSION}"
VIAddVersionKey "LegalCopyright" "lilcham1"

!include "MUI2.nsh"
!include "FileFunc.nsh"
!include "LogicLib.nsh"

!define MUI_ICON "icon.ico"
!define MUI_UNICON "icon.ico"
!define MUI_ABORTWARNING
!define MUI_FINISHPAGE_RUN "$INSTDIR\thetracker.exe"
!define MUI_FINISHPAGE_RUN_TEXT "Open ${PRODUCT}"

!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

Var Relaunch
Var Upgrade

Function .onInit
  ${GetParameters} $0
  ClearErrors
  ${GetOptions} $0 "/P" $1
  ${IfNot} ${Errors}
    SetSilent silent
  ${EndIf}
  StrCpy $Relaunch "0"
  ClearErrors
  ${GetOptions} $0 "/R" $1
  ${IfNot} ${Errors}
    StrCpy $Relaunch "1"
  ${EndIf}
  ClearErrors
  ${GetOptions} $0 "/M" $1
  ${IfNot} ${Errors}
    StrCpy $Relaunch "2"
  ${EndIf}

  ; Always the per-user location, whatever an older install recorded.
  StrCpy $INSTDIR "$LOCALAPPDATA\${PRODUCT}"
  StrCpy $Upgrade "0"
  ${If} ${FileExists} "$INSTDIR\thetracker.exe"
    StrCpy $Upgrade "1"
  ${EndIf}

  ; The app's window is drawn by Microsoft's WebView2 runtime. It ships with
  ; Windows 11 and almost every Windows 10; say so plainly if it is missing
  ; rather than installing an app that opens to nothing.
  ReadRegStr $2 HKLM "${WEBVIEW2KEY}" "pv"
  ${If} $2 == ""
    ReadRegStr $2 HKCU "Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
  ${EndIf}
  ${If} $2 == ""
  ${AndIfNot} ${Silent}
    MessageBox MB_YESNO|MB_ICONEXCLAMATION "${PRODUCT} needs Microsoft's WebView2 runtime, which isn't installed on this PC.$\n$\nOpen Microsoft's download page now? Install it, then run this setup again." IDNO +2
      ExecShell "open" "https://developer.microsoft.com/microsoft-edge/webview2/#download"
    Abort
  ${EndIf}
FunctionEnd

Section "Install"
  ; A running copy holds the exe open; close it first. On an update the app
  ; has already asked to quit, so this is usually a no-op.
  nsExec::Exec 'taskkill /IM thetracker.exe'
  Pop $0
  Sleep 1200
  nsExec::Exec 'taskkill /F /IM thetracker.exe'
  Pop $0
  Sleep 400

  SetOutPath "$INSTDIR"
  SetOverwrite on
  ClearErrors
  File "/oname=thetracker.exe" "${EXE}"
  ${If} ${Errors}
    ${IfNot} ${Silent}
      MessageBox MB_OK|MB_ICONSTOP "${PRODUCT} is still running and couldn't be replaced. Quit it from the tray icon, then run setup again."
    ${EndIf}
    Abort
  ${EndIf}

  ; Left behind by releases before 1.0, which needed it beside the exe.
  Delete "$INSTDIR\WebView2Loader.dll"

  WriteUninstaller "$INSTDIR\uninstall.exe"

  CreateShortcut "$SMPROGRAMS\${PRODUCT}.lnk" "$INSTDIR\thetracker.exe"
  ; A desktop shortcut on a first install only: on an upgrade, its absence
  ; means the player deleted it.
  ${If} $Upgrade == "0"
    CreateShortcut "$DESKTOP\${PRODUCT}.lnk" "$INSTDIR\thetracker.exe"
  ${EndIf}

  WriteRegStr HKCU "${UNINSTKEY}" "DisplayName" "${PRODUCT}"
  WriteRegStr HKCU "${UNINSTKEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "${UNINSTKEY}" "Publisher" "lilcham1"
  WriteRegStr HKCU "${UNINSTKEY}" "InstallLocation" "$\"$INSTDIR$\""
  WriteRegStr HKCU "${UNINSTKEY}" "DisplayIcon" "$\"$INSTDIR\thetracker.exe$\""
  WriteRegStr HKCU "${UNINSTKEY}" "UninstallString" "$\"$INSTDIR\uninstall.exe$\""
  WriteRegDWORD HKCU "${UNINSTKEY}" "NoModify" 1
  WriteRegDWORD HKCU "${UNINSTKEY}" "NoRepair" 1
  ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
  IntFmt $0 "0x%08X" $0
  WriteRegDWORD HKCU "${UNINSTKEY}" "EstimatedSize" "$0"

  ${If} $Relaunch == "1"
    Exec '"$INSTDIR\thetracker.exe"'
  ${ElseIf} $Relaunch == "2"
    Exec '"$INSTDIR\thetracker.exe" --minimized'
  ${EndIf}
SectionEnd

Section "Uninstall"
  nsExec::Exec 'taskkill /F /IM thetracker.exe'
  Pop $0
  Sleep 800

  Delete "$INSTDIR\thetracker.exe"
  Delete "$INSTDIR\WebView2Loader.dll"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR"
  Delete "$SMPROGRAMS\${PRODUCT}.lnk"
  Delete "$DESKTOP\${PRODUCT}.lnk"

  ; The start-with-Windows entry, if it was switched on.
  DeleteRegValue HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "${PRODUCT}"
  DeleteRegKey HKCU "${UNINSTKEY}"

  ; Match history and settings in %APPDATA%\TheTracker are deliberately
  ; left: uninstalling the app is not a request to delete your records.
SectionEnd
