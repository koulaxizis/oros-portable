; orOS setup for Windows (Inno Setup 6).
; Built in CI from out/installers/windows/ (see scripts/build-installers.sh):
;   iscc /DAppVer=0.2.0 /DNumVer=0.2.0 orOS.iss
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

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"

[InstallDelete]
; A new browser or app version replaces the old folders completely.
Type: filesandordirs; Name: "{app}\browser"
Type: filesandordirs; Name: "{app}\app"

[Files]
Source: "files\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
Name: "{userprograms}\orOS"; Filename: "{app}\orOS.exe"; WorkingDir: "{app}"
Name: "{userprograms}\Update orOS"; Filename: "{app}\orOS.exe"; Parameters: "update"; WorkingDir: "{app}"
Name: "{userdesktop}\orOS"; Filename: "{app}\orOS.exe"; WorkingDir: "{app}"; Tasks: desktopicon

[Run]
Filename: "{app}\orOS.exe"; Description: "{cm:LaunchProgram,orOS}"; Flags: nowait postinstall skipifsilent
; An automatic update runs this setup silently with /RELAUNCH=1: open orOS again.
Filename: "{app}\orOS.exe"; Flags: nowait; Check: Relaunch

[Code]
function Relaunch(): Boolean;
begin
  Result := ExpandConstant('{param:RELAUNCH|0}') = '1';
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  Data: String;
begin
  if (CurUninstallStep = usPostUninstall) and not UninstallSilent() then
  begin
    Data := ExpandConstant('{localappdata}\orOS');
    if DirExists(Data) and
       (MsgBox(CustomMessage('DeleteData'), mbConfirmation, MB_YESNO or MB_DEFBUTTON2) = IDYES) then
      DelTree(Data, True, True, True);
  end;
end;
