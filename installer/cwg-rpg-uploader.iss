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
DisableProgramGroupPage=yes
DisableDirPage=yes
LicenseFile={#BuildDir}\LICENSE
UninstallDisplayIcon={app}\cwg-rpg-uploader.exe
AppMutex=Local\CWGRPGUploader
CloseApplications=no
RestartApplications=no
Compression=lzma2
SolidCompression=yes
WizardStyle=modern

[Files]
Source: "{#BuildDir}\cwg-rpg-uploader.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#BuildDir}\README.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#BuildDir}\VERIFYING.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#BuildDir}\LICENSE"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#BuildDir}\THIRD_PARTY_NOTICES.md"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{group}\CWG RPG Uploader"; Filename: "{app}\cwg-rpg-uploader.exe"
Name: "{group}\Read me"; Filename: "{app}\README.md"

; Intentionally no [Run] entries: installation must not start live uploads.
; Configuration, upload history, logs, saves, and startup registration are never
; installed or overwritten. The app owns sign-in registration through its tray.

[Code]
procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  Command: String;
  StartupKey: String;
begin
  if CurUninstallStep = usUninstall then
  begin
    StartupKey := 'Software\Microsoft\Windows\CurrentVersion\Run';
    if RegQueryStringValue(HKCU, StartupKey, 'CWGRPGUploader', Command) then
    begin
      // Do not delete a registration belonging to a separate portable copy.
      if CompareText(Trim(Command), '"' + ExpandConstant('{app}\cwg-rpg-uploader.exe') + '"') = 0 then
        RegDeleteValue(HKCU, StartupKey, 'CWGRPGUploader');
    end;
  end;
end;
