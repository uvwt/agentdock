using System.ComponentModel;
using System.Diagnostics;
using System.IO;
using System.Net.Http;
using System.Security.Cryptography;
using System.Security.Principal;
using System.Text;
using System.Text.Json;
using System.Text.Json.Nodes;
using System.Xml.Linq;
using Microsoft.Win32;

namespace AgentDock.ControlPanel;

public sealed class RuntimeService : IDisposable
{
    private const string AuthEntropy = "agentdock.startup.v1";
    private const string OAuthPasswordEntropy = "agentdock.oauth.password.v1";
    private const string TunnelTokenEntropy = "agentdock.cloudflare.tunnel.v1";
    private const string RunKeyPath = @"Software\Microsoft\Windows\CurrentVersion\Run";

    private static readonly JsonSerializerOptions JsonOptions = new()
    {
        PropertyNameCaseInsensitive = true,
        WriteIndented = true
    };

    private readonly HttpClient _httpClient = new() { Timeout = TimeSpan.FromSeconds(10) };

    public RuntimeService(string? runtimeRoot = null)
    {
        RuntimeRoot = string.IsNullOrWhiteSpace(runtimeRoot)
            ? ResolveRuntimeRoot()
            : Path.GetFullPath(runtimeRoot);
    }

    public string RuntimeRoot { get; }
    public string ManifestPath => Path.Combine(RuntimeRoot, "runtime.json");
    public string SettingsPath => Path.Combine(RuntimeRoot, "control-panel-settings.json");
    public string LogsDirectory => Path.Combine(RuntimeRoot, "logs");
    public string ConfigDirectory => RuntimeRoot;

    public async Task<RuntimeSnapshot> GetSnapshotAsync(
        CancellationToken cancellationToken = default,
        bool includeNexusConnection = false)
    {
        var manifest = await ReadRuntimeManifestAsync(cancellationToken) ?? new RuntimeManifest();
        var manifestPort = manifest.ListenPort is >= 1 and <= 65535 ? manifest.ListenPort : 8765;
        var settings = await ReadJsonAsync<ControlPanelSettings>(SettingsPath, cancellationToken);
        if (settings is null)
        {
            settings = new ControlPanelSettings { Port = manifestPort };
        }
        else if (settings.Port is < 1 or > 65535)
        {
            settings.Port = manifestPort;
        }

        if (string.IsNullOrWhiteSpace(settings.LogLevel))
        {
            settings.LogLevel = "info";
        }
        settings.AcpAgent = NormalizeAcpAgent(settings.AcpAgent);
        settings.AcpCommand ??= "";
        settings.AcpArgs ??= [];

        var localOrigin = $"http://127.0.0.1:{settings.Port}";
        var localMcpUrl = localOrigin + "/mcp";
        var publicOrigin = ReadFirstNonEmpty(
            Path.Combine(RuntimeRoot, "quick-tunnel-url.txt"),
            Path.Combine(RuntimeRoot, "server-url.txt"));
        if (string.IsNullOrWhiteSpace(publicOrigin))
        {
            publicOrigin = manifest.PublicUrl;
        }
        if (string.IsNullOrWhiteSpace(publicOrigin) && Uri.TryCreate(manifest.PublicMcpUrl, UriKind.Absolute, out var manifestPublicUri))
        {
            publicOrigin = manifestPublicUri.GetLeftPart(UriPartial.Authority);
        }

        publicOrigin = publicOrigin.TrimEnd('/');
        var publicMcpUrl = string.IsNullOrWhiteSpace(publicOrigin) ? "" : publicOrigin + "/mcp";
        var savedNamedOrigin = ReadText(Path.Combine(RuntimeRoot, "named-server-url.txt")).TrimEnd('/');
        var binaryPath = ResolveCoreBinaryPath(manifest);
        var health = await ReadHealthAsync(localOrigin, cancellationToken);
        var version = health.Version;
        if (string.IsNullOrWhiteSpace(version))
        {
            version = await ReadCoreVersionAsync(binaryPath, cancellationToken);
        }
        var coreRunning = health.Healthy || IsProcessRunningAtPath("agentdock", binaryPath);
        var nexus = ReadNexusDeviceStatus();
        var nexusConnected = includeNexusConnection && coreRunning && nexus.Paired && string.IsNullOrWhiteSpace(nexus.Error)
            && await ReadNexusConnectionAsync(binaryPath, cancellationToken);
        var cloudflaredRunning = IsProcessRunningAtPath("cloudflared", manifest.CloudflaredBinary);
        var tunnelMode = ReadText(Path.Combine(RuntimeRoot, "cloudflared-mode.txt"));
        if (string.IsNullOrWhiteSpace(tunnelMode))
        {
            tunnelMode = string.IsNullOrWhiteSpace(manifest.TunnelMode) ? "none" : manifest.TunnelMode;
        }

        return new RuntimeSnapshot(
            manifest,
            settings,
            version,
            coreRunning,
            health.Healthy,
            cloudflaredRunning,
            localMcpUrl,
            publicOrigin,
            publicMcpUrl,
            savedNamedOrigin,
            tunnelMode,
            IsCoreStartupEnabled(manifest),
            IsRunValuePresent(manifest.TrayStartupValueName, "AgentDockTray"),
            File.Exists(Path.Combine(RuntimeRoot, "cloudflared-token.dpapi")),
            nexus,
            nexusConnected,
            DateTimeOffset.Now);
    }

