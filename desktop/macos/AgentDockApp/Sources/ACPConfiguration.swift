import Foundation

struct ACPAdapterResolution: Equatable {
    let available: Bool
    let command: String
    let arguments: [String]
    let message: String
}

private struct ACPNodePackage {
    let name: String
    let binName: String
}

enum ACPAgentPreset: String, CaseIterable {
    case codex
    case claude
    case grok
    case custom

    var title: String {
        switch self {
        case .codex: return "Codex"
        case .claude: return "Claude"
        case .grok: return "Grok Build"
        case .custom: return "自定义"
        }
    }

    var executableNames: [String] {
        switch self {
        case .codex: return ["codex-acp"]
        case .claude: return ["claude-agent-acp"]
        case .grok: return ["grok"]
        case .custom: return []
        }
    }

    var arguments: [String] {
        switch self {
        case .grok: return ["agent", "stdio"]
        case .codex, .claude, .custom: return []
        }
    }

    private var nodePackage: ACPNodePackage? {
        switch self {
        case .codex:
            return ACPNodePackage(name: "@agentclientprotocol/codex-acp", binName: "codex-acp")
        case .claude:
            return ACPNodePackage(name: "@agentclientprotocol/claude-agent-acp", binName: "claude-agent-acp")
        case .grok, .custom:
            return nil
        }
    }

    var missingAdapterMessage: String {
        if self == .custom {
            return "请填写可执行的 ACP Adapter 绝对路径"
        }
        guard let nodePackage else {
            return "未找到 \(executableNames[0])"
        }
        return "未找到 \(executableNames[0]) 或 \(nodePackage.name)"
    }

    static func parse(_ raw: String) -> ACPAgentPreset? {
        ACPAgentPreset(rawValue: raw.trimmingCharacters(in: .whitespacesAndNewlines).lowercased())
    }

    func resolveAdapter(
        configuredCommand: String = "",
        configuredArguments: [String] = [],
        home: URL = FileManager.default.homeDirectoryForCurrentUser,
        environment: [String: String] = ProcessInfo.processInfo.environment
    ) -> ACPAdapterResolution {
        let directories = searchDirectories(home: home, environment: environment)
        let configured = resolveConfiguredAdapter(
            command: configuredCommand,
            arguments: configuredArguments,
            directories: directories
        )
        if self == .custom {
            return configured ?? ACPAdapterResolution(
                available: false,
                command: "",
                arguments: [],
                message: "未配置 · \(missingAdapterMessage)"
            )
        }

        var discovered: ACPAdapterResolution?
        for directory in directories {
            for executableName in executableNames {
                let candidate = directory.appendingPathComponent(executableName)
                if let executable = executableFile(candidate),
                   let resolution = resolveExecutableAdapter(
                       executable,
                       arguments: arguments,
                       directories: directories
                   ) {
                    discovered = resolution
                    break
                }
            }
            if discovered != nil { break }
        }

        if discovered == nil,
           let nodePackage,
           let node = resolveNodeExecutable(directories: directories) {
            for packageRoot in npmPackageRoots(nodePackage, directories: directories) {
                guard let entry = readNPMBinEntry(
                    packageRoot: packageRoot,
                    binName: nodePackage.binName
                ) else {
                    continue
                }
                let resolvedArguments = [entry.path] + arguments
                discovered = ACPAdapterResolution(
                    available: true,
                    command: node.path,
                    arguments: resolvedArguments,
                    message: "已检测到 · \(node.path) · \(entry.path)"
                )
                break
            }
        }

        if let configured {
            // 旧版曾把 ~/.local/bin/grok、Homebrew Node 等稳定 symlink 保存成版本化 target。
            // 只有重新发现的入口与既有配置实际指向同一组文件时才换回稳定路径，避免覆盖手工配置。
            if let discovered, equivalentAdapterFiles(configured, discovered) {
                return discovered
            }
            return configured
        }
        if let discovered {
            return discovered
        }

        return ACPAdapterResolution(
            available: false,
            command: "",
            arguments: [],
            message: "未安装 · \(missingAdapterMessage)"
        )
    }

