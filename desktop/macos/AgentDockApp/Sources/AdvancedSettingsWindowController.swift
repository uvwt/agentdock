import AppKit
import Foundation

private enum BrowserConnectionMode: CaseIterable {
    case managed
    case reuseExisting
    case specifiedCDP

    var title: String {
        switch self {
        case .managed:
            return "隔离浏览器"
        case .reuseExisting:
            return "优先使用本机已有 CDP 浏览器"
        case .specifiedCDP:
            return "连接指定 CDP 浏览器"
        }
    }

    static func resolve(cdpURL: String, reuseExisting: Bool) -> BrowserConnectionMode {
        // 兼容旧配置和运行时优先级：显式 CDP URL 始终优先于复用开关。
        if !cdpURL.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            return .specifiedCDP
        }
        return reuseExisting ? .reuseExisting : .managed
    }
}

@MainActor
final class AdvancedSettingsWindowController: NSWindowController, NSTextFieldDelegate {
    private let service: ServiceController
    private let configurationController: ServiceConfigurationController
    private let menuLoginAgent: MenuLoginAgentController
    private let onChanged: () -> Void

    private let serviceAutostart = NSButton(checkboxWithTitle: "允许 AgentDock 后台运行", target: nil, action: nil)
    private let menuAutostart = NSButton(checkboxWithTitle: "登录后显示 AgentDock 菜单栏", target: nil, action: nil)
    private let portField = NSTextField(string: "8765")
    private let logLevel = NSPopUpButton(frame: .zero, pullsDown: false)
    private let browserEnabled = NSButton(checkboxWithTitle: "启用浏览器 CDP 控制", target: nil, action: nil)
    private let browserConnectionMode = NSPopUpButton(frame: .zero, pullsDown: false)
    private let browserCDPURL = NSTextField(string: "")
    private let browserStatus = NSTextField(wrappingLabelWithString: "")
    private let acpEnabled = NSButton(checkboxWithTitle: "启用 Coding Agent", target: nil, action: nil)
    private let acpAgent = NSPopUpButton(frame: .zero, pullsDown: false)
    private let acpCommand = NSTextField(string: "")
    private let acpArgsJSON = NSTextField(string: "[]")
    private let acpStatus = NSTextField(wrappingLabelWithString: "")
    private let nexusEndpoint = NSTextField(string: "")
    private let nexusPairingCode = NSSecureTextField(string: "")
    private let nexusPairButton = NSButton(title: "配对并重启", target: nil, action: nil)
    private let nexusDeviceTokenStatus = NSTextField(wrappingLabelWithString: "")
    private let progress = NSProgressIndicator()
    private let statusLabel = NSTextField(wrappingLabelWithString: "")
    private let applyButton = NSButton(title: "应用并重启", target: nil, action: nil)
    private let cancelButton = NSButton(title: "取消", target: nil, action: nil)

    private var currentConfiguration: ServiceConfiguration?
    private var initialServiceAutostart = true
    private var initialMenuAutostart = true
    private var initialPort = 8765
    private var initialLogLevel = "info"
    private var initialBrowserEnabled = false
    private var initialBrowserCDPURL = ""
    private var initialBrowserConnectionMode = BrowserConnectionMode.managed
    private var initialACPEnabled = false
    private var initialACPAgent = ACPAgentPreset.codex
    private var initialACPCommand = ""
    private var initialACPArgsJSON = "[]"
    private var isBusy = false
    private var browserCDPRow: NSView?
    private var acpCommandRow: NSView?
    private var acpArgsRow: NSView?

