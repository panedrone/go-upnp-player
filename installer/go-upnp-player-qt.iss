; Windows installer for go-upnp-player with the Qt window (Inno Setup 6, ADR-027). Built at
; design time, never in CI:
;
;   ISCC /DAppVersion=1.1.27 installer\go-upnp-player-qt.iss
;
; from the repository root, after bin\qt has been filled for the release: the Qt build of
; bin\qt\go-upnp-player-qt.exe, windeployqt6 run on it (the Qt DLLs and plugins next to it)
; and assets\icon.ico copied there. The result is bin\go-upnp-player-qt-setup.exe.
;
; A program of its own beside the web one (installer\go-upnp-player.iss): its own AppId,
; folder and shortcuts. Per-user install: no administrator rights. The two share the port, so
; one runs at a time, and the settings, the playlist and the library cache, which stay where
; the player keeps them (%AppData% and %LocalAppData%\go-upnp-player).

#ifndef AppVersion
  #error Pass the version: ISCC /DAppVersion=X.Y.Z installer\go-upnp-player-qt.iss
#endif

#define AppName "go-upnp-player Qt"
#define AppExe "go-upnp-player-qt.exe"

[Setup]
; the AppId identifies the installation for updates and uninstall: never change it
AppId={{9C2E4A71-5B3F-4D8E-A6C0-7E1F3B9D2A58}
AppName={#AppName}
AppVersion={#AppVersion}
AppVerName={#AppName} {#AppVersion}
AppPublisher=panedrone
AppPublisherURL=https://github.com/panedrone/go-upnp-player
AppSupportURL=https://github.com/panedrone/go-upnp-player/issues
AppUpdatesURL=https://github.com/panedrone/go-upnp-player/releases
DefaultDirName={autopf}\go-upnp-player-qt
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
SourceDir=..
OutputDir=bin
OutputBaseFilename=go-upnp-player-qt-setup
SetupIconFile=assets\icon.ico
UninstallDisplayIcon={app}\icon.ico
UninstallDisplayName={#AppName}
WizardStyle=modern
Compression=lzma2/max
SolidCompression=yes
; a running player is stopped by the [Code] below, through its own Quit: it stops the
; renderer and saves the position first. The Restart Manager would just kill it.
CloseApplications=no

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"

[Files]
; the exe with the Qt DLLs and plugins windeployqt put next to it, and the icon
Source: "bin\qt\*"; Excludes: "*.old.exe"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
Name: "{autoprograms}\{#AppName}"; Filename: "{app}\{#AppExe}"; IconFilename: "{app}\icon.ico"
Name: "{autodesktop}\{#AppName}"; Filename: "{app}\{#AppExe}"; IconFilename: "{app}\icon.ico"; Tasks: desktopicon

[Run]
Filename: "{app}\{#AppExe}"; Description: "{cm:LaunchProgram,{#AppName}}"; Flags: nowait postinstall skipifsilent

[Code]
// The same as installer\go-upnp-player.iss: either player answers on the default port.
const
  PlayerURL = 'http://127.0.0.1:8090/api/';

// One HTTP request to a player on the default port; false when nothing answers.
function PlayerRequest(Method, Path: String): Boolean;
var
  Req: Variant;
begin
  Result := False;
  try
    Req := CreateOleObject('WinHttp.WinHttpRequest.5.1');
    Req.SetTimeouts(1000, 1000, 1000, 3000);
    Req.Open(Method, PlayerURL + Path, False);
    Req.Send('');
    Result := Req.Status = 200;
  except
  end;
end;

// Stops a running player the way its Quit button does and waits until it has exited,
// so its exe can be replaced or removed.
procedure StopRunningPlayer;
var
  I: Integer;
begin
  if not PlayerRequest('GET', 'version') then
    exit;
  PlayerRequest('POST', 'shutdown');
  for I := 1 to 40 do
  begin
    Sleep(250);
    if not PlayerRequest('GET', 'version') then
    begin
      Sleep(500); // the process lets go of its exe a moment after the port
      exit;
    end;
  end;
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
begin
  StopRunningPlayer;
  Result := '';
end;

function InitializeUninstall(): Boolean;
begin
  StopRunningPlayer;
  Result := True;
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if CurUninstallStep <> usPostUninstall then
    exit;
  if UninstallSilent then
    exit; // a silent uninstall keeps the user's data
  if SuppressibleMsgBox('Also delete the settings, the playlist and the library cache?' + #13#10#13#10 +
    'They are shared with go-upnp-player (the web one): deleting them resets it too. ' +
    'Keep them to find everything in place after installing again.',
    mbConfirmation, MB_YESNO or MB_DEFBUTTON2, IDNO) = IDYES then
  begin
    DelTree(ExpandConstant('{userappdata}\go-upnp-player'), True, True, True);
    DelTree(ExpandConstant('{localappdata}\go-upnp-player'), True, True, True);
  end;
end;