    private func equivalentAdapterFiles(_ lhs: ACPAdapterResolution, _ rhs: ACPAdapterResolution) -> Bool {
        guard sameResolvedPath(lhs.command, rhs.command), lhs.arguments.count == rhs.arguments.count else {
            return false
        }
        for (leftArgument, rightArgument) in zip(lhs.arguments, rhs.arguments) {
            if leftArgument == rightArgument {
                continue
            }
            guard leftArgument.hasPrefix("/"), rightArgument.hasPrefix("/"),
                  sameResolvedPath(leftArgument, rightArgument) else {
                return false
            }
        }
        return true
    }

    private func sameResolvedPath(_ lhs: String, _ rhs: String) -> Bool {
        URL(fileURLWithPath: lhs).resolvingSymlinksInPath().standardizedFileURL.path ==
            URL(fileURLWithPath: rhs).resolvingSymlinksInPath().standardizedFileURL.path
    }

    private func resolveConfiguredAdapter(
        command: String,
        arguments: [String],
        directories: [URL]
    ) -> ACPAdapterResolution? {
        let trimmedCommand = command.trimmingCharacters(in: .whitespacesAndNewlines)
        guard trimmedCommand.hasPrefix("/"),
              let executable = executableFile(URL(fileURLWithPath: trimmedCommand)) else {
            return nil
        }
        return resolveExecutableAdapter(executable, arguments: arguments, directories: directories)
    }

    private func resolveExecutableAdapter(
        _ executable: URL,
        arguments: [String],
        directories: [URL]
    ) -> ACPAdapterResolution? {
        var resolvedArguments = arguments
        if executable.lastPathComponent == "node" {
            guard let firstArgument = resolvedArguments.first,
                  let entry = regularFile(URL(fileURLWithPath: firstArgument)) else {
                return nil
            }
            resolvedArguments[0] = entry.path
        } else if isNodeScript(executable) {
            guard let node = resolveNodeExecutable(directories: directories) else {
                return nil
            }
            return ACPAdapterResolution(
                available: true,
                command: node.path,
                arguments: [executable.path] + resolvedArguments,
                message: "已检测到 · \(node.path) · \(executable.path)"
            )
        }
        return ACPAdapterResolution(
            available: true,
            command: executable.path,
            arguments: resolvedArguments,
            message: "已检测到 · \(executable.path)"
        )
    }

    private func isNodeScript(_ executable: URL) -> Bool {
        guard let handle = try? FileHandle(forReadingFrom: executable) else {
            return false
        }
        defer { try? handle.close() }
        guard let data = try? handle.read(upToCount: 256),
              let prefix = String(data: data, encoding: .utf8),
              let firstLine = prefix.split(whereSeparator: { $0.isNewline }).first else {
            return false
        }
        let shebang = firstLine.lowercased()
        return shebang.hasPrefix("#!") && shebang.contains("node")
    }

    private func searchDirectories(home: URL, environment: [String: String]) -> [URL] {
        var directories = [
            home.appendingPathComponent(".local/bin", isDirectory: true),
            URL(fileURLWithPath: "/opt/homebrew/bin", isDirectory: true),
            URL(fileURLWithPath: "/usr/local/bin", isDirectory: true),
            URL(fileURLWithPath: "/usr/bin", isDirectory: true),
        ]
        if let path = environment["PATH"] {
            directories += path.split(separator: ":", omittingEmptySubsequences: true)
                .map { URL(fileURLWithPath: String($0), isDirectory: true) }
        }
        return uniqueURLs(directories)
    }

    private func resolveNodeExecutable(directories: [URL]) -> URL? {
        for directory in directories {
            if let node = executableFile(directory.appendingPathComponent("node")) {
                return node
            }
        }
        return nil
    }

    private func npmPackageRoots(_ package: ACPNodePackage, directories: [URL]) -> [URL] {
        var roots: [URL] = []
        for directory in directories {
            roots.append(appendingPackage(package.name, to: directory.appendingPathComponent("node_modules", isDirectory: true)))
            if directory.lastPathComponent == "bin" {
                let globalModules = directory.deletingLastPathComponent()
                    .appendingPathComponent("lib/node_modules", isDirectory: true)
                roots.append(appendingPackage(package.name, to: globalModules))
            }
        }
        return uniqueURLs(roots)
    }

