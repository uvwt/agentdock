import AppKit
import SwiftUI

@MainActor
final class NativeControlPanelWindowController: NSWindowController, NSWindowDelegate {
    private let model: ControlPanelModel
    private lazy var permissionsWindow = DesktopPermissionsWindowController()

    var hasActiveServiceOperation: Bool { model.isBusy }

    init(
        service: ServiceController,
        menuLoginAgent: MenuLoginAgentController,
        onChanged: @escaping () -> Void,
        onUpdateRequested: @escaping () -> Void
    ) {
        model = ControlPanelModel(
            service: service,
            menuLoginAgent: menuLoginAgent,
            onChanged: onChanged,
            onUpdateRequested: onUpdateRequested
        )
        let window = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: 1060, height: 720),
            styleMask: [.titled, .closable, .miniaturizable, .resizable, .fullSizeContentView],
            backing: .buffered,
            defer: false
        )
        window.title = "AgentDock"
        window.titlebarAppearsTransparent = true
        window.isReleasedWhenClosed = false
        window.setFrame(NSRect(x: 0, y: 0, width: 1060, height: 720), display: false)
        window.minSize = NSSize(width: 900, height: 620)
        window.center()
        super.init(window: window)
        window.delegate = self
        window.contentViewController = NSHostingController(rootView: ControlPanelRootView(model: model))
        NotificationCenter.default.addObserver(
            forName: .agentDockOpenPermissions,
            object: nil,
            queue: .main
        ) { [weak self] _ in
            Task { @MainActor in self?.presentPermissions() }
        }
    }

    required init?(coder: NSCoder) { fatalError("init(coder:) has not been implemented") }

    func present(status: ServiceStatus) {
        model.update(status)
        showWindow(nil)
        window?.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }

    func update(status: ServiceStatus) { model.update(status) }
    func refreshServiceStatus(_ status: ServiceStatus) { model.update(status) }

    func setUpdateInProgress(_ inProgress: Bool, status: String? = nil) {
        model.isUpdateInProgress = inProgress
        model.message = inProgress ? (status ?? L10n.text("Updating AgentDock…")) : nil
    }

    func presentPermissions() { permissionsWindow.present() }
}

@MainActor
private final class ControlPanelModel: ObservableObject {
    enum Page: String, CaseIterable, Identifiable {
        case home, connections, capabilities, activity, settings
        var id: String { rawValue }
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

    enum SettingsPage: String, CaseIterable, Identifiable {
        case runtime, permissions, startup, credentials
        var id: String { rawValue }
        var title: String {
            switch self {
            case .runtime: return "Runtime"
            case .permissions: return "Permissions"
            case .startup: return "Startup"
            case .credentials: return "Access Credentials"
            }
        }
    }

    @Published var page: Page = .home
    @Published var settingsPage: SettingsPage = .runtime
    @Published var status: ServiceStatus = .missing
    @Published var statusUpdatedAt = Date()
    @Published var isBusy = false
    @Published var isUpdateInProgress = false
    @Published var message: String?

    let service: ServiceController
    let menuLoginAgent: MenuLoginAgentController
    let onChanged: () -> Void
    let onUpdateRequested: () -> Void
    private lazy var installer = InstallerRunner(service: service)
    private lazy var configurationController = ServiceConfigurationController(service: service)

    init(service: ServiceController, menuLoginAgent: MenuLoginAgentController, onChanged: @escaping () -> Void, onUpdateRequested: @escaping () -> Void) {
        self.service = service
        self.menuLoginAgent = menuLoginAgent
        self.onChanged = onChanged
        self.onUpdateRequested = onUpdateRequested
    }

    var nexusDevice: NexusDeviceStatus { service.nexusDeviceStatus() }

    func update(_ status: ServiceStatus) {
        self.status = status
        statusUpdatedAt = Date()
    }

    func openLogs() { service.openLogs() }
    func openConfiguration() { service.openConfiguration() }

    func refresh() async {
        update(await service.status())
    }

