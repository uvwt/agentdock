using System.Diagnostics;
using System.IO;
using System.Runtime.InteropServices;
using System.Security.Principal;
using System.Text.Json;

namespace AgentDock.ControlPanel;

internal static class TaskAdminService
{
    private const string TaskName = "AgentDock";
    private const int TaskActionExec = 0;
    private const int TaskTriggerLogon = 9;
    private const int TaskCreateOrUpdate = 6;
    private const int TaskLogonInteractiveToken = 3;
    private const int TaskRunLevelHighest = 1;
    private const int TaskInstancesIgnoreNew = 2;
    private const int DaclSecurityInformation = 0x4;

    internal static int Run(string[] arguments)
    {
        try
        {
            var request = Parse(arguments);
            EnsureAdministrator();
            if (request.Action is "prepare-elevated" or "set-enabled")
            {
                EnsureSameWindowsUser(request.UserSid);
            }

            using var scheduler = new SchedulerSession();
            switch (request.Action)
            {
                case "prepare-elevated":
                    RequireBackupDirectory(request);
                    RequireRuntimeRoot(request);
                    SaveBackup(scheduler.Root, request.BackupDirectory);
                    try
                    {
                        RemoveTask(scheduler.Root);
                        StopInstalledCore(request.RuntimeRoot);
                        CreateElevatedTask(scheduler.Service, scheduler.Root, request);
                    }
                    catch
                    {
                        RestoreBackup(scheduler.Root, request.BackupDirectory);
                        throw;
                    }
                    break;
                case "prepare-standard":
                    RequireBackupDirectory(request);
                    RequireRuntimeRoot(request);
                    SaveBackup(scheduler.Root, request.BackupDirectory);
                    try
                    {
                        RemoveTask(scheduler.Root);
                        StopInstalledCore(request.RuntimeRoot);
                    }
                    catch
                    {
                        RestoreBackup(scheduler.Root, request.BackupDirectory);
                        throw;
                    }
                    break;
                case "restore":
                    RequireBackupDirectory(request);
                    RestoreBackup(scheduler.Root, request.BackupDirectory);
                    break;
                case "remove":
                    RequireRuntimeRoot(request);
                    RemoveTask(scheduler.Root);
                    StopInstalledCore(request.RuntimeRoot);
                    break;
                case "set-enabled":
                    SetTaskEnabled(scheduler.Root, request.Enabled);
                    break;
                default:
                    throw new InvalidOperationException(UiText.Format("UnsupportedTaskAdminAction", request.Action));
            }
            return 0;
        }
        catch (Exception ex)
        {
            Console.Error.WriteLine(ex.Message);
            return 1;
        }
    }

    private static TaskAdminRequest Parse(string[] arguments)
    {
        var action = ReadArgument(arguments, "--task-admin");
        if (action is not ("prepare-elevated" or "prepare-standard" or "restore" or "remove" or "set-enabled"))
        {
            throw new InvalidOperationException(UiText.Get("InvalidTaskAdminAction"));
        }
        return new TaskAdminRequest(
            action,
            ReadArgument(arguments, "--backup-directory", required: false),
            ReadArgument(arguments, "--launcher-path", required: false),
            ReadArgument(arguments, "--runtime-root", required: false),
            ReadArgument(arguments, "--user-sid", required: false),
            ReadArgument(arguments, "--user-name", required: false),
            ReadOptionalBoolArgument(arguments, "--enabled"));
    }

    private static string ReadArgument(string[] arguments, string name, bool required = true)
    {
        for (var index = 0; index < arguments.Length - 1; index++)
        {
            if (string.Equals(arguments[index], name, StringComparison.OrdinalIgnoreCase))
            {
                var value = arguments[index + 1];
                if (!string.IsNullOrWhiteSpace(value))
                {
                    return value;
                }
                break;
            }
        }
        if (required)
        {
            throw new InvalidOperationException(UiText.Format("MissingAdminArgument", name));
        }
        return "";
    }

    private static void EnsureAdministrator()
    {
        using var identity = WindowsIdentity.GetCurrent();
        var principal = new WindowsPrincipal(identity);
        if (!principal.IsInRole(WindowsBuiltInRole.Administrator))
        {
            throw new InvalidOperationException(UiText.Get("TaskAdminRequiresElevation"));
        }
    }

    private static void EnsureSameWindowsUser(string expectedSid)
    {
        if (string.IsNullOrWhiteSpace(expectedSid))
        {
            throw new InvalidOperationException(UiText.Get("ElevatedModeMissingSid"));
        }
        using var identity = WindowsIdentity.GetCurrent();
        if (!string.Equals(identity.User?.Value, expectedSid, StringComparison.OrdinalIgnoreCase))
        {
            throw new InvalidOperationException(UiText.Get("ElevatedModeRequiresCurrentUserUac"));
        }
    }