    private func appendingPackage(_ packageName: String, to base: URL) -> URL {
        packageName.split(separator: "/").reduce(base) { partial, component in
            partial.appendingPathComponent(String(component), isDirectory: true)
        }
    }

    private func readNPMBinEntry(
        packageRoot: URL,
        binName: String
    ) -> URL? {
        let manifest = packageRoot.appendingPathComponent("package.json")
        guard let data = try? Data(contentsOf: manifest),
              let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let bin = object["bin"] else {
            return nil
        }

        let relativeEntry: String?
        if let value = bin as? String {
            relativeEntry = value
        } else if let values = bin as? [String: Any] {
            relativeEntry = values[binName] as? String
        } else {
            relativeEntry = nil
        }
        guard let relativeEntry = relativeEntry?.trimmingCharacters(in: .whitespacesAndNewlines),
              !relativeEntry.isEmpty,
              !relativeEntry.hasPrefix("/") else {
            return nil
        }

        let normalizedRoot = packageRoot.standardizedFileURL
        let candidate = normalizedRoot.appendingPathComponent(relativeEntry).standardizedFileURL
        guard isDescendant(candidate, of: normalizedRoot),
              let resolved = resolvedRegularFile(candidate) else {
            return nil
        }

        // package.json 的 bin 只能指向包目录内部，避免通过符号链接把 Node 入口解析到包外。
        let resolvedRoot = normalizedRoot.resolvingSymlinksInPath().standardizedFileURL
        guard isDescendant(resolved, of: resolvedRoot) else {
            return nil
        }
        return candidate
    }

    private func executableFile(_ candidate: URL) -> URL? {
        let normalized = candidate.standardizedFileURL
        guard FileManager.default.isExecutableFile(atPath: normalized.path) else {
            return nil
        }
        return regularFile(normalized)
    }

    private func regularFile(_ candidate: URL) -> URL? {
        let normalized = candidate.standardizedFileURL
        guard resolvedRegularFile(normalized) != nil else {
            return nil
        }
        // 保留用户安装器提供的稳定符号链接路径。Grok、Homebrew Node 和 npm bin
        // 都可能把稳定入口指向版本化文件；保存真实 target 会在升级清理旧版本后失效。
        return normalized
    }

    private func resolvedRegularFile(_ candidate: URL) -> URL? {
        let resolved = candidate.resolvingSymlinksInPath().standardizedFileURL
        guard let values = try? resolved.resourceValues(forKeys: [.isRegularFileKey]),
              values.isRegularFile == true else {
            return nil
        }
        return resolved
    }

    private func isDescendant(_ candidate: URL, of root: URL) -> Bool {
        let rootPath = root.standardizedFileURL.path
        let candidatePath = candidate.standardizedFileURL.path
        return candidatePath.hasPrefix(rootPath.hasSuffix("/") ? rootPath : rootPath + "/")
    }

    private func uniqueURLs(_ urls: [URL]) -> [URL] {
        var seen = Set<String>()
        return urls.compactMap { url in
            let normalized = url.standardizedFileURL
            return seen.insert(normalized.path).inserted ? normalized : nil
        }
    }
}

struct ACPDesktopConfiguration {
    static func encodeArguments(_ arguments: [String]) throws -> String {
        let data = try JSONEncoder().encode(arguments)
        guard let value = String(data: data, encoding: .utf8) else {
            throw ValidationError("无法编码 Coding Agent 启动参数。")
        }
        return value
    }

    static func decodeArguments(_ raw: String) throws -> [String] {
        let value = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !value.isEmpty else { return [] }
        guard let data = value.data(using: .utf8),
              let arguments = try? JSONDecoder().decode([String].self, from: data) else {
            throw ValidationError("Coding Agent 启动参数必须是 JSON 字符串数组，例如 [\"--flag\",\"value\"]。")
        }
        return arguments
    }
}
