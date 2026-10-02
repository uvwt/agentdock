import AppKit
import Foundation

private enum QuickTunnelRefreshState {
    case idle
    case refreshing
    case failed
}

private enum ControlPanelPage: Int, CaseIterable {
    case home
    case connections
    case capabilities
    case activity
    case settings

    var title: String {
        switch self {
        case .home: return L10n.text("Home")
        case .connections: return L10n.text("Connections")
        case .capabilities: return L10n.text("Capabilities")
        case .activity: return L10n.text("Activity")
        case .settings: return L10n.text("Settings")
        }
    }

    var symbol: String {
        switch self {
        case .home: return "house"
        case .connections: return "link"
        case .capabilities: return "square.grid.2x2"
        case .activity: return "clock.arrow.circlepath"
        case .settings: return "gearshape"
        }
    }
}

@MainActor
final class SetupWindowController: NSWindowController, NSWindowDelegate {
    private lazy var installer = InstallerRunner(service: service)
    private let publicEndpointChecker = PublicEndpointChecker()
    private let service: ServiceController
    private let menuLoginAgent: MenuLoginAgentController
    private let onChanged: () -> Void
    private let onUpdateRequested: () -> Void

    private let titleLabel = NSTextField(labelWithString: "AgentDock")
    private let subtitleLabel = NSTextField(labelWithString: L10n.text("Local MCP service and public access management"))
    private let stateLabel = NSTextField(labelWithString: L10n.text("Not installed"))
    private let nexusStateLabel = NSTextField(labelWithString: L10n.text("Not configured"))

    // 文档内容不足一屏时保持顶部对齐，把剩余空间自然留在底部。
    private let scrollDocumentView = TopAlignedDocumentView()
    private let contentStack = TopAlignedStackView()
    private let serviceSection = NSStackView()
    private var serviceCard: NSBox?
    private let localAddress = NSTextField(labelWithString: L10n.text("Not installed"))
    private let publicAddress = NSTextField(labelWithString: L10n.text("Disabled"))
    private let publicCheckStatus = NSTextField(labelWithString: "")
    private let publicTestButton = NSButton(title: L10n.text("Test"), target: nil, action: nil)
    private let publicCopyButton = NSButton(title: L10n.text("Copy"), target: nil, action: nil)
    private let authToken = NSTextField(labelWithString: L10n.text("Not generated"))
    private let oauthPassword = NSTextField(labelWithString: L10n.text("Not generated"))
    private let authReveal = NSButton(title: L10n.text("Show"), target: nil, action: nil)
    private let oauthReveal = NSButton(title: L10n.text("Show"), target: nil, action: nil)
    private let startStopButton = NSButton(title: L10n.text("Start service"), target: nil, action: nil)
    private let restartButton = NSButton(title: L10n.text("Restart"), target: nil, action: nil)
    private let updateButton = NSButton(title: L10n.text("Check for updates"), target: nil, action: nil)

    private let publicMode = NSSegmentedControl(
        labels: [L10n.text("Local only"), L10n.text("Temporary address"), L10n.text("Custom domain")],
        trackingMode: .selectOne,
        target: nil,
        action: nil
    )
    private let modeDescription = NSTextField(wrappingLabelWithString: "")
    private let namedFields = NSStackView()
    private let serverURLField = NSTextField(string: "")
    private let tunnelTokenField = NSSecureTextField(string: "")

    private let progress = NSProgressIndicator()
    private let statusLabel = NSTextField(wrappingLabelWithString: "")
    private let applyButton = NSButton(title: L10n.text("Configure and enable"), target: nil, action: nil)
    private let permissionsButton = NSButton(title: L10n.text("Check permissions"), target: nil, action: nil)
    private let advancedButton = NSButton(title: L10n.text("Advanced settings"), target: nil, action: nil)
    private let logsButton = NSButton(title: L10n.text("Open logs"), target: nil, action: nil)
    private let capabilitiesConfigureButton = NSButton(title: L10n.text("Configure"), target: nil, action: nil)
    private let nexusConfigureButton = NSButton(title: L10n.text("Configure"), target: nil, action: nil)
    private let remoteAccessToggleButton = NSButton(title: L10n.text("Change connection method"), target: nil, action: nil)
    private let remoteConfigurationStack = NSStackView()
    private let browserCapabilityStatus = NSTextField(labelWithString: L10n.text("Not configured"))
    private let codingCapabilityStatus = NSTextField(labelWithString: L10n.text("Not configured"))
    private let mcpCapabilityStatus = NSTextField(labelWithString: L10n.text("Not configured"))
    private let homeBrowserCapabilityStatus = NSTextField(labelWithString: L10n.text("Not configured"))
    private let homeCodingCapabilityStatus = NSTextField(labelWithString: L10n.text("Not configured"))
    private let homeMcpCapabilityStatus = NSTextField(labelWithString: L10n.text("Not configured"))
    private let homeServiceMetric = NSTextField(labelWithString: L10n.text("Not configured"))
    private let homeHealthMetric = NSTextField(labelWithString: L10n.text("Not configured"))
    private let homePublicMetric = NSTextField(labelWithString: L10n.text("Not configured"))
    private let homeVersionMetric = NSTextField(labelWithString: L10n.text("Not configured"))
    private let homeLocalMcpSummary = NSTextField(labelWithString: L10n.text("Not configured"))
    private let homePublicMcpSummary = NSTextField(labelWithString: L10n.text("Disabled"))
    private let activityRuntimeStatus = NSTextField(labelWithString: L10n.text("Not installed"))
    private let activityPublicStatus = NSTextField(wrappingLabelWithString: L10n.text("Not checked"))
    private var sidebarButtons: [ControlPanelPage: NSButton] = [:]
    private var pageViews: [ControlPanelPage: NSView] = [:]
    private var selectedPage: ControlPanelPage = .home

    private var currentStatus = ServiceStatus.missing
    private var initialMode: TunnelMode = .local
    private var initialServerURL = ""
    private var authTokenValue = ""
    private var oauthPasswordValue = ""
    private var authVisible = false
    private var oauthVisible = false
    private var isBusy = false
    private var isUpdateInProgress = false
    private var quickTunnelRefreshState: QuickTunnelRefreshState = .idle

    private var migrationRequired: Bool {
        currentStatus.migrationRequired
    }

    private var controlsLocked: Bool {
        isBusy || isUpdateInProgress
    }

    var hasActiveServiceOperation: Bool {
        isBusy || (advancedSettings?.hasActiveServiceOperation ?? false)
    }
    private var displayedPublicMCPURL: URL?
    private var lastCheckedPublicMCPURL: URL?
    private var activePublicCheckURL: URL?
    private var publicCheckTask: Task<Void, Never>?

    private var advancedSettings: AdvancedSettingsWindowController?
    private lazy var permissionsWindow = DesktopPermissionsWindowController()

