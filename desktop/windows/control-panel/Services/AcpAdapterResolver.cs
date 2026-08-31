using System.IO;
using System.Text.Json;

namespace AgentDock.ControlPanel;

internal static class AcpAdapterResolver
{
    public static AcpAdapterResolution Resolve(
        string agent,
        string runtimeRoot,
        string configuredCommand = "",
        IReadOnlyList<string>? configuredArguments = null)
    {
        var normalizedAgent = NormalizeAgent(agent);
        if (normalizedAgent == "custom")
        {
            return TryResolveConfiguredAdapter(configuredCommand, configuredArguments, out var custom)
                ? custom
                : new AcpAdapterResolution(false, "", [], "未配置 · 请填写可执行的 ACP Adapter 绝对路径");
        }

        string[] executableNames;
        string[] arguments;
        string[]? npmPackageSegments;
        string? npmBinName;
        switch (normalizedAgent)
        {
            case "claude":
                executableNames = ["claude-agent-acp.exe", "claude-agent-acp.com"];
                arguments = [];
                npmPackageSegments = ["@agentclientprotocol", "claude-agent-acp"];
                npmBinName = "claude-agent-acp";
                break;
            case "grok":
                executableNames = ["grok.exe", "grok.com"];
                arguments = ["agent", "stdio"];
                npmPackageSegments = null;
                npmBinName = null;
                break;
            case "codex":
                executableNames = ["codex-acp.exe", "codex-acp.com"];
                arguments = [];
                npmPackageSegments = ["@agentclientprotocol", "codex-acp"];
                npmBinName = "codex-acp";
                break;
            default:
                throw new InvalidOperationException($"不支持的 Coding Agent: {normalizedAgent}");
        }

        if (TryResolveConfiguredAdapter(configuredCommand, configuredArguments, out var configured))
        {
            return configured;
        }

        var directories = SearchDirectories(runtimeRoot);
        var seen = new HashSet<string>(StringComparer.OrdinalIgnoreCase);
        foreach (var directory in directories)
        {
            foreach (var executableName in executableNames)
            {
                if (!TryResolveWindowsExecutable(Path.Combine(directory, executableName), out var fullPath) ||
                    !seen.Add(fullPath))
                {
                    continue;
                }
                return Available(fullPath, arguments, $"已检测到 · {fullPath}");
            }
        }

        if (npmPackageSegments is not null && npmBinName is not null &&
            TryResolveNpmAdapter(directories, npmPackageSegments, npmBinName, arguments, out var npmAdapter))
        {
            return npmAdapter;
        }

        var packageHint = npmPackageSegments is null
            ? ""
            : $" 或 {string.Join("/", npmPackageSegments)}";
        return new AcpAdapterResolution(false, "", arguments, $"未安装 · 未找到 {executableNames[0]}{packageHint}");
    }

    private static bool TryResolveConfiguredAdapter(
        string configuredCommand,
        IReadOnlyList<string>? configuredArguments,
        out AcpAdapterResolution resolution)
    {
        resolution = Unavailable();
        if (string.IsNullOrWhiteSpace(configuredCommand) || !Path.IsPathFullyQualified(configuredCommand) ||
            !TryResolveWindowsExecutable(configuredCommand, out var command))
        {
            return false;
        }

        var arguments = configuredArguments?.ToArray() ?? [];
        if (IsNodeExecutable(command))
        {
            if (arguments.Length == 0 || !TryResolveRegularFile(arguments[0], out var entry))
            {
                return false;
            }
            arguments[0] = entry;
        }

        resolution = Available(command, arguments, $"已检测到 · {command}");
        return true;
    }