    func applyTunnel(mode: TunnelMode, serverURL: String, tunnelToken: String) async {
        await perform {
            _ = try await self.installer.run(request: InstallRequest(mode: mode, serverURL: serverURL, tunnelToken: tunnelToken))
        }
    }

    func pairNexus(endpoint: String, code: String) async {
        await perform { try await self.service.pairNexus(endpoint: endpoint, pairingCode: code) }
    }

    func applySettings(_ settings: EditableServiceSettings) async {
        await perform { try await self.configurationController.apply(settings) }
    }

    func setCoreAutostart(_ enabled: Bool) async {
        await perform { try await self.service.setAutostart(enabled: enabled) }
    }

    func setMenuAutostart(_ enabled: Bool) async {
        await perform { try self.menuLoginAgent.setEnabled(enabled) }
    }

    func requestUpdate() { onUpdateRequested() }

    private func perform(_ operation: @escaping () async throws -> Void) async {
        guard !isBusy, !isUpdateInProgress else { return }
        isBusy = true
        message = nil
        do {
            try await operation()
            update(await service.status())
            onChanged()
        } catch {
            message = error.localizedDescription
        }
        isBusy = false
    }

    func toggleRuntime() {
        guard !isBusy, !isUpdateInProgress else { return }
        isBusy = true
        message = nil
        Task {
            do {
                if status.loaded { try await service.stop() } else { try await service.start() }
                status = await service.status()
                onChanged()
            } catch {
                message = error.localizedDescription
            }
            isBusy = false
        }
    }
}

private struct ControlPanelRootView: View {
    @ObservedObject var model: ControlPanelModel

    var body: some View {
        NavigationSplitView {
            VStack(spacing: 0) {
                HStack(spacing: 9) {
                    Image(systemName: "shippingbox.fill")
                        .foregroundStyle(.tint)
                    Text("AgentDock").font(.system(size: 15, weight: .semibold))
                    Spacer()
                }
                .padding(.horizontal, 14)
                .padding(.top, 18)
                .padding(.bottom, 12)

                List(selection: $model.page) {
                    ForEach(ControlPanelModel.Page.allCases) { page in
                        Label(page.title, systemImage: page.symbol)
                            .tag(page)
                    }
                }
                .listStyle(.sidebar)
            }
            .navigationSplitViewColumnWidth(min: 160, ideal: 168, max: 180)
        } detail: {
            pageContent
                .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
                .background(Color(nsColor: .windowBackgroundColor))
        }
        .navigationSplitViewStyle(.balanced)
    }

    @ViewBuilder private var pageContent: some View {
        switch model.page {
        case .home: HomeView(model: model)
        case .settings: SettingsView(model: model)
        case .connections: ConnectionsView(model: model)
        case .capabilities: CapabilitiesView(model: model)
        case .activity: ActivityView(model: model)
        }
    }
}

private struct PageHeader: View {
    let title: String
    var detail: String? = nil
    var body: some View {
        VStack(alignment: .leading, spacing: 5) {
            Text(title).font(.system(size: 20, weight: .semibold))
            if let detail { Text(detail).font(.system(size: 12.5)).foregroundStyle(.secondary) }
        }
    }
}

private struct StatusPill: View {
    let text: String
    let active: Bool
    var body: some View {
        HStack(spacing: 6) {
            Circle().fill(active ? Color.green : Color.secondary).frame(width: 7, height: 7)
            Text(text).font(.system(size: 12, weight: .medium))
        }
        .foregroundStyle(active ? .primary : .secondary)
    }
}

private struct SettingsSection<Content: View>: View {
    let title: String
    @ViewBuilder let content: Content
    init(_ title: String, @ViewBuilder content: () -> Content) {
        self.title = title
        self.content = content()
    }
    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(title).font(.system(size: 12, weight: .semibold)).foregroundStyle(.secondary)
            VStack(spacing: 0) { content }
                .background(Color(nsColor: .controlBackgroundColor))
                .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
                .overlay(RoundedRectangle(cornerRadius: 10, style: .continuous).stroke(Color.primary.opacity(0.08), lineWidth: 1))
        }
    }
}

