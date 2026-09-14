import AppKit
import Foundation

private enum BrowserConnectionMode: CaseIterable {
    case managed
    case reuseExisting
    case specifiedCDP

    var title: String {
        switch self {
        case .managed:
            return L10n.text("Isolated browser")
        case .reuseExisting:
            return L10n.text("Prefer an existing local CDP browser")
        case .specifiedCDP:
            return L10n.text("Connect to a specified CDP browser")
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
final class AdvancedSettingsWindowController: NSWindowController, NSTextFieldDelegate, NSWindowDelegate {
    private let service: ServiceController
    private let configurationController: ServiceConfigurationController
    private let menuLoginAgent: MenuLoginAgentController
    private let onChanged: () -> Void

    private let languagePreference = NSPopUpButton(frame: .zero, pullsDown: false)
    private let serviceAutostart = NSButton(checkboxWithTitle: L10n.text("Allow AgentDock to run in the background"), target: nil, action: nil)
    private let menuAutostart = NSButton(checkboxWithTitle: L10n.text("Show AgentDock in the menu bar after sign-in"), target: nil, action: nil)
    private let portField = NSTextField(string: "8765")
    private let logLevel = NSPopUpButton(frame: .zero, pullsDown: false)
    private let mcpAppsEnabled = NSButton(checkboxWithTitle: L10n.text("Enable MCP Apps UI"), target: nil, action: nil)
    private let browserEnabled = NSButton(checkboxWithTitle: L10n.text("Enable browser CDP control"), target: nil, action: nil)
    private let browserConnectionMode = NSPopUpButton(frame: .zero, pullsDown: false)
    private let browserCDPURL = NSTextField(string: "")
    private let browserStatus = NSTextField(wrappingLabelWithString: "")
    private let acpEnabled = NSButton(checkboxWithTitle: L10n.text("Enable Coding Agent"), target: nil, action: nil)
    private let acpProfileList = NSStackView()
    private let acpOverviewContainer = NSStackView()
    private let acpDefaultProfileMenu = NSPopUpButton(frame: .zero, pullsDown: false)
    private let acpAddCustomProfile = NSButton(title: "+ " + L10n.text("Add custom ACP"), target: nil, action: nil)
    private let nexusEndpoint = NSTextField(string: "")
    private let nexusPairingCode = NSSecureTextField(string: "")
    private let nexusPairButton = NSButton(title: L10n.text("Pair and restart"), target: nil, action: nil)
    private let nexusDeviceTokenStatus = NSTextField(labelWithString: "")
    private let progress = NSProgressIndicator()
    private let statusLabel = NSTextField(wrappingLabelWithString: "")
    private let applyButton = NSButton(title: L10n.text("Apply and restart"), target: nil, action: nil)
    private let cancelButton = NSButton(title: L10n.text("Cancel"), target: nil, action: nil)
    private weak var activeCustomACPDialog: NSPanel?

    private var currentConfiguration: ServiceConfiguration?
    private var initialServiceAutostart = true
    private var initialMenuAutostart = true
    private var initialPort = 8765
    private var initialLogLevel = "info"
    private var initialMCPAppsEnabled = true
    private var initialBrowserEnabled = false
    private var initialBrowserCDPURL = ""
    private var initialBrowserConnectionMode = BrowserConnectionMode.managed
    private var initialACPEnabled = false
    private var initialACPProfiles: [ACPProfileConfiguration] = []
    private var initialACPDefaultProfile = ""
    private var acpProfiles: [ACPProfileConfiguration] = []
    private var acpDefaultProfile = ""
    private var isBusy = false
    private var isUpdateInProgress = false
    private var browserCDPRow: NSView?

    private struct CustomACPProfileDialogResult {
        let name: String
        let command: String
        let arguments: [String]
        let deleteRequested: Bool
    }

    private var controlsLocked: Bool {
        isBusy || isUpdateInProgress
    }

    var hasActiveServiceOperation: Bool {
        isBusy
    }

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
            contentRect: NSRect(x: 0, y: 0, width: 700, height: 760),
            styleMask: [.titled, .closable, .resizable],
            backing: .buffered,
            defer: false
        )
        window.title = L10n.text("AgentDock Advanced Settings")
        window.isReleasedWhenClosed = false
        window.minSize = NSSize(width: 700, height: 560)
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
        initialMCPAppsEnabled = configuration.mcpAppsEnabled
        initialBrowserEnabled = configuration.browserEnabled
        initialBrowserCDPURL = configuration.browserCDPURL
        initialBrowserConnectionMode = BrowserConnectionMode.resolve(
            cdpURL: configuration.browserCDPURL,
            reuseExisting: configuration.browserReuseExistingCDP
        )
        initialACPEnabled = configuration.acpEnabled
        initialACPProfiles = configuration.acpProfiles
        initialACPDefaultProfile = configuration.acpDefaultProfile
        acpProfiles = configuration.acpProfiles
        acpDefaultProfile = configuration.acpDefaultProfile

        serviceAutostart.state = status.autostartEnabled ? .on : .off
        menuAutostart.state = initialMenuAutostart ? .on : .off
        portField.integerValue = initialPort
        logLevel.selectItem(withTitle: initialLogLevel)
        mcpAppsEnabled.state = initialMCPAppsEnabled ? .on : .off
        browserEnabled.state = initialBrowserEnabled ? .on : .off
        browserCDPURL.stringValue = initialBrowserCDPURL
        selectBrowserConnectionMode(initialBrowserConnectionMode)
        acpEnabled.state = initialACPEnabled ? .on : .off
        refreshACPProfileOverview()
        nexusPairingCode.stringValue = ""
        refreshNexusStatus()
        refreshBrowserStatus()
        showStatus("", isError: false)
        setBusy(false)
        refreshApplyState()
        fitWindowToVisibleScreen()
        showWindow(nil)
        window?.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }

    func setUpdateInProgress(_ inProgress: Bool) {
        isUpdateInProgress = inProgress
        setBusy(isBusy)
        if inProgress {
            showStatus(L10n.text("Updating AgentDock…"), isError: false)
        } else if statusLabel.stringValue == L10n.text("Updating AgentDock…") {
            statusLabel.isHidden = true
        }
    }

    private func configureUI() {
        guard let contentView = window?.contentView else { return }

        // 同类下拉框使用统一宽度；连接方式文案更长，单独保留一档较宽尺寸。
        let compactPopUpWidth: CGFloat = 110
        let widePopUpWidth: CGFloat = 220
        let acpChildIndent: CGFloat = 18
        let acpListWidth: CGFloat = 250

        let scrollView = NSScrollView()
        scrollView.hasVerticalScroller = true
        scrollView.autohidesScrollers = true
        scrollView.drawsBackground = false
        scrollView.translatesAutoresizingMaskIntoConstraints = false
        contentView.addSubview(scrollView)

        let scrollDocumentView = NSView()
        scrollDocumentView.translatesAutoresizingMaskIntoConstraints = false
        scrollView.documentView = scrollDocumentView

        for preference in UILanguagePreference.allCases {
            languagePreference.addItem(withTitle: preference.title)
            languagePreference.lastItem?.representedObject = preference.rawValue
        }
        selectLanguagePreference(L10n.languagePreference())
        languagePreference.widthAnchor.constraint(equalToConstant: compactPopUpWidth).isActive = true
        languagePreference.target = self
        languagePreference.action = #selector(languageChanged)

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
        portField.toolTip = L10n.text("The service port for a standard user must be between 1024 and 65535")
        portField.widthAnchor.constraint(equalToConstant: 96).isActive = true
        portField.target = self
        portField.action = #selector(markChanged)
        portField.delegate = self

        logLevel.addItems(withTitles: ["debug", "info", "warn", "error"])
        logLevel.widthAnchor.constraint(equalToConstant: compactPopUpWidth).isActive = true
        logLevel.target = self
        logLevel.action = #selector(markChanged)

        mcpAppsEnabled.target = self
        mcpAppsEnabled.action = #selector(markChanged)

        browserEnabled.target = self
        browserEnabled.action = #selector(browserToggled)
        browserConnectionMode.addItems(withTitles: BrowserConnectionMode.allCases.map(\.title))
        browserConnectionMode.widthAnchor.constraint(equalToConstant: widePopUpWidth).isActive = true
        browserConnectionMode.target = self
        browserConnectionMode.action = #selector(browserConnectionChanged)
        browserCDPURL.placeholderString = L10n.text("For example: http://127.0.0.1:9222")
        browserCDPURL.target = self
        browserCDPURL.action = #selector(markChanged)
        browserCDPURL.delegate = self
        browserStatus.textColor = .secondaryLabelColor
        browserStatus.font = .systemFont(ofSize: 12)

        acpEnabled.target = self
        acpEnabled.action = #selector(acpChanged)

        acpProfileList.orientation = .vertical
        acpProfileList.alignment = .leading
        acpProfileList.spacing = 0
        acpAddCustomProfile.bezelStyle = .inline
        acpAddCustomProfile.target = self
        acpAddCustomProfile.action = #selector(addCustomACPProfile)
        acpDefaultProfileMenu.target = self
        acpDefaultProfileMenu.action = #selector(defaultACPProfileChanged)
        acpDefaultProfileMenu.widthAnchor.constraint(equalToConstant: compactPopUpWidth).isActive = true

        nexusEndpoint.placeholderString = "https://nexus.example.com"
        nexusEndpoint.target = self
        nexusEndpoint.action = #selector(markChanged)
        nexusEndpoint.delegate = self
        nexusPairingCode.placeholderString = L10n.text("One-time pairing code generated by NexusDock")
        nexusPairingCode.target = self
        nexusPairingCode.action = #selector(markChanged)
        nexusPairingCode.delegate = self
        nexusPairButton.target = self
        nexusPairButton.action = #selector(pairNexusPressed)
        nexusDeviceTokenStatus.textColor = .secondaryLabelColor
        nexusDeviceTokenStatus.font = .systemFont(ofSize: 12)
        nexusDeviceTokenStatus.lineBreakMode = .byCharWrapping
        nexusDeviceTokenStatus.maximumNumberOfLines = 2
        nexusDeviceTokenStatus.heightAnchor.constraint(equalToConstant: 34).isActive = true

        for flexibleView in [browserCDPURL, browserStatus, nexusEndpoint, nexusPairingCode, nexusDeviceTokenStatus] {
            flexibleView.setContentHuggingPriority(.defaultLow, for: .horizontal)
            flexibleView.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        }

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

        let openLogs = NSButton(title: L10n.text("Open logs"), target: self, action: #selector(openLogsPressed))
        openLogs.bezelStyle = .inline
        let openConfig = NSButton(title: L10n.text("Open configuration folder"), target: self, action: #selector(openConfigurationPressed))
        openConfig.bezelStyle = .inline

        let startupStack = NSStackView(views: [
            serviceAutostart,
            menuAutostart,
        ])
        startupStack.orientation = .vertical
        startupStack.alignment = .leading
        startupStack.spacing = 8

        let serviceForm = NSStackView(views: [
            mcpAppsEnabled,
            formRow(title: L10n.text("Service port"), control: portField),
            formRow(title: L10n.text("Log level"), control: logLevel),
            formRow(title: L10n.text("Interface language"), control: languagePreference),
        ])
        serviceForm.orientation = .vertical
        serviceForm.alignment = .leading
        serviceForm.spacing = 10

        let cdpRow = formRow(title: L10n.text("CDP address"), control: browserCDPURL, fillsAvailableWidth: true)
        browserCDPRow = cdpRow
        let browserStack = NSStackView(views: [
            browserEnabled,
            formRow(title: L10n.text("Connection mode"), control: browserConnectionMode),
            cdpRow,
            browserStatus,
        ])
        browserStack.orientation = .vertical
        browserStack.alignment = .leading
        browserStack.spacing = 5
        cdpRow.widthAnchor.constraint(equalTo: browserStack.widthAnchor).isActive = true
        browserStatus.widthAnchor.constraint(equalTo: browserStack.widthAnchor).isActive = true

        let defaultProfileRow = formRow(title: L10n.text("Default ACP"), control: acpDefaultProfileMenu)
        acpOverviewContainer.setViews([defaultProfileRow, acpProfileList, acpAddCustomProfile], in: .top)
        acpOverviewContainer.orientation = .vertical
        acpOverviewContainer.alignment = .leading
        acpOverviewContainer.spacing = 8
        // 总开关保持一级；默认项、Agent 列表和新增入口整体缩进，形成清晰的父子层级。
        acpOverviewContainer.edgeInsets = NSEdgeInsets(top: 0, left: acpChildIndent, bottom: 0, right: 0)
        // 列表与默认 ACP 表单保持接近的内容宽度，让复选框落在下拉框右缘附近。
        acpProfileList.widthAnchor.constraint(equalToConstant: acpListWidth).isActive = true

        let acpStack = NSStackView(views: [acpEnabled, acpOverviewContainer])
        acpStack.orientation = .vertical
        acpStack.alignment = .leading
        acpStack.spacing = 10
        acpOverviewContainer.widthAnchor.constraint(equalTo: acpStack.widthAnchor).isActive = true

        let nexusEndpointRow = formRow(title: "Endpoint", control: nexusEndpoint, fillsAvailableWidth: true)
        let nexusPairingCodeRow = formRow(title: L10n.text("Pairing code"), control: nexusPairingCode, fillsAvailableWidth: true)
        let nexusPairRow = NSView()
        nexusPairButton.translatesAutoresizingMaskIntoConstraints = false
        nexusDeviceTokenStatus.translatesAutoresizingMaskIntoConstraints = false
        nexusPairRow.addSubview(nexusPairButton)
        nexusPairRow.addSubview(nexusDeviceTokenStatus)
        NSLayoutConstraint.activate([
            nexusPairButton.leadingAnchor.constraint(equalTo: nexusPairRow.leadingAnchor),
            nexusPairButton.centerYAnchor.constraint(equalTo: nexusPairRow.centerYAnchor),
            nexusDeviceTokenStatus.leadingAnchor.constraint(equalTo: nexusPairRow.leadingAnchor, constant: 140),
            nexusDeviceTokenStatus.trailingAnchor.constraint(equalTo: nexusPairRow.trailingAnchor),
            nexusDeviceTokenStatus.topAnchor.constraint(equalTo: nexusPairRow.topAnchor),
            nexusDeviceTokenStatus.bottomAnchor.constraint(equalTo: nexusPairRow.bottomAnchor),
        ])
        let nexusStack = NSStackView(views: [
            nexusEndpointRow,
            nexusPairingCodeRow,
            nexusPairRow,
        ])
        nexusStack.orientation = .vertical
        nexusStack.alignment = .leading
        nexusStack.spacing = 8
        for row in [nexusEndpointRow, nexusPairingCodeRow, nexusPairRow] {
            row.widthAnchor.constraint(equalTo: nexusStack.widthAnchor).isActive = true
        }

        let utilityRow = NSStackView(views: [openLogs, openConfig, NSView()])
        utilityRow.orientation = .horizontal
        utilityRow.spacing = 14

        let actionRow = NSStackView(views: [progress, statusLabel, NSView(), cancelButton, applyButton])
        actionRow.orientation = .horizontal
        actionRow.alignment = .centerY
        actionRow.spacing = 10
        statusLabel.setContentHuggingPriority(.defaultLow, for: .horizontal)
        statusLabel.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)

        let root = NSStackView(views: [
            sectionTitle(L10n.text("Startup")),
            startupStack,
            separator(),
            sectionTitle(L10n.text("Service")),
            serviceForm,
            separator(),
            sectionTitle("Coding Agent（ACP）"),
            acpStack,
            separator(),
            sectionTitle(L10n.text("Browser")),
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
        scrollDocumentView.addSubview(root)

        for separator in root.arrangedSubviews.compactMap({ $0 as? NSBox }) {
            separator.widthAnchor.constraint(equalTo: root.widthAnchor).isActive = true
        }
        for section in [startupStack, serviceForm, browserStack, acpStack, nexusStack, utilityRow, actionRow] {
            section.widthAnchor.constraint(equalTo: root.widthAnchor).isActive = true
        }

        NSLayoutConstraint.activate([
            scrollView.leadingAnchor.constraint(equalTo: contentView.leadingAnchor),
            scrollView.trailingAnchor.constraint(equalTo: contentView.trailingAnchor),
            scrollView.topAnchor.constraint(equalTo: contentView.topAnchor),
            scrollView.bottomAnchor.constraint(equalTo: contentView.bottomAnchor),
            scrollDocumentView.widthAnchor.constraint(equalTo: scrollView.contentView.widthAnchor),
            root.leadingAnchor.constraint(equalTo: scrollDocumentView.leadingAnchor, constant: 28),
            root.trailingAnchor.constraint(equalTo: scrollDocumentView.trailingAnchor, constant: -28),
            root.topAnchor.constraint(equalTo: scrollDocumentView.topAnchor, constant: 24),
            root.bottomAnchor.constraint(equalTo: scrollDocumentView.bottomAnchor, constant: -22),
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
        return box
    }

    private func formRow(title: String, control: NSView, fillsAvailableWidth: Bool = false) -> NSView {
        let row = NSView()
        let label = NSTextField(labelWithString: title)
        label.textColor = .secondaryLabelColor
        label.lineBreakMode = .byClipping
        label.translatesAutoresizingMaskIntoConstraints = false
        control.translatesAutoresizingMaskIntoConstraints = false
        label.widthAnchor.constraint(equalToConstant: 128).isActive = true
        label.setContentCompressionResistancePriority(.required, for: .horizontal)
        if fillsAvailableWidth {
            control.setContentHuggingPriority(.defaultLow, for: .horizontal)
            control.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        }

        row.addSubview(label)
        row.addSubview(control)
        NSLayoutConstraint.activate([
            label.leadingAnchor.constraint(equalTo: row.leadingAnchor),
            label.centerYAnchor.constraint(equalTo: row.centerYAnchor),
            control.leadingAnchor.constraint(equalTo: label.trailingAnchor, constant: 12),
            control.topAnchor.constraint(equalTo: row.topAnchor),
            control.bottomAnchor.constraint(equalTo: row.bottomAnchor),
            fillsAvailableWidth
                ? control.trailingAnchor.constraint(equalTo: row.trailingAnchor)
                : row.trailingAnchor.constraint(equalTo: control.trailingAnchor),
        ])
        return row
    }

    private func fitWindowToVisibleScreen() {
        guard let window else { return }
        let visibleFrame = (window.screen ?? NSScreen.main)?.visibleFrame
        let margin: CGFloat = 12
        guard let visibleFrame else { return }

        var frame = window.frame
        frame.size.width = min(frame.width, visibleFrame.width - margin * 2)
        frame.size.height = min(frame.height, visibleFrame.height - margin * 2)
        frame.origin.x = min(max(frame.origin.x, visibleFrame.minX + margin), visibleFrame.maxX - margin - frame.width)
        frame.origin.y = min(max(frame.origin.y, visibleFrame.minY + margin), visibleFrame.maxY - margin - frame.height)
        window.setFrame(frame, display: false)
    }

    @objc private func markChanged() {
        refreshApplyState()
    }

    @objc private func languageChanged() {
        guard !isUpdateInProgress else { return }
        let previous = L10n.languagePreference()
        let selected = selectedLanguagePreference()
        guard selected != previous else { return }

        let warning = NSAlert()
        warning.messageText = L10n.text("Change interface language?")
        warning.informativeText = L10n.text("Changing the interface language restarts the AgentDock interface. Any unsaved changes in this window will be lost.")
        warning.alertStyle = .warning
        warning.addButton(withTitle: L10n.text("Continue"))
        warning.addButton(withTitle: L10n.text("Cancel"))
        guard warning.runModal() == .alertFirstButtonReturn else {
            selectLanguagePreference(previous)
            return
        }

        L10n.setLanguagePreference(selected)
        let relaunch = Process()
        relaunch.executableURL = URL(fileURLWithPath: "/usr/bin/open")
        relaunch.arguments = ["-n", Bundle.main.bundlePath]
        do {
            // AppKit 的静态控件在创建时取本地化文本；只重启菜单栏 UI，Core/Tunnel 不受影响。
            try relaunch.run()
            NSApp.terminate(nil)
        } catch {
            L10n.setLanguagePreference(previous)
            selectLanguagePreference(previous)
            showStatus(L10n.format("Failed to restart AgentDock interface: %@", error.localizedDescription), isError: true)
        }
    }

    func controlTextDidChange(_ obj: Notification) {
        if obj.object as? NSTextField === browserCDPURL {
            refreshBrowserStatus()
        }
        refreshApplyState()
    }

    @objc private func acpChanged() {
        refreshApplyState()
    }

    @objc private func acpOverviewToggleChanged(_ sender: NSButton) {
        guard !controlsLocked, let key = sender.identifier?.rawValue else { return }
        if key.hasPrefix("builtin:") {
            let rawKind = String(key.dropFirst("builtin:".count))
            guard let preset = ACPAgentPreset(rawValue: rawKind), preset != .custom else { return }
            if let index = acpProfiles.firstIndex(where: { $0.kind == preset }) {
                updateACPProfileEnabled(at: index, enabled: sender.state == .on)
            } else if sender.state == .on {
                let resolution = preset.resolveAdapter()
                acpProfiles.append(ACPProfileConfiguration(
                    id: preset.rawValue,
                    displayName: nil,
                    kind: preset,
                    command: resolution.command,
                    args: resolution.arguments,
                    envFromEnv: nil,
                    enabled: true
                ))
                if acpDefaultProfile.isEmpty {
                    acpDefaultProfile = preset.rawValue
                }
            }
        } else if key.hasPrefix("profile:") {
            let profileID = String(key.dropFirst("profile:".count))
            guard let index = acpProfiles.firstIndex(where: { $0.id == profileID }) else { return }
            updateACPProfileEnabled(at: index, enabled: sender.state == .on)
        }
        refreshACPProfileOverview()
        refreshApplyState()
    }

    private func updateACPProfileEnabled(at index: Int, enabled: Bool) {
        let profileID = acpProfiles[index].id
        acpProfiles[index].enabled = enabled
        if !enabled, acpDefaultProfile == profileID {
            acpDefaultProfile = acpProfiles.first(where: { $0.id != profileID && $0.enabled })?.id ?? ""
        }
        if enabled, acpDefaultProfile.isEmpty {
            acpDefaultProfile = profileID
        }
    }

    @objc private func defaultACPProfileChanged() {
        guard !controlsLocked,
              let profileID = acpDefaultProfileMenu.selectedItem?.representedObject as? String,
              acpProfiles.contains(where: { $0.id == profileID && $0.enabled }) else { return }
        acpDefaultProfile = profileID
        refreshApplyState()
    }

    @objc private func editCustomACPProfile(_ sender: NSClickGestureRecognizer) {
        guard !controlsLocked,
              let key = sender.view?.identifier?.rawValue,
              key.hasPrefix("profile:") else { return }
        let profileID = String(key.dropFirst("profile:".count))
        guard let index = acpProfiles.firstIndex(where: { $0.id == profileID && $0.kind == .custom }),
              let result = showCustomACPProfileDialog(existing: acpProfiles[index]) else { return }
        if result.deleteRequested {
            let removedID = acpProfiles[index].id
            acpProfiles.remove(at: index)
            if acpDefaultProfile == removedID {
                acpDefaultProfile = acpProfiles.first(where: \.enabled)?.id ?? ""
            }
        } else {
            // profile.id 是已有 Session 的稳定身份；重命名或修改命令时只更新可见配置。
            acpProfiles[index].displayName = result.name
            acpProfiles[index].command = result.command
            acpProfiles[index].args = result.arguments
        }
        refreshACPProfileOverview()
        refreshApplyState()
    }

    @objc private func addCustomACPProfile() {
        guard !controlsLocked, let result = showCustomACPProfileDialog(existing: nil) else { return }
        let id = uniqueCustomACPProfileID(for: result.name)
        acpProfiles.append(ACPProfileConfiguration(
            id: id,
            displayName: result.name,
            kind: .custom,
            command: result.command,
            args: result.arguments,
            envFromEnv: nil,
            enabled: false
        ))
        refreshACPProfileOverview()
        refreshApplyState()
    }

    private func showCustomACPProfileDialog(existing: ACPProfileConfiguration?) -> CustomACPProfileDialogResult? {
        let editing = existing != nil
        let panel = NSPanel(
            contentRect: NSRect(x: 0, y: 0, width: 520, height: 252),
            styleMask: [.titled, .closable],
            backing: .buffered,
            defer: false
        )
        panel.title = L10n.text(editing ? "Edit custom ACP" : "Add custom ACP")
        panel.isReleasedWhenClosed = false

        let nameField = NSTextField(string: existing.map(acpDisplayName) ?? "")
        nameField.placeholderString = L10n.text("Agent name")
        let commandField = NSTextField(string: existing?.command ?? "")
        commandField.placeholderString = "/absolute/path/to/acp-adapter"
        let argsField = NSTextField(string: (try? ACPDesktopConfiguration.encodeArguments(existing?.args ?? [])) ?? "[]")
        argsField.placeholderString = "[]"

        let form = NSGridView(views: [
            [NSTextField(labelWithString: L10n.text("Name")), nameField],
            [NSTextField(labelWithString: L10n.text("Command")), commandField],
            [NSTextField(labelWithString: L10n.text("Args JSON")), argsField],
        ])
        form.rowSpacing = 10
        form.columnSpacing = 12
        form.column(at: 0).xPlacement = .leading
        form.column(at: 0).width = 72
        form.column(at: 1).xPlacement = .fill
        for field in [nameField, commandField, argsField] {
            field.widthAnchor.constraint(equalToConstant: 360).isActive = true
        }

        let confirmButton = NSButton(title: L10n.text(editing ? "Save" : "Add"), target: nil, action: nil)
        confirmButton.bezelStyle = .rounded
        confirmButton.keyEquivalent = "\r"
        let dialogCancelButton = NSButton(title: L10n.text("Cancel"), target: nil, action: nil)
        dialogCancelButton.bezelStyle = .rounded
        dialogCancelButton.keyEquivalent = "\u{1b}"
        let deleteButton = editing ? NSButton(title: L10n.text("Delete"), target: nil, action: nil) : nil
        deleteButton?.bezelStyle = .rounded

        let spacer = NSView()
        spacer.setContentHuggingPriority(.defaultLow, for: .horizontal)
        spacer.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        let buttons = NSStackView()
        buttons.orientation = .horizontal
        buttons.alignment = .centerY
        buttons.spacing = 8
        if let deleteButton {
            buttons.addArrangedSubview(deleteButton)
        }
        buttons.addArrangedSubview(spacer)
        buttons.addArrangedSubview(dialogCancelButton)
        buttons.addArrangedSubview(confirmButton)

        form.translatesAutoresizingMaskIntoConstraints = false
        buttons.translatesAutoresizingMaskIntoConstraints = false
        guard let contentView = panel.contentView else { return nil }
        contentView.addSubview(form)
        contentView.addSubview(buttons)
        NSLayoutConstraint.activate([
            form.leadingAnchor.constraint(equalTo: contentView.leadingAnchor, constant: 24),
            form.topAnchor.constraint(equalTo: contentView.topAnchor, constant: 24),
            form.trailingAnchor.constraint(lessThanOrEqualTo: contentView.trailingAnchor, constant: -24),
            buttons.leadingAnchor.constraint(equalTo: contentView.leadingAnchor, constant: 24),
            buttons.trailingAnchor.constraint(equalTo: contentView.trailingAnchor, constant: -24),
            buttons.bottomAnchor.constraint(equalTo: contentView.bottomAnchor, constant: -18),
        ])

        let deleteResponse = NSApplication.ModalResponse(rawValue: 1001)
        confirmButton.target = self
        confirmButton.action = #selector(confirmCustomACPDialog(_:))
        dialogCancelButton.target = self
        dialogCancelButton.action = #selector(cancelCustomACPDialog(_:))
        if let deleteButton {
            deleteButton.target = self
            deleteButton.action = #selector(deleteCustomACPDialog(_:))
        }
        activeCustomACPDialog = panel
        panel.delegate = self

        if let parentWindow = window {
            let parentFrame = parentWindow.frame
            let panelFrame = panel.frame
            panel.setFrameOrigin(NSPoint(
                x: parentFrame.midX - panelFrame.width / 2,
                y: parentFrame.midY - panelFrame.height / 2
            ))
        } else {
            panel.center()
        }
        panel.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
        let response = NSApp.runModal(for: panel)
        activeCustomACPDialog = nil
        panel.delegate = nil
        panel.orderOut(nil)

        if editing, response == deleteResponse {
            return CustomACPProfileDialogResult(name: "", command: "", arguments: [], deleteRequested: true)
        }
        guard response == .OK else { return nil }

        let name = nameField.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !name.isEmpty else {
            showStatus(L10n.text("Agent name cannot be empty."), isError: true)
            return nil
        }
        let arguments: [String]
        do {
            arguments = try ACPDesktopConfiguration.decodeArguments(argsField.stringValue)
        } catch {
            showStatus(error.localizedDescription, isError: true)
            return nil
        }
        return CustomACPProfileDialogResult(
            name: name,
            command: commandField.stringValue.trimmingCharacters(in: .whitespacesAndNewlines),
            arguments: arguments,
            deleteRequested: false
        )
    }

    @objc private func confirmCustomACPDialog(_ sender: Any?) {
        NSApp.stopModal(withCode: .OK)
    }

    @objc private func cancelCustomACPDialog(_ sender: Any?) {
        NSApp.stopModal(withCode: .cancel)
    }

    @objc private func deleteCustomACPDialog(_ sender: Any?) {
        NSApp.stopModal(withCode: NSApplication.ModalResponse(rawValue: 1001))
    }

    func windowShouldClose(_ sender: NSWindow) -> Bool {
        guard sender === activeCustomACPDialog else { return true }
        NSApp.stopModal(withCode: .cancel)
        return false
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
            showStatus(L10n.text("No supported Chromium-based browser was detected and no external CDP is configured."), isError: true)
        }
        refreshBrowserStatus()
        refreshApplyState()
    }

    @objc private func applyPressed() {
        guard !isUpdateInProgress else { return }
        guard currentConfiguration != nil else { return }
        if acpEnabled.state == .on, !acpProfiles.contains(where: { $0.id == acpDefaultProfile && $0.enabled }) {
            showStatus(L10n.text("Choose a default Coding Agent profile."), isError: true)
            return
        }
        let browserMode = selectedBrowserConnectionMode()
        let configuredCDP = browserCDPURL.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
        if browserMode == .specifiedCDP, configuredCDP.isEmpty {
            showStatus(L10n.text("A CDP address is required when “Connect to a specified CDP browser” is selected."), isError: true)
            return
        }
        let settings = EditableServiceSettings(
            port: portField.integerValue,
            logLevel: logLevel.titleOfSelectedItem ?? "info",
            mcpAppsEnabled: mcpAppsEnabled.state == .on,
            browserEnabled: browserEnabled.state == .on,
            browserCDPURL: browserMode == .specifiedCDP ? configuredCDP : "",
            browserReuseExistingCDP: browserMode == .reuseExisting,
            acpEnabled: acpEnabled.state == .on,
            acpProfiles: acpProfiles,
            acpDefaultProfile: acpDefaultProfile
        )
        setBusy(true)
        showStatus(L10n.text("Saving configuration and validating AgentDock…"), isError: false)
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
                initialMCPAppsEnabled = validatedSettings.mcpAppsEnabled
                initialBrowserEnabled = validatedSettings.browserEnabled
                initialBrowserCDPURL = validatedSettings.browserCDPURL
                initialBrowserConnectionMode = BrowserConnectionMode.resolve(
                    cdpURL: validatedSettings.browserCDPURL,
                    reuseExisting: validatedSettings.browserReuseExistingCDP
                )
                initialACPEnabled = validatedSettings.acpEnabled
                initialACPProfiles = validatedSettings.acpProfiles
                initialACPDefaultProfile = validatedSettings.acpDefaultProfile
                acpProfiles = validatedSettings.acpProfiles
                acpDefaultProfile = validatedSettings.acpDefaultProfile
                refreshACPProfileOverview()
                portField.integerValue = initialPort
                logLevel.selectItem(withTitle: initialLogLevel)
                mcpAppsEnabled.state = initialMCPAppsEnabled ? .on : .off
                browserCDPURL.stringValue = initialBrowserCDPURL
                selectBrowserConnectionMode(initialBrowserConnectionMode)
                if let updatedConfiguration = ServiceConfiguration.load(from: service.paths.environment) {
                    currentConfiguration = updatedConfiguration
                }
                refreshBrowserStatus()
                showStatus(L10n.text("Settings saved."), isError: false)
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
        guard !isUpdateInProgress else { return }
        let endpoint = nexusEndpoint.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
        let pairingCode = nexusPairingCode.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !endpoint.isEmpty, !pairingCode.isEmpty else {
            showStatus(L10n.text("Enter the NexusDock address and one-time pairing code."), isError: true)
            return
        }
        setBusy(true)
        showStatus(L10n.text("Pairing and restarting AgentDock…"), isError: false)
        Task {
            do {
                try await service.pairNexus(endpoint: endpoint, pairingCode: pairingCode)
                nexusPairingCode.stringValue = ""
                refreshNexusStatus()
                showStatus(L10n.text("NexusDock pairing completed and the Device Token was saved securely."), isError: false)
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

    private func selectedLanguagePreference() -> UILanguagePreference {
        guard let rawValue = languagePreference.selectedItem?.representedObject as? String,
              let preference = UILanguagePreference(rawValue: rawValue) else {
            return .system
        }
        return preference
    }

    private func selectLanguagePreference(_ preference: UILanguagePreference) {
        for item in languagePreference.itemArray where (item.representedObject as? String) == preference.rawValue {
            languagePreference.select(item)
            return
        }
        languagePreference.selectItem(at: 0)
    }

    private func refreshACPProfileOverview() {
        for view in acpProfileList.arrangedSubviews {
            acpProfileList.removeArrangedSubview(view)
            view.removeFromSuperview()
        }

        for preset in [ACPAgentPreset.codex, .claude, .grok] {
            let profile = acpProfiles.first(where: { $0.kind == preset })
            addACPOverviewRow(acpOverviewRow(preset: preset, profile: profile))
        }
        for profile in acpProfiles where profile.kind == .custom {
            addACPOverviewRow(acpOverviewRow(preset: .custom, profile: profile))
        }
        refreshACPDefaultProfileMenu()
        acpAddCustomProfile.isEnabled = !controlsLocked
    }

    private func refreshACPDefaultProfileMenu() {
        let enabledProfiles = acpProfiles.filter(\.enabled)
        if !enabledProfiles.contains(where: { $0.id == acpDefaultProfile }) {
            acpDefaultProfile = enabledProfiles.first?.id ?? ""
        }
        acpDefaultProfileMenu.removeAllItems()
        for profile in enabledProfiles {
            acpDefaultProfileMenu.addItem(withTitle: acpDisplayName(profile))
            acpDefaultProfileMenu.lastItem?.representedObject = profile.id
        }
        if let index = enabledProfiles.firstIndex(where: { $0.id == acpDefaultProfile }) {
            acpDefaultProfileMenu.selectItem(at: index)
        }
        acpDefaultProfileMenu.isEnabled = !controlsLocked && !enabledProfiles.isEmpty
    }

    private func addACPOverviewRow(_ row: NSView) {
        // 必须先把行加入 StackView 层级，再激活跨视图宽度约束；
        // 否则 AppKit 会因为两边尚无共同祖先直接抛出 NSGenericException。
        acpProfileList.addArrangedSubview(row)
        row.widthAnchor.constraint(equalTo: acpProfileList.widthAnchor).isActive = true
    }

    private func acpOverviewRow(preset: ACPAgentPreset, profile: ACPProfileConfiguration?) -> NSView {
        let title = profile.map(acpDisplayName) ?? preset.title
        let key = profile.map { "profile:\($0.id)" } ?? "builtin:\(preset.rawValue)"
        let nameView = NSTextField(labelWithString: title)
        nameView.font = .systemFont(ofSize: 13, weight: .medium)
        if profile?.kind == .custom {
            nameView.identifier = NSUserInterfaceItemIdentifier(key)
            let editGesture = NSClickGestureRecognizer(target: self, action: #selector(editCustomACPProfile(_:)))
            nameView.addGestureRecognizer(editGesture)
        }

        let toggle = NSButton(checkboxWithTitle: "", target: self, action: #selector(acpOverviewToggleChanged(_:)))
        toggle.identifier = NSUserInterfaceItemIdentifier(key)
        toggle.state = profile?.enabled == true ? .on : .off
        toggle.isEnabled = !controlsLocked

        let row = NSView()
        for view in [nameView, toggle] {
            view.translatesAutoresizingMaskIntoConstraints = false
            row.addSubview(view)
        }
        NSLayoutConstraint.activate([
            row.heightAnchor.constraint(equalToConstant: 28),
            nameView.leadingAnchor.constraint(equalTo: row.leadingAnchor, constant: 4),
            nameView.centerYAnchor.constraint(equalTo: row.centerYAnchor),
            nameView.trailingAnchor.constraint(lessThanOrEqualTo: toggle.leadingAnchor, constant: -12),
            toggle.trailingAnchor.constraint(equalTo: row.trailingAnchor, constant: -6),
            toggle.centerYAnchor.constraint(equalTo: row.centerYAnchor),
        ])
        return row
    }

    private func acpDisplayName(_ profile: ACPProfileConfiguration) -> String {
        guard profile.kind == .custom else { return profile.kind.title }
        let name = profile.displayName?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return name.isEmpty ? profile.id : name
    }

    private func uniqueCustomACPProfileID(for name: String) -> String {
        let latin = name.applyingTransform(.toLatin, reverse: false) ?? name
        var base = latin.lowercased().unicodeScalars.map { scalar -> Character in
            let value = scalar.value
            if (48...57).contains(value) || (97...122).contains(value) {
                return Character(String(scalar))
            }
            return "-"
        }.reduce(into: "") { $0.append($1) }
        while base.contains("--") { base = base.replacingOccurrences(of: "--", with: "-") }
        base = base.trimmingCharacters(in: CharacterSet(charactersIn: "-"))
        if base.isEmpty { base = "custom" }
        if ["codex", "claude", "grok"].contains(base) { base += "-custom" }
        if base.count > 48 { base = String(base.prefix(48)).trimmingCharacters(in: CharacterSet(charactersIn: "-")) }

        let existing = Set(acpProfiles.map(\.id))
        if !existing.contains(base) { return base }
        var suffix = 2
        while existing.contains("\(base)-\(suffix)") { suffix += 1 }
        return "\(base)-\(suffix)"
    }

    private func selectedBrowserConnectionMode() -> BrowserConnectionMode {
        let title = browserConnectionMode.titleOfSelectedItem ?? ""
        return BrowserConnectionMode.allCases.first { $0.title == title } ?? .managed
    }

    private func selectBrowserConnectionMode(_ mode: BrowserConnectionMode) {
        browserConnectionMode.selectItem(withTitle: mode.title)
    }

    private func refreshBrowserStatus() {
        let enabled = browserEnabled.state == .on
        let mode = selectedBrowserConnectionMode()
        let configuredCDP = browserCDPURL.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
        browserCDPRow?.isHidden = mode != .specifiedCDP
        browserCDPURL.isEnabled = !controlsLocked && mode == .specifiedCDP

        switch mode {
        case .specifiedCDP:
            if configuredCDP.isEmpty {
                browserStatus.stringValue = L10n.text("Enter the CDP address to connect to.")
                browserStatus.textColor = .systemRed
                return
            }
            browserStatus.stringValue = L10n.text("Use the specified CDP browser. Existing login state in that browser may be reused.")
            browserStatus.textColor = .secondaryLabelColor
        case .reuseExisting:
            browserStatus.stringValue = L10n.text("Prefer an existing local CDP browser; fall back to an isolated browser when none is found. Existing login state may be reused.")
            browserStatus.textColor = .secondaryLabelColor
        case .managed:
            if BrowserSupportController.detectExecutable() != nil {
                browserStatus.stringValue = L10n.text("Use an isolated browser without reusing the login state from your everyday browser.")
                browserStatus.textColor = .secondaryLabelColor
            } else {
                browserStatus.stringValue = L10n.text("No supported Chromium-based browser was detected.")
                browserStatus.textColor = enabled ? .systemRed : .secondaryLabelColor
            }
        }
    }

    private func refreshNexusStatus() {
        let status = service.nexusDeviceStatus()
        if status.paired {
            nexusEndpoint.stringValue = status.endpoint
            nexusDeviceTokenStatus.stringValue = L10n.format("Saved securely · node_id=%@", status.nodeID)
            nexusDeviceTokenStatus.textColor = .secondaryLabelColor
            return
        }
        nexusDeviceTokenStatus.stringValue = status.error
            ?? L10n.text("Not paired yet; Device Token will be generated automatically during one-time pairing.")
        nexusDeviceTokenStatus.textColor = status.error == nil ? .secondaryLabelColor : .systemRed
    }

    private func refreshApplyState() {
        guard !controlsLocked, currentConfiguration != nil else {
            applyButton.isEnabled = false
            return
        }
        let acpIsEnabled = acpEnabled.state == .on
        let acpSettingsChanged = acpIsEnabled != initialACPEnabled
            || acpProfiles != initialACPProfiles
            || acpDefaultProfile != initialACPDefaultProfile
        let browserMode = selectedBrowserConnectionMode()
        let browserCDP = browserMode == .specifiedCDP
            ? browserCDPURL.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
            : ""
        let changed = (serviceAutostart.state == .on) != initialServiceAutostart
            || (menuAutostart.state == .on) != initialMenuAutostart
            || portField.integerValue != initialPort
            || (logLevel.titleOfSelectedItem ?? "info") != initialLogLevel
            || (mcpAppsEnabled.state == .on) != initialMCPAppsEnabled
            || (browserEnabled.state == .on) != initialBrowserEnabled
            || browserMode != initialBrowserConnectionMode
            || browserCDP != initialBrowserCDPURL
            || acpSettingsChanged
        applyButton.isEnabled = changed
        nexusPairButton.isEnabled = !controlsLocked
            && !nexusEndpoint.stringValue.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            && !nexusPairingCode.stringValue.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    private func setBusy(_ busy: Bool) {
        isBusy = busy
        let locked = controlsLocked
        for control in [languagePreference, serviceAutostart, menuAutostart, portField, logLevel, mcpAppsEnabled, browserEnabled, browserConnectionMode, browserCDPURL, acpEnabled, acpDefaultProfileMenu, acpAddCustomProfile, nexusEndpoint, nexusPairingCode, nexusPairButton] {
            control.isEnabled = !locked
        }
        refreshACPProfileOverview()
        refreshBrowserStatus()
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
