; Windows installer for go-upnp-player (Inno Setup 6). Built at design time, never in CI:
;
;   ISCC /DAppVersion=1.1.14 installer\go-upnp-player.iss
;
; from the repository root, after bin\go-upnp-player.exe has been built for the release.
; The result is bin\go-upnp-player-setup.exe.
;
; Per-user install: no administrator rights, no UAC prompt. The program goes to
; %LocalAppData%\Programs\go-upnp-player, shortcuts to the Start menu and (by default) the
; desktop, and an entry to Settings -> Apps with an uninstaller. Settings, playlist and the
; library cache stay where the player keeps them (%AppData% and %LocalAppData%\go-upnp-player);
; the uninstaller asks whether to delete them too.

#ifndef AppVersion
  #error Pass the version: ISCC /DAppVersion=X.Y.Z installer\go-upnp-player.iss
#endif

#define AppName "go-upnp-player"
#define AppExe "go-upnp-player.exe"

[Setup]
; the AppId identifies the installation for updates and uninstall: never change it
AppId={{6F3B8E2A-4C1D-4E7B-9A55-2D8C0F1B7E43}
AppName={#AppName}
AppVersion={#AppVersion}
AppVerName={#AppName} {#AppVersion}
AppPublisher=panedrone
AppPublisherURL=https://github.com/panedrone/go-upnp-player
AppSupportURL=https://github.com/panedrone/go-upnp-player/issues
AppUpdatesURL=https://github.com/panedrone/go-upnp-player/releases
DefaultDirName={autopf}\{#AppName}
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
SourceDir=..
OutputDir=bin
OutputBaseFilename=go-upnp-player-setup
SetupIconFile=assets\icon.ico
UninstallDisplayIcon={app}\icon.ico
UninstallDisplayName={#AppName}
WizardStyle=modern
Compression=lzma2/max
SolidCompression=yes
; a running player is stopped by the [Code] below, through its own Quit: it pauses the
; renderer and saves the position first (ADR-002, ADR-012). The Restart Manager would just
; kill it.
CloseApplications=no

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"

[Files]
Source: "bin\{#AppExe}"; DestDir: "{app}"; Flags: ignoreversion
Source: "assets\icon.ico"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\{#AppName}"; Filename: "{app}\{#AppExe}"; IconFilename: "{app}\icon.ico"
Name: "{autodesktop}\{#AppName}"; Filename: "{app}\{#AppExe}"; IconFilename: "{app}\icon.ico"; Tasks: desktopicon

[Run]
Filename: "{app}\{#AppExe}"; Description: "{cm:LaunchProgram,{#AppName}}"; Flags: nowait postinstall skipifsilent

[Code]
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
    'Keep them to find everything in place after installing again.',
    mbConfirmation, MB_YESNO or MB_DEFBUTTON2, IDNO) = IDYES then
  begin
    DelTree(ExpandConstant('{userappdata}\go-upnp-player'), True, True, True);
    DelTree(ExpandConstant('{localappdata}\go-upnp-player'), True, True, True);
  end;
end;