private struct SettingsRow<Trailing: View>: View {
    let title: String
    var detail: String? = nil
    @ViewBuilder let trailing: Trailing
    init(_ title: String, detail: String? = nil, @ViewBuilder trailing: () -> Trailing) {
        self.title = title; self.detail = detail; self.trailing = trailing()
    }
    var body: some View {
        HStack(spacing: 16) {
            VStack(alignment: .leading, spacing: 2) {
                Text(title).font(.system(size: 13))
                if let detail { Text(detail).font(.system(size: 11.5)).foregroundStyle(.secondary) }
            }
            Spacer(minLength: 20)
            trailing
        }
        .padding(.horizontal, 13)
        .frame(minHeight: detail == nil ? 44 : 52)
    }
}

private struct RowDivider: View {
    var body: some View { Divider().padding(.leading, 13) }
}

private struct HomeView: View {
    @ObservedObject var model: ControlPanelModel

    private var running: Bool { model.status.loaded && model.status.healthy }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 22) {
                PageHeader(title: L10n.text("Home"))

                HStack(alignment: .center, spacing: 16) {
                    VStack(alignment: .leading, spacing: 5) {
                        Text("AgentDock \(running ? L10n.text("Running") : L10n.text("Stopped"))")
                            .font(.system(size: 14, weight: .semibold))
                        Text(running ? L10n.text("The local service is ready for AI clients.") : L10n.text("Start AgentDock to accept AI client connections."))
                            .font(.system(size: 13))
                            .foregroundStyle(.secondary)
                        if let message = model.message {
                            Text(message).font(.system(size: 12)).foregroundStyle(.red)
                        }
                    }
                    Spacer()
                    Button(running ? L10n.text("Stop") : L10n.text("Start")) { model.toggleRuntime() }
                        .controlSize(.regular)
                        .disabled(model.isBusy || model.isUpdateInProgress)
                }

                SettingsSection(L10n.text("Runtime status")) {
                    SettingsRow("Runtime") { StatusPill(text: running ? L10n.text("Running") : L10n.text("Stopped"), active: running) }
                    RowDivider()
                    SettingsRow("MCP") { StatusPill(text: model.status.healthy ? L10n.text("Ready") : L10n.text("Unavailable"), active: model.status.healthy) }
                    RowDivider()
                    SettingsRow("NexusDock") { StatusPill(text: nexusText, active: nexusActive) }
                }

                SettingsSection(L10n.text("Quick access")) {
                    SettingsRow(L10n.text("Connect AI clients")) { Image(systemName: "chevron.right").foregroundStyle(.tertiary) }
                        .contentShape(Rectangle()).onTapGesture { model.page = .connections }
                    RowDivider()
                    SettingsRow(L10n.text("Manage capabilities")) { Image(systemName: "chevron.right").foregroundStyle(.tertiary) }
                        .contentShape(Rectangle()).onTapGesture { model.page = .capabilities }
                    RowDivider()
                    SettingsRow(L10n.text("View activity")) { Image(systemName: "chevron.right").foregroundStyle(.tertiary) }
                        .contentShape(Rectangle()).onTapGesture { model.page = .activity }
                }
            }
            .padding(.horizontal, 28).padding(.top, 24).padding(.bottom, 32)
            .frame(maxWidth: 760, alignment: .leading)
        }
    }

    private var nexusActive: Bool {
        if case .connected = model.status.nexusConnection { return true }
        return false
    }
    private var nexusText: String {
        switch model.status.nexusConnection {
        case .connected: return L10n.text("Connected")
        case .disconnected: return L10n.text("Offline")
        case .configurationError: return L10n.text("Unavailable")
        case .unconfigured: return L10n.text("Not configured")
        }
    }
}