    init(
        service: ServiceController,
        menuLoginAgent: MenuLoginAgentController,
        onChanged: @escaping () -> Void,
        onUpdateRequested: @escaping () -> Void
    ) {
        self.service = service
        self.menuLoginAgent = menuLoginAgent
        self.onChanged = onChanged
        self.onUpdateRequested = onUpdateRequested
        let window = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: 920, height: 700),
            styleMask: [.titled, .closable, .miniaturizable, .resizable],
            backing: .buffered,
            defer: false
        )
        window.title = "AgentDock"
        window.isReleasedWhenClosed = false
        window.minSize = NSSize(width: 800, height: 540)
        window.center()
        super.init(window: window)
        window.delegate = self
        configureUI()
    }

    required init?(coder: NSCoder) {
        fatalError("init(coder:) has not been implemented")
    }

    func present(status: ServiceStatus) {
        update(status: status)
        showWindow(nil)
        window?.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }

    func update(status: ServiceStatus) {
        currentStatus = status
        authVisible = false
        oauthVisible = false
        statusLabel.isHidden = true
        quickTunnelRefreshState = .idle
        cancelPublicCheck(clearLastResult: true)
        setBusy(false)

        if status.installed {
            titleLabel.stringValue = "AgentDock"
            subtitleLabel.stringValue = L10n.text("Your local AI runtime")
            applyButton.title = L10n.text("Apply changes")
            advancedButton.isEnabled = !controlsLocked
            capabilitiesConfigureButton.isEnabled = !controlsLocked
            nexusConfigureButton.isEnabled = !controlsLocked
            logsButton.isEnabled = true
            serviceSection.isHidden = false
            serviceCard?.isHidden = false
            updateServiceSection(status)
            selectCurrentMode(configuration: status.configuration)
        } else {
            titleLabel.stringValue = L10n.text("Set up AgentDock")
            subtitleLabel.stringValue = L10n.text("Configure the local service and allow AgentDock to run in the background")
            stateLabel.stringValue = L10n.text("● Not configured")
            stateLabel.textColor = .secondaryLabelColor
            applyButton.title = L10n.text("Configure and enable")
            applyButton.isEnabled = true
            advancedButton.isEnabled = false
            capabilitiesConfigureButton.isEnabled = false
            nexusConfigureButton.isEnabled = false
            logsButton.isEnabled = false
            serviceSection.isHidden = true
            serviceCard?.isHidden = true
            authTokenValue = ""
            oauthPasswordValue = ""
            select(mode: .local)
            activityRuntimeStatus.stringValue = L10n.text("Not configured")
            activityRuntimeStatus.textColor = .secondaryLabelColor
            for metric in [homeServiceMetric, homeHealthMetric, homePublicMetric, homeVersionMetric] {
                metric.stringValue = L10n.text("Not configured")
                metric.textColor = .secondaryLabelColor
            }
            homeLocalMcpSummary.stringValue = L10n.text("Not configured")
            homePublicMcpSummary.stringValue = L10n.text("Disabled")
        }
        updateNexusState(status.nexusConnection)
        updateCapabilityState(status)
        refreshCredentialFields()
        refreshChangeState()
        updateWindowHeight()
    }

    func setUpdateInProgress(_ inProgress: Bool, status: String? = nil) {
        isUpdateInProgress = inProgress
        setBusy(isBusy)
        advancedSettings?.setUpdateInProgress(inProgress)
        if inProgress {
            showStatus(status ?? L10n.text("Updating AgentDock…"), isError: false)
        } else if statusLabel.stringValue == L10n.text("Updating AgentDock…") ||
                    statusLabel.stringValue == L10n.text("Checking for updates…") {
            statusLabel.isHidden = true
        }
    }

    func refreshServiceStatus(_ status: ServiceStatus) {
        let installationChanged = currentStatus.installed != status.installed
        currentStatus = status
        if installationChanged {
            update(status: status)
            return
        }
        guard status.installed else { return }
        updateServiceSection(status)
        updateNexusState(status.nexusConnection)
        updateCapabilityState(status)
        refreshCredentialFields()
    }

    func presentPermissions() {
        permissionsWindow.present()
    }

    func windowDidResignKey(_ notification: Notification) {
        authVisible = false
        oauthVisible = false
        refreshCredentialFields()
    }

    private func configureUI() {
        guard let contentView = window?.contentView else { return }

        let contentBackground = NSBox()
        contentBackground.boxType = .custom
        contentBackground.borderWidth = 0
        contentBackground.fillColor = ControlPanelUI.canvasColor
        contentBackground.translatesAutoresizingMaskIntoConstraints = false
        contentView.addSubview(contentBackground)

        let sidebar = NSBox()
        sidebar.boxType = .custom
        sidebar.titlePosition = .noTitle
        sidebar.borderWidth = 0
        sidebar.fillColor = ControlPanelUI.sidebarColor
        sidebar.translatesAutoresizingMaskIntoConstraints = false
        contentView.addSubview(sidebar)

        let sidebarSeparator = NSBox()
        sidebarSeparator.boxType = .custom
        sidebarSeparator.borderWidth = 0
        sidebarSeparator.fillColor = ControlPanelUI.surfaceBorderColor
        sidebarSeparator.translatesAutoresizingMaskIntoConstraints = false
        contentView.addSubview(sidebarSeparator)

        let scrollView = NSScrollView()
        scrollView.hasVerticalScroller = true
        scrollView.autohidesScrollers = true
        scrollView.drawsBackground = false
        scrollView.translatesAutoresizingMaskIntoConstraints = false
        contentView.addSubview(scrollView)

        scrollDocumentView.translatesAutoresizingMaskIntoConstraints = false
        scrollView.documentView = scrollDocumentView

        contentStack.orientation = .vertical
        contentStack.alignment = .leading
        contentStack.spacing = ControlPanelUI.sectionSpacing
        contentStack.translatesAutoresizingMaskIntoConstraints = false
        scrollDocumentView.addSubview(contentStack)

        let brandTitle = NSTextField(labelWithString: "AgentDock")
        brandTitle.font = .systemFont(ofSize: 18, weight: .semibold)
        let brandSubtitle = NSTextField(labelWithString: L10n.text("Your local AI runtime"))
        brandSubtitle.font = .systemFont(ofSize: 11)
        brandSubtitle.textColor = .secondaryLabelColor
        let brandText = NSStackView(views: [brandTitle, brandSubtitle])
        brandText.orientation = .vertical
        brandText.alignment = .leading
        brandText.spacing = 2
        let brand = NSStackView(views: [ControlPanelUI.appMark(), brandText])
        brand.orientation = .horizontal
        brand.alignment = .centerY
        brand.spacing = 10

        let navigation = NSStackView()
        navigation.orientation = .vertical
        navigation.alignment = .leading
        navigation.spacing = 5

        for page in ControlPanelPage.allCases {
            let button = ControlPanelUI.sidebarButton(
                title: page.title,
                symbol: page.symbol,
                tag: page.rawValue,
                target: self,
                action: #selector(sidebarPressed)
            )
            sidebarButtons[page] = button
            navigation.addArrangedSubview(button)
            button.widthAnchor.constraint(equalTo: navigation.widthAnchor).isActive = true
        }

        let sidebarStack = NSStackView(views: [brand, navigation, NSView()])
        sidebarStack.orientation = .vertical
        sidebarStack.alignment = .leading
        sidebarStack.spacing = 22
        sidebarStack.edgeInsets = NSEdgeInsets(top: 24, left: 16, bottom: 18, right: 14)
        sidebarStack.translatesAutoresizingMaskIntoConstraints = false
        sidebar.addSubview(sidebarStack)

        titleLabel.font = .systemFont(ofSize: 24, weight: .semibold)
        subtitleLabel.stringValue = L10n.text("Your local AI runtime")
        subtitleLabel.textColor = .secondaryLabelColor
        stateLabel.alignment = .left
        stateLabel.font = .systemFont(ofSize: 13, weight: .medium)
        stateLabel.lineBreakMode = .byTruncatingMiddle

        startStopButton.target = self
        startStopButton.action = #selector(startStopPressed)
        restartButton.target = self
        restartButton.action = #selector(restartPressed)
        updateButton.target = self
        updateButton.action = #selector(updatePressed)
        ControlPanelUI.configurePrimaryAction(startStopButton)
        ControlPanelUI.configureSecondaryAction(restartButton, symbol: "arrow.clockwise")
        ControlPanelUI.configureSecondaryAction(updateButton, symbol: "arrow.down.circle")

        let serviceActions = NSStackView(views: [startStopButton, restartButton, updateButton, NSView()])
        serviceActions.orientation = .horizontal
        serviceActions.alignment = .centerY
        serviceActions.spacing = 8

        let homeRuntime = NSStackView(views: [titleLabel, subtitleLabel, stateLabel, serviceActions])
        homeRuntime.orientation = .vertical
        homeRuntime.alignment = .leading
        homeRuntime.spacing = 9
        serviceActions.widthAnchor.constraint(equalTo: homeRuntime.widthAnchor).isActive = true

        let heroIcon = NSImageView()
        heroIcon.image = NSImage(
            systemSymbolName: "bolt.horizontal.circle.fill",
            accessibilityDescription: L10n.text("Service")
        )?.withSymbolConfiguration(NSImage.SymbolConfiguration(pointSize: 31, weight: .medium))
        heroIcon.contentTintColor = ControlPanelUI.accentColor
        heroIcon.imageScaling = .scaleProportionallyDown
        heroIcon.widthAnchor.constraint(equalToConstant: 46).isActive = true
        heroIcon.heightAnchor.constraint(equalToConstant: 46).isActive = true

        let homeHeroTop = NSStackView(views: [heroIcon, homeRuntime])
        homeHeroTop.orientation = .horizontal
        homeHeroTop.alignment = .top
        homeHeroTop.spacing = 16

        let homeMetricStrip = ControlPanelUI.metricStrip([
            (L10n.text("Service"), homeServiceMetric),
            (L10n.text("Health check"), homeHealthMetric),
            (L10n.text("Public access"), homePublicMetric),
            (L10n.text("Version"), homeVersionMetric),
        ])
        let homeHeroStack = NSStackView(views: [homeHeroTop, homeMetricStrip])
        homeHeroStack.orientation = .vertical
        homeHeroStack.alignment = .leading
        homeHeroStack.spacing = 18
        homeHeroTop.widthAnchor.constraint(equalTo: homeHeroStack.widthAnchor).isActive = true
        homeMetricStrip.widthAnchor.constraint(equalTo: homeHeroStack.widthAnchor).isActive = true
        let homeRuntimeCard = ControlPanelUI.hero(content: homeHeroStack)

        for field in [localAddress, publicAddress, authToken, oauthPassword] {
            field.isSelectable = true
            field.font = .monospacedSystemFont(ofSize: 12, weight: .regular)
            field.setContentHuggingPriority(.defaultLow, for: .horizontal)
            field.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        }
        for field in [localAddress, authToken, oauthPassword] {
            field.lineBreakMode = .byTruncatingMiddle
        }
        publicAddress.lineBreakMode = .byCharWrapping
        publicAddress.maximumNumberOfLines = 2
        publicAddress.setContentCompressionResistancePriority(.fittingSizeCompression, for: .horizontal)
        publicAddress.cell?.wraps = true

        publicCheckStatus.font = .systemFont(ofSize: 11.5)
        publicCheckStatus.textColor = .secondaryLabelColor
        publicCheckStatus.isHidden = true
        publicCheckStatus.lineBreakMode = .byTruncatingTail
        publicCheckStatus.setContentHuggingPriority(.defaultLow, for: .horizontal)
        publicCheckStatus.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)

        publicTestButton.target = self
        publicTestButton.action = #selector(testPublicAddressPressed)
        publicCopyButton.target = self
        publicCopyButton.action = #selector(copyPublicAddress)
        ControlPanelUI.configurePrimaryAction(publicTestButton)
        ControlPanelUI.configureSecondaryAction(publicCopyButton, symbol: "doc.on.doc")

        nexusStateLabel.alignment = .left
        nexusStateLabel.font = .systemFont(ofSize: 12)
        nexusStateLabel.setContentHuggingPriority(.defaultLow, for: .horizontal)
        nexusStateLabel.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        ControlPanelUI.configureQuietAction(nexusConfigureButton, symbol: "chevron.right")
        nexusConfigureButton.target = self
        nexusConfigureButton.action = #selector(openAdvancedPressed)

        serviceSection.orientation = .vertical
        serviceSection.alignment = .leading
        serviceSection.spacing = 8
        addFullWidth(valueRow(title: L10n.text("Local MCP"), field: localAddress, actions: [copyButton(#selector(copyLocalAddress))]), to: serviceSection)
        addFullWidth(valueRow(title: "NexusDock", field: nexusStateLabel, actions: [nexusConfigureButton]), to: serviceSection)

        let connectionCard = ControlPanelUI.card(
            title: L10n.text("Connections"),
            symbol: "link",
            content: serviceSection
        )
        serviceCard = connectionCard

        publicMode.target = self
        publicMode.action = #selector(modeChanged)
        publicMode.segmentStyle = .rounded
        publicMode.widthAnchor.constraint(equalToConstant: 360).isActive = true

        modeDescription.textColor = .secondaryLabelColor
        modeDescription.font = .systemFont(ofSize: 12)

        serverURLField.placeholderString = "https://mini.example.com"
        tunnelTokenField.placeholderString = L10n.text("Paste Cloudflare Tunnel Token")
        for field in [serverURLField, tunnelTokenField] {
            field.setContentHuggingPriority(.defaultLow, for: .horizontal)
            field.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        }
        serverURLField.target = self
        serverURLField.action = #selector(configurationEdited)
        tunnelTokenField.target = self
        tunnelTokenField.action = #selector(configurationEdited)

        namedFields.orientation = .vertical
        namedFields.alignment = .leading
        namedFields.spacing = 7
        addFullWidth(formRow(title: L10n.text("Public address"), control: serverURLField), to: namedFields)
        addFullWidth(formRow(title: "Tunnel Token", control: tunnelTokenField), to: namedFields)

        progress.style = .spinning
        progress.controlSize = .small
        progress.isDisplayedWhenStopped = false
        statusLabel.textColor = .secondaryLabelColor
        statusLabel.lineBreakMode = .byTruncatingTail
        statusLabel.setContentHuggingPriority(.defaultLow, for: .horizontal)
        statusLabel.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        statusLabel.isHidden = true

        ControlPanelUI.configurePrimaryAction(applyButton)
        applyButton.target = self
        applyButton.action = #selector(applyPressed)

        let remoteActions = NSStackView(views: [progress, statusLabel, NSView(), applyButton])
        remoteActions.orientation = .horizontal
        remoteActions.alignment = .centerY
        remoteActions.spacing = 8

        remoteConfigurationStack.orientation = .vertical
        remoteConfigurationStack.alignment = .leading
        remoteConfigurationStack.spacing = 9
        remoteConfigurationStack.setViews([publicMode, modeDescription, namedFields, remoteActions], in: .top)
        modeDescription.widthAnchor.constraint(equalTo: remoteConfigurationStack.widthAnchor).isActive = true
        namedFields.widthAnchor.constraint(equalTo: remoteConfigurationStack.widthAnchor).isActive = true
        remoteActions.widthAnchor.constraint(equalTo: remoteConfigurationStack.widthAnchor).isActive = true
        remoteConfigurationStack.isHidden = true

        ControlPanelUI.configureSecondaryAction(remoteAccessToggleButton, symbol: "slider.horizontal.3")
        remoteAccessToggleButton.target = self
        remoteAccessToggleButton.action = #selector(toggleRemoteConfiguration)

        let remoteDescription = NSTextField(wrappingLabelWithString: L10n.text("Access this AgentDock securely from outside this device."))
        remoteDescription.font = .systemFont(ofSize: 12)
        remoteDescription.textColor = .secondaryLabelColor

        let remoteContent = NSStackView(views: [
            remoteDescription,
            remoteAccessToggleButton,
            remoteConfigurationStack,
        ])
        remoteContent.orientation = .vertical
        remoteContent.alignment = .leading
        remoteContent.spacing = 10
        for child in [remoteDescription, remoteConfigurationStack] {
            child.widthAnchor.constraint(equalTo: remoteContent.widthAnchor).isActive = true
        }

        let remoteCard = ControlPanelUI.card(
            title: L10n.text("Remote access"),
            symbol: "slider.horizontal.3",
            content: remoteContent
        )

        let remoteHeroIcon = NSImageView()
        remoteHeroIcon.image = NSImage(
            systemSymbolName: "network",
            accessibilityDescription: L10n.text("Remote access")
        )?.withSymbolConfiguration(NSImage.SymbolConfiguration(pointSize: 28, weight: .medium))
        remoteHeroIcon.contentTintColor = ControlPanelUI.accentColor
        remoteHeroIcon.imageScaling = .scaleProportionallyDown
        remoteHeroIcon.widthAnchor.constraint(equalToConstant: 46).isActive = true
        remoteHeroIcon.heightAnchor.constraint(equalToConstant: 46).isActive = true

        let remoteHeroTitle = NSTextField(labelWithString: L10n.text("Remote access"))
        remoteHeroTitle.font = .systemFont(ofSize: 20, weight: .semibold)
        let remoteHeroText = NSStackView(views: [remoteHeroTitle, publicAddress, publicCheckStatus])
        remoteHeroText.orientation = .vertical
        remoteHeroText.alignment = .leading
        remoteHeroText.spacing = 6

        let remoteHeroActions = NSStackView(views: [publicCopyButton, publicTestButton])
        remoteHeroActions.orientation = .horizontal
        remoteHeroActions.alignment = .centerY
        remoteHeroActions.spacing = 8

        let remoteHeroTop = NSStackView(views: [remoteHeroIcon, remoteHeroText, NSView(), remoteHeroActions])
        remoteHeroTop.orientation = .horizontal
        remoteHeroTop.alignment = .centerY
        remoteHeroTop.spacing = 16
        let remoteHero = ControlPanelUI.hero(content: remoteHeroTop)

        for label in [browserCapabilityStatus, codingCapabilityStatus, mcpCapabilityStatus] {
            label.font = .systemFont(ofSize: 12, weight: .medium)
            label.alignment = .right
            label.textColor = .secondaryLabelColor
            label.setContentHuggingPriority(.required, for: .horizontal)
        }

        ControlPanelUI.configureSecondaryAction(capabilitiesConfigureButton, symbol: "slider.horizontal.3")
        capabilitiesConfigureButton.target = self
        capabilitiesConfigureButton.action = #selector(openAdvancedPressed)

        let browserCapabilityContent = capabilityCardContent(status: browserCapabilityStatus)
        let browserCapabilityCard = ControlPanelUI.card(
            title: L10n.text("Browser automation"),
            symbol: "safari",
            content: browserCapabilityContent
        )

        let codingCapabilityContent = capabilityCardContent(status: codingCapabilityStatus)
        let codingCapabilityCard = ControlPanelUI.card(
            title: L10n.text("Coding Agent"),
            symbol: "terminal",
            content: codingCapabilityContent
        )

        let mcpCapabilityContent = capabilityCardContent(status: mcpCapabilityStatus)
        let mcpCapabilityCard = ControlPanelUI.card(
            title: L10n.text("MCP runtime"),
            symbol: "shippingbox",
            content: mcpCapabilityContent
        )

        let capabilityActions = NSStackView(views: [capabilitiesConfigureButton, NSView()])
        capabilityActions.orientation = .horizontal
        capabilityActions.alignment = .centerY

        activityRuntimeStatus.font = .systemFont(ofSize: 12, weight: .medium)
        activityPublicStatus.font = .systemFont(ofSize: 12)
        activityRuntimeStatus.textColor = .secondaryLabelColor
        activityPublicStatus.textColor = .secondaryLabelColor

        let activityTestButton = NSButton(title: L10n.text("Test"), target: self, action: #selector(testPublicAddressPressed))
        ControlPanelUI.configureQuietAction(activityTestButton, symbol: "network.badge.shield.half.filled")
        ControlPanelUI.configureSecondaryAction(logsButton, symbol: "doc.text")
        logsButton.target = self
        logsButton.action = #selector(openLogsPressed)

        let diagnosticsContent = NSStackView()
        diagnosticsContent.orientation = .vertical
        diagnosticsContent.alignment = .leading
        diagnosticsContent.spacing = 10
        addFullWidth(valueRow(title: L10n.text("Service"), field: activityRuntimeStatus, actions: []), to: diagnosticsContent)
        addFullWidth(valueRow(title: L10n.text("Public access"), field: activityPublicStatus, actions: [activityTestButton]), to: diagnosticsContent)

        let diagnosticsCard = ControlPanelUI.card(
            title: L10n.text("Diagnostics"),
            symbol: "stethoscope",
            content: diagnosticsContent
        )

        let activityActions = NSStackView(views: [logsButton, NSView()])
        activityActions.orientation = .horizontal

        let activityCard = ControlPanelUI.card(
            title: L10n.text("Activity"),
            symbol: "clock.arrow.circlepath",
            content: activityActions
        )

        authReveal.bezelStyle = .inline
        authReveal.target = self
        authReveal.action = #selector(toggleAuthToken)
        oauthReveal.bezelStyle = .inline
        oauthReveal.target = self
        oauthReveal.action = #selector(toggleOAuthPassword)

        let credentialStack = NSStackView()
        credentialStack.orientation = .vertical
        credentialStack.alignment = .leading
        credentialStack.spacing = 8
        addFullWidth(valueRow(title: "Bearer Token", field: authToken, actions: [authReveal, copyButton(#selector(copyAuthToken))]), to: credentialStack)
        addFullWidth(valueRow(title: L10n.text("OAuth password"), field: oauthPassword, actions: [oauthReveal, copyButton(#selector(copyOAuthPassword))]), to: credentialStack)

        let credentialCard = ControlPanelUI.card(
            title: L10n.text("Connection information"),
            symbol: "key",
            content: credentialStack
        )

        ControlPanelUI.configureSecondaryAction(permissionsButton, symbol: "checkmark.shield")
        permissionsButton.target = self
        permissionsButton.action = #selector(openPermissionsPressed)
        ControlPanelUI.configureSecondaryAction(advancedButton, symbol: "slider.horizontal.3")
        advancedButton.target = self
        advancedButton.action = #selector(openAdvancedPressed)

        let permissionsActions = NSStackView(views: [permissionsButton, NSView()])
        permissionsActions.orientation = .horizontal
        permissionsActions.alignment = .centerY
        let permissionsCard = ControlPanelUI.card(
            title: L10n.text("Permissions"),
            symbol: "checkmark.shield",
            content: permissionsActions
        )

        let runtimeActions = NSStackView(views: [advancedButton, NSView()])
        runtimeActions.orientation = .horizontal
        runtimeActions.alignment = .centerY
        let runtimeSettingsCard = ControlPanelUI.card(
            title: L10n.text("Runtime"),
            symbol: "gearshape.2",
            content: runtimeActions
        )

        let homeSummaryRow = NSStackView(views: [
            ControlPanelUI.summaryTile(
                title: L10n.text("Browser automation"),
                symbol: "safari",
                status: homeBrowserCapabilityStatus
            ),
            ControlPanelUI.summaryTile(
                title: L10n.text("Coding Agent"),
                symbol: "terminal",
                status: homeCodingCapabilityStatus
            ),
            ControlPanelUI.summaryTile(
                title: L10n.text("MCP runtime"),
                symbol: "shippingbox",
                status: homeMcpCapabilityStatus
            ),
        ])
        homeSummaryRow.orientation = .horizontal
        homeSummaryRow.alignment = .top
        homeSummaryRow.distribution = .fillEqually
        homeSummaryRow.spacing = 12

        let homeSummaryTitle = NSTextField(labelWithString: L10n.text("Capabilities"))
        homeSummaryTitle.font = .systemFont(ofSize: 15, weight: .semibold)
        let homeSummarySection = NSStackView(views: [homeSummaryTitle, homeSummaryRow])
        homeSummarySection.orientation = .vertical
        homeSummarySection.alignment = .leading
        homeSummarySection.spacing = 10
        homeSummaryRow.widthAnchor.constraint(equalTo: homeSummarySection.widthAnchor).isActive = true

        for field in [homeLocalMcpSummary, homePublicMcpSummary] {
            field.font = .monospacedSystemFont(ofSize: 12, weight: .regular)
            field.lineBreakMode = .byTruncatingMiddle
            field.isSelectable = true
            field.setContentHuggingPriority(.defaultLow, for: .horizontal)
            field.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        }
        let homeConnectionContent = NSStackView()
        homeConnectionContent.orientation = .vertical
        homeConnectionContent.alignment = .leading
        homeConnectionContent.spacing = 10
        addFullWidth(valueRow(title: L10n.text("Local MCP"), field: homeLocalMcpSummary, actions: []), to: homeConnectionContent)
        addFullWidth(valueRow(title: L10n.text("Public MCP"), field: homePublicMcpSummary, actions: []), to: homeConnectionContent)
        let homeConnectionCard = ControlPanelUI.card(
            title: L10n.text("Connections"),
            symbol: "link",
            content: homeConnectionContent
        )

        pageViews = [
            .home: makePage(
                title: L10n.text("Home"),
                subtitle: L10n.text("Runtime health, connection state, and the actions you use most."),
                views: [homeRuntimeCard, homeSummarySection, homeConnectionCard]
            ),
            .connections: makePage(
                title: L10n.text("Connections"),
                subtitle: L10n.text("Manage how AI clients reach AgentDock locally and remotely."),
                views: [remoteHero, connectionCard, remoteCard]
            ),
            .capabilities: makePage(
                title: L10n.text("Capabilities"),
                subtitle: L10n.text("Configure what AgentDock can do after a client connects."),
                views: [browserCapabilityCard, codingCapabilityCard, mcpCapabilityCard, capabilityActions]
            ),
            .activity: makePage(
                title: L10n.text("Activity"),
                subtitle: L10n.text("Inspect runtime health, reachability, and logs."),
                views: [diagnosticsCard, activityCard]
            ),
            .settings: makePage(
                title: L10n.text("Settings"),
                subtitle: L10n.text("Customize runtime behavior and desktop preferences."),
                views: [credentialCard, permissionsCard, runtimeSettingsCard]
            ),
        ]

        NSLayoutConstraint.activate([
            contentBackground.leadingAnchor.constraint(equalTo: contentView.leadingAnchor),
            contentBackground.trailingAnchor.constraint(equalTo: contentView.trailingAnchor),
            contentBackground.topAnchor.constraint(equalTo: contentView.topAnchor),
            contentBackground.bottomAnchor.constraint(equalTo: contentView.bottomAnchor),

            sidebar.leadingAnchor.constraint(equalTo: contentView.leadingAnchor),
            sidebar.topAnchor.constraint(equalTo: contentView.topAnchor),
            sidebar.bottomAnchor.constraint(equalTo: contentView.bottomAnchor),
            sidebar.widthAnchor.constraint(equalToConstant: ControlPanelUI.sidebarWidth),

            sidebarSeparator.leadingAnchor.constraint(equalTo: sidebar.trailingAnchor),
            sidebarSeparator.topAnchor.constraint(equalTo: contentView.topAnchor),
            sidebarSeparator.bottomAnchor.constraint(equalTo: contentView.bottomAnchor),
            sidebarSeparator.widthAnchor.constraint(equalToConstant: 1),

            sidebarStack.leadingAnchor.constraint(equalTo: sidebar.leadingAnchor),
            sidebarStack.trailingAnchor.constraint(equalTo: sidebar.trailingAnchor),
            sidebarStack.topAnchor.constraint(equalTo: sidebar.topAnchor),
            sidebarStack.bottomAnchor.constraint(lessThanOrEqualTo: sidebar.bottomAnchor),

            scrollView.leadingAnchor.constraint(equalTo: sidebarSeparator.trailingAnchor),
            scrollView.trailingAnchor.constraint(equalTo: contentView.trailingAnchor),
            scrollView.topAnchor.constraint(equalTo: contentView.topAnchor),
            scrollView.bottomAnchor.constraint(equalTo: contentView.bottomAnchor),

            scrollDocumentView.widthAnchor.constraint(equalTo: scrollView.contentView.widthAnchor),
            contentStack.leadingAnchor.constraint(equalTo: scrollDocumentView.leadingAnchor, constant: 30),
            contentStack.trailingAnchor.constraint(equalTo: scrollDocumentView.trailingAnchor, constant: -30),
            contentStack.topAnchor.constraint(equalTo: scrollDocumentView.topAnchor, constant: 26),
            contentStack.bottomAnchor.constraint(equalTo: scrollDocumentView.bottomAnchor, constant: -26),
        ])

        selectPage(.home)
    }

    private func makePage(title: String, subtitle: String, views: [NSView]) -> NSStackView {
        let page = NSStackView(views: [ControlPanelUI.pageHeader(title: title, subtitle: subtitle)] + views)
        page.orientation = .vertical
        page.alignment = .leading
        page.spacing = ControlPanelUI.sectionSpacing
        for view in page.arrangedSubviews {
            view.widthAnchor.constraint(equalTo: page.widthAnchor).isActive = true
        }
        return page
    }

    private func capabilityCardContent(status: NSTextField) -> NSView {
        let label = NSTextField(labelWithString: L10n.text("Status"))
        label.textColor = .secondaryLabelColor
        label.font = .systemFont(ofSize: 12)

        status.alignment = .right
        status.setContentHuggingPriority(.required, for: .horizontal)

        let row = NSStackView(views: [label, NSView(), status])
        row.orientation = .horizontal
        row.alignment = .centerY
        row.spacing = 10
        return row
    }

    @objc private func sidebarPressed(_ sender: NSButton) {
        guard let page = ControlPanelPage(rawValue: sender.tag) else { return }
        selectPage(page)
    }

    private func selectPage(_ page: ControlPanelPage) {
        guard let pageView = pageViews[page] else { return }
        selectedPage = page

        for view in contentStack.arrangedSubviews {
            contentStack.removeArrangedSubview(view)
            view.removeFromSuperview()
        }
        addFullWidth(pageView, to: contentStack)

        for (candidate, button) in sidebarButtons {
            ControlPanelUI.setSidebarSelection(button, selected: candidate == page)
        }
        scrollDocumentView.scroll(.zero)
    }

    @objc private func toggleRemoteConfiguration() {
        remoteConfigurationStack.isHidden.toggle()
        remoteAccessToggleButton.title = remoteConfigurationStack.isHidden
            ? L10n.text("Change connection method")
            : L10n.text("Hide")
        updateWindowHeight()
    }


    private func updateServiceSection(_ status: ServiceStatus) {
        if migrationRequired {
            stateLabel.stringValue = L10n.text("● Migration required")
            stateLabel.textColor = .systemOrange
        } else if status.healthy {
            if AppVersion.matchesHealthVersion(status.version) {
                stateLabel.stringValue = L10n.format("● Running normally · %@", AppVersion.current)
                stateLabel.textColor = .systemGreen
            } else {
                stateLabel.stringValue = L10n.format(
                    "● Version mismatch · AgentDock %@ · Core %@",
                    AppVersion.current,
                    AppVersion.display(status.version)
                )
                stateLabel.textColor = .systemRed
            }
        } else if status.requiresApproval {
            stateLabel.stringValue = L10n.text("● Background permission required")
            stateLabel.textColor = .systemOrange
        } else if status.loaded {
            stateLabel.stringValue = L10n.text("● Service error")
            stateLabel.textColor = .systemRed
        } else {
            stateLabel.stringValue = L10n.text("● Stopped")
            stateLabel.textColor = .secondaryLabelColor
        }
        activityRuntimeStatus.stringValue = stateLabel.stringValue
        activityRuntimeStatus.textColor = stateLabel.textColor

        let configuration = status.configuration
        localAddress.stringValue = configuration?.localMCPURL?.absoluteString ?? L10n.text("Configuration unavailable")
        homeServiceMetric.stringValue = status.loaded ? L10n.text("Running normally") : L10n.text("Stopped")
        homeServiceMetric.textColor = status.loaded ? .systemGreen : .secondaryLabelColor
        homeHealthMetric.stringValue = status.healthy ? L10n.text("Healthy") : L10n.text("Unavailable")
        homeHealthMetric.textColor = status.healthy ? .systemGreen : .systemRed
        homeVersionMetric.stringValue = AppVersion.display(status.version)
        homeVersionMetric.textColor = .labelColor
        homeLocalMcpSummary.stringValue = localAddress.stringValue
        renderPublicAddress(configuration?.publicMCPURL, automaticallyCheck: true)
        authTokenValue = configuration?.authToken ?? ""
        oauthPasswordValue = configuration?.oauthPassword ?? ""
        startStopButton.title = migrationRequired
            ? L10n.text("Waiting for migration")
            : (status.requiresApproval ? L10n.text("Open background settings") : (status.loaded ? L10n.text("Stop service") : L10n.text("Start service")))
        if !controlsLocked {
            startStopButton.isEnabled = status.installed && !migrationRequired
            restartButton.isEnabled = status.installed && !status.requiresApproval && !migrationRequired
            updateButton.isEnabled = status.installed && !migrationRequired
        }
    }

    private func updateNexusState(_ state: NexusConnectionState) {
        switch state {
        case .connected:
            nexusStateLabel.stringValue = L10n.text("Connected")
        case .disconnected:
            nexusStateLabel.stringValue = L10n.text("Disconnected")
        case .unconfigured:
            nexusStateLabel.stringValue = L10n.text("Not configured")
        case .configurationError:
            nexusStateLabel.stringValue = L10n.text("Configuration error")
        }
    }

    private func updateCapabilityState(_ status: ServiceStatus) {
        let allCapabilityLabels = [
            browserCapabilityStatus,
            codingCapabilityStatus,
            mcpCapabilityStatus,
            homeBrowserCapabilityStatus,
            homeCodingCapabilityStatus,
            homeMcpCapabilityStatus,
        ]

        guard let configuration = status.configuration else {
            for label in allCapabilityLabels {
                label.stringValue = L10n.text("Not configured")
                label.textColor = .secondaryLabelColor
            }
            return
        }

        setCapabilityLabel(browserCapabilityStatus, enabled: configuration.browserEnabled)
        setCapabilityLabel(homeBrowserCapabilityStatus, enabled: configuration.browserEnabled)
        setCapabilityLabel(codingCapabilityStatus, enabled: configuration.acpEnabled)
        setCapabilityLabel(homeCodingCapabilityStatus, enabled: configuration.acpEnabled)
        setCapabilityLabel(mcpCapabilityStatus, enabled: configuration.mcpAppsMode != .off)
        setCapabilityLabel(homeMcpCapabilityStatus, enabled: configuration.mcpAppsMode != .off)
    }

    private func setCapabilityLabel(_ label: NSTextField, enabled: Bool) {
        label.stringValue = enabled ? L10n.text("Ready") : L10n.text("Disabled")
        label.textColor = enabled ? .systemGreen : .secondaryLabelColor
    }

    private func renderPublicAddress(_ publicMCPURL: URL?, automaticallyCheck: Bool) {
        switch quickTunnelRefreshState {
        case .refreshing:
            cancelPublicCheck(clearLastResult: true)
            displayedPublicMCPURL = nil
            publicAddress.stringValue = L10n.text("Generating a new address…")
            publicCheckStatus.stringValue = L10n.text("The old address is hidden; waiting for a new temporary public address")
            publicCheckStatus.textColor = .secondaryLabelColor
            publicCheckStatus.isHidden = false
            activityPublicStatus.stringValue = publicCheckStatus.stringValue
            activityPublicStatus.textColor = publicCheckStatus.textColor
            homePublicMcpSummary.stringValue = publicAddress.stringValue
            homePublicMetric.stringValue = L10n.text("Not configured")
            homePublicMetric.textColor = .secondaryLabelColor
            refreshPublicActions()
        case .failed:
            cancelPublicCheck(clearLastResult: true)
            displayedPublicMCPURL = nil
            publicAddress.stringValue = L10n.text("No new address generated")
            publicCheckStatus.stringValue = L10n.text("Refresh failed; the old address is not shown as the new address")
            publicCheckStatus.textColor = .systemRed
            publicCheckStatus.isHidden = false
            activityPublicStatus.stringValue = publicCheckStatus.stringValue
            activityPublicStatus.textColor = publicCheckStatus.textColor
            homePublicMcpSummary.stringValue = publicAddress.stringValue
            homePublicMetric.stringValue = L10n.text("Not configured")
            homePublicMetric.textColor = .systemRed
            refreshPublicActions()
        case .idle:
            setDisplayedPublicMCPURL(publicMCPURL, automaticallyCheck: automaticallyCheck)
        }
    }

    private func setDisplayedPublicMCPURL(_ publicMCPURL: URL?, automaticallyCheck: Bool) {
        let addressChanged = displayedPublicMCPURL != publicMCPURL
        if addressChanged {
            cancelPublicCheck(clearLastResult: true)
        }
        displayedPublicMCPURL = publicMCPURL
        publicAddress.stringValue = publicMCPURL?.absoluteString ?? L10n.text("Disabled")
        activityPublicStatus.stringValue = publicAddress.stringValue
        activityPublicStatus.textColor = publicMCPURL == nil ? .secondaryLabelColor : .labelColor
        homePublicMcpSummary.stringValue = publicAddress.stringValue
        homePublicMetric.stringValue = publicMCPURL == nil ? L10n.text("Disabled") : L10n.text("Connected")
        homePublicMetric.textColor = publicMCPURL == nil ? .secondaryLabelColor : .systemGreen

        guard let publicMCPURL else {
            publicCheckStatus.stringValue = ""
            publicCheckStatus.isHidden = true
            refreshPublicActions()
            return
        }

        refreshPublicActions()
        if automaticallyCheck,
           activePublicCheckURL != publicMCPURL,
           lastCheckedPublicMCPURL != publicMCPURL {
            beginPublicCheck(publicMCPURL, automatic: true)
        }
    }

    private func beginQuickTunnelRefresh() {
        quickTunnelRefreshState = .refreshing
        renderPublicAddress(nil, automaticallyCheck: false)
    }

    private func markQuickTunnelRefreshFailed() {
        quickTunnelRefreshState = .failed
        renderPublicAddress(nil, automaticallyCheck: false)
    }

    private func beginPublicCheck(_ publicMCPURL: URL, automatic: Bool) {
        guard quickTunnelRefreshState == .idle else { return }
        if automatic, lastCheckedPublicMCPURL == publicMCPURL { return }

        cancelPublicCheck(clearLastResult: !automatic)
        activePublicCheckURL = publicMCPURL
        publicCheckStatus.stringValue = L10n.text("Checking public access…")
        publicCheckStatus.textColor = .secondaryLabelColor
        publicCheckStatus.isHidden = false
        refreshPublicActions()

        publicCheckTask = Task { [weak self] in
            guard let self else { return }
            let maximumAttempts = automatic ? 3 : 1
            var finalResult: PublicEndpointCheckResult?

            for attempt in 1...maximumAttempts {
                if Task.isCancelled { return }
                if maximumAttempts > 1 {
                    publicCheckStatus.stringValue = L10n.format(
                        "Checking public access (%d/%d)…",
                        attempt,
                        maximumAttempts
                    )
                }
                let result = await publicEndpointChecker.check(publicMCPURL: publicMCPURL)
                finalResult = result
                if result.isReachable || attempt == maximumAttempts { break }
                try? await Task.sleep(nanoseconds: 1_000_000_000)
            }

            guard !Task.isCancelled, let finalResult else { return }
            finishPublicCheck(finalResult, for: publicMCPURL)
        }
    }

    private func finishPublicCheck(_ result: PublicEndpointCheckResult, for publicMCPURL: URL) {
        guard quickTunnelRefreshState == .idle,
              activePublicCheckURL == publicMCPURL,
              displayedPublicMCPURL == publicMCPURL else {
            return
        }

        activePublicCheckURL = nil
        publicCheckTask = nil
        lastCheckedPublicMCPURL = publicMCPURL
        let latency = result.latencyMilliseconds.map { " · \($0) ms" } ?? ""
        publicCheckStatus.stringValue = "● \(result.message)\(latency)"
        publicCheckStatus.textColor = result.isReachable ? .systemGreen : .systemRed
        publicCheckStatus.isHidden = false
        activityPublicStatus.stringValue = publicCheckStatus.stringValue
        activityPublicStatus.textColor = publicCheckStatus.textColor
        refreshPublicActions()
    }

    private func cancelPublicCheck(clearLastResult: Bool) {
        publicCheckTask?.cancel()
        publicCheckTask = nil
        activePublicCheckURL = nil
        if clearLastResult {
            lastCheckedPublicMCPURL = nil
        }
    }

    private func refreshPublicActions() {
        let hasAddress = quickTunnelRefreshState == .idle && displayedPublicMCPURL != nil
        publicCopyButton.isEnabled = hasAddress
        publicTestButton.title = activePublicCheckURL == nil ? L10n.text("Test") : L10n.text("Checking")
        publicTestButton.isEnabled = hasAddress && activePublicCheckURL == nil && !controlsLocked
    }

    private func selectCurrentMode(configuration: ServiceConfiguration?) {
        guard let publicURL = configuration?.publicURL, !publicURL.isEmpty else {
            initialMode = .local
            initialServerURL = ""
            select(mode: .local)
            return
        }
        if publicURL.contains(".trycloudflare.com") {
            initialMode = .quick
            initialServerURL = ""
            select(mode: .quick)
        } else {
            initialMode = .named
            initialServerURL = publicURL
            serverURLField.stringValue = publicURL
            select(mode: .named)
        }
        tunnelTokenField.stringValue = ""
    }

    private func select(mode: TunnelMode) {
        publicMode.selectedSegment = segment(for: mode)
        modeDescription.stringValue = mode.detail
        namedFields.isHidden = mode != .named
        if mode == .named, currentStatus.installed, initialMode == .named {
            tunnelTokenField.placeholderString = L10n.text("Leave blank to keep the existing Tunnel Token")
        } else {
            tunnelTokenField.placeholderString = L10n.text("Paste Cloudflare Tunnel Token")
        }
        updateWindowHeight()
    }

    private func segment(for mode: TunnelMode) -> Int {
        switch mode {
        case .local: return 0
        case .quick: return 1
        case .named: return 2
        }
    }

    private var selectedMode: TunnelMode {
        switch publicMode.selectedSegment {
        case 1: return .quick
        case 2: return .named
        default: return .local
        }
    }

    @objc private func modeChanged() {
        select(mode: selectedMode)
        refreshChangeState()
    }

    @objc private func configurationEdited() { refreshChangeState() }

    @objc private func applyPressed() {
        guard !isUpdateInProgress else { return }
        let request = InstallRequest(
            mode: selectedMode,
            serverURL: serverURLField.stringValue,
            tunnelToken: tunnelTokenField.stringValue
        )
        do {
            _ = try request.validatedServerURL()
            _ = try request.validatedTunnelToken()
        } catch {
            showStatus(error.localizedDescription, isError: true)
            return
        }

        let refreshingQuickTunnel = currentStatus.installed && initialMode == .quick && selectedMode == .quick
        if refreshingQuickTunnel {
            // 生成过程中立即隐藏旧地址，避免用户把旧地址误认为本次生成结果。
            beginQuickTunnelRefresh()
        }
        setBusy(true)
        showStatus(
            refreshingQuickTunnel ? L10n.text("Generating a new temporary public address…") : L10n.text("Validating and applying AgentDock configuration…"),
            isError: false
        )
        Task {
            do {
                let result = try await installer.run(request: request)
                let resultPublicMCPURL: URL?
                if result.publicMCPURL.isEmpty {
                    resultPublicMCPURL = nil
                } else if let parsedURL = URL(string: result.publicMCPURL) {
                    resultPublicMCPURL = parsedURL
                } else {
                    throw ValidationError(L10n.text("The installer returned an invalid public MCP address."))
                }

                authTokenValue = result.authToken
                oauthPasswordValue = result.oauthPassword
                localAddress.stringValue = result.localMCPURL
                quickTunnelRefreshState = .idle
                setDisplayedPublicMCPURL(resultPublicMCPURL, automaticallyCheck: true)
                tunnelTokenField.stringValue = ""
                authVisible = false
                oauthVisible = false
                refreshCredentialFields()
                showStatus(
                    refreshingQuickTunnel
                        ? L10n.text("A new temporary public address was generated; checking public access automatically.")
                        : L10n.format("AgentDock %@ is configured and running normally.", result.version),
                    isError: false
                )
                setBusy(false)
                initialMode = selectedMode
                initialServerURL = selectedMode == .named ? (try? request.validatedServerURL()) ?? "" : ""
                refreshChangeState()
                onChanged()
            } catch {
                if refreshingQuickTunnel {
                    markQuickTunnelRefreshFailed()
                }
                setBusy(false)
                showStatus(error.localizedDescription, isError: true)
            }
        }
    }

    @objc private func startStopPressed() {
        guard !isUpdateInProgress else { return }
        if currentStatus.requiresApproval {
            service.openBackgroundItemsSettings()
            return
        }
        performServiceAction(
            inProgress: currentStatus.loaded ? L10n.text("Stopping AgentDock…") : L10n.text("Starting AgentDock…"),
            completed: currentStatus.loaded ? L10n.text("AgentDock stopped.") : L10n.text("AgentDock started.")
        ) {
            if self.currentStatus.loaded { try await self.service.stop() }
            else { try await self.service.start() }
        }
    }

    @objc private func restartPressed() {
        performServiceAction(
            inProgress: L10n.text("Restarting AgentDock…"),
            completed: L10n.text("AgentDock restarted.")
        ) { try await self.service.restart() }
    }

    @objc private func updatePressed() {
        guard !isUpdateInProgress else { return }
        onUpdateRequested()
    }

    private func performServiceAction(
        inProgress: String,
        completed: String,
        operation: @escaping () async throws -> Void
    ) {
        guard !isUpdateInProgress else { return }
        setBusy(true)
        showStatus(inProgress, isError: false)
        Task {
            do {
                try await operation()
                setBusy(false)
                showStatus(completed, isError: false)
                onChanged()
            } catch {
                setBusy(false)
                showStatus(error.localizedDescription, isError: true)
            }
        }
    }

    @objc private func openPermissionsPressed() { permissionsWindow.present() }
    @objc private func openAdvancedPressed() {
        guard !isUpdateInProgress else { return }
        if advancedSettings == nil {
            advancedSettings = AdvancedSettingsWindowController(
                service: service,
                menuLoginAgent: menuLoginAgent,
                onChanged: onChanged
            )
        }
        advancedSettings?.present(status: currentStatus)
    }
    @objc private func openLogsPressed() { service.openLogs() }

    @objc private func testPublicAddressPressed() {
        guard let displayedPublicMCPURL else { return }
        beginPublicCheck(displayedPublicMCPURL, automatic: false)
    }

    @objc private func copyLocalAddress(_ sender: NSButton) { copy(localAddress.stringValue, button: sender) }
    @objc private func copyPublicAddress(_ sender: NSButton) {
        copy(displayedPublicMCPURL?.absoluteString ?? "", button: sender)
    }
    @objc private func copyAuthToken(_ sender: NSButton) { copy(authTokenValue, button: sender) }
    @objc private func copyOAuthPassword(_ sender: NSButton) { copy(oauthPasswordValue, button: sender) }

    @objc private func toggleAuthToken() {
        authVisible.toggle()
        refreshCredentialFields()
    }

    @objc private func toggleOAuthPassword() {
        oauthVisible.toggle()
        refreshCredentialFields()
    }

    private func refreshCredentialFields() {
        authToken.stringValue = displayedSecret(authTokenValue, visible: authVisible, empty: L10n.text("Not generated"))
        oauthPassword.stringValue = displayedSecret(oauthPasswordValue, visible: oauthVisible, empty: L10n.text("Disabled"))
        authReveal.title = authVisible ? L10n.text("Hide") : L10n.text("Show")
        oauthReveal.title = oauthVisible ? L10n.text("Hide") : L10n.text("Show")
    }

    private func displayedSecret(_ value: String, visible: Bool, empty: String) -> String {
        guard !value.isEmpty else { return empty }
        return visible ? value : String(repeating: "•", count: min(max(value.count, 12), 24))
    }

    private func copy(_ value: String, button: NSButton) {
        guard !value.isEmpty else { return }
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(value, forType: .string)
        let original = button.title
        button.title = L10n.text("Copied")
        DispatchQueue.main.asyncAfter(deadline: .now() + 1.2) {
            button.title = original
        }
    }

    private func refreshChangeState() {
        guard currentStatus.installed else {
            applyButton.title = L10n.text("Configure and enable")
            applyButton.isEnabled = !controlsLocked
            return
        }

        if migrationRequired {
            applyButton.title = L10n.text("Migrate and enable")
            applyButton.isEnabled = !controlsLocked
            return
        }

        let refreshingQuickTunnel = initialMode == .quick && selectedMode == .quick
        applyButton.title = refreshingQuickTunnel ? L10n.text("Regenerate temporary address") : L10n.text("Apply changes")

        let serverChanged = selectedMode == .named
            && serverURLField.stringValue.trimmingCharacters(in: .whitespacesAndNewlines).trimmingCharacters(in: CharacterSet(charactersIn: "/"))
                != initialServerURL.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
        let changed = refreshingQuickTunnel
            || selectedMode != initialMode
            || serverChanged
            || !tunnelTokenField.stringValue.isEmpty
        applyButton.isEnabled = changed && !controlsLocked
    }

    private func setBusy(_ busy: Bool) {
        isBusy = busy
        let locked = controlsLocked
        for control in [publicMode, serverURLField, tunnelTokenField, startStopButton, restartButton, updateButton, advancedButton] {
            control.isEnabled = !locked && (control !== advancedButton || currentStatus.installed)
        }
        if !locked && migrationRequired {
            startStopButton.isEnabled = false
            restartButton.isEnabled = false
            updateButton.isEnabled = false
        }
        // 查看日志在更新期间仍然安全，保留给用户排查长时间操作。
        logsButton.isEnabled = !busy && currentStatus.installed
        if busy {
            applyButton.isEnabled = false
            progress.startAnimation(nil)
        } else {
            progress.stopAnimation(nil)
            refreshChangeState()
        }
        refreshPublicActions()
    }

    private func showStatus(_ message: String, isError: Bool) {
        statusLabel.stringValue = message
        statusLabel.textColor = isError ? .systemRed : .secondaryLabelColor
        statusLabel.isHidden = message.isEmpty
    }

    private func updateWindowHeight() {
        guard let window,
              let visibleFrame = (window.screen ?? NSScreen.main)?.visibleFrame else {
            return
        }

        let verticalMargin: CGFloat = 12
        let maximumHeight = max(window.minSize.height, visibleFrame.height - verticalMargin * 2)
        guard window.frame.height > maximumHeight else { return }

        // 新控制面板使用可滚动 App Shell；切换连接方式不再改变窗口高度，只在小屏幕上做可见区域兜底。
        var frame = window.frame
        frame.origin.y = max(visibleFrame.minY + verticalMargin, frame.maxY - maximumHeight)
        frame.size.height = maximumHeight
        window.setFrame(frame, display: true, animate: window.isVisible)
    }

    private func formRow(title: String, control: NSView) -> NSView {
        let label = NSTextField(labelWithString: title)
        label.textColor = .secondaryLabelColor
        label.widthAnchor.constraint(equalToConstant: 96).isActive = true
        let row = NSStackView(views: [label, control])
        row.orientation = .horizontal
        row.alignment = .centerY
        row.spacing = 10
        return row
    }

    private func valueDetailRow(_ detail: NSTextField) -> NSView {
        let spacer = NSView()
        spacer.widthAnchor.constraint(equalToConstant: 102).isActive = true
        let row = NSStackView(views: [spacer, detail])
        row.orientation = .horizontal
        row.alignment = .centerY
        row.spacing = 0
        return row
    }

    private func valueRow(title: String, field: NSTextField, actions: [NSButton]) -> NSView {
        let label = NSTextField(labelWithString: title)
        label.textColor = .secondaryLabelColor
        label.widthAnchor.constraint(equalToConstant: 94).isActive = true
        let row = NSStackView(views: [label, field] + actions)
        row.orientation = .horizontal
        row.alignment = .centerY
        row.spacing = 8
        return row
    }

    private func addFullWidth(_ view: NSView, to stack: NSStackView, widthAdjustment: CGFloat = 0) {
        stack.addArrangedSubview(view)
        view.widthAnchor.constraint(equalTo: stack.widthAnchor, constant: widthAdjustment).isActive = true
    }

    private func copyButton(_ action: Selector) -> NSButton {
        let button = NSButton(title: L10n.text("Copy"), target: self, action: action)
        button.bezelStyle = .inline
        return button
    }
}
