#ifndef AppVersion
  #error AppVersion must be supplied by scripts/build-installer.ps1
#endif
#ifndef BuildDir
  #error BuildDir must point to the completed application build
#endif

[Setup]
AppId=cwg-rpg-community.uploader
AppName=CWG2 Uploader
AppVersion={#AppVersion}
AppPublisher=CWG RPG Community
AppPublisherURL=https://github.com/cwg-rpg-community/uploader
AppSupportURL=https://github.com/cwg-rpg-community/uploader/issues
AppUpdatesURL=https://github.com/cwg-rpg-community/uploader/releases
DefaultDirName={localappdata}\Programs\CWG2Uploader
DefaultGroupName=CWG2 Uploader
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0
DisableProgramGroupPage=yes
DisableDirPage=yes
LicenseFile={#BuildDir}\LICENSE
UninstallDisplayIcon={app}\cwg2-uploader.exe
AppMutex=Local\CWG2Uploader
CloseApplications=no
RestartApplications=no
Compression=lzma2
SolidCompression=yes
WizardStyle=modern

[Files]
Source: "{#BuildDir}\cwg2-uploader.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#BuildDir}\README.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#BuildDir}\VERIFYING.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#BuildDir}\LICENSE"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#BuildDir}\THIRD_PARTY_NOTICES.md"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{group}\CWG2 Uploader"; Filename: "{app}\cwg2-uploader.exe"
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
    if RegQueryStringValue(HKCU, StartupKey, 'CWG2Uploader', Command) then
    begin
      // Do not delete a registration belonging to a separate portable copy.
      if CompareText(Trim(Command), '"' + ExpandConstant('{app}\cwg2-uploader.exe') + '"') = 0 then
        RegDeleteValue(HKCU, StartupKey, 'CWG2Uploader');
    end;
  end;
end;