private struct ConnectionsView: View {
    @ObservedObject var model: ControlPanelModel
    @State private var tunnelMode: TunnelMode = .local
    @State private var serverURL = ""
    @State private var tunnelToken = ""
    @State private var nexusEndpoint = "https://mcp.nexusdock.co"
    @State private var pairingCode = ""

    private var configuration: ServiceConfiguration? { model.status.configuration }
    private var nexusDevice: NexusDeviceStatus { model.nexusDevice }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 22) {
                PageHeader(title: L10n.text("Connections"), detail: L10n.text("Connect AI clients to this device."))
                SettingsSection(L10n.text("Local connection")) {
                    SettingsRow("Local MCP", detail: configuration?.localMCPURL?.absoluteString ?? "—") {
                        Button(L10n.text("Copy")) { copy(configuration?.localMCPURL?.absoluteString) }.controlSize(.small)
                    }
                    RowDivider()
                    SettingsRow("Public MCP", detail: configuration?.publicMCPURL?.absoluteString ?? L10n.text("Disabled")) {
                        Button(L10n.text("Copy")) { copy(configuration?.publicMCPURL?.absoluteString) }.controlSize(.small)
                    }
                    RowDivider()
                    VStack(alignment: .leading, spacing: 10) {
                        Picker(L10n.text("Public MCP mode"), selection: $tunnelMode) {
                            Text(L10n.text("Local only")).tag(TunnelMode.local)
                            Text(L10n.text("Temporary public address")).tag(TunnelMode.quick)
                            Text(L10n.text("Custom domain")).tag(TunnelMode.named)
                        }.pickerStyle(.segmented)
                        if tunnelMode == .named {
                            TextField("https://mcp.example.com", text: $serverURL).textFieldStyle(.roundedBorder)
                            SecureField(L10n.text("Cloudflare Tunnel Token"), text: $tunnelToken).textFieldStyle(.roundedBorder)
                        }
                        Button(L10n.text("Apply")) {
                            Task {
                                await model.applyTunnel(mode: tunnelMode, serverURL: serverURL, tunnelToken: tunnelToken)
                                tunnelToken = ""
                            }
                        }.disabled(model.isBusy)
                    }.padding(13)
                }

                SettingsSection(L10n.text("Remote connection")) {
                    SettingsRow("NexusDock", detail: nexusDevice.paired ? nexusDevice.endpoint : L10n.text("Not configured")) {
                        StatusPill(text: nexusText, active: nexusActive)
                    }
                    RowDivider()
                    VStack(alignment: .leading, spacing: 10) {
                        TextField(L10n.text("NexusDock address"), text: $nexusEndpoint).textFieldStyle(.roundedBorder)
                        SecureField(L10n.text("One-time pairing code"), text: $pairingCode).textFieldStyle(.roundedBorder)
                        Button(L10n.text("Pair")) {
                            Task {
                                await model.pairNexus(endpoint: nexusEndpoint, code: pairingCode)
                                pairingCode = ""
                            }
                        }.disabled(model.isBusy || nexusEndpoint.isEmpty || pairingCode.isEmpty)
                    }.padding(13)
                }
                if let message = model.message { Text(message).font(.system(size: 12)).foregroundStyle(.secondary) }
            }
            .padding(.horizontal, 28).padding(.top, 24).padding(.bottom, 32)
            .frame(maxWidth: 760, alignment: .leading)
        }
        .task(id: model.statusUpdatedAt) {
            tunnelMode = (try? model.service.configuredTunnelMode()) ?? .local
            serverURL = configuration?.publicURL ?? ""
            nexusEndpoint = nexusDevice.paired ? nexusDevice.endpoint : "https://mcp.nexusdock.co"
        }
    }

    private var nexusActive: Bool { if case .connected = model.status.nexusConnection { return true }; return false }
    private var nexusText: String {
        switch model.status.nexusConnection {
        case .connected: return L10n.text("Connected")
        case .disconnected: return L10n.text("Disconnected")
        case .configurationError: return L10n.text("Unavailable")
        case .unconfigured: return L10n.text("Not configured")
        }
    }
    private func copy(_ value: String?) {
        guard let value, !value.isEmpty else { return }
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(value, forType: .string)
    }
}