    private static List<string> SearchDirectories(string runtimeRoot)
    {
        var directories = new List<string>
        {
            Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.UserProfile), ".local", "bin"),
            Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.ApplicationData), "npm"),
            Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "Programs", "Grok"),
            Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "Microsoft", "WinGet", "Links"),
            Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "npm"),
            Path.Combine(runtimeRoot, "bin")
        };
        var pathValue = Environment.GetEnvironmentVariable("PATH") ?? "";
        directories.AddRange(pathValue.Split(Path.PathSeparator, StringSplitOptions.RemoveEmptyEntries | StringSplitOptions.TrimEntries));

        var seen = new HashSet<string>(StringComparer.OrdinalIgnoreCase);
        var unique = new List<string>();
        foreach (var directory in directories)
        {
            try
            {
                if (string.IsNullOrWhiteSpace(directory))
                {
                    continue;
                }
                var fullPath = Path.GetFullPath(directory.Trim().Trim('"'));
                if (seen.Add(fullPath))
                {
                    unique.Add(fullPath);
                }
            }
            catch (Exception ex) when (ex is ArgumentException or NotSupportedException or PathTooLongException)
            {
                // 忽略单个无效 PATH 项，继续检查其他标准安装位置。
            }
        }
        return unique;
    }

    private static bool TryResolveNpmAdapter(
        IReadOnlyList<string> directories,
        IReadOnlyList<string> packageSegments,
        string binName,
        IReadOnlyList<string> presetArguments,
        out AcpAdapterResolution resolution)
    {
        resolution = Unavailable();
        if (!TryResolveNodeExecutable(directories, out var nodePath))
        {
            return false;
        }

        foreach (var directory in directories)
        {
            var packageRoot = Path.Combine(directory, "node_modules");
            foreach (var segment in packageSegments)
            {
                packageRoot = Path.Combine(packageRoot, segment);
            }
            if (!TryReadNpmBinEntry(packageRoot, binName, out var entryPath))
            {
                continue;
            }

            var arguments = new List<string> { entryPath };
            arguments.AddRange(presetArguments);
            resolution = Available(
                nodePath,
                arguments,
                $"已检测到 · {nodePath} · {entryPath}");
            return true;
        }
        return false;
    }

    private static bool TryResolveNodeExecutable(IReadOnlyList<string> directories, out string nodePath)
    {
        foreach (var directory in directories)
        {
            foreach (var executableName in new[] { "node.exe", "node.com" })
            {
                if (TryResolveWindowsExecutable(Path.Combine(directory, executableName), out nodePath))
                {
                    return true;
                }
            }
        }
        nodePath = "";
        return false;
    }

    private static bool TryReadNpmBinEntry(string packageRoot, string binName, out string entryPath)
    {
        entryPath = "";
        try
        {
            var packageJsonPath = Path.Combine(packageRoot, "package.json");
            if (!File.Exists(packageJsonPath))
            {
                return false;
            }

            using var document = JsonDocument.Parse(File.ReadAllText(packageJsonPath));
            if (!document.RootElement.TryGetProperty("bin", out var binElement))
            {
                return false;
            }

            string? relativeEntry = binElement.ValueKind switch
            {
                JsonValueKind.String => binElement.GetString(),
                JsonValueKind.Object when binElement.TryGetProperty(binName, out var namedEntry) => namedEntry.GetString(),
                _ => null
            };
            if (string.IsNullOrWhiteSpace(relativeEntry) || Path.IsPathFullyQualified(relativeEntry))
            {
                return false;
            }

            var fullPackageRoot = Path.GetFullPath(packageRoot);
            var candidate = Path.GetFullPath(Path.Combine(fullPackageRoot, relativeEntry.Replace('/', Path.DirectorySeparatorChar)));
            var relativeToPackage = Path.GetRelativePath(fullPackageRoot, candidate);
            if (relativeToPackage.Equals("..", StringComparison.Ordinal) ||
                relativeToPackage.StartsWith($"..{Path.DirectorySeparatorChar}", StringComparison.Ordinal))
            {
                return false;
            }
            return TryResolveRegularFile(candidate, out entryPath);
        }
        catch (Exception ex) when (ex is IOException or UnauthorizedAccessException or JsonException or ArgumentException or NotSupportedException or PathTooLongException)
        {
            return false;
        }
    }

    private static bool TryResolveWindowsExecutable(string candidate, out string fullPath)
    {
        fullPath = "";
        if (!TryResolveRegularFile(candidate, out var resolved))
        {
            return false;
        }
        var extension = Path.GetExtension(resolved);
        if (!extension.Equals(".exe", StringComparison.OrdinalIgnoreCase) &&
            !extension.Equals(".com", StringComparison.OrdinalIgnoreCase))
        {
            // npm 的 .cmd、.ps1 和无扩展名 Unix shim 不能直接承载 ACP 的 NDJSON 标准输出。
            return false;
        }
        fullPath = resolved;
        return true;
    }

    private static bool TryResolveRegularFile(string candidate, out string fullPath)
    {
        fullPath = "";
        if (string.IsNullOrWhiteSpace(candidate))
        {
            return false;
        }
        try
        {
            var resolved = Path.GetFullPath(candidate);
            if (!File.Exists(resolved))
            {
                return false;
            }
            fullPath = resolved;
            return true;
        }
        catch (Exception ex) when (ex is ArgumentException or NotSupportedException or PathTooLongException)
        {
            return false;
        }
    }

    private static bool IsNodeExecutable(string path) =>
        Path.GetFileName(path).Equals("node.exe", StringComparison.OrdinalIgnoreCase) ||
        Path.GetFileName(path).Equals("node.com", StringComparison.OrdinalIgnoreCase);

    private static AcpAdapterResolution Available(string command, IReadOnlyList<string> arguments, string message) =>
        new(true, command, arguments, message);

    private static AcpAdapterResolution Unavailable() => new(false, "", [], "");

    private static string NormalizeAgent(string value) => value.Trim().ToLowerInvariant() switch
    {
        "codex" => "codex",
        "claude" => "claude",
        "grok" => "grok",
        "custom" => "custom",
        var unsupported => throw new ArgumentException($"不支持的 Coding Agent: {unsupported}", nameof(value))
    };
}
