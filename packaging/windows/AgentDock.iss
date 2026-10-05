#ifndef AppVersion
#define AppVersion "0.0.0"
#endif

#define AppIdValue "{D6788C7A-4104-48D4-B5C3-F4858B5606EA}"

#ifndef OutputDir
#define OutputDir "..\..\dist"
#endif

#ifndef PayloadDir
#define PayloadDir "..\..\dist\windows-setup-payload"
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
AppUpdatesURL=https://www.nexusdock.co/download/
DefaultDirName={localappdata}\AgentDock
DefaultGroupName=AgentDock
DisableProgramGroupPage=yes
DisableDirPage=yes
DisableReadyPage=yes
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
Source: "ensure-windows-runtimes.ps1"; Flags: dontcopy
Source: "runtime-dependencies.json"; Flags: dontcopy
Source: "{#PayloadDir}\agentdock_windows_{#PayloadArchitecture}.zip"; Flags: dontcopy
Source: "{#PayloadDir}\agentdock_windows_{#PayloadArchitecture}.zip.sha256"; Flags: dontcopy
Source: "..\..\scripts\install\uninstall-windows.ps1"; DestDir: "{app}\installer"; Flags: ignoreversion
Source: "assets\agentdock.ico"; DestDir: "{app}\installer"; Flags: ignoreversion

[InstallDelete]
; Remove bootstrap-only files persisted by older Setup builds. Current Setup extracts these to TEMP only.
Type: files; Name: "{app}\installer\install.ps1"
Type: files; Name: "{app}\installer\ensure-windows-runtimes.ps1"
Type: files; Name: "{app}\installer\runtime-dependencies.json"
; Remove shortcuts created by older Setup builds so upgrades converge on the current product surface.
Type: files; Name: "{userdesktop}\AgentDock Control Panel.lnk"
Type: files; Name: "{userdesktop}\AgentDock 控制面板.lnk"
Type: files; Name: "{group}\AgentDock documentation.lnk"
Type: files; Name: "{group}\AgentDock 使用文档.lnk"
Type: files; Name: "{group}\Uninstall AgentDock.lnk"
Type: files; Name: "{group}\卸载 AgentDock.lnk"

[UninstallDelete]
Type: filesandordirs; Name: "{app}\bin"
Type: filesandordirs; Name: "{app}\versions"
Type: filesandordirs; Name: "{app}\update"
Type: files; Name: "{app}\active-version.json"
Type: files; Name: "{app}\desktop-version.txt"
Type: files; Name: "{app}\installer\install.ps1"
Type: files; Name: "{app}\installer\ensure-windows-runtimes.ps1"
Type: files; Name: "{app}\installer\runtime-dependencies.json"
Type: files; Name: "{userdesktop}\{code:GetLocalizedMessage|DesktopShortcutName}.lnk"
Type: files; Name: "{userdesktop}\AgentDock Control Panel.lnk"
Type: files; Name: "{userdesktop}\AgentDock 控制面板.lnk"
Type: files; Name: "{group}\AgentDock documentation.lnk"
Type: files; Name: "{group}\AgentDock 使用文档.lnk"
Type: files; Name: "{group}\Uninstall AgentDock.lnk"
Type: files; Name: "{group}\卸载 AgentDock.lnk"

[Icons]
Name: "{group}\AgentDock"; Filename: "{app}\bin\agentdock-tray.exe"; WorkingDir: "{app}"; IconFilename: "{app}\bin\agentdock-tray.exe"; AppUserModelID: "com.uvwt.agentdock.controlpanel"

#include "includes\code.iss"
