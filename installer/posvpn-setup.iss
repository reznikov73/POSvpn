; POSvpn — установщик.
; Компиляция: ISCC.exe posvpn-setup.iss   (или .\release.ps1 из корня проекта)
; Версия хранится в version.txt.

#define MyAppName "POSvpn"
#define MyAppVersion Trim(FileRead(FileOpen(AddBackslash(SourcePath) + "version.txt")))
#define MyAppPublisher "reznikov73"
#define MyAppURL "https://github.com/reznikov73/POSvpn"
#define RepoRoot ".."

[Setup]
AppId={{725A8BEA-B8C8-47B7-97BD-68FCBBFB600C}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppVerName={#MyAppName} {#MyAppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
AppSupportURL={#MyAppURL}
VersionInfoVersion={#MyAppVersion}.0
VersionInfoCompany={#MyAppPublisher}
VersionInfoProductName={#MyAppName}
VersionInfoProductVersion={#MyAppVersion}
DefaultDirName={autopf}\POSvpn
DefaultGroupName={#MyAppName}
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
PrivilegesRequiredOverridesAllowed=dialog
OutputDir=out
OutputBaseFilename=POSvpn-Setup-{#MyAppVersion}
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0
CloseApplications=yes
RestartApplications=no
UninstallDisplayName={#MyAppName} {#MyAppVersion}
UninstallDisplayIcon={app}\posvpn-client.exe

[Languages]
Name: "russian"; MessagesFile: "compiler:Languages\Russian.isl"
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: unchecked

[Components]
Name: "client"; Description: "Клиент POSvpn — создание сети и вход по имени и паролю"; Types: full
Name: "server"; Description: "Сервер координации POSvpn — реестр сетей и узлов (для ПК с публичным адресом)"; Types: full

[Files]
Source: "{#RepoRoot}\out\posvpn-client.exe"; DestDir: "{app}"; Components: client; Flags: ignoreversion
Source: "{#RepoRoot}\out\posvpn-server.exe"; DestDir: "{app}"; Components: server; Flags: ignoreversion
Source: "{#RepoRoot}\README.md"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{group}\POSvpn Client"; Filename: "{app}\posvpn-client.exe"; Components: client
Name: "{group}\POSvpn Server"; Filename: "{app}\posvpn-server.exe"; Components: server
Name: "{group}\{cm:UninstallProgram,POSvpn}"; Filename: "{uninstallexe}"
Name: "{autodesktop}\POSvpn Client"; Filename: "{app}\posvpn-client.exe"; Components: client; Tasks: desktopicon

[Run]
Filename: "{app}\README.md"; Description: "Открыть README"; Flags: postinstall shellexec skipifsilent unchecked
