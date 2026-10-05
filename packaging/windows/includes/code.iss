[Code]
var
  UpgradeModePage: TInputOptionWizardPage;
  StartupPage: TInputOptionWizardPage;
  DesktopShortcutCheckBox: TNewCheckBox;
  PurgeState: Boolean;
  UninstallCleanupExecuted: Boolean;
  ResultFilePath: String;
  ExistingInstallDetected: Boolean;
  ExistingInstallVersion: String;
  ExistingInstallSource: String;
  ResolvedInstallRoot: String;
  InstallProgressPage: TOutputProgressWizardPage;
  InstallWarningCode: String;
  InstallWarningMessage: String;

function GetLocalizedMessage(Key: String): String;
begin
  Result := CustomMessage(Key);
end;

function ResolveInstallRoot(): String;
var
  UninstallKey: String;
  InstallLocation: String;
begin
  Result := Trim(ExpandConstant('{param:DIR|}'));
  if Result <> '' then
    Exit;

  UninstallKey := 'Software\Microsoft\Windows\CurrentVersion\Uninstall\{#AppIdValue}_is1';
  if RegQueryStringValue(HKCU, UninstallKey, 'InstallLocation', InstallLocation) and
    (Trim(InstallLocation) <> '') then
  begin
    Result := RemoveBackslashUnlessRoot(Trim(InstallLocation));
    Exit;
  end;

  Result := ExpandConstant('{localappdata}\AgentDock');
end;

function ExistingInstallRoot(): String;
begin
  Result := ResolvedInstallRoot;
end;

function DetectExistingInstallation(): Boolean;
var
  UninstallKey: String;
  BinaryPath: String;
  VersionValue: String;
begin
  ExistingInstallVersion := '';
  ExistingInstallSource := '';
  UninstallKey := 'Software\Microsoft\Windows\CurrentVersion\Uninstall\{#AppIdValue}_is1';

  if RegQueryStringValue(HKCU, UninstallKey, 'DisplayVersion', VersionValue) then
  begin
    ExistingInstallVersion := Trim(VersionValue);
    ExistingInstallSource := 'setup';
    Result := True;
    Exit;
  end;

  BinaryPath := AddBackslash(ExistingInstallRoot()) + 'bin\agentdock.exe';
  if FileExists(BinaryPath) or
    FileExists(AddBackslash(ExistingInstallRoot()) + 'runtime.json') or
    FileExists(AddBackslash(ExistingInstallRoot()) + 'start-agentdock.ps1') then
  begin
    if GetVersionNumbersString(BinaryPath, VersionValue) then
      ExistingInstallVersion := Trim(VersionValue);
    ExistingInstallSource := 'powershell';
    Result := True;
    Exit;
  end;

  Result := False;
end;

function LegacyAgentDockScheduledTaskExists(): Boolean;
var
  ExitCode: Integer;
  SchTasksPath: String;
begin
  SchTasksPath := ExpandConstant('{sys}\schtasks.exe');
  if not FileExists(SchTasksPath) then
  begin
    Log('Windows schtasks.exe is unavailable; skipping legacy AgentDock task detection.');
    Result := False;
    Exit;
  end;

  Result :=
    Exec(
      SchTasksPath,
      '/Query /TN "\AgentDock"',
      '',
      SW_HIDE,
      ewWaitUntilTerminated,
      ExitCode) and
    (ExitCode = 0);
  if Result then
    Log('AgentDock legacy scheduled task detected.');
end;

function RuntimeUsesElevatedCore(): Boolean;
var
  Content: AnsiString;
  Normalized: String;
  ManifestPath: String;
