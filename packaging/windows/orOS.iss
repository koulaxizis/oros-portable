; orOS setup for Windows (Inno Setup 6).
; Built in CI from out/installers/windows/ (see scripts/build-installers.sh):
;   iscc /DAppVer=0.2.0 /DNumVer=0.2.0 [/DDriveVer=0.6.0] orOS.iss
; With DriveVer, drive\orOS-Drive-Setup.exe (the latest orOS Drive release,
; checked against its SHA256SUMS.txt in CI) goes inside, and the setup
; offers to install orOS Drive too (Chris 2026-10-28, "one installer").
; Drive stays its own program: its own folder, uninstaller and updates.
; Installs for the current user only, so it needs no administrator rights,
; and puts the "installed" marker next to the launcher, which keeps the
; browser data and app updates in %LOCALAPPDATA%\orOS.

#ifndef AppVer
  #define AppVer "0.0.0"
#endif
#ifndef NumVer
  #define NumVer "0.0.0"
#endif

[Setup]
AppId={{4AABB5BE-C57D-4A02-891C-400A48085E33}
AppName=orOS
AppVersion={#AppVer}
AppVerName=orOS {#AppVer}
AppPublisher=orOS
AppPublisherURL=https://useoros.online
AppSupportURL=https://github.com/koulaxizis/oros-portable/issues
AppUpdatesURL=https://github.com/koulaxizis/oros-portable/releases
VersionInfoVersion={#NumVer}
DefaultDirName={localappdata}\Programs\orOS
DisableDirPage=yes
DisableProgramGroupPage=yes
DisableReadyPage=yes
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0
OutputDir=.
OutputBaseFilename=orOS-Setup-{#AppVer}
SetupIconFile=oros.ico
UninstallDisplayIcon={app}\orOS.exe
UninstallDisplayName=orOS
LicenseFile=files\LICENSES\orOS-Portable-MIT.txt
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
CloseApplications=yes

[Languages]
Name: "en"; MessagesFile: "compiler:Default.isl"
Name: "el"; MessagesFile: "Greek.isl"

[CustomMessages]
en.DeleteData=Also delete your orOS data on this computer (notes, files, settings)?%n%nChoose No to keep it for a later install. Data synced to your Dropbox is not affected.
el.DeleteData=Να σβηστούν και τα δεδομένα του orOS σε αυτόν τον υπολογιστή (σημειώσεις, αρχεία, ρυθμίσεις);%n%nΕπιλέξτε Όχι για να μείνουν για μελλοντική εγκατάσταση. Ό,τι έχει συγχρονιστεί στο Dropbox σας δεν επηρεάζεται.

en.AutoUpdate=Keep orOS up to date automatically (checks GitHub at most every 6 hours)
el.AutoUpdate=Αυτόματη ενημέρωση του orOS (έλεγχος στο GitHub το πολύ κάθε 6 ώρες)

en.AutoStart=Start orOS in the tray with Windows (click it to add a task or search)
el.AutoStart=Εκκίνηση του orOS στο tray μαζί με τα Windows (κλικ για προσθήκη εργασίας ή αναζήτηση)

#ifdef DriveVer
en.Drive=Also install orOS Drive (keeps folders of this computer in sync with orOS Files)
el.Drive=Εγκατάσταση και του orOS Drive (συγχρονίζει φακέλους του υπολογιστή με τα Αρχεία του orOS)

en.DriveGroup=orOS Drive {#DriveVer}:
el.DriveGroup=orOS Drive {#DriveVer}:

en.DriveInstalling=Installing orOS Drive...
el.DriveInstalling=Εγκατάσταση του orOS Drive...
#endif

en.QuickName=orOS Add or search
el.QuickName=orOS Προσθήκη ή αναζήτηση

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"
Name: "autoupdate"; Description: "{cm:AutoUpdate}"
; Off by default (Quick window plan, 2026-10-27); the tray menu turns it on or off too.
Name: "autostart"; Description: "{cm:AutoStart}"; Flags: unchecked
#ifdef DriveVer
; Offered only while orOS Drive is not installed: a silent update of orOS
; never runs the bundled Drive setup over a newer Drive.
Name: "drive"; Description: "{cm:Drive}"; GroupDescription: "{cm:DriveGroup}"; Flags: unchecked; Check: not DriveInstalled
#endif

[Registry]
; The same value as the tray's "Start with the computer" (launcher autostart_windows.go).
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "orOS"; ValueData: """{app}\orOS.exe"" tray"; Tasks: autostart; Flags: uninsdeletevalue

[InstallDelete]
; A new browser or app version replaces the old folders completely.
Type: filesandordirs; Name: "{app}\browser"
Type: filesandordirs; Name: "{app}\app"
Type: filesandordirs; Name: "{app}\bridge"

[Files]
Source: "files\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs
#ifdef DriveVer
Source: "drive\orOS-Drive-Setup.exe"; DestDir: "{tmp}"; Flags: deleteafterinstall; Tasks: drive
#endif

[Icons]
Name: "{userprograms}\orOS"; Filename: "{app}\orOS.exe"; WorkingDir: "{app}"
Name: "{userprograms}\Update orOS"; Filename: "{app}\orOS.exe"; Parameters: "update"; WorkingDir: "{app}"
Name: "{userprograms}\{cm:QuickName}"; Filename: "{app}\orOS.exe"; Parameters: "quick"; WorkingDir: "{app}"
Name: "{userdesktop}\orOS"; Filename: "{app}\orOS.exe"; WorkingDir: "{app}"; Tasks: desktopicon

[Run]
#ifdef DriveVer
; orOS Drive's own setup, silent, per user (its defaults: start with Windows).
Filename: "{tmp}\orOS-Drive-Setup.exe"; Parameters: "/VERYSILENT /SUPPRESSMSGBOXES /NORESTART /CURRENTUSER"; StatusMsg: "{cm:DriveInstalling}"; Flags: waituntilterminated; Tasks: drive
; Its settings page asks for the Dropbox or WebDAV sign-in.
Filename: "{localappdata}\Programs\orOS Drive\orOS Drive.exe"; Flags: nowait skipifsilent skipifdoesntexist; Tasks: drive
#endif
Filename: "{app}\orOS.exe"; Description: "{cm:LaunchProgram,orOS}"; Flags: nowait postinstall skipifsilent
; An automatic update runs this setup silently with /RELAUNCH=1: open orOS again.
Filename: "{app}\orOS.exe"; Flags: nowait; Check: Relaunch
; The tray handed over to this setup (/RELAUNCH=tray): start the tray again.
Filename: "{app}\orOS.exe"; Parameters: "tray"; Flags: nowait; Check: RelaunchTray
; Chosen here: the tray starts now too (a second one stops at its lock).
Filename: "{app}\orOS.exe"; Parameters: "tray"; Flags: nowait skipifsilent; Tasks: autostart

[UninstallRun]
; The tray keeps orOS.exe running; stop it so its files can go.
Filename: "{sys}\taskkill.exe"; Parameters: "/F /IM orOS.exe"; Flags: runhidden; RunOnceId: "StopOrOS"

[Code]
// The launcher skips its automatic updates while this file exists.
procedure CurStepChanged(CurStep: TSetupStep);
var
  Off: String;
begin
  if CurStep = ssPostInstall then
  begin
    Off := ExpandConstant('{localappdata}\orOS\no-auto-update');
    if WizardIsTaskSelected('autoupdate') then
      DeleteFile(Off)
    else
    begin
      ForceDirectories(ExtractFileDir(Off));
      SaveStringToFile(Off, 'Turned off in the orOS setup.' + #13#10, False);
    end;
  end;
end;

// orOS Drive's own setup registers this uninstall key (its AppId).
function DriveInstalled(): Boolean;
begin
  Result := RegKeyExists(HKEY_CURRENT_USER, 'Software\Microsoft\Windows\CurrentVersion\Uninstall\{9E3C5B71-4D2A-4F0E-8B6C-2A7D1E5F9C40}_is1') or
    FileExists(ExpandConstant('{localappdata}\Programs\orOS Drive\orOS Drive.exe'));
end;

function Relaunch(): Boolean;
begin
  Result := ExpandConstant('{param:RELAUNCH|0}') = '1';
end;

function RelaunchTray(): Boolean;
begin
  Result := ExpandConstant('{param:RELAUNCH|0}') = 'tray';
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  Data: String;
begin
  // "Start with the computer" may have been turned on in the tray menu.
  if CurUninstallStep = usUninstall then
    RegDeleteValue(HKEY_CURRENT_USER, 'Software\Microsoft\Windows\CurrentVersion\Run', 'orOS');
  if (CurUninstallStep = usPostUninstall) and not UninstallSilent() then
  begin
    Data := ExpandConstant('{localappdata}\orOS');
    if DirExists(Data) and
       (MsgBox(CustomMessage('DeleteData'), mbConfirmation, MB_YESNO or MB_DEFBUTTON2) = IDYES) then
      DelTree(Data, True, True, True);
  end;
end;