    public string ReadBearerToken() => ReadProtectedText(Path.Combine(RuntimeRoot, "auth-token.dpapi"), AuthEntropy);
    public string ReadOAuthPassword() => ReadProtectedText(Path.Combine(RuntimeRoot, "oauth-password.dpapi"), OAuthPasswordEntropy);
    public string ReadTunnelToken() => ReadProtectedText(Path.Combine(RuntimeRoot, "cloudflared-token.dpapi"), TunnelTokenEntropy);
    public AcpAdapterResolution ResolveAcpAdapter(
        string agent,
        string configuredCommand = "",
        IReadOnlyList<string>? configuredArguments = null) =>
        AcpAdapterResolver.Resolve(agent, RuntimeRoot, configuredCommand, configuredArguments);

    public async Task RunActionAsync(string action, CancellationToken cancellationToken = default)
    {
        switch (action)
        {
            case "start":
                await RunCoreActionAsync("start", cancellationToken);
                await RunTunnelActionAsync("start", cancellationToken);
                break;
            case "stop":
                await RunTunnelActionAsync("stop", cancellationToken);
                await RunCoreActionAsync("stop", cancellationToken);
                break;
            case "restart":
                var mode = ReadText(Path.Combine(RuntimeRoot, "cloudflared-mode.txt")).ToLowerInvariant();
                if (mode == "quick")
                {
                    // Quick Tunnel 重建会清理旧地址、重启核心、等待新地址并再次应用 OAuth Origin。
                    await RunTunnelActionAsync("regenerate", cancellationToken);
                }
                else
                {
                    await RunTunnelActionAsync("stop", cancellationToken);
                    await RunCoreActionAsync("restart", cancellationToken);
                    await RunTunnelActionAsync("start", cancellationToken);
                }
                break;
            default:
                throw new ArgumentOutOfRangeException(nameof(action), action, "不支持的运行时操作。");
        }
    }

    public async Task<UpdateCheckResult> CheckForUpdatesAsync(CancellationToken cancellationToken = default)
    {
        var binaryPath = await ResolveCoreBinaryAsync(cancellationToken);
        var startInfo = CreateRedirectedProcessStartInfo(binaryPath);
        startInfo.ArgumentList.Add("update");
        startInfo.ArgumentList.Add("--check");
        var output = await RunProcessAsync(startInfo, cancellationToken);
        try
        {
            return JsonSerializer.Deserialize<UpdateCheckResult>(output, JsonOptions)
                ?? throw new JsonException("更新检查结果为空");
        }
        catch (JsonException ex)
        {
            throw new InvalidOperationException("无法解析 AgentDock 更新检查结果。", ex);
        }
    }

    public async Task<string> RunUpdateAsync(
        IProgress<UpdateProgress>? progress,
        CancellationToken cancellationToken = default)
    {
        var binaryPath = await ResolveCoreBinaryAsync(cancellationToken);
        var startInfo = CreateRedirectedProcessStartInfo(binaryPath);
        startInfo.ArgumentList.Add("update");
        return await RunUpdateProcessAsync(startInfo, progress, cancellationToken);
    }

    public async Task SetTunnelModeAsync(
        string mode,
        string serverUrl,
        string tunnelToken,
        CancellationToken cancellationToken = default)
    {
        var arguments = new List<string>
        {
            "configure",
            "--mode", mode,
            "--server-url", serverUrl ?? ""
        };
        string? secretFile = null;
        try
        {
            if (!string.IsNullOrWhiteSpace(tunnelToken))
            {
                secretFile = await WriteSecretFileAsync(tunnelToken, cancellationToken);
                arguments.AddRange(["--token-file", secretFile]);
            }

            await RunNativeAgentDockAsync("tunnel", arguments, cancellationToken);
        }
        finally
        {
            DeleteSecretFile(secretFile);
        }
    }

    public Task RegenerateQuickTunnelAsync(CancellationToken cancellationToken = default) =>
        RunTunnelActionAsync("regenerate", cancellationToken);

    public async Task SaveSettingsAsync(
        ControlPanelSettings settings,
        CancellationToken cancellationToken = default)
    {
        var arguments = new List<string>
        {
            "update",
            "--port", settings.Port.ToString(),
            "--log-level", settings.LogLevel,
            "--oauth-access-token-ttl", settings.OAuthAccessTokenTtl ?? "",
            $"--mcp-apps-enabled={settings.McpAppsEnabled.ToString().ToLowerInvariant()}",
            $"--browser-enabled={settings.BrowserEnabled.ToString().ToLowerInvariant()}",
            "--browser-cdp-url", settings.BrowserCdpUrl ?? "",
            $"--browser-reuse-existing-cdp={settings.BrowserReuseExistingCdp.ToString().ToLowerInvariant()}",
            $"--acp-enabled={settings.AcpEnabled.ToString().ToLowerInvariant()}",
            "--acp-agent", NormalizeAcpAgent(settings.AcpAgent),
            "--acp-command", settings.AcpCommand ?? "",
            "--acp-args-json", JsonSerializer.Serialize(settings.AcpArgs ?? [])
        };
        await RunNativeAgentDockAsync("config", arguments, cancellationToken);
    }

    public async Task PairNexusAsync(string endpoint, string pairingCode, CancellationToken cancellationToken = default)
    {
        endpoint = endpoint.Trim();
        pairingCode = pairingCode.Trim();
        if (endpoint.Length == 0 || pairingCode.Length == 0)
        {
            throw new InvalidOperationException("NexusDock 地址和一次性配对码不能为空。");
        }

        var binaryPath = await ResolveCoreBinaryAsync(cancellationToken);
        var startInfo = CreateRedirectedProcessStartInfo(binaryPath);
        foreach (var argument in new[] { "nexus", "pair", "--endpoint", endpoint, "--code", pairingCode })
        {
            startInfo.ArgumentList.Add(argument);
        }
        _ = await RunProcessAsync(startInfo, cancellationToken);
        await RunCoreActionAsync("restart", cancellationToken);
    }