    private static bool? ReadOptionalBoolArgument(string[] arguments, string name)
    {
        var value = ReadArgument(arguments, name, required: false);
        if (string.IsNullOrWhiteSpace(value))
        {
            return null;
        }
        if (!bool.TryParse(value, out var parsed))
        {
            throw new InvalidOperationException(UiText.Format("AdminArgumentBoolean", name));
        }
        return parsed;
    }

    private static void SetTaskEnabled(dynamic root, bool? enabled)
    {
        if (enabled is null)
        {
            throw new InvalidOperationException(UiText.Get("TaskEnabledStateRequired"));
        }
        dynamic? task = FindTask(root);
        if (task is null)
        {
            throw new InvalidOperationException(UiText.Get("ScheduledTaskMissing"));
        }
        task.Enabled = enabled.Value;
    }

    private static void RequireBackupDirectory(TaskAdminRequest request)
    {
        if (string.IsNullOrWhiteSpace(request.BackupDirectory))
        {
            throw new InvalidOperationException(UiText.Get("TaskBackupDirectoryRequired"));
        }
    }

    private static void RequireRuntimeRoot(TaskAdminRequest request)
    {
        if (string.IsNullOrWhiteSpace(request.RuntimeRoot))
        {
            throw new InvalidOperationException(UiText.Get("RuntimeDirectoryRequired"));
        }
    }

    private static void StopInstalledCore(string runtimeRoot)
    {
        var expectedBinary = Path.GetFullPath(Path.Combine(runtimeRoot, "bin", "agentdock.exe"));
        var deadline = DateTime.UtcNow.AddSeconds(15);

        // 升级 helper 已处于 High Integrity；这里按完整路径只终止当前安装的 Core，
        // 兜底清理旧任务实现或异常退出 host 遗留的 elevated 孤儿进程。
        while (true)
        {
            var foundTarget = false;
            foreach (var process in Process.GetProcessesByName("agentdock"))
            {
                using (process)
                {
                    string? processPath;
                    try
                    {
                        processPath = process.MainModule?.FileName;
                    }
                    catch (System.ComponentModel.Win32Exception)
                    {
                        continue;
                    }
                    catch (InvalidOperationException)
                    {
                        continue;
                    }

                    if (string.IsNullOrWhiteSpace(processPath) ||
                        !string.Equals(Path.GetFullPath(processPath), expectedBinary, StringComparison.OrdinalIgnoreCase))
                    {
                        continue;
                    }

                    foundTarget = true;
                    try
                    {
                        process.Kill(entireProcessTree: true);
                    }
                    catch (InvalidOperationException)
                    {
                        // 进程可能在枚举后自行退出，下一轮会重新确认。
                    }
                }
            }

            if (!foundTarget)
            {
                return;
            }
            if (DateTime.UtcNow >= deadline)
            {
                throw new InvalidOperationException(UiText.Format("StopCoreFailed", expectedBinary));
            }
            Thread.Sleep(250);
        }
    }

    private static void SaveBackup(dynamic root, string backupDirectory)
    {
        Directory.CreateDirectory(backupDirectory);
        dynamic? task = FindTask(root);
        var state = new TaskBackupState();
        if (task is not null)
        {
            state.Exists = true;
            state.WasEnabled = task.Enabled;
            state.WasRunning = Convert.ToInt32(task.State) == 4;
            try
            {
                state.SecurityDescriptor = task.GetSecurityDescriptor(DaclSecurityInformation);
            }
            catch (COMException)
            {
                state.SecurityDescriptor = "";
            }
            File.WriteAllText(
                Path.Combine(backupDirectory, "task.xml"),
                (string)task.Xml,
                System.Text.Encoding.Unicode);
        }
        File.WriteAllText(
            Path.Combine(backupDirectory, "state.json"),
            JsonSerializer.Serialize(state),
            new System.Text.UTF8Encoding(false));
    }

    private static void RestoreBackup(dynamic root, string backupDirectory)
    {
        var statePath = Path.Combine(backupDirectory, "state.json");
        if (!File.Exists(statePath))
        {
            throw new InvalidOperationException(UiText.Format("TaskBackupStateMissing", statePath));
        }
        var state = JsonSerializer.Deserialize<TaskBackupState>(File.ReadAllText(statePath))
            ?? throw new InvalidOperationException(UiText.Get("TaskBackupStateReadFailed"));

        RemoveTask(root);
        if (!state.Exists)
        {
            return;
        }

        var xmlPath = Path.Combine(backupDirectory, "task.xml");
        if (!File.Exists(xmlPath))
        {
            throw new InvalidOperationException(UiText.Format("TaskBackupXmlMissing", xmlPath));
        }
        var xml = File.ReadAllText(xmlPath);
        var userId = ReadTaskUserId(xml);
        dynamic task = root.RegisterTask(
            TaskName,
            xml,
            TaskCreateOrUpdate,
            userId,
            null,
            TaskLogonInteractiveToken,
            null);
        task.Enabled = state.WasEnabled;
        if (!string.IsNullOrWhiteSpace(state.SecurityDescriptor))
        {
            task.SetSecurityDescriptor(state.SecurityDescriptor, 0);
        }
        if (state.WasRunning)
        {
            task.Run(null);
        }
    }

