using System.IO;
using System.Security.Cryptography;
using System.Text;
using System.Text.Json;

namespace AgentDock.ControlPanel;

internal static class ControlPanelDiagnostics
{
    private const int MaxResultDetailBytes = 8 * 1024;
    private const int MaxLogDetailBytes = 16 * 1024;
    private const int SchemaVersion = 1;

    internal static string CreateOperationId() => Guid.NewGuid().ToString("N");

    internal static bool IsValidOperationId(string? operationId) =>
        Guid.TryParseExact(operationId, "N", out _);

    internal static ControlPanelRequestLease CreateRequestLease(
        string runtimeRoot,
        string operationId,
        string command,
        IReadOnlyCollection<string> arguments)
    {
        var root = ValidateRuntimeRoot(runtimeRoot);
        var operationDirectory = OperationDirectory(root);
        Directory.CreateDirectory(operationDirectory);
        EnsureDiagnosticLogFile(root);

        var requestPath = RequestPath(root, operationId);
        var resultPath = ResultPath(root, operationId);
        var request = new ControlPanelCommandRequest
        {
            SchemaVersion = SchemaVersion,
            OperationId = operationId,
            Command = command,
            Arguments = [.. arguments]
        };
        var requestBytes = JsonSerializer.SerializeToUtf8Bytes(request);
        var requestHash = Convert.ToHexString(SHA256.HashData(requestBytes)).ToLowerInvariant();

        FileStream? requestStream = null;
        try
        {
            // Keep the request open without write/delete sharing until the UAC child exits.
            // This blocks replacement of the exact file while still allowing the elevated
            // child to open it read-only. The hash additionally binds the pathname read.
            requestStream = new FileStream(
                requestPath,
                FileMode.CreateNew,
                FileAccess.ReadWrite,
                FileShare.Read,
                bufferSize: 4096,
                FileOptions.WriteThrough);
            requestStream.Write(requestBytes);
            requestStream.Flush(flushToDisk: true);

            // Create the result at medium integrity before UAC. The elevated child only
            // truncates this existing file, so it cannot leave a high-integrity result
            // that the parent cannot read or delete.
            using (new FileStream(
                resultPath,
                FileMode.CreateNew,
                FileAccess.Write,
                FileShare.Read,
                bufferSize: 1,
                FileOptions.WriteThrough))
            {
            }

            return new ControlPanelRequestLease(requestStream, requestHash);
        }
        catch
        {
            requestStream?.Dispose();
            DeleteOperationFiles(root, operationId);
            throw;
        }
    }

    internal static ControlPanelCommandRequest? ReadRequest(
        string runtimeRoot,
        string operationId,
        string expectedSha256)
    {
        try
        {
            var root = ValidateRuntimeRoot(runtimeRoot);
            var expectedHash = Convert.FromHexString(expectedSha256);
            if (expectedHash.Length != 32)
            {
                return null;
            }

            byte[] requestBytes;
            using (var requestStream = new FileStream(
                RequestPath(root, operationId),
                FileMode.Open,
                FileAccess.Read,
                FileShare.ReadWrite))
            {
                using var buffer = new MemoryStream();
                requestStream.CopyTo(buffer);
                requestBytes = buffer.ToArray();
            }
            var actualHash = SHA256.HashData(requestBytes);
            if (!CryptographicOperations.FixedTimeEquals(actualHash, expectedHash))
            {
                return null;
            }

            var request = JsonSerializer.Deserialize<ControlPanelCommandRequest>(requestBytes);
            if (request is null ||
                request.SchemaVersion != SchemaVersion ||
                !string.Equals(request.OperationId, operationId, StringComparison.Ordinal))
            {
                return null;
            }
            request.Arguments ??= [];
            return request;
        }
        catch
        {
            return null;
        }
    }