    public Task SetStartupAsync(string component, bool enabled, CancellationToken cancellationToken = default)
    {
        if (component is not ("core" or "tray"))
        {
            throw new ArgumentOutOfRangeException(nameof(component), component, "不支持的开机启动组件。");
        }
        return RunNativeAgentDockAsync(
            "service",
            ["autostart", "--component", component, "--enabled", enabled ? "true" : "false"],
            cancellationToken);
    }

    public async Task SetPrivilegeModeAsync(bool elevated, CancellationToken cancellationToken = default)
    {
        var manifest = await ReadRuntimeManifestAsync(cancellationToken)
            ?? throw new InvalidOperationException("找不到 AgentDock runtime.json。");
        var wasElevated = string.Equals(manifest.PrivilegeMode, "elevated", StringComparison.OrdinalIgnoreCase);
        if (wasElevated == elevated)
        {
            return;
        }

        var snapshot = await GetSnapshotAsync(cancellationToken);
        var backupDirectory = Path.Combine(Path.GetTempPath(), $"agentdock-privilege-{Guid.NewGuid():N}");
        var taskTransitionPrepared = false;
        Directory.CreateDirectory(backupDirectory);

        try
        {
            await RunTaskAdminTransitionAsync(
                elevated ? "prepare-elevated" : "prepare-standard",
                manifest,
                backupDirectory,
                cancellationToken);
            taskTransitionPrepared = true;

            // 任务迁移完成后再切换 manifest，避免普通控制链提前把半完成状态当成新模式。
            await WritePrivilegeModeAsync(elevated, cancellationToken);
            if (elevated)
            {
                SetStandardCoreStartup(manifest, enabled: false);

                // 任务创建时默认禁用。若 Core 当前正在运行，则临时启用任务用于启动；
                // 最后再恢复原本的开机启动选择，从而让“当前运行”和“开机启动”保持彼此独立。
                if (snapshot.CoreStartupEnabled || snapshot.CoreRunning)
                {
                    await SetStartupAsync("core", true, cancellationToken);
                }
                if (snapshot.CoreRunning)
                {
                    await RunCoreActionAsync("start", cancellationToken);
                }
                if (!snapshot.CoreStartupEnabled)
                {
                    await SetStartupAsync("core", false, cancellationToken);
                }
            }
            else
            {
                SetStandardCoreStartup(manifest, snapshot.CoreStartupEnabled);
                if (snapshot.CoreRunning)
                {
                    await RunCoreActionAsync("start", cancellationToken);
                }
            }
        }
        catch (Exception transitionError)
        {
            if (!taskTransitionPrepared)
            {
                throw;
            }

            try
            {
                await RunTaskAdminTransitionAsync("restore", manifest, backupDirectory, cancellationToken);
                await WritePrivilegeModeAsync(wasElevated, cancellationToken);
                SetStandardCoreStartup(manifest, !wasElevated && snapshot.CoreStartupEnabled);
                if (!wasElevated && snapshot.CoreRunning)
                {
                    await RunCoreActionAsync("start", cancellationToken);
                }
            }
            catch (Exception rollbackError)
            {
                throw new AggregateException("切换 AgentDock 管理员模式失败，且回滚未完成。", transitionError, rollbackError);
            }
            throw;
        }
        finally
        {
            try
            {
                Directory.Delete(backupDirectory, recursive: true);
            }
            catch
            {
            }
        }
    }

    public async Task<UrlTestResult> TestUrlAsync(string value, CancellationToken cancellationToken = default)
    {
        if (!Uri.TryCreate(value, UriKind.Absolute, out var uri) ||
            (uri.Scheme != Uri.UriSchemeHttp && uri.Scheme != Uri.UriSchemeHttps))
        {
            return new UrlTestResult(false, null, TimeSpan.Zero, "地址无效");
        }

        var healthUri = new UriBuilder(uri) { Path = "/healthz", Query = "", Fragment = "" }.Uri;
        var stopwatch = Stopwatch.StartNew();
        try
        {
            using var request = new HttpRequestMessage(HttpMethod.Get, healthUri);
            using var response = await _httpClient.SendAsync(request, HttpCompletionOption.ResponseHeadersRead, cancellationToken);
            stopwatch.Stop();
            var success = response.IsSuccessStatusCode;
            return new UrlTestResult(
                success,
                (int)response.StatusCode,
                stopwatch.Elapsed,
                success ? $"访问正常 · {(int)response.StatusCode} · {stopwatch.ElapsedMilliseconds} ms" : $"访问失败 · {(int)response.StatusCode}");
        }
        catch (Exception ex) when (ex is HttpRequestException or TaskCanceledException)
        {
            stopwatch.Stop();
            return new UrlTestResult(false, null, stopwatch.Elapsed, ex is TaskCanceledException ? "访问超时" : ex.Message);
        }
    }

    public void OpenLogsDirectory() => OpenDirectory(LogsDirectory);
    public void OpenConfigDirectory() => OpenDirectory(ConfigDirectory);

