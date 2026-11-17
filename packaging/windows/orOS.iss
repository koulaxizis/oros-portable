; orOS setup for Windows (Inno Setup 6).
; Built in CI from out/installers/windows/ (see scripts/build-installers.sh):
;   iscc /DAppVer=0.2.0 /DNumVer=0.2.0 [/DDriveVer=0.6.0] orOS.iss
; With DriveVer, drive\orOS-Drive-Setup.exe (the latest orOS Drive release,
; checked against its SHA256SUMS.txt in CI) goes inside, and the setup
; installs orOS Drive too, ticked (Chris 2026-10-28 "one installer",
; 2026-11-17 "one package", ticked by default). Drive stays its own program
; (its own folder, settings, sign-in and uninstaller) but is managed by
; orOS: /MANAGEDBY makes it show in the orOS tray icon and leaves its start
; with Windows to orOS (launcher drive.go). A Drive already installed is
; updated when the one inside is newer, never made older; an automatic
; (silent) update of orOS never adds Drive by itself. Uninstalling orOS
; asks whether orOS Drive goes too; kept, it shows its own icon again.
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
; Tell Explorer to refresh its icon cache after install and uninstall
; (SHChangeNotify SHCNE_ASSOCCHANGED): the exe keeps its path, so without
; it Start menu, desktop and taskbar can keep showing the old logo.
ChangesAssociations=yes

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

en.DriveUninstall=Also uninstall orOS Drive?%n%nChoose No to keep it: it then shows its own icon in the tray and keeps your folders in sync. Your folders and the files in them are not touched either way.
el.DriveUninstall=Να απεγκατασταθεί και το orOS Drive;%n%nΕπιλέξτε Όχι για να μείνει: θα δείχνει τότε το δικό του εικονίδιο στο tray και θα συνεχίσει να συγχρονίζει τους φακέλους σας. Οι φάκελοι και τα αρχεία τους δεν αλλάζουν σε καμία περίπτωση.
#endif

en.QuickName=orOS Add or search
el.QuickName=orOS Προσθήκη ή αναζήτηση

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"
Name: "autoupdate"; Description: "{cm:AutoUpdate}"
; Off by default (Quick window plan, 2026-10-27); the tray menu turns it on or off too.
Name: "autostart"; Description: "{cm:AutoStart}"; Flags: unchecked
#ifdef DriveVer
; Offered only while orOS Drive is not installed (an installed one is
; updated by DriveUpdate, below), and not in a silent update of an
; installed orOS: an automatic update never adds Drive by itself.
Name: "drive"; Description: "{cm:Drive}"; GroupDescription: "{cm:DriveGroup}"; Check: OfferDrive
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
Source: "drive\orOS-Drive-Setup.exe"; DestDir: "{tmp}"; Flags: deleteafterinstall; Check: DriveSetupNeeded
#endif

[Icons]
Name: "{userprograms}\orOS"; Filename: "{app}\orOS.exe"; WorkingDir: "{app}"
Name: "{userprograms}\Update orOS"; Filename: "{app}\orOS.exe"; Parameters: "update"; WorkingDir: "{app}"
Name: "{userprograms}\{cm:QuickName}"; Filename: "{app}\orOS.exe"; Parameters: "quick"; WorkingDir: "{app}"
Name: "{userdesktop}\orOS"; Filename: "{app}\orOS.exe"; WorkingDir: "{app}"; Tasks: desktopicon

[Run]
#ifdef DriveVer
; orOS Drive's own setup, silent, per user, managed by this orOS: no tray
; icon and no start with Windows of its own (oros-drive /MANAGEDBY).
Filename: "{tmp}\orOS-Drive-Setup.exe"; Parameters: "/VERYSILENT /SUPPRESSMSGBOXES /NORESTART /CURRENTUSER ""/MANAGEDBY={app}\orOS.exe"""; StatusMsg: "{cm:DriveInstalling}"; Flags: waituntilterminated; Check: DriveSetupNeeded
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
; The local AI this orOS started (launcher/ai.go); an Ollama the person
; installed is not touched.
Filename: "{app}\orOS.exe"; Parameters: "ai --stop"; Flags: runhidden waituntilterminated; RunOnceId: "StopAI"
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