    internal static bool TryWriteResult(
        string runtimeRoot,
        string operationId,
        string command,
        string action,
        int exitCode,
        string detail)
    {
        try
        {
            var root = ValidateRuntimeRoot(runtimeRoot);
            var path = ResultPath(root, operationId);
            if (!File.Exists(path))
            {
                return false;
            }

            var result = new ControlPanelCommandResult
            {
                SchemaVersion = SchemaVersion,
                OperationId = operationId,
                Command = command,
                Action = action,
                ExitCode = exitCode,
                Detail = LimitUtf8(detail, MaxResultDetailBytes),
                CompletedAt = DateTimeOffset.Now
            };
            var bytes = JsonSerializer.SerializeToUtf8Bytes(result);

            // The medium-integrity parent pre-created this file. Truncate it in place
            // rather than replacing it with a high-integrity temporary file.
            using var stream = new FileStream(
                path,
                FileMode.Truncate,
                FileAccess.Write,
                FileShare.Read,
                bufferSize: 4096,
                FileOptions.WriteThrough);
            stream.Write(bytes);
            stream.Flush(flushToDisk: true);
            return true;
        }
        catch
        {
            return false;
        }
    }

    internal static ControlPanelCommandResult? ReadResult(string runtimeRoot, string operationId)
    {
        try
        {
            var root = ValidateRuntimeRoot(runtimeRoot);
            var result = JsonSerializer.Deserialize<ControlPanelCommandResult>(
                File.ReadAllText(ResultPath(root, operationId), Encoding.UTF8));
            if (result is null ||
                result.SchemaVersion != SchemaVersion ||
                !string.Equals(result.OperationId, operationId, StringComparison.Ordinal))
            {
                return null;
            }
            return result;
        }
        catch
        {
            return null;
        }
    }

    internal static void DeleteOperationFiles(string runtimeRoot, string operationId)
    {
        foreach (var path in new[] { RequestPath(runtimeRoot, operationId), ResultPath(runtimeRoot, operationId) })
        {
            try
            {
                File.Delete(path);
            }
            catch
            {
                // Cleanup must never replace the original management outcome.
            }
        }
    }

    internal static void RecordFailure(
        string runtimeRoot,
        string source,
        string action,
        Exception exception) =>
        RecordFailureCore(runtimeRoot, source, action, exception, createIfMissing: true);

    internal static void RecordFailureExistingLog(
        string runtimeRoot,
        string source,
        string action,
        Exception exception) =>
        RecordFailureCore(runtimeRoot, source, action, exception, createIfMissing: false);

    internal static string LastNonEmptyLine(string? value)
    {
        var line = (value ?? "")
            .Split(['\r', '\n'], StringSplitOptions.RemoveEmptyEntries | StringSplitOptions.TrimEntries)
            .LastOrDefault();
        return string.IsNullOrWhiteSpace(line) ? UiText.Get("NoDiagnosticDetail") : line;
    }

    private static void RecordFailureCore(
        string runtimeRoot,
        string source,
        string action,
        Exception exception,
        bool createIfMissing)
    {
        try
        {
            var stage = exception as RuntimeActionStageException;
            var phase = stage is null ? "" : $"{stage.Component}:{stage.StageAction}";
            var diagnosticException = stage?.InnerException ?? exception;
            var detail = LimitUtf8(diagnosticException.Message, MaxLogDetailBytes)
                .Replace("\r", " ", StringComparison.Ordinal)
                .Replace("\n", " | ", StringComparison.Ordinal);
            var logsDirectory = Path.Combine(runtimeRoot, "logs");
            if (createIfMissing)
            {
                Directory.CreateDirectory(logsDirectory);
            }

            var logPath = Path.Combine(logsDirectory, "control-panel.err.log");
            var mode = createIfMissing ? FileMode.OpenOrCreate : FileMode.Open;
            using var stream = new FileStream(logPath, mode, FileAccess.Write, FileShare.ReadWrite);
            stream.Seek(0, SeekOrigin.End);
            var message =
                $"{DateTimeOffset.Now:O} source={source} action={action} phase={phase} failed: {detail}{Environment.NewLine}";
            var bytes = new UTF8Encoding(false).GetBytes(message);
            stream.Write(bytes);
            stream.Flush();
        }
        catch
        {
            // Diagnostic writes must never replace the original failure.
        }
    }