    private async Task<RuntimeManifest?> ReadRuntimeManifestAsync(CancellationToken cancellationToken)
    {
        var manifest = await ReadJsonAsync<RuntimeManifest>(ManifestPath, cancellationToken);
        if (manifest is null)
        {
            return null;
        }

        // runtime.json 的实际目录才是当前安装位置。打包应用文件系统重定向可能让
        // 安装时记录的绝对路径失效，因此先把属于旧 install_root 的路径整体重定位。
        var recordedRoot = manifest.InstallRoot;
        manifest.BinaryPath = ResolveRuntimeManagedPath(
            recordedRoot,
            manifest.BinaryPath,
            Path.Combine("bin", "agentdock.exe"),
            required: true);
        manifest.TrayBinaryPath = ResolveRuntimeManagedPath(
            recordedRoot,
            manifest.TrayBinaryPath,
            Path.Combine("bin", "agentdock-tray.exe"));
        manifest.LauncherPath = ResolveRuntimeManagedPath(
            recordedRoot,
            manifest.LauncherPath,
            "start-agentdock.ps1");
        manifest.CloudflaredBinary = ResolveRuntimeManagedPath(
            recordedRoot,
            manifest.CloudflaredBinary,
            Path.Combine("bin", "cloudflared.exe"));
        manifest.CloudflaredLauncher = ResolveRuntimeManagedPath(
            recordedRoot,
            manifest.CloudflaredLauncher,
            "start-cloudflared.ps1");
        manifest.InstallRoot = RuntimeRoot;
        return manifest;
    }

    private string ResolveRuntimeManagedPath(
        string? recordedRoot,
        string? recordedPath,
        string fallbackRelativePath,
        bool required = false)
    {
        var fallbackPath = Path.GetFullPath(Path.Combine(RuntimeRoot, fallbackRelativePath));
        var candidate = recordedPath?.Trim() ?? "";
        if (string.IsNullOrWhiteSpace(candidate))
        {
            return required || File.Exists(fallbackPath) ? fallbackPath : "";
        }

        var managedPath = IsPathWithinRoot(RuntimeRoot, candidate) ||
            (!string.IsNullOrWhiteSpace(recordedRoot) && IsPathWithinRoot(recordedRoot, candidate));
        if (!string.IsNullOrWhiteSpace(recordedRoot) &&
            IsPathWithinRoot(recordedRoot, candidate) &&
            !PathsEqual(recordedRoot, RuntimeRoot))
        {
            candidate = RebaseRuntimePath(recordedRoot, candidate);
        }

        if (File.Exists(candidate))
        {
            return Path.GetFullPath(candidate);
        }
        if (managedPath && File.Exists(fallbackPath))
        {
            return fallbackPath;
        }
        return candidate;
    }

    private static bool IsPathWithinRoot(string root, string path)
    {
        try
        {
            var relative = Path.GetRelativePath(Path.GetFullPath(root), Path.GetFullPath(path));
            return !Path.IsPathRooted(relative) &&
                !string.Equals(relative, "..", StringComparison.Ordinal) &&
                !relative.StartsWith($"..{Path.DirectorySeparatorChar}", StringComparison.Ordinal) &&
                !relative.StartsWith($"..{Path.AltDirectorySeparatorChar}", StringComparison.Ordinal) &&
                !string.Equals(relative, ".", StringComparison.Ordinal);
        }
        catch (Exception ex) when (ex is ArgumentException or NotSupportedException or PathTooLongException)
        {
            return false;
        }
    }

    private string RebaseRuntimePath(string recordedRoot, string recordedPath)
    {
        try
        {
            var relative = Path.GetRelativePath(Path.GetFullPath(recordedRoot), Path.GetFullPath(recordedPath));
            if (Path.IsPathRooted(relative) ||
                string.Equals(relative, "..", StringComparison.Ordinal) ||
                relative.StartsWith($"..{Path.DirectorySeparatorChar}", StringComparison.Ordinal) ||
                relative.StartsWith($"..{Path.AltDirectorySeparatorChar}", StringComparison.Ordinal))
            {
                return recordedPath;
            }
            return Path.GetFullPath(Path.Combine(RuntimeRoot, relative));
        }
        catch (Exception ex) when (ex is ArgumentException or NotSupportedException or PathTooLongException)
        {
            return recordedPath;
        }
    }

    private static bool PathsEqual(string left, string right)
    {
        try
        {
            return string.Equals(
                Path.TrimEndingDirectorySeparator(Path.GetFullPath(left)),
                Path.TrimEndingDirectorySeparator(Path.GetFullPath(right)),
                StringComparison.OrdinalIgnoreCase);
        }
        catch (Exception ex) when (ex is ArgumentException or NotSupportedException or PathTooLongException)
        {
            return false;
        }
    }

    private async Task<string> ResolveCoreBinaryAsync(CancellationToken cancellationToken)
    {
        var manifest = await ReadRuntimeManifestAsync(cancellationToken);
        var binaryPath = ResolveCoreBinaryPath(manifest);
        if (!File.Exists(binaryPath))
        {
            throw new FileNotFoundException($"找不到 AgentDock 核心程序（{binaryPath}），请运行 Setup.exe 修复安装。", binaryPath);
        }
        return binaryPath;
    }

    private string ResolveCoreBinaryPath(RuntimeManifest? manifest)
    {
        var rootRelativePath = Path.Combine(RuntimeRoot, "bin", "agentdock.exe");
        var binaryPath = manifest?.BinaryPath;
        if (string.IsNullOrWhiteSpace(binaryPath))
        {
            return rootRelativePath;
        }
        return !File.Exists(binaryPath) && File.Exists(rootRelativePath)
            ? rootRelativePath
            : binaryPath;
    }