private struct CapabilitiesView: View {
    @ObservedObject var model: ControlPanelModel
    @State private var browserEnabled = false
    @State private var browserMode = 0
    @State private var browserCDPURL = ""
    @State private var acpEnabled = false
    @State private var profiles: [ACPProfileConfiguration] = []
    @State private var defaultProfile = ""
    @State private var selectedMCPAppsMode: MCPAppsMode = .full
    @State private var customName = ""
    @State private var customCommand = ""
    @State private var customArguments = ""

    private var configuration: ServiceConfiguration? { model.status.configuration }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 22) {
                PageHeader(title: L10n.text("Capabilities"), detail: L10n.text("Configure the capabilities AgentDock provides to AI."))
                SettingsSection(L10n.text("Available capabilities")) {
                    SettingsRow(L10n.text("Browser"), detail: L10n.text("Browser automation and web operations")) {
                        Toggle("", isOn: $browserEnabled).labelsHidden()
                    }
                    VStack(alignment: .leading, spacing: 8) {
                        Picker(L10n.text("Browser connection"), selection: $browserMode) {
                            Text(L10n.text("Isolated browser")).tag(0)
                            Text(L10n.text("Reuse local browser")).tag(1)
                            Text(L10n.text("Specified CDP")).tag(2)
                        }.pickerStyle(.segmented)
                        if browserMode == 2 {
                            TextField("http://127.0.0.1:9222", text: $browserCDPURL).textFieldStyle(.roundedBorder)
                        }
                    }.padding(.horizontal, 13).padding(.bottom, 12)
                    RowDivider()
                    SettingsRow("Coding Agent", detail: codingAgentDetail) {
                        Toggle("", isOn: $acpEnabled).labelsHidden()
                    }
                    VStack(alignment: .leading, spacing: 8) {
                        ForEach(profiles.indices, id: \.self) { index in
                            Toggle(profileTitle(profiles[index]), isOn: $profiles[index].enabled)
                        }
                        if !enabledProfiles.isEmpty {
                            Picker(L10n.text("Default Coding Agent"), selection: $defaultProfile) {
                                ForEach(enabledProfiles, id: \.id) { profile in
                                    Text(profileTitle(profile)).tag(profile.id)
                                }
                            }
                        }
                        DisclosureGroup(L10n.text("Add custom Coding Agent")) {
                            VStack(alignment: .leading, spacing: 8) {
                                TextField(L10n.text("Name"), text: $customName).textFieldStyle(.roundedBorder)
                                TextField(L10n.text("Command"), text: $customCommand).textFieldStyle(.roundedBorder)
                                TextField(L10n.text("Args JSON"), text: $customArguments).textFieldStyle(.roundedBorder)
                                Button(L10n.text("Add")) { addCustomProfile() }
                                    .disabled(customName.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                            }.padding(.top, 8)
                        }
                    }.padding(.horizontal, 13).padding(.bottom, 12)
                    RowDivider()
                    SettingsRow("MCP Apps", detail: L10n.text("Interactive MCP app presentation")) {
                        Picker("", selection: $selectedMCPAppsMode) {
                            Text(L10n.text("Full")).tag(MCPAppsMode.full)
                            Text(L10n.text("Compact")).tag(MCPAppsMode.compact)
                            Text(L10n.text("Off")).tag(MCPAppsMode.off)
                        }.labelsHidden().frame(width: 120)
                    }
                }
                Button(L10n.text("Save and restart Runtime")) {
                    let settings = EditableServiceSettings(
                        port: configuration?.port ?? 8765,
                        logLevel: configuration?.logLevel ?? "info",
                        mcpAppsMode: selectedMCPAppsMode,
                        browserEnabled: browserEnabled,
                        browserCDPURL: browserMode == 2 ? browserCDPURL : "",
                        browserReuseExistingCDP: browserMode == 1,
                        acpEnabled: acpEnabled,
                        acpProfiles: profiles,
                        acpDefaultProfile: defaultProfile
                    )
                    Task { await model.applySettings(settings) }
                }.disabled(model.isBusy)
                if let message = model.message { Text(message).font(.system(size: 12)).foregroundStyle(.secondary) }
            }
            .padding(.horizontal, 28).padding(.top, 24).padding(.bottom, 32)
            .frame(maxWidth: 760, alignment: .leading)
        }
        .task(id: model.statusUpdatedAt) { loadConfiguration() }
        .onChange(of: profiles.map(\.enabled)) { _ in
            if !enabledProfiles.contains(where: { $0.id == defaultProfile }) {
                defaultProfile = enabledProfiles.first?.id ?? ""
            }
        }
    }

    private var enabledProfiles: [ACPProfileConfiguration] { profiles.filter(\.enabled) }
    private var codingAgentDetail: String {
        let enabledCount = profiles.filter(\.enabled).count
        return enabledCount == 0 ? L10n.text("No profiles configured") : L10n.format("%d enabled · default %@", enabledCount, defaultProfile)
    }
    private func profileTitle(_ profile: ACPProfileConfiguration) -> String {
        profile.displayName?.isEmpty == false ? profile.displayName! : profile.id
    }
    private func loadConfiguration() {
        guard let configuration else { return }
        browserEnabled = configuration.browserEnabled
        browserCDPURL = configuration.browserCDPURL
        browserMode = !configuration.browserCDPURL.isEmpty ? 2 : (configuration.browserReuseExistingCDP ? 1 : 0)
        acpEnabled = configuration.acpEnabled
        profiles = configuration.acpProfiles
        for preset in [ACPAgentPreset.codex, .claude, .grok] where !profiles.contains(where: { $0.id == preset.rawValue }) {
            profiles.append(ACPProfileConfiguration(
                id: preset.rawValue,
                kind: preset,
                command: "",
                args: [],
                envFromEnv: nil,
                enabled: false
            ))
        }
        defaultProfile = configuration.acpDefaultProfile
        selectedMCPAppsMode = configuration.mcpAppsMode
    }

    private func addCustomProfile() {
        let name = customName.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !name.isEmpty else { return }
        let base = name.lowercased().map { character -> Character in
            character.isLetter || character.isNumber || character == "." || character == "_" || character == "-" ? character : "-"
        }
        var id = String(base).trimmingCharacters(in: CharacterSet(charactersIn: "-"))
        if id.isEmpty { id = "custom" }
        var suffix = 2
        let original = id
        while profiles.contains(where: { $0.id == id }) { id = "\(original)-\(suffix)"; suffix += 1 }
        let args = (try? ACPDesktopConfiguration.decodeArguments(customArguments)) ?? []
        profiles.append(ACPProfileConfiguration(
            id: id,
            displayName: name,
            kind: .custom,
            command: customCommand.trimmingCharacters(in: .whitespacesAndNewlines),
            args: args,
            envFromEnv: nil,
            enabled: true
        ))
        if defaultProfile.isEmpty { defaultProfile = id }
        customName = ""; customCommand = ""; customArguments = ""
    }
}