    private static string ReadTaskUserId(string xml)
    {
        var document = System.Xml.Linq.XDocument.Parse(xml);
        var userId = document.Descendants()
            .FirstOrDefault(element => element.Name.LocalName == "UserId")?.Value;
        if (string.IsNullOrWhiteSpace(userId))
        {
            throw new InvalidOperationException(UiText.Get("TaskBackupUserMissing"));
        }
        return userId;
    }

    private static void RemoveTask(dynamic root)
    {
        dynamic? task = FindTask(root);
        if (task is null)
        {
            return;
        }
        try
        {
            task.Stop(0);
        }
        catch (COMException)
        {
            // 任务可能已经退出；删除操作仍应继续。
        }
        root.DeleteTask(TaskName, 0);
    }

    private static dynamic? FindTask(dynamic root)
    {
        dynamic tasks = root.GetTasks(0);
        foreach (dynamic task in tasks)
        {
            if (string.Equals((string)task.Name, TaskName, StringComparison.OrdinalIgnoreCase))
            {
                return task;
            }
        }
        return null;
    }

    private static void CreateElevatedTask(dynamic service, dynamic root, TaskAdminRequest request)
    {
        if (string.IsNullOrWhiteSpace(request.LauncherPath) ||
            string.IsNullOrWhiteSpace(request.RuntimeRoot) ||
            string.IsNullOrWhiteSpace(request.UserSid) ||
            string.IsNullOrWhiteSpace(request.UserName))
        {
            throw new InvalidOperationException(UiText.Get("ElevatedTaskArgumentsIncomplete"));
        }
        dynamic definition = service.NewTask(0);
        definition.RegistrationInfo.Description = "AgentDock privileged core service for the current desktop user.";
        definition.Settings.DisallowStartIfOnBatteries = false;
        definition.Settings.StopIfGoingOnBatteries = false;
        definition.Settings.StartWhenAvailable = true;
        definition.Settings.RestartCount = 3;
        definition.Settings.RestartInterval = "PT1M";
        definition.Settings.ExecutionTimeLimit = "PT0S";
        definition.Settings.MultipleInstances = TaskInstancesIgnoreNew;

        dynamic principal = definition.Principal;
        principal.UserId = request.UserSid;
        principal.LogonType = TaskLogonInteractiveToken;
        principal.RunLevel = TaskRunLevelHighest;

        dynamic trigger = definition.Triggers.Create(TaskTriggerLogon);
        trigger.UserId = request.UserName;

        dynamic action = definition.Actions.Create(TaskActionExec);
        action.Path = Path.GetFullPath(request.LauncherPath);
        action.Arguments = $"--run-core-task --runtime-root \"{Path.GetFullPath(request.RuntimeRoot)}\"";

        dynamic task = root.RegisterTaskDefinition(
            TaskName,
            definition,
            TaskCreateOrUpdate,
            request.UserSid,
            null,
            TaskLogonInteractiveToken,
            null);
        task.Enabled = false;
        task.SetSecurityDescriptor(
            $"D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;GA;;;{request.UserSid})",
            0);
    }

    private sealed class SchedulerSession : IDisposable
    {
        internal SchedulerSession()
        {
            var schedulerType = Type.GetTypeFromProgID("Schedule.Service")
                ?? throw new InvalidOperationException(UiText.Get("TaskSchedulerComUnavailable"));
            Service = Activator.CreateInstance(schedulerType)
                ?? throw new InvalidOperationException(UiText.Get("TaskSchedulerConnectFailed"));
            Service.Connect();
            Root = Service.GetFolder("\\");
        }

        internal dynamic Service { get; }
        internal dynamic Root { get; }

        public void Dispose()
        {
            if (Marshal.IsComObject(Root))
            {
                Marshal.FinalReleaseComObject(Root);
            }
            if (Marshal.IsComObject(Service))
            {
                Marshal.FinalReleaseComObject(Service);
            }
        }
    }

    private sealed record TaskAdminRequest(
        string Action,
        string BackupDirectory,
        string LauncherPath,
        string RuntimeRoot,
        string UserSid,
        string UserName,
        bool? Enabled);

    private sealed class TaskBackupState
    {
        public bool Exists { get; set; }
        public bool WasEnabled { get; set; }
        public bool WasRunning { get; set; }
        public string SecurityDescriptor { get; set; } = "";
    }
}