    private async Task RunTaskAdminTransitionAsync(
        string action,
        RuntimeManifest manifest,
        string backupDirectory,
        CancellationToken cancellationToken)
    {
        var trayBinary = string.IsNullOrWhiteSpace(manifest.TrayBinaryPath)
            ? Path.Combine(RuntimeRoot, "bin", "agentdock-tray.exe")
            : manifest.TrayBinaryPath;
        if (!File.Exists(trayBinary))
        {
            throw new FileNotFoundException($"找不到 AgentDock 管理程序（{trayBinary}）。", trayBinary);
        }

        var arguments = new List<string>
        {
            "--task-admin", action,
            "--backup-directory", backupDirectory,
            "--runtime-root", RuntimeRoot
        };
        if (action == "prepare-elevated")
        {
            using var identity = WindowsIdentity.GetCurrent();
            var userSid = identity.User?.Value;
            if (string.IsNullOrWhiteSpace(userSid) || string.IsNullOrWhiteSpace(identity.Name))
            {
                throw new InvalidOperationException("无法读取当前 Windows 用户身份。");
            }
            arguments.AddRange([
                "--launcher-path", trayBinary,
                "--user-sid", userSid,
                "--user-name", identity.Name
            ]);
        }

        try
        {
            await RunElevatedProcessAsync(trayBinary, arguments, cancellationToken);
        }
        catch (Win32Exception ex) when (ex.NativeErrorCode == 1223)
        {
            throw new InvalidOperationException("已取消 AgentDock 管理员权限切换。", ex);
        }
    }

    private async Task WritePrivilegeModeAsync(bool elevated, CancellationToken cancellationToken)
    {
        var text = await File.ReadAllTextAsync(ManifestPath, cancellationToken);
        var manifest = JsonNode.Parse(text) as JsonObject
            ?? throw new InvalidOperationException("AgentDock runtime.json 格式无效。");
        manifest["privilege_mode"] = elevated ? "elevated" : "standard";
        manifest["agentdock_task_name"] = elevated ? "AgentDock" : "";

        var temporaryPath = ManifestPath + $".tmp.{Guid.NewGuid():N}";
        try
        {
            await File.WriteAllTextAsync(
                temporaryPath,
                manifest.ToJsonString(JsonOptions),
                new UTF8Encoding(false),
                cancellationToken);
            File.Move(temporaryPath, ManifestPath, overwrite: true);
        }
        finally
        {
            try
            {
                File.Delete(temporaryPath);
            }
            catch
            {
            }
        }
    }

    private void SetStandardCoreStartup(RuntimeManifest manifest, bool enabled)
    {
        var valueName = string.IsNullOrWhiteSpace(manifest.StartupValueName) ? "AgentDock" : manifest.StartupValueName;
        using var key = Registry.CurrentUser.CreateSubKey(RunKeyPath, writable: true)
            ?? throw new InvalidOperationException("无法打开 Windows 开机启动注册表。");
        if (!enabled)
        {
            key.DeleteValue(valueName, throwOnMissingValue: false);
            return;
        }

        var trayBinary = string.IsNullOrWhiteSpace(manifest.TrayBinaryPath)
            ? Path.Combine(RuntimeRoot, "bin", "agentdock-tray.exe")
            : manifest.TrayBinaryPath;
        if (!File.Exists(trayBinary))
        {
            throw new FileNotFoundException($"找不到 AgentDock 托盘程序（{trayBinary}）。", trayBinary);
        }
        var command = $"\"{trayBinary}\" --start-core --runtime-root \"{RuntimeRoot}\"";
        key.SetValue(valueName, command, RegistryValueKind.String);
    }

