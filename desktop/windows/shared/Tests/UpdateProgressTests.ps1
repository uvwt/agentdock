$ErrorActionPreference = 'Stop'
$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../../../..'))
$modelsPath = Join-Path $repoRoot 'desktop/windows/shared/Models/RuntimeModels.cs'
$modelsSource = [IO.File]::ReadAllText($modelsPath)
$testSource = @'
public static class UpdateProgressRegressionTests
{
    public static void Run()
    {
        // 1. 明确收到 handed_off 时应识别为 HandedOff 并记录目标版本，避免误弹“更新完成”。
        var handedOffTracker = new UpdateProgressStreamTracker();
        handedOffTracker.Observe(Deserialize("{\"schema_version\":1,\"type\":\"stage\",\"stage\":\"installing\",\"target_version\":\"v1.2.0\"}"));
        handedOffTracker.Observe(Deserialize("{\"schema_version\":1,\"type\":\"handed_off\",\"stage\":\"restarting\",\"current_version\":\"v1.1.1\",\"target_version\":\"v1.2.0\"}"));
        ExpectOutcome(UpdateRunOutcome.HandedOff, handedOffTracker.ResolveExitZeroOutcome());
        ExpectString("v1.2.0", handedOffTracker.TargetVersion);

        // 2. 收到 completed 时应识别为 Completed。
        var completedTracker = new UpdateProgressStreamTracker();
        completedTracker.Observe(Deserialize("{\"schema_version\":1,\"type\":\"stage\",\"stage\":\"installing\",\"target_version\":\"v1.2.0\"}"));
        completedTracker.Observe(Deserialize("{\"schema_version\":1,\"type\":\"completed\",\"target_version\":\"v1.2.0\"}"));
        ExpectOutcome(UpdateRunOutcome.Completed, completedTracker.ResolveExitZeroOutcome());

        // 3. 兼容旧版 Core：发出进度事件但未发 completed 即退出 0，一律按 HandedOff 处理。
        var legacyInstallerTracker = new UpdateProgressStreamTracker();
        legacyInstallerTracker.Observe(Deserialize("{\"schema_version\":1,\"type\":\"stage\",\"stage\":\"installing\",\"target_version\":\"v1.2.0\"}"));
        ExpectOutcome(UpdateRunOutcome.HandedOff, legacyInstallerTracker.ResolveExitZeroOutcome());

        // 4. 无任何进度事件退出 0 时回退为 Completed。
        var plainTracker = new UpdateProgressStreamTracker();
        ExpectOutcome(UpdateRunOutcome.Completed, plainTracker.ResolveExitZeroOutcome());

        // 5. 重启后安装器完成提示校验：版本一致且在窗口期内才提示。
        var now = new DateTimeOffset(2026, 4, 15, 12, 10, 0, TimeSpan.Zero);
        ExpectBool(true, UpdateProgressStreamTracker.ShouldNotifyInstallerUpdateCompletion("v1.2.0", "2026-04-15T12:00:00Z", "1.2.0", now));
        ExpectBool(false, UpdateProgressStreamTracker.ShouldNotifyInstallerUpdateCompletion("v1.2.0", "2026-04-15T12:00:00Z", "1.1.1", now));
        ExpectBool(false, UpdateProgressStreamTracker.ShouldNotifyInstallerUpdateCompletion("v1.2.0", "2026-04-15T11:00:00Z", "v1.2.0", now));
        ExpectBool(false, UpdateProgressStreamTracker.ShouldNotifyInstallerUpdateCompletion("", "2026-04-15T12:00:00Z", "v1.2.0", now));

        System.Console.WriteLine("Windows update progress handoff & completion marker tests passed.");
    }

    private static UpdateProgressEvent Deserialize(string json) =>
        System.Text.Json.JsonSerializer.Deserialize<UpdateProgressEvent>(json)!;

    private static void ExpectOutcome(UpdateRunOutcome expected, UpdateRunOutcome actual)
    {
        if (expected != actual)
            throw new System.InvalidOperationException("Expected outcome " + expected + ", got " + actual);
    }

    private static void ExpectString(string expected, string actual)
    {
        if (!System.String.Equals(expected, actual, System.StringComparison.Ordinal))
            throw new System.InvalidOperationException("Expected " + expected + ", got " + actual);
    }

    private static void ExpectBool(bool expected, bool actual)
    {
        if (expected != actual)
            throw new System.InvalidOperationException("Expected " + expected + ", got " + actual);
    }
}
'@
$source = "using System;`nusing System.IO;`nusing System.Collections.Generic;`n" + $modelsSource + "`n" + $testSource
$references = @(Get-ChildItem (Join-Path $PSHOME 'ref') -Filter '*.dll' | ForEach-Object FullName)
Add-Type -TypeDefinition $source -ReferencedAssemblies $references -CompilerOptions '/nullable:enable' -WarningAction SilentlyContinue
[AgentDock.ControlPanel.UpdateProgressRegressionTests]::Run()
