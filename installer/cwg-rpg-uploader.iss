#ifndef AppVersion
  #error AppVersion must be supplied by scripts/build-installer.ps1
#endif
#ifndef BuildDir
  #error BuildDir must point to the completed application build
#endif

[Setup]
AppId=cwg-rpg-community.uploader
AppName=CWG RPG Uploader
AppVersion={#AppVersion}
AppPublisher=CWG RPG Community
AppPublisherURL=https://github.com/cwg-rpg-community/uploader
AppSupportURL=https://github.com/cwg-rpg-community/uploader/issues
AppUpdatesURL=https://github.com/cwg-rpg-community/uploader/releases
DefaultDirName={localappdata}\Programs\CWGRPGUploader
DefaultGroupName=CWG RPG Uploader
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0
DisableProgramGroupPage=no
AllowNoIcons=yes
DisableDirPage=yes
LicenseFile={#BuildDir}\LICENSE
SetupIconFile=..\internal\windows\icon.ico
UninstallDisplayIcon={app}\cwg-rpg-uploader.exe
; Let Restart Manager close the installed app before replacing its executable.
; Do not force termination or let Restart Manager bypass the final launch choice.
CloseApplications=yes
CloseApplicationsFilter=cwg-rpg-uploader.exe
RestartApplications=no
Compression=lzma2
SolidCompression=yes
WizardStyle=modern

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: unchecked
Name: "startup"; Description: "Start CWG RPG Uploader when I sign in"; GroupDescription: "Windows startup:"; Flags: unchecked

[Files]
Source: "{#BuildDir}\cwg-rpg-uploader.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#BuildDir}\README.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#BuildDir}\CONFIGURATION.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#BuildDir}\VERIFYING.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#BuildDir}\LICENSE"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#BuildDir}\THIRD_PARTY_NOTICES.md"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{group}\CWG RPG Uploader"; Filename: "{app}\cwg-rpg-uploader.exe"
Name: "{group}\Read me"; Filename: "{app}\README.md"
Name: "{autodesktop}\CWG RPG Uploader"; Filename: "{app}\cwg-rpg-uploader.exe"; Tasks: desktopicon

[Registry]
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "CWGRPGUploader"; ValueData: """{app}\cwg-rpg-uploader.exe"" --windows-sign-in-launch"; Tasks: startup

[Run]
Filename: "{app}\cwg-rpg-uploader.exe"; WorkingDir: "{app}"; Description: "Launch CWG RPG Uploader (automatic uploads follow your settings)"; Flags: nowait postinstall skipifsilent runasoriginaluser

; Configuration, upload history, logs, and saves are never installed/overwritten.
; Sign-in startup uses the same registry entry as the app's tray.

[Code]
const
  StartupKey = 'Software\Microsoft\Windows\CurrentVersion\Run';
  StartupValue = 'CWGRPGUploader';

var
  StartupChoiceInitialized: Boolean;

function OwnsStartupEntry: Boolean;
var
  Command, Executable: String;
begin
  Executable := '"' + ExpandConstant('{app}\cwg-rpg-uploader.exe') + '"';
  Result := RegQueryStringValue(HKCU, StartupKey, StartupValue, Command) and
    ((CompareText(Trim(Command), Executable) = 0) or
     (CompareText(Trim(Command), Executable + ' --windows-sign-in-launch') = 0));
end;

procedure RemoveOwnedStartupEntry;
begin
  // Leave a registration belonging to a separate portable copy alone.
  if OwnsStartupEntry then
    if not RegDeleteValue(HKCU, StartupKey, StartupValue) then
      RaiseException('Could not remove the sign-in startup entry.');
end;

procedure CurPageChanged(CurPageID: Integer);
var
  I: Integer;
begin
  // Tasks are populated when this page is shown, not in InitializeWizard.
  // Initialize only once so Back/Next navigation preserves the user's choice.
  if (CurPageID <> wpSelectTasks) or StartupChoiceInitialized then
    Exit;
  StartupChoiceInitialized := True;

  // Explicit command-line task selections take precedence over live defaults.
  for I := 1 to ParamCount do
    if (Pos('/TASKS=', Uppercase(ParamStr(I))) = 1) or
       (Pos('/MERGETASKS=', Uppercase(ParamStr(I))) = 1) then
      Exit;

  // The registry, not a previous installer's remembered tasks, is authoritative.
  if OwnsStartupEntry then
    WizardSelectTasks('startup')
  else
    WizardSelectTasks('!startup');
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if (CurStep = ssPostInstall) and not WizardIsTaskSelected('startup') then
    RemoveOwnedStartupEntry;
end;

function InitializeUninstall: Boolean;
begin
  // Removing AppMutex lets setup reach Restart Manager. Keep uninstall's safe
  // quit-first guard rather than deleting files beneath a running instance.
  Result := not CheckForMutexes('Local\CWGRPGUploader');
  if not Result then
    SuppressibleMsgBox('Please quit CWG RPG Uploader before uninstalling.', mbError, MB_OK, IDOK);
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if CurUninstallStep = usUninstall then
    RemoveOwnedStartupEntry;
end;