    internal async Task<int> RunElevatedCoreTaskAsync(CancellationToken cancellationToken = default)
    {
        var manifest = await ReadRuntimeManifestAsync(cancellationToken) ?? new RuntimeManifest();
        var binaryPath = ResolveCoreBinaryPath(manifest);
        if (!File.Exists(binaryPath))
        {
            throw new FileNotFoundException($"找不到 AgentDock 核心程序（{binaryPath}）。", binaryPath);
        }

        var workingDirectory = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.UserProfile), "AgentDock");
        Directory.CreateDirectory(workingDirectory);

        var startInfo = new ProcessStartInfo
        {
            FileName = binaryPath,
            WorkingDirectory = workingDirectory,
            UseShellExecute = false,
            CreateNoWindow = true,
            WindowStyle = ProcessWindowStyle.Hidden
        };
        startInfo.ArgumentList.Add("service");
        startInfo.ArgumentList.Add("launch-core");
        startInfo.ArgumentList.Add("--runtime-root");
        startInfo.ArgumentList.Add(RuntimeRoot);

        // Highest 计划任务运行 WinExe 托管进程，再由它无控制台启动核心并持续等待。
        // Core 加入 KILL_ON_JOB_CLOSE Job Object，确保 Task Scheduler 强制结束 host 时不会留下孤儿进程。
        using var job = KillOnCloseJob.Create();
        using var process = Process.Start(startInfo) ?? throw new InvalidOperationException("无法启动 AgentDock 核心程序。");
        try
        {
            job.Assign(process);
        }
        catch
        {
            process.Kill(entireProcessTree: true);
            throw;
        }
        // Core 自己持有受限轮转日志；宿主只负责生命周期，避免第二个追加句柄绕过大小上限。
        await process.WaitForExitAsync(cancellationToken);
        return process.ExitCode;
    }

    internal Task RunCoreStartupAsync(CancellationToken cancellationToken = default) =>
        RunNativeAgentDockAsync("service", ["start"], cancellationToken, allowElevation: false);

    internal Task RunTunnelStartupAsync(CancellationToken cancellationToken = default) =>
        RunNativeAgentDockAsync("tunnel", ["start"], cancellationToken, allowElevation: false);

    internal Task RunCoreActionAsync(string action, CancellationToken cancellationToken = default)
    {
        if (action is not ("start" or "stop" or "restart"))
        {
            throw new ArgumentOutOfRangeException(nameof(action), action, "不支持的核心服务操作。");
        }
        return RunNativeAgentDockAsync("service", [action], cancellationToken);
    }

    internal Task RunTunnelActionAsync(string action, CancellationToken cancellationToken = default)
    {
        if (action is not ("start" or "stop" or "restart" or "regenerate"))
        {
            throw new ArgumentOutOfRangeException(nameof(action), action, "不支持的 Tunnel 操作。");
        }
        return RunNativeAgentDockAsync("tunnel", [action], cancellationToken);
    }

    private async Task RunNativeAgentDockAsync(
        string command,
        IReadOnlyCollection<string> arguments,
        CancellationToken cancellationToken,
        bool allowElevation = true)
    {
        var manifest = await ReadRuntimeManifestAsync(cancellationToken) ?? new RuntimeManifest();
        var binaryPath = await ResolveCoreBinaryAsync(cancellationToken);
        var commandArguments = new List<string> { command };
        commandArguments.AddRange(arguments);
        commandArguments.AddRange(["--runtime-root", RuntimeRoot]);

        var startInfo = CreateRedirectedProcessStartInfo(binaryPath);
        foreach (var argument in commandArguments)
        {
            startInfo.ArgumentList.Add(argument);
        }

        try
        {
            _ = await RunProcessAsync(startInfo, cancellationToken);
        }
        catch (InvalidOperationException) when (
            allowElevation &&
            string.Equals(manifest.PrivilegeMode, "elevated", StringComparison.OrdinalIgnoreCase))
        {
            // 最高权限计划任务启动的核心进程不能保证允许普通托盘终止。
            // 原生命令真正失败时才请求 UAC；命令设计为幂等，可安全重试已完成的前置状态变更。
            await RunElevatedProcessAsync(binaryPath, commandArguments, cancellationToken);
        }
    }

    private static async Task RunElevatedProcessAsync(
        string binaryPath,
        IReadOnlyCollection<string> arguments,
        CancellationToken cancellationToken)
    {
        var startInfo = new ProcessStartInfo
        {
            FileName = binaryPath,
            UseShellExecute = true,
            Verb = "runas",
            WindowStyle = ProcessWindowStyle.Hidden
        };
        foreach (var argument in arguments)
        {
            startInfo.ArgumentList.Add(argument);
        }

        using var process = Process.Start(startInfo) ?? throw new InvalidOperationException("无法启动 AgentDock 管理程序。");
        await process.WaitForExitAsync(cancellationToken);
        if (process.ExitCode != 0)
        {
            throw new InvalidOperationException($"AgentDock 管理程序执行失败，退出码：{process.ExitCode}。");
        }
    }

    private static ProcessStartInfo CreateRedirectedProcessStartInfo(string fileName)
    {
        var utf8 = new UTF8Encoding(false);
        return new ProcessStartInfo
        {
            FileName = fileName,
            UseShellExecute = false,
            CreateNoWindow = true,
            RedirectStandardOutput = true,
            RedirectStandardError = true,
            StandardOutputEncoding = utf8,
            StandardErrorEncoding = utf8
        };
    }

    private async Task<bool> ReadNexusConnectionAsync(string binaryPath, CancellationToken cancellationToken)
    {
        var startInfo = CreateRedirectedProcessStartInfo(binaryPath);
        foreach (var argument in new[] { "service", "status", "--runtime-root", RuntimeRoot })
        {
            startInfo.ArgumentList.Add(argument);
        }

        try
        {
            var output = await RunProcessAsync(startInfo, cancellationToken);
            var status = JsonSerializer.Deserialize<NativeServiceStatus>(output, JsonOptions);
            return status?.NexusConnected == true;
        }
        catch (Exception ex) when (ex is InvalidOperationException or JsonException)
        {
            // 实时状态读取失败时按未连接处理；配对身份与配置异常仍由 NexusDeviceStatus 单独表达。
            return false;
        }
    }

    private static NexusDeviceStatus ReadNexusDeviceStatus()
    {
        var userProfile = Environment.GetFolderPath(Environment.SpecialFolder.UserProfile);
        var path = Path.Combine(userProfile, ".agentdock", "nexus", "device.json");
        if (!File.Exists(path))
        {
            return new NexusDeviceStatus(false, "", "", "", false);
        }
        try
        {
            var identity = JsonSerializer.Deserialize<NexusDeviceIdentity>(File.ReadAllText(path), JsonOptions);
            if (identity is null || string.IsNullOrWhiteSpace(identity.Endpoint) ||
                string.IsNullOrWhiteSpace(identity.NodeId) || string.IsNullOrWhiteSpace(identity.DeviceId) ||
                string.IsNullOrWhiteSpace(identity.DeviceToken))
            {
                return new NexusDeviceStatus(false, "", "", "", false, "设备身份文件无效，请重新配对。");
            }
            return new NexusDeviceStatus(true, identity.Endpoint, identity.NodeId, identity.DeviceId, true);
        }
        catch (Exception ex) when (ex is IOException or JsonException or UnauthorizedAccessException)
        {
            return new NexusDeviceStatus(false, "", "", "", false, $"无法读取设备身份：{ex.Message}");
        }
    }

    private static async Task<string> RunProcessAsync(ProcessStartInfo startInfo, CancellationToken cancellationToken)
    {
        using var process = Process.Start(startInfo) ?? throw new InvalidOperationException($"无法启动 {startInfo.FileName}。");
        var standardOutput = process.StandardOutput.ReadToEndAsync(cancellationToken);
        var standardError = process.StandardError.ReadToEndAsync(cancellationToken);
        await process.WaitForExitAsync(cancellationToken);
        var output = (await standardOutput).Trim();
        var error = (await standardError).Trim();
        if (process.ExitCode != 0)
        {
            throw new InvalidOperationException(string.IsNullOrWhiteSpace(error) ? output : error);
        }

        if (string.IsNullOrWhiteSpace(output))
        {
            return error;
        }
        return string.IsNullOrWhiteSpace(error) ? output : output + Environment.NewLine + error;
    }

    private static async Task<string> RunUpdateProcessAsync(
        ProcessStartInfo startInfo,
        IProgress<UpdateProgress>? progress,
        CancellationToken cancellationToken)
    {
        using var process = Process.Start(startInfo) ?? throw new InvalidOperationException($"无法启动 {startInfo.FileName}。");
        var output = new StringBuilder();
        var error = new StringBuilder();
        var outputTask = ReadProcessLinesAsync(process.StandardOutput, output, progress, cancellationToken);
        var errorTask = ReadProcessLinesAsync(process.StandardError, error, null, cancellationToken);

        await Task.WhenAll(process.WaitForExitAsync(cancellationToken), outputTask, errorTask);
        var outputText = output.ToString().Trim();
        var errorText = error.ToString().Trim();
        if (process.ExitCode != 0)
        {
            throw new InvalidOperationException(string.IsNullOrWhiteSpace(errorText) ? outputText : errorText);
        }

        var result = string.IsNullOrWhiteSpace(outputText)
            ? errorText
            : string.IsNullOrWhiteSpace(errorText) ? outputText : outputText + Environment.NewLine + errorText;
        progress?.Report(new UpdateProgress(100, LastNonEmptyLine(result, "更新完成。")));
        return result;
    }

    private static async Task ReadProcessLinesAsync(
        StreamReader reader,
        StringBuilder buffer,
        IProgress<UpdateProgress>? progress,
        CancellationToken cancellationToken)
    {
        while (await reader.ReadLineAsync(cancellationToken) is { } line)
        {
            buffer.AppendLine(line);
            if (!string.IsNullOrWhiteSpace(line))
            {
                progress?.Report(MapUpdateProgress(line));
            }
        }
    }

    private static UpdateProgress MapUpdateProgress(string line)
    {
        var message = line.Trim();
        var percentage = message switch
        {
            var value when value.Contains("正在下载", StringComparison.Ordinal) => 20,
            var value when value.Contains("文件校验通过", StringComparison.Ordinal) => 50,
            var value when value.Contains("正在备份并安装", StringComparison.Ordinal) => 70,
            var value when value.Contains("交给辅助进程", StringComparison.Ordinal) => 80,
            var value when value.Contains("正在更新官方核心 Skill", StringComparison.Ordinal) => 90,
            var value when value.Contains("更新完成", StringComparison.Ordinal) => 100,
            var value when value.Contains("当前已是最新版本", StringComparison.Ordinal) => 100,
            _ => 10
        };
        return new UpdateProgress(percentage, message);
    }

    private static string LastNonEmptyLine(string value, string fallback)
    {
        var line = value
            .Split(['\r', '\n'], StringSplitOptions.RemoveEmptyEntries | StringSplitOptions.TrimEntries)
            .LastOrDefault();
        return string.IsNullOrWhiteSpace(line) ? fallback : line;
    }

    private static string NormalizeAcpAgent(string? value)
    {
        return value?.Trim().ToLowerInvariant() switch
        {
            "codex" => "codex",
            "claude" => "claude",
            "grok" => "grok",
            "custom" => "custom",
            var unsupported => throw new InvalidOperationException($"不支持的 Coding Agent: {unsupported ?? "<null>"}")
        };
    }

    private static string ResolveRuntimeRoot()
    {
        var configured = Environment.GetEnvironmentVariable("AGENTDOCK_RUNTIME_DIR");
        if (!string.IsNullOrWhiteSpace(configured))
        {
            return Path.GetFullPath(configured);
        }

        var executableDirectory = new DirectoryInfo(AppContext.BaseDirectory);
        var baseDirectory = executableDirectory.FullName;
        if (File.Exists(Path.Combine(baseDirectory, "runtime.json")))
        {
            return baseDirectory;
        }

        var parent = executableDirectory.Parent?.FullName;
        if (!string.IsNullOrWhiteSpace(parent) && File.Exists(Path.Combine(parent, "runtime.json")))
        {
            return parent;
        }

        // 安装器会先启动 bin 中的托盘，再写入 runtime.json；此时仍应绑定当前安装目录。
        if (!string.IsNullOrWhiteSpace(parent) &&
            string.Equals(executableDirectory.Name, "bin", StringComparison.OrdinalIgnoreCase))
        {
            return parent;
        }

        return Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "AgentDock");
    }

    private async Task<(bool Healthy, string Version)> ReadHealthAsync(string origin, CancellationToken cancellationToken)
    {
        try
        {
            using var response = await _httpClient.GetAsync(origin.TrimEnd('/') + "/healthz", cancellationToken);
            if (!response.IsSuccessStatusCode)
            {
                return (false, "");
            }

            try
            {
                var body = await response.Content.ReadAsStringAsync(cancellationToken);
                var health = JsonSerializer.Deserialize<CoreVersionInfo>(body, JsonOptions);
                return (true, health?.Version?.Trim() ?? "");
            }
            catch (Exception ex) when (ex is IOException or JsonException)
            {
                // 健康端点已返回成功时，版本解析失败不应把服务误判为离线；随后再回退读取本地二进制 BuildInfo。
                return (true, "");
            }
        }
        catch (TaskCanceledException) when (!cancellationToken.IsCancellationRequested)
        {
            return (false, "");
        }
        catch (HttpRequestException)
        {
            return (false, "");
        }
    }

    private async Task<string> ReadCoreVersionAsync(string binaryPath, CancellationToken cancellationToken)
    {
        if (!File.Exists(binaryPath))
        {
            return "";
        }

        var startInfo = CreateRedirectedProcessStartInfo(binaryPath);
        startInfo.ArgumentList.Add("version");
        startInfo.ArgumentList.Add("--json");
        try
        {
            var output = await RunProcessAsync(startInfo, cancellationToken);
            return JsonSerializer.Deserialize<CoreVersionInfo>(output, JsonOptions)?.Version?.Trim() ?? "";
        }
        catch (OperationCanceledException) when (cancellationToken.IsCancellationRequested)
        {
            throw;
        }
        catch (Exception ex) when (ex is IOException or UnauthorizedAccessException or InvalidOperationException or Win32Exception or JsonException)
        {
            return "";
        }
    }

    private static async Task<T?> ReadJsonAsync<T>(string path, CancellationToken cancellationToken)
    {
        try
        {
            await using var stream = File.OpenRead(path);
            return await JsonSerializer.DeserializeAsync<T>(stream, JsonOptions, cancellationToken);
        }
        catch (Exception ex) when (ex is IOException or UnauthorizedAccessException or JsonException)
        {
            return default;
        }
    }

    private static string ReadFirstNonEmpty(params string[] paths)
    {
        foreach (var path in paths)
        {
            var value = ReadText(path);
            if (!string.IsNullOrWhiteSpace(value))
            {
                return value;
            }
        }
        return "";
    }

    private static string ReadText(string path)
    {
        try
        {
            return File.Exists(path) ? File.ReadAllText(path, Encoding.UTF8).Trim() : "";
        }
        catch
        {
            return "";
        }
    }

    private static string ReadProtectedText(string path, string entropy)
    {
        try
        {
            var encoded = ReadText(path);
            if (string.IsNullOrWhiteSpace(encoded))
            {
                return "";
            }
            var protectedBytes = Convert.FromBase64String(encoded);
            var plainBytes = ProtectedData.Unprotect(protectedBytes, Encoding.UTF8.GetBytes(entropy), DataProtectionScope.CurrentUser);
            return Encoding.UTF8.GetString(plainBytes);
        }
        catch
        {
            return "";
        }
    }

    private static bool IsProcessRunningAtPath(string processName, string expectedPath)
    {
        if (string.IsNullOrWhiteSpace(expectedPath))
        {
            return false;
        }
        try
        {
            var normalizedExpected = Path.GetFullPath(expectedPath);
            return Process.GetProcessesByName(processName).Any(process =>
            {
                using (process)
                {
                    try
                    {
                        var actual = process.MainModule?.FileName;
                        return !string.IsNullOrWhiteSpace(actual) &&
                               string.Equals(Path.GetFullPath(actual), normalizedExpected, StringComparison.OrdinalIgnoreCase);
                    }
                    catch
                    {
                        return false;
                    }
                }
            });
        }
        catch
        {
            return false;
        }
    }

    private static bool IsCoreStartupEnabled(RuntimeManifest manifest)
    {
        if (!string.Equals(manifest.PrivilegeMode, "elevated", StringComparison.OrdinalIgnoreCase))
        {
            return IsRunValuePresent(manifest.StartupValueName, "AgentDock");
        }

        try
        {
            var taskName = string.IsNullOrWhiteSpace(manifest.AgentDockTaskName) ? "AgentDock" : manifest.AgentDockTaskName;
            var startInfo = new ProcessStartInfo
            {
                FileName = "schtasks.exe",
                UseShellExecute = false,
                CreateNoWindow = true,
                RedirectStandardOutput = true,
                RedirectStandardError = true
            };
            startInfo.ArgumentList.Add("/Query");
            startInfo.ArgumentList.Add("/TN");
            startInfo.ArgumentList.Add($"\\{taskName}");
            startInfo.ArgumentList.Add("/XML");

            using var process = Process.Start(startInfo);
            if (process is null)
            {
                return false;
            }
            var taskXml = process.StandardOutput.ReadToEnd();
            process.WaitForExit(3000);
            if (process.ExitCode != 0 || string.IsNullOrWhiteSpace(taskXml))
            {
                return false;
            }

            var enabledElement = XDocument.Parse(taskXml)
                .Descendants()
                .FirstOrDefault(element => element.Name.LocalName == "Enabled");
            // Task Scheduler 省略 Enabled 时使用 schema 默认值 true。
            return enabledElement is null || bool.TryParse(enabledElement.Value, out var enabled) && enabled;
        }
        catch
        {
            return false;
        }
    }

    private static bool IsRunValuePresent(string configuredName, string fallbackName)
    {
        var name = string.IsNullOrWhiteSpace(configuredName) ? fallbackName : configuredName;
        using var key = Registry.CurrentUser.OpenSubKey(RunKeyPath, false);
        return key?.GetValue(name) is string value && !string.IsNullOrWhiteSpace(value);
    }

    private static async Task<string> WriteSecretFileAsync(string value, CancellationToken cancellationToken)
    {
        var path = Path.Combine(Path.GetTempPath(), $"agentdock-secret-{Guid.NewGuid():N}.txt");
        await File.WriteAllTextAsync(path, value, new UTF8Encoding(false), cancellationToken);
        return path;
    }

    private static void DeleteSecretFile(string? path)
    {
        if (string.IsNullOrWhiteSpace(path))
        {
            return;
        }
        try
        {
            File.Delete(path);
        }
        catch
        {
        }
    }

    private static void OpenDirectory(string path)
    {
        Directory.CreateDirectory(path);
        Process.Start(new ProcessStartInfo("explorer.exe", $"\"{path}\"") { UseShellExecute = true });
    }

    public void Dispose()
    {
        _httpClient.Dispose();
    }
}