begin
  Result := False;
  ManifestPath := AddBackslash(ExistingInstallRoot()) + 'runtime.json';
  if not FileExists(ManifestPath) then
    Exit;
  if not LoadStringFromFile(ManifestPath, Content) then
    Exit;
  Normalized := Lowercase(String(Content));
  StringChangeEx(Normalized, ' ', '', True);
  StringChangeEx(Normalized, #13, '', True);
  StringChangeEx(Normalized, #10, '', True);
  Result := Pos('"privilege_mode":"elevated"', Normalized) > 0;
end;

procedure LoadExistingSettings();
var
  RunKey: String;
begin
  if not ExistingInstallDetected then
    Exit;

  RunKey := 'Software\Microsoft\Windows\CurrentVersion\Run';
  StartupPage.Values[0] :=
    RegValueExists(HKCU, RunKey, 'AgentDock') or
    RegValueExists(HKCU, RunKey, 'AgentDockTray') or
    LegacyAgentDockScheduledTaskExists();
  StartupPage.Values[1] := RuntimeUsesElevatedCore() or LegacyAgentDockScheduledTaskExists();
end;

procedure ApplyExistingInstallPresentation();
var
  Details: String;
begin
  if not ExistingInstallDetected then
    Exit;

  WizardForm.WelcomeLabel1.Caption := GetLocalizedMessage('UpgradeWelcome');
  Details := '';
  if ExistingInstallVersion <> '' then
    Details := GetLocalizedMessage('UpgradeExistingVersion') + ' ' + ExistingInstallVersion + #13#10;
  Details := Details + GetLocalizedMessage('UpgradeTargetVersion') + ' {#AppVersion}' + #13#10#13#10;
  if ExistingInstallSource = 'setup' then
    Details := Details + GetLocalizedMessage('UpgradeSetupManaged')
  else
    Details := Details + GetLocalizedMessage('UpgradeLegacyManaged');
  WizardForm.WelcomeLabel2.Caption := Details;
  Log('AgentDock existing installation detected: source=' + ExistingInstallSource +
    ', version=' + ExistingInstallVersion + ', root=' + ExistingInstallRoot());
end;

function QuoteArgument(const Value: String): String;
begin
  Result := '"' + Value + '"';
end;

function LaunchRuntimeProcess(const Filename: String; const Arguments: String): Boolean;
var
  ExitCode: Integer;
  Parameters: String;
begin
  Parameters :=
    '-NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File ' +
    QuoteArgument(ExpandConstant('{tmp}\launch-windows-process.ps1')) +
    ' -FilePath ' + QuoteArgument(Filename) +
    ' -AgentDockBinary ' + QuoteArgument(ExpandConstant('{app}\bin\agentdock.exe')) +
    ' -HiddenHostBinary ' + QuoteArgument(ExpandConstant('{app}\bin\agentdock-tray.exe'));
  if Arguments <> '' then
    Parameters := Parameters + ' -Arguments ' + QuoteArgument(Arguments);

  Result := Exec(
    ExpandConstant('{sys}\WindowsPowerShell\v1.0\powershell.exe'),
    Parameters,
    '',
    SW_HIDE,
    ewWaitUntilTerminated,
    ExitCode);
  if Result and (ExitCode <> 0) then
  begin
    Log('AgentDock runtime launch broker exited with code ' + IntToStr(ExitCode) + '.');
    Result := False;
  end;
end;

procedure InitializeWizard();
var
  AutoStartParam: String;
begin
  Log('AgentDock active language: ' + ActiveLanguage());
  ResolvedInstallRoot := ResolveInstallRoot();
  ExistingInstallDetected := DetectExistingInstallation();

  UpgradeModePage := CreateInputOptionPage(
    wpWelcome,
    GetLocalizedMessage('UpgradeModeCaption'),
    GetLocalizedMessage('UpgradeModeDescription'),
    GetLocalizedMessage('UpgradeModeSubCaption'),
    True,
    False
  );
  UpgradeModePage.Add(GetLocalizedMessage('UpgradeKeepSettings'));
  UpgradeModePage.Add(GetLocalizedMessage('UpgradeChangeSettings'));
  UpgradeModePage.SelectedValueIndex := 0;

  StartupPage := CreateInputOptionPage(
    UpgradeModePage.ID,
    GetLocalizedMessage('StartupPageCaption'),
    GetLocalizedMessage('StartupPageDescription'),
    GetLocalizedMessage('StartupPageSubCaption'),
    False,
    False
  );
  StartupPage.Add(GetLocalizedMessage('StartupOption'));
  StartupPage.Add(GetLocalizedMessage('ElevatedCoreOption'));
  StartupPage.Values[0] := True;
  StartupPage.Values[1] := False;

  LoadExistingSettings();

  AutoStartParam := Lowercase(ExpandConstant('{param:AUTOSTART|}'));
  if (AutoStartParam = '0') or (AutoStartParam = 'false') then
    StartupPage.Values[0] := False
  else if (AutoStartParam = '1') or (AutoStartParam = 'true') then
    StartupPage.Values[0] := True;

  AutoStartParam := Lowercase(ExpandConstant('{param:ADMINMODE|}'));
  if (AutoStartParam = '0') or (AutoStartParam = 'false') or (AutoStartParam = 'standard') then
    StartupPage.Values[1] := False
  else if (AutoStartParam = '1') or (AutoStartParam = 'true') or (AutoStartParam = 'elevated') then
    StartupPage.Values[1] := True;

  ApplyExistingInstallPresentation();

  InstallProgressPage := CreateOutputProgressPage(
    GetLocalizedMessage('OfflineProgressCaption'),
    GetLocalizedMessage('OfflineProgressDescription')
  );

  DesktopShortcutCheckBox := TNewCheckBox.Create(WizardForm);
  DesktopShortcutCheckBox.Parent := WizardForm.FinishedPage;
  DesktopShortcutCheckBox.Left := WizardForm.FinishedLabel.Left;
  DesktopShortcutCheckBox.Top := WizardForm.FinishedLabel.Top + WizardForm.FinishedLabel.Height + ScaleY(18);
  DesktopShortcutCheckBox.Width := WizardForm.FinishedPage.ClientWidth -
    DesktopShortcutCheckBox.Left - ScaleX(8);
  DesktopShortcutCheckBox.Caption := GetLocalizedMessage('CreateDesktopShortcut');
  DesktopShortcutCheckBox.Checked := True;
end;

function ShouldSkipPage(PageID: Integer): Boolean;
var
  PreserveExisting: Boolean;
begin
  PreserveExisting := ExistingInstallDetected and (UpgradeModePage.SelectedValueIndex = 0);
  Result :=
    ((PageID = UpgradeModePage.ID) and (not ExistingInstallDetected)) or
    (PreserveExisting and (PageID = StartupPage.ID));
end;

function ApplyDesktopControlPanelShortcut(CreateRequested: Boolean): Boolean;
var
  ShortcutPath: String;
  CreatedShortcutPath: String;
begin
  ShortcutPath := AddBackslash(ExpandConstant('{userdesktop}')) +
    GetLocalizedMessage('DesktopShortcutName') + '.lnk';
  if not CreateRequested then
  begin
    DeleteFile(ShortcutPath);
    Result := not FileExists(ShortcutPath);
    Exit;
  end;

  CreatedShortcutPath := CreateShellLink(
    ShortcutPath,
    GetLocalizedMessage('DesktopShortcutDescription'),
    ExpandConstant('{app}\bin\agentdock-tray.exe'),
    '',
    ExpandConstant('{app}'),
    ExpandConstant('{app}\bin\agentdock-tray.exe'),
    0,
    SW_SHOWNORMAL
  );
  Result := CreatedShortcutPath <> '';
end;

function NextButtonClick(CurPageID: Integer): Boolean;
begin
  Result := True;
  if CurPageID = wpFinished then
  begin
    if not ApplyDesktopControlPanelShortcut(DesktopShortcutCheckBox.Checked) then
      Log('AgentDock desktop shortcut state could not be applied.');
    if Pos('runtime-launch-deferred', InstallWarningCode) = 0 then
    begin
      if not LaunchRuntimeProcess(ExpandConstant('{app}\bin\agentdock-tray.exe'), '') then
        Log('AgentDock control panel could not be opened from the Finish button.');
    end
    else
      Log('AgentDock runtime activation was deferred; skipping Finish-page control panel launch.');
    Exit;
  end;
  if (CurPageID = StartupPage.ID) and StartupPage.Values[1] then
    StartupPage.Values[0] := True;
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  PowerShellPath: String;
  InstallScriptPath: String;
  RuntimeScriptPath: String;
  RuntimeMetadataPath: String;
  RuntimeResultFilePath: String;
  OfflineArchivePath: String;
  OfflineChecksumPath: String;
  RuntimeParameters: String;
  RuntimeDependency: String;
  RuntimeMessage: String;
  Parameters: String;
  TunnelMode: String;
  PrivilegeMode: String;
  ExitCode: Integer;
  RuntimeExitCode: Integer;
  ErrorCode: String;
  ErrorMessage: String;
  ErrorType: String;
  ErrorId: String;
  ErrorCategory: String;
  ErrorScript: String;
  ErrorLine: String;
  ErrorColumn: String;
  ErrorStack: String;
begin
  Result := '';
  InstallProgressPage.Show;
  try
    InstallProgressPage.SetText(GetLocalizedMessage('OfflineProgressPreparing'), '');
    InstallProgressPage.SetProgress(1, 5);
    ExtractTemporaryFile('install.ps1');
    ExtractTemporaryFile('launch-windows-process.ps1');
    ExtractTemporaryFile('ensure-windows-runtimes.ps1');
    ExtractTemporaryFile('runtime-dependencies.json');
    ExtractTemporaryFile('agentdock_windows_{#PayloadArchitecture}.zip');
    ExtractTemporaryFile('agentdock_windows_{#PayloadArchitecture}.zip.sha256');

    PowerShellPath := ExpandConstant('{sys}\WindowsPowerShell\v1.0\powershell.exe');
    InstallScriptPath := ExpandConstant('{tmp}\install.ps1');
    RuntimeScriptPath := ExpandConstant('{tmp}\ensure-windows-runtimes.ps1');
    RuntimeMetadataPath := ExpandConstant('{tmp}\runtime-dependencies.json');
    RuntimeResultFilePath := ExpandConstant('{tmp}\agentdock-runtime-result.ini');
    OfflineArchivePath := ExpandConstant('{tmp}\agentdock_windows_{#PayloadArchitecture}.zip');
    OfflineChecksumPath := ExpandConstant('{tmp}\agentdock_windows_{#PayloadArchitecture}.zip.sha256');
    ResultFilePath := ExpandConstant('{tmp}\agentdock-install-result.ini');
    DeleteFile(RuntimeResultFilePath);
    DeleteFile(ResultFilePath);

    { Runtime 属于系统共享依赖，必须在 generation 激活前满足。失败时直接终止 Setup，
      不让 install.ps1 创建或切换半完成的 AgentDock generation。 }
    InstallProgressPage.SetText(GetLocalizedMessage('RuntimeProgressChecking'), '');
    InstallProgressPage.SetProgress(2, 5);
    RuntimeParameters :=
      '-NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File ' + QuoteArgument(RuntimeScriptPath) +
      ' -Architecture {#PayloadArchitecture}' +
      ' -MetadataPath ' + QuoteArgument(RuntimeMetadataPath) +
      ' -ResultFile ' + QuoteArgument(RuntimeResultFilePath);
    if not Exec(PowerShellPath, RuntimeParameters, '', SW_HIDE, ewWaitUntilTerminated, RuntimeExitCode) then
    begin
      Result := GetLocalizedMessage('RuntimeInstallFailed') + ' prerequisite process could not start.';
      Exit;
    end;
    if RuntimeExitCode <> 0 then
    begin
      RuntimeDependency := GetIniString('AgentDockRuntime', 'Dependency', '', RuntimeResultFilePath);
      RuntimeMessage := GetIniString('AgentDockRuntime', 'Message', '', RuntimeResultFilePath);
      if RuntimeMessage = '' then
        RuntimeMessage := 'exit code ' + IntToStr(RuntimeExitCode);
      if RuntimeDependency <> '' then
        RuntimeMessage := RuntimeDependency + ': ' + RuntimeMessage;
      Result := GetLocalizedMessage('RuntimeInstallFailed') + ' ' + RuntimeMessage;
      Exit;
    end;
    { MODE/SERVERURL/TUNNELTOKENFILE 仅保留给历史 silent automation。交互式 Setup
      不再提供 Cloudflare 配置页，空 MODE 会让基础安装完全绕开 component lifecycle。 }
    TunnelMode := Lowercase(Trim(ExpandConstant('{param:MODE|}')));
    if TunnelMode = 'local' then
      TunnelMode := 'none';
    if (TunnelMode <> 'none') and (TunnelMode <> 'quick') and (TunnelMode <> 'named') then
      TunnelMode := '';
    if StartupPage.Values[1] then
      PrivilegeMode := 'elevated'
    else
      PrivilegeMode := 'standard';

    InstallProgressPage.SetProgress(3, 5);
    Parameters :=
      '-NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File ' + QuoteArgument(InstallScriptPath) +
      ' -OfflineArchive ' + QuoteArgument(OfflineArchivePath) +
      ' -OfflineChecksumFile ' + QuoteArgument(OfflineChecksumPath) +
      ' -InstallDir ' + QuoteArgument(ExpandConstant('{app}\bin')) +
      ' -InstallChannel setup' +
      ' -CorePrivilegeMode ' + PrivilegeMode +
      ' -ResultFile ' + QuoteArgument(ResultFilePath);
    if TunnelMode <> '' then
      Parameters := Parameters + ' -TunnelMode ' + TunnelMode;

    { PORT is intentionally a silent-setup override. The interactive installer keeps the product
      default, while isolated E2E environments can avoid colliding with an already running AgentDock. }
    if Trim(ExpandConstant('{param:PORT|}')) <> '' then
      Parameters := Parameters + ' -Port ' + QuoteArgument(Trim(ExpandConstant('{param:PORT|}')));

    if StartupPage.Values[0] or (TunnelMode = 'quick') or (TunnelMode = 'named') then
      Parameters := Parameters + ' -RegisterStartup';

    if TunnelMode = 'named' then
    begin
      if Trim(ExpandConstant('{param:SERVERURL|}')) <> '' then
        Parameters := Parameters + ' -ServerUrl ' + QuoteArgument(Trim(ExpandConstant('{param:SERVERURL|}')));
      if Trim(ExpandConstant('{param:TUNNELTOKENFILE|}')) <> '' then
        Parameters := Parameters + ' -TunnelTokenFile ' + QuoteArgument(Trim(ExpandConstant('{param:TUNNELTOKENFILE|}')));
    end;

    InstallProgressPage.SetText(GetLocalizedMessage('OfflineProgressApplying'), '');
    InstallProgressPage.SetProgress(4, 5);
    if not Exec(PowerShellPath, Parameters, '', SW_HIDE, ewWaitUntilTerminated, ExitCode) then
    begin
      Result := GetLocalizedMessage('InstallerStartFailed');
      Exit;
    end;
    if ExitCode <> 0 then
    begin
      ErrorCode := GetIniString('AgentDock', 'Code', '', ResultFilePath);
      ErrorMessage := GetIniString('AgentDock', 'Message', '', ResultFilePath);
      ErrorType := GetIniString('AgentDock', 'ErrorType', '', ResultFilePath);
      ErrorId := GetIniString('AgentDock', 'ErrorId', '', ResultFilePath);
      ErrorCategory := GetIniString('AgentDock', 'ErrorCategory', '', ResultFilePath);
      ErrorScript := GetIniString('AgentDock', 'ErrorScript', '', ResultFilePath);
      ErrorLine := GetIniString('AgentDock', 'ErrorLine', '', ResultFilePath);
      ErrorColumn := GetIniString('AgentDock', 'ErrorColumn', '', ResultFilePath);
      ErrorStack := GetIniString('AgentDock', 'ErrorStack', '', ResultFilePath);
      if (ErrorType <> '') or (ErrorId <> '') or (ErrorCategory <> '') then
        Log('AgentDock installation diagnostics: type=' + ErrorType +
          '; id=' + ErrorId + '; category=' + ErrorCategory);
      if (ErrorScript <> '') or (ErrorLine <> '') or (ErrorColumn <> '') then
        Log('AgentDock installation location: script=' + ErrorScript +
          '; line=' + ErrorLine + '; column=' + ErrorColumn);
      if ErrorStack <> '' then
        Log('AgentDock installation stack: ' + ErrorStack);
      if ErrorCode = 'setup-elevated-context' then
        ErrorMessage := GetLocalizedMessage('ElevatedSetupUnsupported');
      if ErrorCode = 'credential-user-mismatch' then
        ErrorMessage := GetLocalizedMessage('CredentialUserMismatch');
      if ErrorMessage = '' then
        ErrorMessage := GetLocalizedMessage('InstallerExitCode') + ' ' + IntToStr(ExitCode);
      Result := GetLocalizedMessage('InstallFailed') + ' ' + ErrorMessage;
      Exit;
    end;
    InstallWarningCode := GetIniString('AgentDock', 'WarningCode', '', ResultFilePath);
    InstallWarningMessage := GetIniString('AgentDock', 'WarningMessage', '', ResultFilePath);
    if InstallWarningCode <> '' then
      Log('AgentDock installation warning: ' + InstallWarningCode);
    if InstallWarningMessage <> '' then
      Log('AgentDock installation warning detail: ' + InstallWarningMessage);
    InstallProgressPage.SetText(GetLocalizedMessage('OfflineProgressFinishing'), '');
    InstallProgressPage.SetProgress(5, 5);
  finally
    InstallProgressPage.Hide;
  end;
end;

procedure CurPageChanged(CurPageID: Integer);
begin
  if (CurPageID = wpReady) and ExistingInstallDetected then
    WizardForm.ReadyLabel.Caption := GetLocalizedMessage('ReadyUpgrade');
  if CurPageID = wpFinished then
  begin
    if Pos('runtime-launch-deferred', InstallWarningCode) > 0 then
      WizardForm.FinishedLabel.Caption := GetLocalizedMessage('FinishedDeferredControlPanel')
    else
      WizardForm.FinishedLabel.Caption := GetLocalizedMessage('FinishedControlPanel');
    if Pos('elevated-mode-fallback', InstallWarningCode) > 0 then
      WizardForm.FinishedLabel.Caption := WizardForm.FinishedLabel.Caption + #13#10#13#10 +
        GetLocalizedMessage('ElevatedModeFallbackNotice');
  end;
end;

function InitializeUninstall(): Boolean;
begin
  PurgeState := False;
  UninstallCleanupExecuted := False;
  if not UninstallSilent then
    PurgeState := MsgBox(
      GetLocalizedMessage('PurgeStateQuestion'),
      mbConfirmation,
      MB_YESNO
    ) = IDYES;
  Result := True;
end;

function GetUninstallParameters(Param: String): String;
begin
  Result := '-NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File ' +
    QuoteArgument(ExpandConstant('{app}\installer\uninstall-windows.ps1')) +
    ' -InstallDir ' + QuoteArgument(ExpandConstant('{app}\bin')) +
    ' -KeepInstallDir';
  if PurgeState then
    Result := Result + ' -PurgeState';
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  PowerShellPath: String;
  ScriptPath: String;
  ExitCode: Integer;
begin
  if (CurUninstallStep <> usAppMutexCheck) or UninstallCleanupExecuted then
    Exit;

  UninstallCleanupExecuted := True;
  Log('AgentDock: running managed cleanup before uninstall file removal.');
  ScriptPath := ExpandConstant('{app}\installer\uninstall-windows.ps1');
  if not FileExists(ScriptPath) then
    RaiseException(GetLocalizedMessage('UninstallScriptMissing'));

  PowerShellPath := ExpandConstant('{sys}\WindowsPowerShell\v1.0\powershell.exe');
  if not Exec(
    PowerShellPath,
    GetUninstallParameters(''),
    '',
    SW_HIDE,
    ewWaitUntilTerminated,
    ExitCode
  ) then
    RaiseException(GetLocalizedMessage('UninstallScriptFailed') + ' start');
  if ExitCode <> 0 then
    RaiseException(
      GetLocalizedMessage('UninstallScriptFailed') + ' ' + IntToStr(ExitCode)
    );
  Log('AgentDock: managed cleanup completed successfully.');
end;

// Inno 保留 TEMP 原生日志；这里额外复制到固定目录，方便用户长期查找和反馈安装问题。
procedure PersistSetupLog();
var
  SourceLog: String;
  LogDirectory: String;
  PersistentLog: String;
begin
  SourceLog := ExpandConstant('{log}');
  if (SourceLog = '') or (not FileExists(SourceLog)) then
    Exit;

  LogDirectory := ExpandConstant('{localappdata}\AgentDock\logs\installer');
  if not ForceDirectories(LogDirectory) then
  begin
    Log('AgentDock: could not create persistent installer log directory: ' + LogDirectory);
    Exit;
  end;

  PersistentLog := AddBackslash(LogDirectory) + 'setup-' +
    GetDateTimeString('yyyymmdd-hhnnss-zzz', '-', ':') + '.log';
  Log('AgentDock installer log target: ' + PersistentLog);
  if not CopyFile(SourceLog, PersistentLog, True) then
    Log('AgentDock: could not persist installer log; original log remains at: ' + SourceLog);
end;

procedure DeinitializeSetup();
begin
  PersistSetupLog();
  if ResultFilePath <> '' then
    DeleteFile(ResultFilePath);
end;