    private static string ValidateRuntimeRoot(string runtimeRoot)
    {
        var root = Path.GetFullPath(runtimeRoot);
        if (!File.Exists(Path.Combine(root, "runtime.json")))
        {
            throw new InvalidOperationException(UiText.Get("RuntimeJsonMissing"));
        }
        return root;
    }

    private static string RequestPath(string runtimeRoot, string operationId) =>
        Path.Combine(OperationDirectory(runtimeRoot), ValidateOperationId(operationId) + ".request.json");

    private static string ResultPath(string runtimeRoot, string operationId) =>
        Path.Combine(OperationDirectory(runtimeRoot), ValidateOperationId(operationId) + ".result.json");

    private static string OperationDirectory(string runtimeRoot) =>
        Path.Combine(Path.GetFullPath(runtimeRoot), "logs", "control-panel-actions");

    private static void EnsureDiagnosticLogFile(string runtimeRoot)
    {
        var logsDirectory = Path.Combine(runtimeRoot, "logs");
        Directory.CreateDirectory(logsDirectory);
        using var stream = new FileStream(
            Path.Combine(logsDirectory, "control-panel.err.log"),
            FileMode.OpenOrCreate,
            FileAccess.Write,
            FileShare.ReadWrite);
    }

    private static string ValidateOperationId(string operationId)
    {
        if (!IsValidOperationId(operationId))
        {
            throw new InvalidOperationException("Invalid control-panel operation id.");
        }
        return operationId;
    }

    private static string LimitUtf8(string? value, int maxBytes)
    {
        var detail = (value ?? "").Trim();
        if (Encoding.UTF8.GetByteCount(detail) <= maxBytes)
        {
            return detail;
        }

        var start = detail.Length;
        var bytes = 0;
        while (start > 0)
        {
            var previous = start - 1;
            if (previous > 0 &&
                char.IsLowSurrogate(detail[previous]) &&
                char.IsHighSurrogate(detail[previous - 1]))
            {
                previous--;
            }
            var runeBytes = Encoding.UTF8.GetByteCount(detail.AsSpan(previous, start - previous));
            if (bytes + runeBytes > maxBytes)
            {
                break;
            }
            bytes += runeBytes;
            start = previous;
        }
        return detail[start..];
    }
}

internal sealed class ControlPanelRequestLease : IDisposable
{
    private readonly FileStream _stream;

    internal ControlPanelRequestLease(FileStream stream, string sha256)
    {
        _stream = stream;
        Sha256 = sha256;
    }

    internal string Sha256 { get; }

    public void Dispose() => _stream.Dispose();
}

internal sealed class ControlPanelCommandRequest
{
    public int SchemaVersion { get; set; }
    public string OperationId { get; set; } = "";
    public string Command { get; set; } = "";
    public List<string> Arguments { get; set; } = [];
}

internal sealed class ControlPanelCommandResult
{
    public int SchemaVersion { get; set; }
    public string OperationId { get; set; } = "";
    public string Command { get; set; } = "";
    public string Action { get; set; } = "";
    public int ExitCode { get; set; }
    public string Detail { get; set; } = "";
    public DateTimeOffset CompletedAt { get; set; }
}

internal sealed class RuntimeActionStageException : InvalidOperationException
{
    internal RuntimeActionStageException(string component, string stageAction, Exception innerException)
        : base(
            UiText.Format(
                "RuntimeActionStageFailed",
                component,
                stageAction,
                ControlPanelDiagnostics.LastNonEmptyLine(innerException.Message)),
            innerException)
    {
        Component = component;
        StageAction = stageAction;
    }

    internal string Component { get; }
    internal string StageAction { get; }
}