private struct ActivityView: View {
    @ObservedObject var model: ControlPanelModel

    private var running: Bool { model.status.loaded && model.status.healthy }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 22) {
                PageHeader(
                    title: L10n.text("Activity"),
                    detail: L10n.text("Runtime status, diagnostics, and local files.")
                )

                SettingsSection("Runtime") {
                    SettingsRow("Runtime") {
                        StatusPill(
                            text: running ? L10n.text("Running") : L10n.text("Stopped"),
                            active: running
                        )
                    }
                    RowDivider()
                    SettingsRow(L10n.text("Version")) {
                        Text(model.status.version ?? "—")
                            .font(.system(size: 12))
                            .foregroundStyle(.secondary)
                    }
                    RowDivider()
                    SettingsRow(L10n.text("Last status refresh")) {
                        Text(model.statusUpdatedAt, style: .time)
                            .font(.system(size: 12))
                            .foregroundStyle(.secondary)
                    }
                }

                SettingsSection(L10n.text("Diagnostics")) {
                    SettingsRow(L10n.text("Logs directory")) {
                        Button(L10n.text("Open")) { model.openLogs() }
                            .controlSize(.small)
                    }
                    RowDivider()
                    SettingsRow(L10n.text("Configuration directory")) {
                        Button(L10n.text("Open")) { model.openConfiguration() }
                            .controlSize(.small)
                    }
                }
            }
            .padding(.horizontal, 28)
            .padding(.top, 24)
            .padding(.bottom, 32)
            .frame(maxWidth: 760, alignment: .leading)
        }
    }
}

