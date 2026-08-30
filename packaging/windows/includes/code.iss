[Code]
var
  UpgradeModePage: TInputOptionWizardPage;
  StartupPage: TInputOptionWizardPage;
  ConnectionPage: TInputOptionWizardPage;
  FixedTunnelPage: TInputQueryWizardPage;
  DesktopShortcutCheckBox: TNewCheckBox;
  PurgeState: Boolean;
  UninstallCleanupExecuted: Boolean;
  ResultFilePath: String;
  TemporaryTokenFilePath: String;
  ExistingInstallDetected: Boolean;
  ExistingInstallVersion: String;
  ExistingInstallSource: String;
  ResolvedInstallRoot: String;
  InstallProgressPage: TOutputProgressWizardPage;
  InstallWarningCode: String;

function GetLocalizedMessage(Key: String): String;
begin
  Result := CustomMessage(Key);
end;

function ReadTrimmedTextFile(Path: String): String;
var
  Content: AnsiString;
begin
  Result := '';
  if FileExists(Path) then
  begin
    if LoadStringFromFile(Path, Content) then
      Result := Trim(String(Content));
  end;
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
begin
  Result :=
    Exec(
      ExpandConstant('{sys}\schtasks.exe'),
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
  Mode: String;
  URL: String;
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

  Mode := Lowercase(ReadTrimmedTextFile(AddBackslash(ExistingInstallRoot()) + 'cloudflared-mode.txt'));
  if Mode = 'quick' then
    ConnectionPage.SelectedValueIndex := 1
  else if Mode = 'named' then
    ConnectionPage.SelectedValueIndex := 2
  else
    ConnectionPage.SelectedValueIndex := 0;

  URL := ReadTrimmedTextFile(AddBackslash(ExistingInstallRoot()) + 'server-url.txt');
  if URL <> '' then
    FixedTunnelPage.Values[0] := URL;
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
    ' -FilePath ' + QuoteArgument(Filename);
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

function SelectedTunnelMode(): String;
begin
  case ConnectionPage.SelectedValueIndex of
    1: Result := 'quick';
    2: Result := 'named';
  else
    Result := 'none';
  end;
end;

procedure InitializeWizard();
var
  ModeParam: String;
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
  StartupPage.Values[1] := True;

  ConnectionPage := CreateInputOptionPage(
    StartupPage.ID,
    GetLocalizedMessage('ConnectionPageCaption'),
    GetLocalizedMessage('ConnectionPageDescription'),
    GetLocalizedMessage('ConnectionPageSubCaption'),
    True,
    False
  );
  ConnectionPage.Add(GetLocalizedMessage('LocalMode'));
  ConnectionPage.Add(GetLocalizedMessage('QuickMode'));
  ConnectionPage.Add(GetLocalizedMessage('NamedMode'));
  ConnectionPage.SelectedValueIndex := 0;

  FixedTunnelPage := CreateInputQueryPage(
    ConnectionPage.ID,
    GetLocalizedMessage('FixedPageCaption'),
    GetLocalizedMessage('FixedPageDescription'),
    GetLocalizedMessage('FixedPageSubCaption')
  );
  FixedTunnelPage.Add(GetLocalizedMessage('ServerURLLabel'), False);
  FixedTunnelPage.Add(GetLocalizedMessage('TunnelTokenLabel'), True);

  LoadExistingSettings();

  ModeParam := Lowercase(ExpandConstant('{param:MODE|}'));
  if ModeParam = 'quick' then
    ConnectionPage.SelectedValueIndex := 1
  else if ModeParam = 'named' then
    ConnectionPage.SelectedValueIndex := 2
  else if ModeParam = 'local' then
    ConnectionPage.SelectedValueIndex := 0;

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

  if ExpandConstant('{param:SERVERURL|}') <> '' then
    FixedTunnelPage.Values[0] := ExpandConstant('{param:SERVERURL|}');

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
    (PreserveExisting and
      ((PageID = StartupPage.ID) or (PageID = ConnectionPage.ID) or (PageID = FixedTunnelPage.ID))) or
    ((PageID = FixedTunnelPage.ID) and (SelectedTunnelMode() <> 'named'));
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
var
  URL: String;
begin
  Result := True;
  if CurPageID = wpFinished then
  begin
    if not ApplyDesktopControlPanelShortcut(DesktopShortcutCheckBox.Checked) then
      Log('AgentDock desktop shortcut state could not be applied.');
    if not LaunchRuntimeProcess(ExpandConstant('{app}\bin\agentdock-tray.exe'), '') then
      Log('AgentDock control panel could not be opened from the Finish button.');
    Exit;
  end;
  if (CurPageID = StartupPage.ID) and StartupPage.Values[1] then
    StartupPage.Values[0] := True;
  if (CurPageID = ConnectionPage.ID) and (SelectedTunnelMode() <> 'none') then
    StartupPage.Values[0] := True;
  if CurPageID = FixedTunnelPage.ID then
  begin
    URL := Trim(FixedTunnelPage.Values[0]);
    if (Pos('https://', Lowercase(URL)) <> 1) or (Pos('"', URL) > 0) then
    begin
      MsgBox(GetLocalizedMessage('InvalidServerURL'), mbError, MB_OK);
      Result := False;
      Exit;
    end;
    if (Trim(FixedTunnelPage.Values[1]) = '') and
      (ExpandConstant('{param:TUNNELTOKENFILE|}') = '') and
      (not FileExists(AddBackslash(ExistingInstallRoot()) + 'cloudflared-token.dpapi')) then
    begin
      MsgBox(GetLocalizedMessage('TokenRequired'), mbError, MB_OK);
      Result := False;
      Exit;
    end;
  end;
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  PowerShellPath: String;
  InstallScriptPath: String;
  OfflineArchivePath: String;
  OfflineChecksumPath: String;
  OfflineCloudflaredPath: String;
  TokenFilePath: String;
  SilentTokenFile: String;
  Parameters: String;
  TunnelMode: String;
  PrivilegeMode: String;
  ExitCode: Integer;
  ErrorCode: String;
  ErrorMessage: String;
  ErrorType: String;
  ErrorId: String;
  ErrorCategory: String;
  ErrorScript: String;
  ErrorLine: String;
  ErrorColumn: String;
  ErrorStack: String;
  DeleteTokenFile: Boolean;
begin
  Result := '';
  InstallProgressPage.Show;
  try
    InstallProgressPage.SetText(GetLocalizedMessage('OfflineProgressPreparing'), '');
    InstallProgressPage.SetProgress(1, 4);
    ExtractTemporaryFile('install.ps1');
    ExtractTemporaryFile('launch-windows-process.ps1');
    ExtractTemporaryFile('agentdock_windows_{#PayloadArchitecture}.zip');
    ExtractTemporaryFile('agentdock_windows_{#PayloadArchitecture}.zip.sha256');
    ExtractTemporaryFile('cloudflared.exe');

    PowerShellPath := ExpandConstant('{sys}\WindowsPowerShell\v1.0\powershell.exe');
    InstallScriptPath := ExpandConstant('{tmp}\install.ps1');
    OfflineArchivePath := ExpandConstant('{tmp}\agentdock_windows_{#PayloadArchitecture}.zip');
    OfflineChecksumPath := ExpandConstant('{tmp}\agentdock_windows_{#PayloadArchitecture}.zip.sha256');
    OfflineCloudflaredPath := ExpandConstant('{tmp}\cloudflared.exe');
    ResultFilePath := ExpandConstant('{tmp}\agentdock-install-result.ini');
    DeleteFile(ResultFilePath);
    TunnelMode := SelectedTunnelMode();
    if StartupPage.Values[1] then
      PrivilegeMode := 'elevated'
    else
      PrivilegeMode := 'standard';
    DeleteTokenFile := False;

    InstallProgressPage.SetProgress(2, 4);
    Parameters :=
      '-NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File ' + QuoteArgument(InstallScriptPath) +
      ' -Version ' + QuoteArgument('{#AppVersion}') +
      ' -OfflineArchive ' + QuoteArgument(OfflineArchivePath) +
      ' -OfflineChecksumFile ' + QuoteArgument(OfflineChecksumPath) +
      ' -OfflineCloudflaredBinary ' + QuoteArgument(OfflineCloudflaredPath) +
      ' -InstallDir ' + QuoteArgument(ExpandConstant('{app}\bin')) +
      ' -TunnelMode ' + TunnelMode +
      ' -InstallChannel setup' +
      ' -CorePrivilegeMode ' + PrivilegeMode +
      ' -ResultFile ' + QuoteArgument(ResultFilePath);

    if StartupPage.Values[0] or (TunnelMode <> 'none') then
      Parameters := Parameters + ' -RegisterStartup';

    if TunnelMode = 'named' then
    begin
      Parameters := Parameters + ' -ServerUrl ' + QuoteArgument(Trim(FixedTunnelPage.Values[0]));
      SilentTokenFile := ExpandConstant('{param:TUNNELTOKENFILE|}');
      if SilentTokenFile <> '' then
        TokenFilePath := SilentTokenFile
      else if Trim(FixedTunnelPage.Values[1]) <> '' then
      begin
        TokenFilePath := ExpandConstant('{tmp}\agentdock-tunnel-token.txt');
        TemporaryTokenFilePath := TokenFilePath;
        DeleteTokenFile := True;
        if not SaveStringToFile(TokenFilePath, Trim(FixedTunnelPage.Values[1]), False) then
        begin
          Result := GetLocalizedMessage('TokenFileFailed');
          Exit;
        end;
      end;
      if TokenFilePath <> '' then
        Parameters := Parameters + ' -TunnelTokenFile ' + QuoteArgument(TokenFilePath);
      if DeleteTokenFile then
        Parameters := Parameters + ' -DeleteTunnelTokenFile';
    end;

    InstallProgressPage.SetText(GetLocalizedMessage('OfflineProgressApplying'), '');
    InstallProgressPage.SetProgress(3, 4);
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
      if ErrorMessage = '' then
        ErrorMessage := GetLocalizedMessage('InstallerExitCode') + ' ' + IntToStr(ExitCode);
      Result := GetLocalizedMessage('InstallFailed') + ' ' + ErrorMessage;
      Exit;
    end;
    InstallWarningCode := GetIniString('AgentDock', 'WarningCode', '', ResultFilePath);
    if InstallWarningCode <> '' then
      Log('AgentDock installation warning: ' + InstallWarningCode);
    InstallProgressPage.SetText(GetLocalizedMessage('OfflineProgressFinishing'), '');
    InstallProgressPage.SetProgress(4, 4);
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
    WizardForm.FinishedLabel.Caption := GetLocalizedMessage('FinishedControlPanel');
    if InstallWarningCode = 'elevated-mode-fallback' then
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
  if TemporaryTokenFilePath <> '' then
    DeleteFile(TemporaryTokenFilePath);
end;