    init(
        service: ServiceController,
        menuLoginAgent: MenuLoginAgentController,
        onChanged: @escaping () -> Void
    ) {
        self.service = service
        self.configurationController = ServiceConfigurationController(service: service)
        self.menuLoginAgent = menuLoginAgent
        self.onChanged = onChanged
        let window = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: 590, height: 850),
            styleMask: [.titled, .closable],
            backing: .buffered,
            defer: false
        )
        window.title = "AgentDock 高级设置"
        window.isReleasedWhenClosed = false
        window.center()
        super.init(window: window)
        configureUI()
    }

    required init?(coder: NSCoder) {
        fatalError("init(coder:) has not been implemented")
    }

    func present(status: ServiceStatus) {
        guard let configuration = status.configuration else { return }
        currentConfiguration = configuration
        initialServiceAutostart = status.autostartEnabled
        initialMenuAutostart = menuLoginAgent.isEnabled
        initialPort = configuration.port
        initialLogLevel = configuration.logLevel
        initialBrowserEnabled = configuration.browserEnabled
        initialBrowserCDPURL = configuration.browserCDPURL
        initialBrowserConnectionMode = BrowserConnectionMode.resolve(
            cdpURL: configuration.browserCDPURL,
            reuseExisting: configuration.browserReuseExistingCDP
        )
        initialACPEnabled = configuration.acpEnabled
        initialACPAgent = configuration.acpAgent
        initialACPCommand = configuration.acpAgent == .custom ? configuration.acpCommand : ""
        initialACPArgsJSON = (try? ACPDesktopConfiguration.encodeArguments(
            configuration.acpAgent == .custom ? configuration.acpArgs : []
        )) ?? "[]"

        serviceAutostart.state = status.autostartEnabled ? .on : .off
        menuAutostart.state = initialMenuAutostart ? .on : .off
        portField.integerValue = initialPort
        logLevel.selectItem(withTitle: initialLogLevel)
        browserEnabled.state = initialBrowserEnabled ? .on : .off
        browserCDPURL.stringValue = initialBrowserCDPURL
        selectBrowserConnectionMode(initialBrowserConnectionMode)
        acpEnabled.state = initialACPEnabled ? .on : .off
        acpAgent.selectItem(withTitle: initialACPAgent.title)
        acpCommand.stringValue = initialACPCommand
        acpArgsJSON.stringValue = initialACPArgsJSON
        nexusPairingCode.stringValue = ""
        refreshNexusStatus()
        refreshBrowserStatus()
        refreshACPStatus()
        showStatus("", isError: false)
        setBusy(false)
        refreshApplyState()
        showWindow(nil)
        window?.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }

    private func configureUI() {
        guard let contentView = window?.contentView else { return }

        serviceAutostart.target = self
        serviceAutostart.action = #selector(markChanged)
        menuAutostart.target = self
        menuAutostart.action = #selector(markChanged)

        let portFormatter = NumberFormatter()
        portFormatter.numberStyle = .none
        portFormatter.allowsFloats = false
        portFormatter.minimum = NSNumber(value: 1024)
        portFormatter.maximum = NSNumber(value: 65535)
        portField.formatter = portFormatter
        portField.alignment = .right
        portField.placeholderString = "1024–65535"
        portField.toolTip = "普通用户服务端口必须在 1024 到 65535 之间"
        portField.widthAnchor.constraint(equalToConstant: 96).isActive = true
        portField.target = self
        portField.action = #selector(markChanged)
        portField.delegate = self

        logLevel.addItems(withTitles: ["debug", "info", "warn", "error"])
        logLevel.widthAnchor.constraint(equalToConstant: 120).isActive = true
        logLevel.target = self
        logLevel.action = #selector(markChanged)

        browserEnabled.target = self
        browserEnabled.action = #selector(browserToggled)
        browserConnectionMode.addItems(withTitles: BrowserConnectionMode.allCases.map(\.title))
        browserConnectionMode.widthAnchor.constraint(equalToConstant: 290).isActive = true
        browserConnectionMode.target = self
        browserConnectionMode.action = #selector(browserConnectionChanged)
        browserCDPURL.placeholderString = "例如 http://127.0.0.1:9222"
        browserCDPURL.target = self
        browserCDPURL.action = #selector(markChanged)
        browserCDPURL.delegate = self
        browserCDPURL.widthAnchor.constraint(equalToConstant: 390).isActive = true
        browserStatus.textColor = .secondaryLabelColor
        browserStatus.font = .systemFont(ofSize: 12)

        acpEnabled.target = self
        acpEnabled.action = #selector(acpChanged)
        acpAgent.addItems(withTitles: ACPAgentPreset.allCases.map(\.title))
        acpAgent.widthAnchor.constraint(equalToConstant: 180).isActive = true
        acpAgent.target = self
        acpAgent.action = #selector(acpChanged)
        acpCommand.placeholderString = "/absolute/path/to/acp-adapter"
        acpCommand.target = self
        acpCommand.action = #selector(markChanged)
        acpCommand.delegate = self
        acpCommand.widthAnchor.constraint(equalToConstant: 390).isActive = true
        acpArgsJSON.placeholderString = "[]"
        acpArgsJSON.target = self
        acpArgsJSON.action = #selector(markChanged)
        acpArgsJSON.delegate = self
        acpArgsJSON.widthAnchor.constraint(equalToConstant: 390).isActive = true
        acpStatus.textColor = .secondaryLabelColor
        acpStatus.font = .systemFont(ofSize: 12)
        acpStatus.widthAnchor.constraint(equalToConstant: 500).isActive = true

        nexusEndpoint.placeholderString = "https://nexus.example.com"
        nexusEndpoint.target = self
        nexusEndpoint.action = #selector(markChanged)
        nexusEndpoint.delegate = self
        nexusPairingCode.placeholderString = "NexusDock 生成的一次性配对码"
        nexusPairingCode.target = self
        nexusPairingCode.action = #selector(markChanged)
        nexusPairingCode.delegate = self
        nexusPairButton.target = self
        nexusPairButton.action = #selector(pairNexusPressed)
        nexusDeviceTokenStatus.textColor = .secondaryLabelColor
        nexusDeviceTokenStatus.font = .systemFont(ofSize: 12)
        nexusDeviceTokenStatus.widthAnchor.constraint(equalToConstant: 390).isActive = true

        progress.style = .spinning
        progress.controlSize = .small
        progress.isDisplayedWhenStopped = false

        statusLabel.textColor = .secondaryLabelColor
        statusLabel.isHidden = true

        applyButton.bezelStyle = .rounded
        applyButton.keyEquivalent = "\r"
        applyButton.target = self
        applyButton.action = #selector(applyPressed)
        cancelButton.target = self
        cancelButton.action = #selector(cancelPressed)

        let openLogs = NSButton(title: "打开日志", target: self, action: #selector(openLogsPressed))
        openLogs.bezelStyle = .inline
        let openConfig = NSButton(title: "打开配置目录", target: self, action: #selector(openConfigurationPressed))
        openConfig.bezelStyle = .inline

        let startupStack = NSStackView(views: [serviceAutostart, menuAutostart])
        startupStack.orientation = .vertical
        startupStack.alignment = .leading
        startupStack.spacing = 8

        let serviceForm = NSStackView(views: [
            formRow(title: "服务端口", control: portField),
            formRow(title: "日志级别", control: logLevel),
        ])
        serviceForm.orientation = .vertical
        serviceForm.alignment = .leading
        serviceForm.spacing = 10

        let cdpRow = formRow(title: "CDP 地址", control: browserCDPURL)
        browserCDPRow = cdpRow
        let browserStack = NSStackView(views: [
            browserEnabled,
            formRow(title: "连接方式", control: browserConnectionMode),
            cdpRow,
            browserStatus,
        ])
        browserStack.orientation = .vertical
        browserStack.alignment = .leading
        browserStack.spacing = 5
        browserStatus.widthAnchor.constraint(equalToConstant: 500).isActive = true

        let commandRow = formRow(title: "Command", control: acpCommand)
        let argsRow = formRow(title: "Args JSON", control: acpArgsJSON)
        acpCommandRow = commandRow
        acpArgsRow = argsRow
        let acpStack = NSStackView(views: [
            acpEnabled,
            formRow(title: "Agent", control: acpAgent),
            commandRow,
            argsRow,
            acpStatus,
        ])
        acpStack.orientation = .vertical
        acpStack.alignment = .leading
        acpStack.spacing = 8

        let nexusStack = NSStackView(views: [
            formRow(title: "Endpoint", control: nexusEndpoint),
            formRow(title: "配对码", control: nexusPairingCode),
            nexusPairButton,
            formRow(title: "Device Token", control: nexusDeviceTokenStatus),
        ])
        nexusStack.orientation = .vertical
        nexusStack.alignment = .leading
        nexusStack.spacing = 8
        nexusEndpoint.widthAnchor.constraint(equalToConstant: 390).isActive = true
        nexusPairingCode.widthAnchor.constraint(equalToConstant: 390).isActive = true

        let utilityRow = NSStackView(views: [openLogs, openConfig])
        utilityRow.orientation = .horizontal
        utilityRow.spacing = 14

        let actionRow = NSStackView(views: [progress, statusLabel, NSView(), cancelButton, applyButton])
        actionRow.orientation = .horizontal
        actionRow.alignment = .centerY
        actionRow.spacing = 10
        statusLabel.setContentHuggingPriority(.defaultLow, for: .horizontal)

        let root = NSStackView(views: [
            sectionTitle("启动"),
            startupStack,
            separator(),
            sectionTitle("服务"),
            serviceForm,
            separator(),
            sectionTitle("Coding Agent（ACP）"),
            acpStack,
            separator(),
            sectionTitle("浏览器"),
            browserStack,
            separator(),
            sectionTitle("Nexus"),
            nexusStack,
            separator(),
            utilityRow,
            actionRow,
        ])
        root.orientation = .vertical
        root.alignment = .leading
        root.spacing = 12
        root.translatesAutoresizingMaskIntoConstraints = false
        contentView.addSubview(root)

        NSLayoutConstraint.activate([
            root.leadingAnchor.constraint(equalTo: contentView.leadingAnchor, constant: 28),
            root.trailingAnchor.constraint(equalTo: contentView.trailingAnchor, constant: -28),
            root.topAnchor.constraint(equalTo: contentView.topAnchor, constant: 24),
            root.bottomAnchor.constraint(lessThanOrEqualTo: contentView.bottomAnchor, constant: -22),
            actionRow.widthAnchor.constraint(equalTo: root.widthAnchor),
        ])
    }

    private func sectionTitle(_ title: String) -> NSTextField {
        let label = NSTextField(labelWithString: title)
        label.font = .systemFont(ofSize: 14, weight: .semibold)
        return label
    }

    private func separator() -> NSBox {
        let box = NSBox()
        box.boxType = .separator
        box.widthAnchor.constraint(equalToConstant: 534).isActive = true
        return box
    }

    private func formRow(title: String, control: NSView) -> NSView {
        let label = NSTextField(labelWithString: title)
        label.textColor = .secondaryLabelColor
        label.widthAnchor.constraint(equalToConstant: 92).isActive = true
        let row = NSStackView(views: [label, control])
        row.orientation = .horizontal
        row.alignment = .centerY
        row.spacing = 12
        return row
    }

    @objc private func markChanged() {
        refreshApplyState()
    }

    func controlTextDidChange(_ obj: Notification) {
        if obj.object as? NSTextField === browserCDPURL {
            refreshBrowserStatus()
        }
        if obj.object as? NSTextField === acpCommand || obj.object as? NSTextField === acpArgsJSON {
            refreshACPStatus()
        }
        refreshApplyState()
    }

    @objc private func acpChanged() {
        refreshACPStatus()
        refreshApplyState()
    }

    @objc private func browserConnectionChanged() {
        refreshBrowserStatus()
        refreshApplyState()
    }

    @objc private func browserToggled() {
        if browserEnabled.state == .on,
           selectedBrowserConnectionMode() == .managed,
           BrowserSupportController.detectExecutable() == nil {
            browserEnabled.state = .off
            showStatus("未检测到受支持的 Chromium 系浏览器，且未配置外部 CDP。", isError: true)
        }
        refreshBrowserStatus()
        refreshApplyState()
    }

    @objc private func applyPressed() {
        guard let configuration = currentConfiguration else { return }
        let selectedAgent = selectedACPAgent()
        let sameAgent = selectedAgent == configuration.acpAgent
        let customCommand = acpCommand.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
        let customArguments: [String]
        do {
            customArguments = selectedAgent == .custom
                ? try ACPDesktopConfiguration.decodeArguments(acpArgsJSON.stringValue)
                : []
        } catch {
            showStatus(error.localizedDescription, isError: true)
            return
        }
        let browserMode = selectedBrowserConnectionMode()
        let configuredCDP = browserCDPURL.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
        if browserMode == .specifiedCDP, configuredCDP.isEmpty {
            showStatus("选择“连接指定 CDP 浏览器”时必须填写 CDP 地址。", isError: true)
            return
        }
        let settings = EditableServiceSettings(
            port: portField.integerValue,
            logLevel: logLevel.titleOfSelectedItem ?? "info",
            browserEnabled: browserEnabled.state == .on,
            browserCDPURL: browserMode == .specifiedCDP ? configuredCDP : "",
            browserReuseExistingCDP: browserMode == .reuseExisting,
            acpEnabled: acpEnabled.state == .on,
            acpAgent: selectedAgent,
            acpCommand: selectedAgent == .custom ? customCommand : (sameAgent ? configuration.acpCommand : ""),
            acpArgs: selectedAgent == .custom ? customArguments : (sameAgent ? configuration.acpArgs : [])
        )
        setBusy(true)
        showStatus("正在保存配置并验证 AgentDock…", isError: false)
        Task {
            do {
                let validatedSettings = try settings.validated()
                try await configurationController.apply(validatedSettings)
                let serviceAutostartValue = serviceAutostart.state == .on
                if serviceAutostartValue != initialServiceAutostart {
                    try await service.setAutostart(enabled: serviceAutostartValue)
                }
                let menuAutostartValue = menuAutostart.state == .on
                if menuAutostartValue != initialMenuAutostart {
                    try menuLoginAgent.setEnabled(menuAutostartValue)
                }
                initialServiceAutostart = serviceAutostartValue
                initialMenuAutostart = menuAutostartValue
                initialPort = validatedSettings.port
                initialLogLevel = validatedSettings.logLevel
                initialBrowserEnabled = validatedSettings.browserEnabled
                initialBrowserCDPURL = validatedSettings.browserCDPURL
                initialBrowserConnectionMode = BrowserConnectionMode.resolve(
                    cdpURL: validatedSettings.browserCDPURL,
                    reuseExisting: validatedSettings.browserReuseExistingCDP
                )
                initialACPEnabled = validatedSettings.acpEnabled
                initialACPAgent = validatedSettings.acpAgent
                initialACPCommand = validatedSettings.acpAgent == .custom ? validatedSettings.acpCommand : ""
                initialACPArgsJSON = (try? ACPDesktopConfiguration.encodeArguments(
                    validatedSettings.acpAgent == .custom ? validatedSettings.acpArgs : []
                )) ?? "[]"
                acpCommand.stringValue = initialACPCommand
                acpArgsJSON.stringValue = initialACPArgsJSON
                portField.integerValue = initialPort
                logLevel.selectItem(withTitle: initialLogLevel)
                browserCDPURL.stringValue = initialBrowserCDPURL
                selectBrowserConnectionMode(initialBrowserConnectionMode)
                if let updatedConfiguration = ServiceConfiguration.load(from: service.paths.environment) {
                    currentConfiguration = updatedConfiguration
                }
                refreshBrowserStatus()
                refreshACPStatus()
                showStatus("设置已保存。", isError: false)
                setBusy(false)
                refreshApplyState()
                onChanged()
            } catch {
                setBusy(false)
                showStatus(error.localizedDescription, isError: true)
            }
        }
    }

    @objc private func cancelPressed() { close() }

    @objc private func pairNexusPressed() {
        let endpoint = nexusEndpoint.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
        let pairingCode = nexusPairingCode.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !endpoint.isEmpty, !pairingCode.isEmpty else {
            showStatus("请填写 NexusDock 地址和一次性配对码。", isError: true)
            return
        }
        setBusy(true)
        showStatus("正在配对并重启 AgentDock…", isError: false)
        Task {
            do {
                try await service.pairNexus(endpoint: endpoint, pairingCode: pairingCode)
                nexusPairingCode.stringValue = ""
                refreshNexusStatus()
                showStatus("NexusDock 配对完成，Device Token 已安全保存。", isError: false)
                setBusy(false)
                onChanged()
            } catch {
                setBusy(false)
                showStatus(error.localizedDescription, isError: true)
            }
        }
    }

    @objc private func openLogsPressed() { service.openLogs() }
    @objc private func openConfigurationPressed() { service.openConfiguration() }

    private func selectedACPAgent() -> ACPAgentPreset {
        let title = acpAgent.titleOfSelectedItem ?? ""
        return ACPAgentPreset.allCases.first { $0.title == title } ?? .codex
    }

    private func selectedBrowserConnectionMode() -> BrowserConnectionMode {
        let title = browserConnectionMode.titleOfSelectedItem ?? ""
        return BrowserConnectionMode.allCases.first { $0.title == title } ?? .managed
    }

    private func selectBrowserConnectionMode(_ mode: BrowserConnectionMode) {
        browserConnectionMode.selectItem(withTitle: mode.title)
    }

    private func refreshACPStatus() {
        let preset = selectedACPAgent()
        let isCustom = preset == .custom
        acpCommandRow?.isHidden = !isCustom
        acpArgsRow?.isHidden = !isCustom

        let configuredCommand = isCustom
            ? acpCommand.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
            : (currentConfiguration?.acpAgent == preset ? currentConfiguration?.acpCommand ?? "" : "")
        let configuredArguments: [String]
        if isCustom {
            configuredArguments = (try? ACPDesktopConfiguration.decodeArguments(acpArgsJSON.stringValue)) ?? []
        } else {
            configuredArguments = currentConfiguration?.acpAgent == preset ? currentConfiguration?.acpArgs ?? [] : []
        }
        let resolution = preset.resolveAdapter(
            configuredCommand: configuredCommand,
            configuredArguments: configuredArguments
        )
        let enabled = acpEnabled.state == .on
        acpAgent.isEnabled = enabled && !isBusy
        acpCommand.isEnabled = enabled && isCustom && !isBusy
        acpArgsJSON.isEnabled = enabled && isCustom && !isBusy
        if isCustom, (try? ACPDesktopConfiguration.decodeArguments(acpArgsJSON.stringValue)) == nil {
            acpStatus.stringValue = "Args JSON 必须是 JSON 字符串数组。"
            acpStatus.textColor = enabled ? .systemRed : .secondaryLabelColor
        } else if resolution.available {
            acpStatus.stringValue = enabled
                ? resolution.message
                : (isCustom ? "已配置 \(preset.title) · 启用后生效" : "已检测到 \(preset.title) · 启用后生效")
            acpStatus.textColor = .secondaryLabelColor
        } else {
            acpStatus.stringValue = resolution.message
            acpStatus.textColor = enabled ? .systemRed : .secondaryLabelColor
        }
    }

    private func refreshBrowserStatus() {
        let enabled = browserEnabled.state == .on
        let mode = selectedBrowserConnectionMode()
        let configuredCDP = browserCDPURL.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
        browserCDPRow?.isHidden = mode != .specifiedCDP
        browserCDPURL.isEnabled = !isBusy && mode == .specifiedCDP

        switch mode {
        case .specifiedCDP:
            if configuredCDP.isEmpty {
                browserStatus.stringValue = "请输入要连接的 CDP 地址。"
                browserStatus.textColor = .systemRed
                return
            }
            browserStatus.stringValue = "使用指定 CDP 浏览器，并可能沿用其中的登录状态。"
            browserStatus.textColor = .secondaryLabelColor
        case .reuseExisting:
            browserStatus.stringValue = "优先使用本机已有 CDP 浏览器；未找到时自动使用隔离浏览器。可能沿用已有登录状态。"
            browserStatus.textColor = .secondaryLabelColor
        case .managed:
            if BrowserSupportController.detectExecutable() != nil {
                browserStatus.stringValue = "使用隔离浏览器，不沿用日常浏览器登录状态。"
                browserStatus.textColor = .secondaryLabelColor
            } else {
                browserStatus.stringValue = "未检测到受支持的 Chromium 系浏览器。"
                browserStatus.textColor = enabled ? .systemRed : .secondaryLabelColor
            }
        }
    }

    private func refreshNexusStatus() {
        let status = service.nexusDeviceStatus()
        if status.paired {
            nexusEndpoint.stringValue = status.endpoint
            nexusDeviceTokenStatus.stringValue = "已安全保存 · node_id=\(status.nodeID)"
            nexusDeviceTokenStatus.textColor = .secondaryLabelColor
            return
        }
        nexusDeviceTokenStatus.stringValue = status.error
            ?? "尚未配对；Device Token 将由一次性配对自动生成。"
        nexusDeviceTokenStatus.textColor = status.error == nil ? .secondaryLabelColor : .systemRed
    }

    private func refreshApplyState() {
        guard !isBusy, currentConfiguration != nil else {
            applyButton.isEnabled = false
            return
        }
        let acpIsEnabled = acpEnabled.state == .on
        let selectedAgent = selectedACPAgent()
        let customSettingsChanged = selectedAgent == .custom && (
            acpCommand.stringValue.trimmingCharacters(in: .whitespacesAndNewlines) != initialACPCommand
                || acpArgsJSON.stringValue.trimmingCharacters(in: .whitespacesAndNewlines) != initialACPArgsJSON
        )
        let acpSettingsChanged = acpIsEnabled != initialACPEnabled
            || ((acpIsEnabled || initialACPEnabled) && selectedAgent != initialACPAgent)
            || ((acpIsEnabled || initialACPEnabled) && customSettingsChanged)
        let browserMode = selectedBrowserConnectionMode()
        let browserCDP = browserMode == .specifiedCDP
            ? browserCDPURL.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
            : ""
        let changed = (serviceAutostart.state == .on) != initialServiceAutostart
            || (menuAutostart.state == .on) != initialMenuAutostart
            || portField.integerValue != initialPort
            || (logLevel.titleOfSelectedItem ?? "info") != initialLogLevel
            || (browserEnabled.state == .on) != initialBrowserEnabled
            || browserMode != initialBrowserConnectionMode
            || browserCDP != initialBrowserCDPURL
            || acpSettingsChanged
        applyButton.isEnabled = changed
        nexusPairButton.isEnabled = !isBusy
            && !nexusEndpoint.stringValue.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            && !nexusPairingCode.stringValue.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    private func setBusy(_ busy: Bool) {
        isBusy = busy
        for control in [serviceAutostart, menuAutostart, portField, logLevel, browserEnabled, browserConnectionMode, browserCDPURL, acpEnabled, acpAgent, acpCommand, acpArgsJSON, nexusEndpoint, nexusPairingCode, nexusPairButton] {
            control.isEnabled = !busy
        }
        refreshBrowserStatus()
        refreshACPStatus()
        cancelButton.isEnabled = !busy
        if busy {
            applyButton.isEnabled = false
            progress.startAnimation(nil)
        } else {
            progress.stopAnimation(nil)
            refreshApplyState()
        }
    }

    private func showStatus(_ message: String, isError: Bool) {
        statusLabel.stringValue = message
        statusLabel.textColor = isError ? .systemRed : .secondaryLabelColor
        statusLabel.isHidden = message.isEmpty
    }
}
