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
final class AdvancedSettingsWindowController: NSWindowController, NSTextFieldDelegate {
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
    private let acpProfile = NSPopUpButton(frame: .zero, pullsDown: false)
    private let acpProfileID = NSTextField(string: "")
    private let acpProfileEnabled = NSButton(checkboxWithTitle: L10n.text("Enable this profile"), target: nil, action: nil)
    private let acpProfileDefault = NSButton(checkboxWithTitle: L10n.text("Use as default"), target: nil, action: nil)
    private let acpAddProfile = NSPopUpButton(frame: .zero, pullsDown: true)
    private let acpRemoveProfile = NSButton(title: L10n.text("Remove profile"), target: nil, action: nil)
    private let acpAgent = NSPopUpButton(frame: .zero, pullsDown: false)
    private let acpCommand = NSTextField(string: "")
    private let acpArgsJSON = NSTextField(string: "[]")
    private let acpStatus = NSTextField(wrappingLabelWithString: "")
    private let nexusEndpoint = NSTextField(string: "")
    private let nexusPairingCode = NSSecureTextField(string: "")
    private let nexusPairButton = NSButton(title: L10n.text("Pair and restart"), target: nil, action: nil)
    private let nexusDeviceTokenStatus = NSTextField(labelWithString: "")
    private let progress = NSProgressIndicator()
    private let statusLabel = NSTextField(wrappingLabelWithString: "")
    private let applyButton = NSButton(title: L10n.text("Apply and restart"), target: nil, action: nil)
    private let cancelButton = NSButton(title: L10n.text("Cancel"), target: nil, action: nil)

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
    private var activeACPProfileID = ""
    private var isBusy = false
    private var isUpdateInProgress = false
    private var browserCDPRow: NSView?
    private var acpCommandRow: NSView?
    private var acpArgsRow: NSView?

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
            contentRect: NSRect(x: 0, y: 0, width: 680, height: 760),
            styleMask: [.titled, .closable, .resizable],
            backing: .buffered,
            defer: false
        )
        window.title = L10n.text("AgentDock Advanced Settings")
        window.isReleasedWhenClosed = false
        window.minSize = NSSize(width: 620, height: 520)
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
        activeACPProfileID = configuration.acpDefaultProfile

        serviceAutostart.state = status.autostartEnabled ? .on : .off
        menuAutostart.state = initialMenuAutostart ? .on : .off
        portField.integerValue = initialPort
        logLevel.selectItem(withTitle: initialLogLevel)
        mcpAppsEnabled.state = initialMCPAppsEnabled ? .on : .off
        browserEnabled.state = initialBrowserEnabled ? .on : .off
        browserCDPURL.stringValue = initialBrowserCDPURL
        selectBrowserConnectionMode(initialBrowserConnectionMode)
        acpEnabled.state = initialACPEnabled ? .on : .off
        refreshACPProfileMenu(selecting: activeACPProfileID)
        loadACPProfileIntoControls(activeACPProfileID)
        nexusPairingCode.stringValue = ""
        refreshNexusStatus()
        refreshBrowserStatus()
        refreshACPStatus()
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
        languagePreference.widthAnchor.constraint(equalToConstant: 220).isActive = true
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
        logLevel.widthAnchor.constraint(equalToConstant: 120).isActive = true
        logLevel.target = self
        logLevel.action = #selector(markChanged)

        mcpAppsEnabled.target = self
        mcpAppsEnabled.action = #selector(markChanged)

        browserEnabled.target = self
        browserEnabled.action = #selector(browserToggled)
        browserConnectionMode.addItems(withTitles: BrowserConnectionMode.allCases.map(\.title))
        browserConnectionMode.widthAnchor.constraint(equalToConstant: 360).isActive = true
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

        acpProfile.widthAnchor.constraint(equalToConstant: 220).isActive = true
        acpProfile.target = self
        acpProfile.action = #selector(acpProfileChanged)
        acpProfileID.placeholderString = "zcode"
        acpProfileID.target = self
        acpProfileID.action = #selector(markChanged)
        acpProfileID.delegate = self
        acpProfileEnabled.target = self
        acpProfileEnabled.action = #selector(acpProfileFlagsChanged)
        acpProfileDefault.target = self
        acpProfileDefault.action = #selector(acpProfileFlagsChanged)
        acpAddProfile.addItem(withTitle: L10n.text("Add profile…"))
        for preset in ACPAgentPreset.allCases {
            acpAddProfile.addItem(withTitle: preset.title)
            acpAddProfile.lastItem?.representedObject = preset.rawValue
        }
        acpAddProfile.target = self
        acpAddProfile.action = #selector(addACPProfile)
        acpRemoveProfile.target = self
        acpRemoveProfile.action = #selector(removeACPProfile)

        acpAgent.addItems(withTitles: ACPAgentPreset.allCases.map(\.title))
        acpAgent.widthAnchor.constraint(equalToConstant: 220).isActive = true
        acpAgent.target = self
        acpAgent.action = #selector(acpChanged)
        acpCommand.placeholderString = "/absolute/path/to/acp-adapter"
        acpCommand.target = self
        acpCommand.action = #selector(markChanged)
        acpCommand.delegate = self
        acpArgsJSON.placeholderString = "[]"
        acpArgsJSON.target = self
        acpArgsJSON.action = #selector(markChanged)
        acpArgsJSON.delegate = self
        acpStatus.textColor = .secondaryLabelColor
        acpStatus.font = .systemFont(ofSize: 12)

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

        for flexibleView in [browserCDPURL, browserStatus, acpProfileID, acpCommand, acpArgsJSON, acpStatus, nexusEndpoint, nexusPairingCode, nexusDeviceTokenStatus] {
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
            formRow(title: L10n.text("Interface language"), control: languagePreference),
        ])
        startupStack.orientation = .vertical
        startupStack.alignment = .leading
        startupStack.spacing = 8

        let serviceForm = NSStackView(views: [
            mcpAppsEnabled,
            formRow(title: L10n.text("Service port"), control: portField),
            formRow(title: L10n.text("Log level"), control: logLevel),
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

        let commandRow = formRow(title: "Command", control: acpCommand, fillsAvailableWidth: true)
        let argsRow = formRow(title: "Args JSON", control: acpArgsJSON, fillsAvailableWidth: true)
        let profileIDRow = formRow(title: "Profile ID", control: acpProfileID, fillsAvailableWidth: true)
        let profileActions = NSStackView(views: [acpProfileEnabled, acpProfileDefault, acpAddProfile, acpRemoveProfile])
        profileActions.orientation = .horizontal
        profileActions.spacing = 8
        acpCommandRow = commandRow
        acpArgsRow = argsRow
        let acpStack = NSStackView(views: [
            acpEnabled,
            formRow(title: L10n.text("Profile"), control: acpProfile),
            profileIDRow,
            profileActions,
            formRow(title: L10n.text("Type"), control: acpAgent),
            commandRow,
            argsRow,
            acpStatus,
        ])
        acpStack.orientation = .vertical
        acpStack.alignment = .leading
        acpStack.spacing = 8
        commandRow.widthAnchor.constraint(equalTo: acpStack.widthAnchor).isActive = true
        argsRow.widthAnchor.constraint(equalTo: acpStack.widthAnchor).isActive = true
        profileIDRow.widthAnchor.constraint(equalTo: acpStack.widthAnchor).isActive = true
        acpStatus.widthAnchor.constraint(equalTo: acpStack.widthAnchor).isActive = true

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
        if obj.object as? NSTextField === acpProfileID
            || obj.object as? NSTextField === acpCommand
            || obj.object as? NSTextField === acpArgsJSON {
            refreshACPStatus()
        }
        refreshApplyState()
    }

    @objc private func acpChanged() {
        refreshACPStatus()
        refreshApplyState()
    }

    @objc private func acpProfileChanged() {
        guard let selectedID = acpProfile.selectedItem?.representedObject as? String,
              selectedID != activeACPProfileID else { return }
        guard saveActiveACPProfileFromControls(showErrors: true) else {
            refreshACPProfileMenu(selecting: activeACPProfileID)
            return
        }
        refreshACPProfileMenu(selecting: selectedID)
        activeACPProfileID = selectedID
        loadACPProfileIntoControls(selectedID)
        refreshACPStatus()
        refreshApplyState()
    }

    @objc private func acpProfileFlagsChanged() {
        guard !activeACPProfileID.isEmpty else { return }
        if acpProfileDefault.state == .off, acpDefaultProfile == activeACPProfileID {
            acpProfileDefault.state = .on
        } else if acpProfileDefault.state == .on {
            acpDefaultProfile = activeACPProfileID
        }
        if acpProfileDefault.state == .on, acpProfileEnabled.state == .off {
            acpProfileEnabled.state = .on
        }
        _ = saveActiveACPProfileFromControls(showErrors: false)
        refreshACPProfileMenu(selecting: activeACPProfileID)
        refreshACPStatus()
        refreshApplyState()
    }

    @objc private func addACPProfile() {
        defer { acpAddProfile.selectItem(at: 0) }
        guard let raw = acpAddProfile.selectedItem?.representedObject as? String,
              let preset = ACPAgentPreset(rawValue: raw) else { return }
        guard saveActiveACPProfileFromControls(showErrors: true) else { return }

        let id: String
        if preset == .custom {
            var candidate = "custom"
            var suffix = 2
            let existing = Set(acpProfiles.map(\.id))
            while existing.contains(candidate) {
                candidate = "custom-\(suffix)"
                suffix += 1
            }
            id = candidate
        } else {
            id = preset.rawValue
            if acpProfiles.contains(where: { $0.id == id }) {
                activeACPProfileID = id
                refreshACPProfileMenu(selecting: id)
                loadACPProfileIntoControls(id)
                return
            }
        }

        let resolution = preset == .custom ? nil : preset.resolveAdapter()
        acpProfiles.append(ACPProfileConfiguration(
            id: id,
            kind: preset,
            command: resolution?.command ?? "",
            args: resolution?.arguments ?? [],
            envFromEnv: nil,
            enabled: true
        ))
        if acpDefaultProfile.isEmpty {
            acpDefaultProfile = id
        }
        activeACPProfileID = id
        refreshACPProfileMenu(selecting: id)
        loadACPProfileIntoControls(id)
        refreshACPStatus()
        refreshApplyState()
    }

    @objc private func removeACPProfile() {
        guard acpProfiles.count > 1,
              let index = acpProfiles.firstIndex(where: { $0.id == activeACPProfileID }) else { return }
        let removedID = acpProfiles[index].id
        acpProfiles.remove(at: index)
        if acpDefaultProfile == removedID {
            acpDefaultProfile = acpProfiles.first(where: \.enabled)?.id ?? acpProfiles[0].id
        }
        activeACPProfileID = acpProfiles[min(index, acpProfiles.count - 1)].id
        refreshACPProfileMenu(selecting: activeACPProfileID)
        loadACPProfileIntoControls(activeACPProfileID)
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
            showStatus(L10n.text("No supported Chromium-based browser was detected and no external CDP is configured."), isError: true)
        }
        refreshBrowserStatus()
        refreshApplyState()
    }

    @objc private func applyPressed() {
        guard !isUpdateInProgress else { return }
        guard currentConfiguration != nil else { return }
        guard saveActiveACPProfileFromControls(showErrors: true) else { return }
        guard acpProfiles.contains(where: { $0.id == acpDefaultProfile }) else {
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
                if !acpProfiles.contains(where: { $0.id == activeACPProfileID }) {
                    activeACPProfileID = acpDefaultProfile
                }
                refreshACPProfileMenu(selecting: activeACPProfileID)
                loadACPProfileIntoControls(activeACPProfileID)
                portField.integerValue = initialPort
                logLevel.selectItem(withTitle: initialLogLevel)
                mcpAppsEnabled.state = initialMCPAppsEnabled ? .on : .off
                browserCDPURL.stringValue = initialBrowserCDPURL
                selectBrowserConnectionMode(initialBrowserConnectionMode)
                if let updatedConfiguration = ServiceConfiguration.load(from: service.paths.environment) {
                    currentConfiguration = updatedConfiguration
                }
                refreshBrowserStatus()
                refreshACPStatus()
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

    private func refreshACPProfileMenu(selecting profileID: String) {
        acpProfile.removeAllItems()
        for profile in acpProfiles {
            let suffix = profile.id == acpDefaultProfile ? " · Default" : ""
            acpProfile.addItem(withTitle: "\(profile.id) · \(profile.kind.title)\(suffix)")
            acpProfile.lastItem?.representedObject = profile.id
        }
        if let item = acpProfile.itemArray.first(where: { ($0.representedObject as? String) == profileID }) {
            acpProfile.select(item)
        } else if !acpProfiles.isEmpty {
            acpProfile.selectItem(at: 0)
        }
    }

    private func loadACPProfileIntoControls(_ profileID: String) {
        guard let profile = acpProfiles.first(where: { $0.id == profileID }) else {
            acpProfileID.stringValue = ""
            acpProfileEnabled.state = .off
            acpProfileDefault.state = .off
            acpAgent.selectItem(withTitle: ACPAgentPreset.custom.title)
            acpCommand.stringValue = ""
            acpArgsJSON.stringValue = "[]"
            return
        }
        activeACPProfileID = profile.id
        acpProfileID.stringValue = profile.id
        acpProfileEnabled.state = profile.enabled ? .on : .off
        acpProfileDefault.state = profile.id == acpDefaultProfile ? .on : .off
        acpAgent.selectItem(withTitle: profile.kind.title)
        acpCommand.stringValue = profile.kind == .custom ? profile.command : ""
        acpArgsJSON.stringValue = (try? ACPDesktopConfiguration.encodeArguments(
            profile.kind == .custom ? profile.args : []
        )) ?? "[]"
    }

    @discardableResult
    private func saveActiveACPProfileFromControls(showErrors: Bool) -> Bool {
        guard let index = acpProfiles.firstIndex(where: { $0.id == activeACPProfileID }) else {
            return acpProfiles.isEmpty
        }
        var profile = acpProfiles[index]
        let oldID = profile.id
        let newID = profile.kind == .custom
            ? acpProfileID.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
            : profile.kind.rawValue
        if newID.isEmpty || acpProfiles.enumerated().contains(where: { $0.offset != index && $0.element.id == newID }) {
            if showErrors {
                showStatus(L10n.text("Coding Agent profile IDs must be non-empty and unique."), isError: true)
            }
            return false
        }

        if profile.kind == .custom {
            do {
                profile.args = try ACPDesktopConfiguration.decodeArguments(acpArgsJSON.stringValue)
            } catch {
                if showErrors {
                    showStatus(error.localizedDescription, isError: true)
                }
                return false
            }
            profile.command = acpCommand.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
        }
        profile.id = newID
        profile.enabled = acpProfileEnabled.state == .on
        acpProfiles[index] = profile
        if acpDefaultProfile == oldID {
            acpDefaultProfile = newID
        }
        activeACPProfileID = newID
        return true
    }

    private func acpControlDiffersFromModel() -> Bool {
        guard let profile = acpProfiles.first(where: { $0.id == activeACPProfileID }) else { return false }
        let displayedID = acpProfileID.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
        if displayedID != profile.id || (acpProfileEnabled.state == .on) != profile.enabled {
            return true
        }
        if (acpProfileDefault.state == .on) != (profile.id == acpDefaultProfile) {
            return true
        }
        guard profile.kind == .custom else { return false }
        if acpCommand.stringValue.trimmingCharacters(in: .whitespacesAndNewlines) != profile.command {
            return true
        }
        let encoded = (try? ACPDesktopConfiguration.encodeArguments(profile.args)) ?? "[]"
        return acpArgsJSON.stringValue.trimmingCharacters(in: .whitespacesAndNewlines) != encoded
    }

    private func selectedBrowserConnectionMode() -> BrowserConnectionMode {
        let title = browserConnectionMode.titleOfSelectedItem ?? ""
        return BrowserConnectionMode.allCases.first { $0.title == title } ?? .managed
    }

    private func selectBrowserConnectionMode(_ mode: BrowserConnectionMode) {
        browserConnectionMode.selectItem(withTitle: mode.title)
    }

    private func refreshACPStatus() {
        guard let profile = acpProfiles.first(where: { $0.id == activeACPProfileID }) else {
            acpStatus.stringValue = L10n.text("Add a Coding Agent profile to continue.")
            acpStatus.textColor = .secondaryLabelColor
            for control in [acpProfile, acpProfileID, acpProfileEnabled, acpProfileDefault, acpAgent, acpCommand, acpArgsJSON, acpRemoveProfile] {
                control.isEnabled = false
            }
            acpAddProfile.isEnabled = !controlsLocked
            return
        }

        let preset = profile.kind
        let isCustom = preset == .custom
        let globallyEnabled = acpEnabled.state == .on
        let profileEnabled = acpProfileEnabled.state == .on
        let effectiveEnabled = globallyEnabled && profileEnabled
        acpCommandRow?.isHidden = !isCustom
        acpArgsRow?.isHidden = !isCustom

        acpProfile.isEnabled = !controlsLocked
        acpProfileID.isEnabled = !controlsLocked && isCustom
        acpProfileEnabled.isEnabled = !controlsLocked
        acpProfileDefault.isEnabled = !controlsLocked
        acpAddProfile.isEnabled = !controlsLocked
        acpRemoveProfile.isEnabled = !controlsLocked && acpProfiles.count > 1
        acpAgent.isEnabled = false
        acpCommand.isEnabled = !controlsLocked && isCustom
        acpArgsJSON.isEnabled = !controlsLocked && isCustom

        let configuredCommand = isCustom
            ? acpCommand.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
            : profile.command
        let configuredArguments: [String]
        if isCustom {
            guard let arguments = try? ACPDesktopConfiguration.decodeArguments(acpArgsJSON.stringValue) else {
                acpStatus.stringValue = L10n.text("Args JSON must be a JSON string array.")
                acpStatus.textColor = effectiveEnabled ? .systemRed : .secondaryLabelColor
                return
            }
            configuredArguments = arguments
        } else {
            configuredArguments = profile.args
        }
        let resolution = preset.resolveAdapter(
            configuredCommand: configuredCommand,
            configuredArguments: configuredArguments
        )
        if resolution.available {
            acpStatus.stringValue = effectiveEnabled
                ? resolution.message
                : L10n.format("Configured %@ · takes effect when enabled", profile.id)
            acpStatus.textColor = .secondaryLabelColor
        } else {
            acpStatus.stringValue = resolution.message
            acpStatus.textColor = effectiveEnabled ? .systemRed : .secondaryLabelColor
        }
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
            || acpControlDiffersFromModel()
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
        for control in [languagePreference, serviceAutostart, menuAutostart, portField, logLevel, mcpAppsEnabled, browserEnabled, browserConnectionMode, browserCDPURL, acpEnabled, acpProfile, acpProfileID, acpProfileEnabled, acpProfileDefault, acpAddProfile, acpRemoveProfile, acpAgent, acpCommand, acpArgsJSON, nexusEndpoint, nexusPairingCode, nexusPairButton] {
            control.isEnabled = !locked
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
