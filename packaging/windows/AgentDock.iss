#ifndef AppVersion
#define AppVersion "0.0.0"
#endif

#define AppIdValue "{D6788C7A-4104-48D4-B5C3-F4858B5606EA}"

#ifndef OutputDir
#define OutputDir "..\..\dist"
#endif

#ifndef OfflinePayloadDir
#define OfflinePayloadDir "..\..\dist\windows-offline-payload"
#endif

#ifdef WindowsARM64
#define PayloadArchitecture "arm64"
#define SetupBaseFilename "AgentDockSetup-arm64"
#else
#define PayloadArchitecture "amd64"
#define SetupBaseFilename "AgentDockSetup-amd64"
#endif

[Setup]
AppId={{D6788C7A-4104-48D4-B5C3-F4858B5606EA}
AppName=AgentDock
AppVersion={#AppVersion}
AppPublisher=AgentDock
AppPublisherURL=https://github.com/uvwt/agentdock
AppSupportURL=https://github.com/uvwt/agentdock/issues
AppUpdatesURL=https://github.com/uvwt/agentdock/releases
DefaultDirName={localappdata}\AgentDock
DefaultGroupName=AgentDock
DisableProgramGroupPage=yes
DisableDirPage=yes
PrivilegesRequired=lowest
OutputDir={#OutputDir}
OutputBaseFilename={#SetupBaseFilename}
SetupIconFile=assets\agentdock.ico
UninstallDisplayIcon={app}\installer\agentdock.ico
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
CloseApplications=yes
RestartApplications=no
SetupLogging=yes
UsePreviousAppDir=yes
UsePreviousLanguage=no
LanguageDetectionMethod=uilanguage
ShowLanguageDialog=no
#ifdef WindowsARM64
ArchitecturesAllowed=arm64
#else
; x64compatible uses the native OS architecture instead of the 32-bit Setup process view.
ArchitecturesAllowed=x64compatible
#endif
#ifdef SignedBuild
SignTool=agentdock-sign
SignedUninstaller=yes
#endif

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"
Name: "chinesesimplified"; MessagesFile: "compiler:Default.isl, languages\ChineseSimplified.isl"


#include "includes\messages.iss"

[Files]
Source: "..\..\scripts\install\install.ps1"; Flags: dontcopy
Source: "..\..\scripts\install\launch-windows-process.ps1"; Flags: dontcopy
Source: "{#OfflinePayloadDir}\agentdock_windows_{#PayloadArchitecture}.zip"; Flags: dontcopy
Source: "{#OfflinePayloadDir}\agentdock_windows_{#PayloadArchitecture}.zip.sha256"; Flags: dontcopy
Source: "{#OfflinePayloadDir}\cloudflared.exe"; Flags: dontcopy
Source: "..\..\scripts\install\install.ps1"; DestDir: "{app}\installer"; Flags: ignoreversion
Source: "..\..\scripts\install\uninstall-windows.ps1"; DestDir: "{app}\installer"; Flags: ignoreversion
Source: "assets\agentdock.ico"; DestDir: "{app}\installer"; Flags: ignoreversion

[UninstallDelete]
Type: filesandordirs; Name: "{app}\bin"
Type: files; Name: "{userdesktop}\{code:GetLocalizedMessage|DesktopShortcutName}.lnk"

[Icons]
Name: "{group}\AgentDock"; Filename: "{app}\bin\agentdock-tray.exe"; WorkingDir: "{app}"; AppUserModelID: "com.uvwt.agentdock.controlpanel"
Name: "{group}\{code:GetLocalizedMessage|DocsShortcut}"; Filename: "https://uvwt.github.io/agentdock-docs/"
Name: "{group}\{code:GetLocalizedMessage|UninstallShortcut}"; Filename: "{uninstallexe}"

#include "includes\code.iss"