// An orOS is installed already (this setup updates it).
function OrOSInstalled(): Boolean;
begin
  Result := RegKeyExists(HKEY_CURRENT_USER, 'Software\Microsoft\Windows\CurrentVersion\Uninstall\{4AABB5BE-C57D-4A02-891C-400A48085E33}_is1');
end;

function OfferDrive(): Boolean;
begin
  Result := (not DriveInstalled()) and not (WizardSilent() and OrOSInstalled());
end;

#ifdef DriveVer
// Compares "1.2.3" versions part by part: <0, 0 or >0.
function CompareVer(A, B: String): Integer;
var
  PA, PB: String;
  NA, NB, I: Integer;
begin
  Result := 0;
  for I := 1 to 4 do
  begin
    PA := A; PB := B;
    if Pos('.', A) > 0 then begin PA := Copy(A, 1, Pos('.', A) - 1); A := Copy(A, Pos('.', A) + 1, Length(A)); end else A := '';
    if Pos('.', B) > 0 then begin PB := Copy(B, 1, Pos('.', B) - 1); B := Copy(B, Pos('.', B) + 1, Length(B)); end else B := '';
    NA := StrToIntDef(PA, 0); NB := StrToIntDef(PB, 0);
    if NA <> NB then
    begin
      Result := NA - NB;
      Exit;
    end;
  end;
end;

// The installed orOS Drive is older than the one inside: update it (and
// hand it to orOS on the way). A newer or equal one is left alone.
function DriveUpdate(): Boolean;
var
  Have: String;
begin
  Result := False;
  if not DriveInstalled() then
    Exit;
  if not RegQueryStringValue(HKEY_CURRENT_USER, 'Software\Microsoft\Windows\CurrentVersion\Uninstall\{9E3C5B71-4D2A-4F0E-8B6C-2A7D1E5F9C40}_is1', 'DisplayVersion', Have) then
    Exit;
  Result := CompareVer('{#DriveVer}', Have) > 0;
end;

function DriveSetupNeeded(): Boolean;
begin
  Result := WizardIsTaskSelected('drive') or DriveUpdate();
end;
#endif

function Relaunch(): Boolean;
begin
  Result := ExpandConstant('{param:RELAUNCH|0}') = '1';
end;

function RelaunchTray(): Boolean;
begin
  Result := ExpandConstant('{param:RELAUNCH|0}') = 'tray';
end;

// orOS Drive, when orOS goes: uninstalled on a yes (its own uninstaller,
// which asks about its settings and never touches the folders), else
// handed back so it shows its own icon and starts with Windows again.
procedure LetGoOfDrive();
var
  Code: Integer;
  Dir: String;
begin
  Dir := ExpandConstant('{localappdata}\Programs\orOS Drive');
  if not FileExists(Dir + '\orosdrive.exe') then
    Exit;
  if (not UninstallSilent()) and FileExists(Dir + '\unins000.exe') and
     (MsgBox(CustomMessage('DriveUninstall'), mbConfirmation, MB_YESNO or MB_DEFBUTTON2) = IDYES) then
    Exec(Dir + '\unins000.exe', '', '', SW_SHOW, ewWaitUntilTerminated, Code)
  else
    Exec(Dir + '\orosdrive.exe', 'manage off', '', SW_HIDE, ewWaitUntilTerminated, Code);
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  Data: String;
begin
  // "Start with the computer" may have been turned on in the tray menu.
  if CurUninstallStep = usUninstall then
  begin
    RegDeleteValue(HKEY_CURRENT_USER, 'Software\Microsoft\Windows\CurrentVersion\Run', 'orOS');
    LetGoOfDrive();
  end;
  if (CurUninstallStep = usPostUninstall) and not UninstallSilent() then
  begin
    Data := ExpandConstant('{localappdata}\orOS');
    if DirExists(Data) and
       (MsgBox(CustomMessage('DeleteData'), mbConfirmation, MB_YESNO or MB_DEFBUTTON2) = IDYES) then
      DelTree(Data, True, True, True);
  end;
end;