private struct SettingsView: View {
    @ObservedObject var model: ControlPanelModel
    @State private var port = 8765
    @State private var logLevel = "info"
    @State private var languagePreference: UILanguagePreference = .system
    @State private var coreAutostart = false
    @State private var menuAutostart = false
    @State private var showAuthToken = false
    @State private var showOAuthPassword = false

    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            PageHeader(title: L10n.text("Settings"))
                .padding(.horizontal, 28).padding(.top, 24)

            HStack(alignment: .top, spacing: 0) {
                List(selection: $model.settingsPage) {
                    ForEach(ControlPanelModel.SettingsPage.allCases) { item in
                        Text(item.title).tag(item)
                    }
                }
                .listStyle(.sidebar)
                .frame(width: 190)
                Divider()
                ScrollView {
                    settingsContent
                        .padding(.horizontal, 28).padding(.vertical, 4).padding(.bottom, 32)
                        .frame(maxWidth: 650, alignment: .leading)
                }
            }
        }
        .task(id: model.statusUpdatedAt) {
            if let configuration = model.status.configuration {
                port = configuration.port
                logLevel = configuration.logLevel
            }
            languagePreference = L10n.languagePreference()
            coreAutostart = model.status.autostartEnabled
            menuAutostart = model.menuLoginAgent.isEnabled
        }
    }

    @ViewBuilder private var settingsContent: some View {
        switch model.settingsPage {
        case .runtime:
            VStack(alignment: .leading, spacing: 20) {
                PageHeader(title: "Runtime", detail: L10n.text("Local AgentDock runtime status and service controls."))
                SettingsSection("AgentDock Runtime") {
                    SettingsRow(L10n.text("Status")) { StatusPill(text: model.status.healthy ? L10n.text("Running") : L10n.text("Stopped"), active: model.status.healthy) }
                    RowDivider()
                    SettingsRow(L10n.text("Local address")) { Text(model.status.configuration?.localMCPURL?.absoluteString ?? "—").font(.system(size: 12)).foregroundStyle(.secondary) }
                    RowDivider()
                    SettingsRow(L10n.text("Service")) {
                        Button(model.status.loaded ? L10n.text("Stop") : L10n.text("Start")) { model.toggleRuntime() }.controlSize(.small)
                    }
                    RowDivider()
                    SettingsRow(L10n.text("Service port")) {
                        TextField("", value: $port, format: .number).textFieldStyle(.roundedBorder).frame(width: 100)
                    }
                    RowDivider()
                    SettingsRow(L10n.text("Log level")) {
                        Picker("", selection: $logLevel) {
                            ForEach(["debug", "info", "warn", "error"], id: \.self) { Text($0).tag($0) }
                        }.labelsHidden().frame(width: 110)
                    }
                    RowDivider()
                    SettingsRow(L10n.text("Interface language")) {
                        Picker("", selection: $languagePreference) {
                            ForEach(UILanguagePreference.allCases, id: \.rawValue) { preference in
                                Text(preference.title).tag(preference)
                            }
                        }.labelsHidden().frame(width: 150)
                    }
                    RowDivider()
                    SettingsRow(L10n.text("Runtime configuration")) {
                        Button(L10n.text("Save and restart Runtime")) {
                            guard let configuration = model.status.configuration else { return }
                            let settings = EditableServiceSettings(
                                port: port,
                                logLevel: logLevel,
                                mcpAppsMode: configuration.mcpAppsMode,
                                browserEnabled: configuration.browserEnabled,
                                browserCDPURL: configuration.browserCDPURL,
                                browserReuseExistingCDP: configuration.browserReuseExistingCDP,
                                acpEnabled: configuration.acpEnabled,
                                acpProfiles: configuration.acpProfiles,
                                acpDefaultProfile: configuration.acpDefaultProfile
                            )
                            Task {
                                await model.applySettings(settings)
                                if model.message == nil, languagePreference != L10n.languagePreference() {
                                    let previous = L10n.languagePreference()
                                    L10n.setLanguagePreference(languagePreference)
                                    let relaunch = Process()
                                    relaunch.executableURL = URL(fileURLWithPath: "/usr/bin/open")
                                    relaunch.arguments = ["-n", Bundle.main.bundlePath]
                                    do {
                                        try relaunch.run()
                                        NSApp.terminate(nil)
                                    } catch {
                                        L10n.setLanguagePreference(previous)
                                    }
                                }
                            }
                        }.controlSize(.small)
                    }
                    RowDivider()
                    SettingsRow(L10n.text("AgentDock update")) {
                        Button(L10n.text("Check for updates")) { model.requestUpdate() }.controlSize(.small)
                    }
                }
                if let message = model.message { Text(message).font(.system(size: 12)).foregroundStyle(.secondary) }
            }
        case .permissions:
            VStack(alignment: .leading, spacing: 20) {
                PageHeader(title: "Permissions", detail: L10n.text("Review macOS permissions required by enabled capabilities."))
                SettingsSection(L10n.text("System permissions")) {
                    SettingsRow(L10n.text("AgentDock permissions"), detail: L10n.text("Screen, accessibility and file access are managed by macOS.")) {
                        Button(L10n.text("Check permissions")) {
                            NotificationCenter.default.post(name: .agentDockOpenPermissions, object: nil)
                        }.controlSize(.small)
                    }
                }
            }
        case .startup:
            VStack(alignment: .leading, spacing: 20) {
                PageHeader(title: "Startup", detail: L10n.text("Background service and menu bar startup behavior."))
                SettingsSection(L10n.text("Startup")) {
                    SettingsRow(L10n.text("Core background service")) {
                        Toggle("", isOn: $coreAutostart).labelsHidden()
                            .onChange(of: coreAutostart) { value in Task { await model.setCoreAutostart(value) } }
                    }
                    RowDivider()
                    SettingsRow(L10n.text("Menu bar app")) {
                        Toggle("", isOn: $menuAutostart).labelsHidden()
                            .onChange(of: menuAutostart) { value in Task { await model.setMenuAutostart(value) } }
                    }
                }
            }
        case .credentials:
            VStack(alignment: .leading, spacing: 20) {
                PageHeader(title: "Access Credentials", detail: L10n.text("Credentials are stored in the protected AgentDock configuration."))
                SettingsSection(L10n.text("Credentials")) {
                    SettingsRow(L10n.text("Authentication token"), detail: showAuthToken ? (model.status.configuration?.authToken ?? "—") : "••••••••••••") {
                        Button(showAuthToken ? L10n.text("Hide") : L10n.text("Show")) { showAuthToken.toggle() }.controlSize(.small)
                    }
                    RowDivider()
                    SettingsRow(L10n.text("OAuth password"), detail: showOAuthPassword ? (model.status.configuration?.oauthPassword ?? "—") : "••••••••••••") {
                        Button(showOAuthPassword ? L10n.text("Hide") : L10n.text("Show")) { showOAuthPassword.toggle() }.controlSize(.small)
                    }
                }
            }
        }
    }
}

private extension Notification.Name {
    static let agentDockOpenPermissions = Notification.Name("AgentDockOpenPermissions")
}
