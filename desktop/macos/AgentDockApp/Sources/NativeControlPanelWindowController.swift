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

    func presentActivity(status: ServiceStatus) {
        model.page = .activity
        present(status: status)
    }

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
        case appearance, permissions, startup, logs, advancedConnection, about

        var id: String { rawValue }
        var title: String {
            switch self {
            case .permissions: return L10n.text("Permissions")
            case .startup: return L10n.text("Startup")
            case .logs: return L10n.text("Logs")

            case .advancedConnection: return L10n.text("Advanced connection")
            case .appearance: return L10n.text("Appearance")
            case .about: return L10n.text("About")

            }
        }
    }

    @Published var page: Page = .home
    @Published var settingsPage: SettingsPage = .appearance
    @Published var status: ServiceStatus = .missing
    @Published var dashboard = RuntimeDashboardSnapshot.empty
    @Published var cloudflaredComponent = CloudflaredComponentStatus.unavailable
    @Published var statusUpdatedAt = Date()
    @Published private(set) var languageRevision = 0
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
        if !status.loaded || !status.healthy {
            dashboard = .empty
        }
        statusUpdatedAt = Date()
    }

    func openLogs() { service.openLogs() }
    func openConfiguration() { service.openConfiguration() }

    func refresh() async {
        update(await service.status())
        cloudflaredComponent = await service.cloudflaredComponentStatus()
    }

    func refreshCloudflaredComponent() async {
        cloudflaredComponent = await service.cloudflaredComponentStatus()
    }

    func refreshDashboard() async {
        guard status.loaded, status.healthy else {
            dashboard = .empty
            return
        }
        dashboard = await service.dashboard(configuration: status.configuration)
    }

    func applyTunnel(mode: TunnelMode, serverURL: String, tunnelToken: String) async {
        await perform {
            try await self.service.configureTunnel(mode: mode, serverURL: serverURL, tunnelToken: tunnelToken)
        }
        cloudflaredComponent = await service.cloudflaredComponentStatus()
    }

    func runCloudflaredComponentAction(_ action: String) async {
        await perform {
            switch action {
            case "update":
                self.cloudflaredComponent = try await self.service.updateCloudflaredComponent()
            case "uninstall":
                self.cloudflaredComponent = try await self.service.uninstallCloudflaredComponent()
            default:
                self.cloudflaredComponent = try await self.service.installCloudflaredComponent()
            }
        }
        cloudflaredComponent = await service.cloudflaredComponentStatus()
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

    func setLanguagePreference(_ preference: UILanguagePreference) {
        guard preference != L10n.languagePreference() else { return }
        L10n.setLanguagePreference(preference)
        languageRevision += 1
    }

    func setThemePreference(_ preference: UIThemePreference) {
        guard preference != AppAppearance.preference() else { return }
        AppAppearance.setPreference(preference)
    }

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
                if !status.installed {
                    _ = try await installer.run(request: InstallRequest(mode: .local, serverURL: "", tunnelToken: ""))
                } else if status.loaded {
                    try await service.stop()
                } else {
                    try await service.start()
                }
                status = await service.status()
                cloudflaredComponent = await service.cloudflaredComponentStatus()
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
                    AgentDockLogoView(size: 20)
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
                .id(model.languageRevision)
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
            SettingsCard { content }
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

private struct SettingsCard<Content: View>: View {
    @ViewBuilder let content: Content
    init(@ViewBuilder content: () -> Content) {
        self.content = content()
    }
    var body: some View {
        VStack(spacing: 0) { content }
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Color(nsColor: .controlBackgroundColor))
            .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: 10, style: .continuous).stroke(Color.primary.opacity(0.08), lineWidth: 1))
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
        GeometryReader { proxy in
            HStack(spacing: 16) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(title).font(.system(size: 13))
                    if let detail {
                        Text(detail)
                            .font(.system(size: 11.5))
                            .foregroundStyle(.secondary)
                    }
                }

                Spacer(minLength: 20)

                trailing
                    .fixedSize(horizontal: true, vertical: false)
            }
            .frame(width: max(0, proxy.size.width - 26), height: proxy.size.height)
            .padding(.horizontal, 13)
        }
        .frame(height: detail == nil ? 44 : 52)
    }
}

private struct RowDivider: View {
    var body: some View { Divider().padding(.leading, 13) }
}

private enum HomeCapabilityState {
    case enabled
    case disabled
    case attention

    var symbol: String {
        switch self {
        case .enabled: return "circle.fill"
        case .disabled: return "circle"
        case .attention: return "exclamationmark.circle.fill"
        }
    }

    var color: Color {
        switch self {
        case .enabled: return .green
        case .disabled: return .secondary
        case .attention: return .orange
        }
    }

    var text: String {
        switch self {
        case .enabled: return L10n.text("Enabled")
        case .disabled: return L10n.text("Disabled")
        case .attention: return L10n.text("Needs attention")
        }
    }
}

private struct HomeCapabilityItem {
    let title: String
    let detail: String
    let state: HomeCapabilityState
}

private struct HomeMetric: View {
    let title: String
    let value: String
    let detail: String

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(title)
                .font(.system(size: 11.5))
                .foregroundStyle(.secondary)
            Text(value)
                .font(.system(size: 20, weight: .semibold))
            Text(detail)
                .font(.system(size: 11))
                .foregroundStyle(.secondary)
                .lineLimit(1)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

private struct HomeCapabilityRow: View {
    let item: HomeCapabilityItem

    var body: some View {
        HStack(spacing: 16) {
            VStack(alignment: .leading, spacing: 2) {
                Text(item.title).font(.system(size: 13))
                Text(item.detail).font(.system(size: 11.5)).foregroundStyle(.secondary)
            }
            Spacer(minLength: 20)
            HStack(spacing: 9) {
                HStack(spacing: 6) {
                    Image(systemName: item.state.symbol)
                        .font(.system(size: 8, weight: .semibold))
                        .foregroundStyle(item.state.color)
                    Text(item.state.text)
                        .font(.system(size: 12, weight: .medium))
                        .foregroundStyle(item.state == .disabled ? .secondary : .primary)
                }
                Image(systemName: "chevron.right")
                    .font(.system(size: 11, weight: .semibold))
                    .foregroundStyle(.tertiary)
            }
        }
        .padding(.horizontal, 13)
        .frame(minHeight: 56)
        .contentShape(Rectangle())
    }
}

private struct HomeRecentActivityRow: View {
    let call: RuntimeDiagnosticCall
    let source: String
    let time: String

    var body: some View {
        HStack(spacing: 16) {
            VStack(alignment: .leading, spacing: 2) {
                Text(call.tool)
                    .font(.system(size: 13, weight: .medium))
                Text("\(source) · \(time)")
                    .font(.system(size: 11.5))
                    .foregroundStyle(.secondary)
            }
            Spacer(minLength: 20)
            HStack(spacing: 6) {
                Image(systemName: call.success ? "circle.fill" : "exclamationmark.circle.fill")
                    .font(.system(size: 8, weight: .semibold))
                    .foregroundStyle(call.success ? Color.green : Color.orange)
                Text(call.success ? L10n.text("Succeeded") : L10n.text("Failed"))
                    .font(.system(size: 12, weight: .medium))
            }
        }
        .padding(.horizontal, 13)
        .frame(minHeight: 54)
    }
}

private struct HomeView: View {
    @ObservedObject var model: ControlPanelModel

    private static let fractionalDateFormatter: ISO8601DateFormatter = {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return formatter
    }()

    private static let basicDateFormatter: ISO8601DateFormatter = {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime]
        return formatter
    }()

    private var serviceLoaded: Bool { model.status.loaded }
    private var serviceHealthy: Bool { model.status.loaded && model.status.healthy }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 22) {
                PageHeader(title: L10n.text("Home"))

                HStack(alignment: .center, spacing: 16) {
                    AgentDockLogoView(size: 48)

                    VStack(alignment: .leading, spacing: 5) {
                        HStack(spacing: 8) {
                            Text("AgentDock")
                                .font(.system(size: 18, weight: .semibold))
                            if let version = model.status.version, !version.isEmpty {
                                Text(version.hasPrefix("v") ? version : "v\(version)")
                                    .font(.system(size: 11.5))
                                    .foregroundStyle(.secondary)
                            }
                        }
                        Text(agentDockDetailText)
                            .font(.system(size: 13))
                            .foregroundStyle(.secondary)
                        if let message = model.message {
                            Text(message).font(.system(size: 12)).foregroundStyle(.red)
                        }
                    }

                    Spacer()

                    HStack(spacing: 10) {
                        Button(serviceLoaded ? L10n.text("Stop") : L10n.text("Start")) { model.toggleRuntime() }
                            .controlSize(.small)
                            .disabled(model.isBusy || model.isUpdateInProgress)
                        StatusPill(text: agentDockStatusText, active: serviceHealthy)
                    }
                }
                .padding(.horizontal, 18)
                .padding(.vertical, 16)
                .background(Color(nsColor: .controlBackgroundColor))
                .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
                .overlay(
                    RoundedRectangle(cornerRadius: 12, style: .continuous)
                        .stroke(Color.primary.opacity(0.08), lineWidth: 1)
                )

                HStack(spacing: 0) {
                    Button {
                        model.page = .connections
                    } label: {
                        HomeMetric(
                            title: L10n.text("Remote connection"),
                            value: nexusText,
                            detail: remoteConnectionDetail
                        )
                        .frame(minWidth: 180, maxWidth: .infinity, alignment: .leading)
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)

                    metricDivider

                    HomeMetric(
                        title: L10n.text("Skills"),
                        value: dashboardCount(model.dashboard.skillCount),
                        detail: L10n.text("Installed")
                    )
                    .frame(width: 120)

                    metricDivider

                    HomeMetric(
                        title: "MCP",
                        value: dashboardCount(model.dashboard.mcpCount),
                        detail: L10n.text("Configured")
                    )
                    .frame(width: 120)

                    metricDivider

                    HomeMetric(
                        title: L10n.text("Plugins"),
                        value: dashboardCount(model.dashboard.pluginCount),
                        detail: L10n.text("Installed")
                    )
                    .frame(width: 120)
                }
                .padding(.horizontal, 18)
                .padding(.vertical, 12)
                .background(Color(nsColor: .controlBackgroundColor))
                .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
                .overlay(
                    RoundedRectangle(cornerRadius: 10, style: .continuous)
                        .stroke(Color.primary.opacity(0.08), lineWidth: 1)
                )

                SettingsSection(L10n.text("Core capabilities")) {
                    ForEach(Array(capabilityItems.enumerated()), id: \.offset) { index, item in
                        if index > 0 { RowDivider() }
                        Button {
                            model.page = .capabilities
                        } label: {
                            HomeCapabilityRow(item: item)
                        }
                        .buttonStyle(.plain)
                    }
                }

                SettingsSection(L10n.text("Recent activity")) {
                    if !model.dashboard.diagnosticsAvailable {
                        emptyActivity(L10n.text("Recent activity is unavailable."))
                    } else if model.dashboard.recentCalls.isEmpty {
                        emptyActivity(L10n.text("No recent activity yet."))
                    } else {
                        ForEach(Array(model.dashboard.recentCalls.prefix(3).enumerated()), id: \.element.id) { index, call in
                            if index > 0 { RowDivider() }
                            Button {
                                model.page = .activity
                            } label: {
                                HomeRecentActivityRow(
                                    call: call,
                                    source: activitySource(call.source),
                                    time: relativeTime(call.startedAt)
                                )
                            }
                            .buttonStyle(.plain)
                        }
                    }
                }
            }
            .padding(.horizontal, 28)
            .padding(.top, 24)
            .padding(.bottom, 32)
            .frame(maxWidth: 840, alignment: .leading)
        }
        .task(id: model.statusUpdatedAt) {
            await model.refreshDashboard()
        }
    }

    @ViewBuilder
    private var metricDivider: some View {
        Divider()
            .frame(height: 56)
            .padding(.horizontal, 18)
    }

    @ViewBuilder
    private func emptyActivity(_ text: String) -> some View {
        Text(text)
            .font(.system(size: 12.5))
            .foregroundStyle(.secondary)
            .padding(.horizontal, 13)
            .frame(minHeight: 52, alignment: .leading)
            .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func dashboardCount(_ value: Int) -> String {
        model.dashboard.countsAvailable ? String(value) : "—"
    }

    private var agentDockStatusText: String {
        if !serviceLoaded { return L10n.text("Stopped") }
        return serviceHealthy ? L10n.text("Running") : L10n.text("Needs attention")
    }

    private var agentDockDetailText: String {
        if !serviceLoaded { return L10n.text("Start AgentDock to accept AI client connections.") }
        return serviceHealthy
            ? L10n.text("This device is ready for AI.")
            : L10n.text("AgentDock is running, but the connection service is not ready.")
    }

    private var nexusText: String {
        switch model.status.nexusConnection {
        case .connected: return L10n.text("Connected")
        case .disconnected: return L10n.text("Offline")
        case .configurationError: return L10n.text("Unavailable")
        case .unconfigured: return L10n.text("Not configured")
        }
    }

    private var remoteConnectionDetail: String {
        let device = model.nexusDevice
        if device.error != nil { return L10n.text("Unavailable") }
        guard device.paired else { return L10n.text("Not configured") }
        let endpoint = device.endpoint.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
        if endpoint.caseInsensitiveCompare("https://mcp.nexusdock.co") == .orderedSame {
            return "\(L10n.text("Official service")) · nexusdock.co"
        }
        return "\(L10n.text("Self-hosted service")) · \(device.endpoint)"
    }

    private var capabilityItems: [HomeCapabilityItem] {
        guard let configuration = model.status.configuration else {
            return [
                HomeCapabilityItem(title: L10n.text("Browser"), detail: L10n.text("Not enabled yet"), state: .disabled),
                HomeCapabilityItem(title: L10n.text("Coding Agent"), detail: L10n.text("Not enabled yet"), state: .disabled),
                HomeCapabilityItem(title: L10n.text("MCP Apps"), detail: L10n.text("Off"), state: .disabled),
            ]
        }

        let browserDetail: String
        if !configuration.browserEnabled {
            browserDetail = L10n.text("Not enabled yet")
        } else if !configuration.browserCDPURL.isEmpty {
            browserDetail = L10n.text("Use the configured CDP browser")
        } else if configuration.browserReuseExistingCDP {
            browserDetail = L10n.text("Prefer an existing local browser")
        } else {
            browserDetail = L10n.text("Use an isolated browser")
        }

        let enabledProfiles = configuration.acpProfiles.filter(\.enabled)
        let codingAgentState: HomeCapabilityState
        let codingAgentDetail: String
        if !configuration.acpEnabled {
            codingAgentState = .disabled
            codingAgentDetail = L10n.text("Not enabled yet")
        } else if let defaultProfile = enabledProfiles.first(where: { $0.id == configuration.acpDefaultProfile }) {
            codingAgentState = .enabled
            let name = defaultProfile.displayName?.trimmingCharacters(in: .whitespacesAndNewlines)
            codingAgentDetail = L10n.format("%@ · default", (name?.isEmpty == false ? name! : defaultProfile.id))
        } else {
            codingAgentState = .attention
            codingAgentDetail = L10n.text("Needs attention")
        }

        let mcpAppsState: HomeCapabilityState = configuration.mcpAppsMode == .off ? .disabled : .enabled
        let mcpAppsDetail: String
        switch configuration.mcpAppsMode {
        case .full: mcpAppsDetail = L10n.text("Full")
        case .compact: mcpAppsDetail = L10n.text("Compact")
        case .off: mcpAppsDetail = L10n.text("Off")
        }

        return [
            HomeCapabilityItem(
                title: L10n.text("Browser"),
                detail: browserDetail,
                state: configuration.browserEnabled ? .enabled : .disabled
            ),
            HomeCapabilityItem(
                title: L10n.text("Coding Agent"),
                detail: codingAgentDetail,
                state: codingAgentState
            ),
            HomeCapabilityItem(
                title: L10n.text("MCP Apps"),
                detail: mcpAppsDetail,
                state: mcpAppsState
            ),
        ]
    }

    private func activitySource(_ source: String) -> String {
        switch source.lowercased() {
        case "nexus": return L10n.text("Remote")
        case "internal": return L10n.text("Local")
        case "mcp": return "MCP"
        default: return source
        }
    }

    private func relativeTime(_ rawDate: String) -> String {
        guard let date = Self.fractionalDateFormatter.date(from: rawDate)
                ?? Self.basicDateFormatter.date(from: rawDate) else {
            return rawDate
        }
        let elapsed = max(0, Date().timeIntervalSince(date))
        if elapsed < 60 { return L10n.text("Just now") }
        if elapsed < 3600 { return L10n.format("%d min ago", max(1, Int(elapsed / 60))) }
        if elapsed < 86400 { return L10n.format("%d hr ago", max(1, Int(elapsed / 3600))) }
        return DateFormatter.localizedString(from: date, dateStyle: .short, timeStyle: .short)
    }
}

private struct ConnectionsView: View {
    private enum RemoteServiceChoice: String, CaseIterable {
        case official
        case selfHosted
    }

    private static let officialEndpoint = "https://mcp.nexusdock.co"
    private static let officialDevicesURL = URL(string: "https://mcp.nexusdock.co/workspace/devices")!

    @ObservedObject var model: ControlPanelModel
    @State private var pairingCode = ""
    @State private var rePairing = false
    @State private var remoteService: RemoteServiceChoice = .official
    @State private var customEndpoint = ""
    @State private var editingRemoteService = false

    private var nexusDevice: NexusDeviceStatus { model.nexusDevice }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 22) {
                PageHeader(
                    title: L10n.text("Connections"),
                    detail: L10n.text("Connect AI clients to this device remotely.")
                )

                SettingsSection(L10n.text("Remote connection")) {
                    SettingsRow(L10n.text("Status")) {
                        StatusPill(text: connectionText, active: connectionActive)
                    }
                    RowDivider()
                    SettingsRow(L10n.text("Remote service"), detail: remoteServiceDetail) {
                        Button(editingRemoteService ? L10n.text("Done") : L10n.text("Change")) {
                            editingRemoteService.toggle()
                        }
                        .controlSize(.small)
                    }

                    if editingRemoteService {
                        RowDivider()
                        VStack(alignment: .leading, spacing: 9) {
                            Picker("", selection: $remoteService) {
                                Text(L10n.text("Official service")).tag(RemoteServiceChoice.official)
                                Text(L10n.text("Self-hosted service")).tag(RemoteServiceChoice.selfHosted)
                            }
                            .labelsHidden()
                            .pickerStyle(.segmented)

                            if remoteService == .selfHosted {
                                TextField(L10n.text("Service address"), text: $customEndpoint)
                                    .textFieldStyle(.roundedBorder)
                            } else {
                                Text(L10n.text("Official service uses nexusdock.co by default."))
                                    .font(.system(size: 12))
                                    .foregroundStyle(.secondary)
                            }
                        }
                        .padding(13)
                    }

                    RowDivider()
                    VStack(alignment: .leading, spacing: 10) {
                        if nexusDevice.paired && !rePairing {
                            if remoteService == .official {
                                Link(
                                    L10n.text("Manage connected devices ↗"),
                                    destination: Self.officialDevicesURL
                                )
                                .font(.system(size: 12))
                            }
                            Button(L10n.text("Re-pair")) {
                                pairingCode = ""
                                rePairing = true
                            }
                            .frame(maxWidth: .infinity, alignment: .trailing)
                        } else {
                            Text(L10n.text("Enter the pairing code to connect this device."))
                                .font(.system(size: 12))
                                .foregroundStyle(.secondary)
                            SecureField(L10n.text("Pairing code"), text: $pairingCode)
                                .textFieldStyle(.roundedBorder)
                            if remoteService == .official {
                                Link(
                                    L10n.text("No pairing code? Get one from NexusDock ↗"),
                                    destination: Self.officialDevicesURL
                                )
                                .font(.system(size: 12))
                            }
                            Button(L10n.text("Connect")) {
                                Task {
                                    await model.pairNexus(endpoint: selectedEndpoint, code: pairingCode)
                                    if model.message == nil {
                                        pairingCode = ""
                                        rePairing = false
                                    }
                                }
                            }
                            .disabled(model.isBusy || selectedEndpoint.isEmpty || pairingCode.isEmpty)
                            .frame(maxWidth: .infinity, alignment: .trailing)
                        }
                    }
                    .padding(13)
                }

                SettingsSection(L10n.text("Advanced connection settings")) {
                    Button {
                        model.settingsPage = .advancedConnection
                        model.page = .settings
                    } label: {
                        HStack(spacing: 12) {
                            VStack(alignment: .leading, spacing: 3) {
                                Text(L10n.text("Advanced connection settings"))
                                    .font(.system(size: 13, weight: .medium))
                                Text(L10n.text("Local MCP, access credentials, and direct public access."))
                                    .font(.system(size: 12))
                                    .foregroundStyle(.secondary)
                            }
                            Spacer(minLength: 20)
                            Image(systemName: "chevron.right")
                                .foregroundStyle(.tertiary)
                        }
                        .padding(.horizontal, 13)
                        .frame(minHeight: 52)
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                }

                if let message = model.message {
                    Text(message)
                        .font(.system(size: 12))
                        .foregroundStyle(.secondary)
                }
            }
            .padding(.horizontal, 28)
            .padding(.top, 24)
            .padding(.bottom, 32)
            .frame(maxWidth: 760, alignment: .leading)
        }
        .task(id: model.statusUpdatedAt) {
            let endpoint = nexusDevice.paired ? nexusDevice.endpoint : Self.officialEndpoint
            if isOfficialEndpoint(endpoint) {
                remoteService = .official
                customEndpoint = ""
            } else {
                remoteService = .selfHosted
                customEndpoint = endpoint
            }
        }
    }

    private var selectedEndpoint: String {
        switch remoteService {
        case .official:
            return Self.officialEndpoint
        case .selfHosted:
            return customEndpoint.trimmingCharacters(in: .whitespacesAndNewlines)
        }
    }

    private var remoteServiceDetail: String {
        switch remoteService {
        case .official:
            return "\(L10n.text("Official service")) · nexusdock.co"
        case .selfHosted:
            let endpoint = customEndpoint.trimmingCharacters(in: .whitespacesAndNewlines)
            return endpoint.isEmpty ? L10n.text("Self-hosted service") : "\(L10n.text("Self-hosted service")) · \(endpoint)"
        }
    }

    private var connectionActive: Bool {
        if case .connected = model.status.nexusConnection { return true }
        return false
    }

    private var connectionText: String {
        switch model.status.nexusConnection {
        case .connected: return L10n.text("Connected")
        case .disconnected: return L10n.text("Disconnected")
        case .configurationError: return L10n.text("Unavailable")
        case .unconfigured: return L10n.text("Not configured")
        }
    }


    private func isOfficialEndpoint(_ endpoint: String) -> Bool {
        endpoint.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
            .caseInsensitiveCompare(Self.officialEndpoint) == .orderedSame

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
                VStack(alignment: .leading, spacing: 8) {
                    Text(L10n.text("Built-in capabilities"))
                        .font(.system(size: 12, weight: .semibold))
                        .foregroundStyle(.secondary)

                    SettingsCard {
                        SettingsRow(L10n.text("Browser"), detail: L10n.text("Browser automation and web operations")) {
                            Toggle("", isOn: $browserEnabled)
                                .labelsHidden()
                                .toggleStyle(.switch)
                        }
                        VStack(alignment: .leading, spacing: 8) {
                            Picker(L10n.text("Browser connection"), selection: $browserMode) {
                                Text(L10n.text("Isolated browser")).tag(0)
                                Text(L10n.text("Reuse local browser")).tag(1)
                                Text(L10n.text("Specified CDP")).tag(2)
                            }
                            .pickerStyle(.segmented)

                            TextField("http://127.0.0.1:9222", text: $browserCDPURL)
                                .textFieldStyle(.roundedBorder)
                                .opacity(browserMode == 2 ? 1 : 0)
                                .disabled(browserMode != 2)
                                .allowsHitTesting(browserMode == 2)
                                .accessibilityHidden(browserMode != 2)
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.horizontal, 13)
                        .padding(.bottom, 12)
                    }

                    SettingsCard {
                        SettingsRow(L10n.text("Coding Agent"), detail: codingAgentDetail) {
                            Toggle("", isOn: $acpEnabled)
                                .labelsHidden()
                                .toggleStyle(.switch)
                        }
                        VStack(alignment: .leading, spacing: 8) {
                            if !enabledProfiles.isEmpty {
                                Picker(L10n.text("Default Coding Agent"), selection: $defaultProfile) {
                                    ForEach(enabledProfiles, id: \.id) { profile in
                                        Text(profileTitle(profile)).tag(profile.id)
                                    }
                                }
                                .frame(width: 260, alignment: .leading)
                                .frame(maxWidth: .infinity, alignment: .leading)
                            }

                            VStack(alignment: .leading, spacing: 8) {
                                ForEach(profiles.indices, id: \.self) { index in
                                    HStack(spacing: 0) {
                                        Toggle("", isOn: $profiles[index].enabled)
                                            .labelsHidden()
                                            .toggleStyle(.checkbox)
                                            .frame(width: 18, alignment: .leading)
                                        Text(profileTitle(profiles[index]))
                                            .padding(.leading, 22)
                                            .frame(maxWidth: .infinity, alignment: .leading)
                                    }
                                    .frame(maxWidth: .infinity, alignment: .leading)
                                }
                            }
                            .frame(width: 260, alignment: .leading)
                            .frame(maxWidth: .infinity, alignment: .leading)

                            DisclosureGroup(L10n.text("Add custom Coding Agent")) {
                                VStack(alignment: .leading, spacing: 8) {
                                    TextField(L10n.text("Name"), text: $customName).textFieldStyle(.roundedBorder)
                                    TextField(L10n.text("Command"), text: $customCommand).textFieldStyle(.roundedBorder)
                                    TextField(L10n.text("Args JSON"), text: $customArguments).textFieldStyle(.roundedBorder)
                                    Button(L10n.text("Add")) { addCustomProfile() }
                                        .disabled(customName.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                                        .frame(maxWidth: .infinity, alignment: .trailing)
                                }.padding(.top, 8)
                            }
                            .frame(width: 260, alignment: .leading)
                            .frame(maxWidth: .infinity, alignment: .leading)
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.horizontal, 13)
                        .padding(.bottom, 12)
                    }

                    SettingsCard {
                        SettingsRow(L10n.text("MCP Apps"), detail: L10n.text("Interactive MCP app presentation")) {
                            Picker("", selection: $selectedMCPAppsMode) {
                                Text(L10n.text("Full")).tag(MCPAppsMode.full)
                                Text(L10n.text("Compact")).tag(MCPAppsMode.compact)
                                Text(L10n.text("Off")).tag(MCPAppsMode.off)
                            }.labelsHidden().frame(width: 120, alignment: .trailing)
                        }
                    }
                }

                Button(L10n.text("Save and restart")) {
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
                }
                .disabled(model.isBusy)
                .frame(maxWidth: .infinity, alignment: .trailing)
                if let message = model.message { Text(message).font(.system(size: 12)).foregroundStyle(.secondary) }

            }
            .padding(.horizontal, 28).padding(.top, 24).padding(.bottom, 32)
            .frame(maxWidth: 760, alignment: .leading)
        }
        .task(id: model.statusUpdatedAt) {
            loadConfiguration()
        }
        .onChange(of: profiles.map(\.enabled)) { _ in
            if !enabledProfiles.contains(where: { $0.id == defaultProfile }) {
                defaultProfile = enabledProfiles.first?.id ?? ""
            }
        }
    }

    private var enabledProfiles: [ACPProfileConfiguration] { profiles.filter(\.enabled) }
    private var codingAgentDetail: String {
        let enabledCount = profiles.filter(\.enabled).count
        guard enabledCount > 0 else { return L10n.text("No profiles configured") }
        let defaultTitle = profiles.first(where: { $0.id == defaultProfile }).map(profileTitle) ?? defaultProfile
        return L10n.format("%d enabled · default %@", enabledCount, defaultTitle)
    }
    private func profileTitle(_ profile: ACPProfileConfiguration) -> String {
        if let displayName = profile.displayName, !displayName.isEmpty {
            return displayName
        }
        if profile.kind != .custom {
            return profile.kind.title
        }
        return profile.id
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

private enum RuntimeActivityFormat {
    private static let fractionalDateFormatter: ISO8601DateFormatter = {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return formatter
    }()
    private static let basicDateFormatter = ISO8601DateFormatter()

    static func duration(_ milliseconds: Double) -> String {
        if milliseconds < 1_000 { return String(format: "%.0f ms", milliseconds) }
        if milliseconds < 60_000 { return String(format: "%.2f s", milliseconds / 1_000) }
        return String(format: "%.1f min", milliseconds / 60_000)
    }

    static func bytes(_ value: UInt64) -> String {
        ByteCountFormatter.string(fromByteCount: Int64(value), countStyle: .memory)
    }

    static func uptime(_ milliseconds: Int64) -> String {
        let seconds = max(0, milliseconds / 1_000)
        if seconds < 60 { return "\(seconds) s" }
        if seconds < 3_600 { return "\(seconds / 60) min" }
        if seconds < 86_400 { return "\(seconds / 3_600) h" }
        return "\(seconds / 86_400) d"
    }

    static func source(_ value: String) -> String {
        switch value.lowercased() {
        case "nexus": return L10n.text("Remote")
        case "internal": return L10n.text("Local")
        case "mcp": return "MCP"
        default: return value
        }
    }

    static func relativeTime(_ value: String) -> String {
        guard let date = fractionalDateFormatter.date(from: value) ?? basicDateFormatter.date(from: value) else { return value }
        let seconds = max(0, Int(Date().timeIntervalSince(date)))
        if seconds < 60 { return L10n.text("Just now") }
        if seconds < 3_600 { return L10n.format("%d min ago", seconds / 60) }
        return L10n.format("%d hr ago", seconds / 3_600)
    }

    static func stage(_ name: String) -> String {
        switch name {
        case "mcp.refresh": return L10n.text("MCP initialize & discover")
        case "mcp.remote_call": return L10n.text("MCP remote call")
        case "command.start": return L10n.text("Command start")
        case "command.foreground_wait": return L10n.text("Foreground wait")
        default: return name
        }
    }
}

private struct RuntimeActivityCallRow: View {
    let call: RuntimeAnalyticsCall
    @State private var isExpanded = false

    private var hasDetails: Bool {
        !(call.errorCode?.isEmpty ?? true) || !call.stages.isEmpty
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if hasDetails {
                Button {
                    withAnimation(.easeInOut(duration: 0.16)) {
                        isExpanded.toggle()
                    }
                } label: {
                    callLabel
                }
                .buttonStyle(.plain)
            } else {
                callLabel
            }

            if hasDetails && isExpanded {
                callDetails
                    .padding(.top, 8)
                    .transition(.opacity.combined(with: .move(edge: .top)))
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, 13)
        .padding(.vertical, 8)
    }

    private var callLabel: some View {
        HStack(spacing: 16) {
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 6) {
                    Text(call.tool)
                        .font(.system(size: 13, weight: .medium))
                    if hasDetails {
                        Image(systemName: isExpanded ? "chevron.down" : "chevron.right")
                            .font(.system(size: 9.5, weight: .semibold))
                            .foregroundStyle(.secondary)
                    }
                }
                Text("\(RuntimeActivityFormat.source(call.source)) · \(RuntimeActivityFormat.relativeTime(call.startedAt))")
                    .font(.system(size: 11.5))
                    .foregroundStyle(.secondary)
            }
            Spacer(minLength: 16)
            Text(RuntimeActivityFormat.duration(call.durationMS))
                .font(.system(size: 11.5, design: .monospaced))
                .foregroundStyle(.secondary)
            HStack(spacing: 6) {
                Circle().fill(call.success ? Color.green : Color.orange).frame(width: 7, height: 7)
                Text(call.success ? L10n.text("Succeeded") : L10n.text("Failed"))
                    .font(.system(size: 12, weight: .medium))
            }
        }
        .contentShape(Rectangle())
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var callDetails: some View {
        VStack(alignment: .leading, spacing: 8) {
            if let errorCode = call.errorCode, !errorCode.isEmpty {
                Text("\(L10n.text("Error code")): \(errorCode)")
                    .font(.system(size: 11.5))
                    .foregroundStyle(.secondary)
            }
            ForEach(call.stages) { stage in
                HStack(spacing: 10) {
                    Text(RuntimeActivityFormat.stage(stage.name))
                    Spacer()
                    Text(RuntimeActivityFormat.duration(stage.durationMS))
                        .foregroundStyle(.secondary)
                    Image(systemName: stage.success ? "checkmark.circle.fill" : "exclamationmark.circle.fill")
                        .foregroundStyle(stage.success ? Color.green : Color.orange)
                }
                .font(.system(size: 11.5))
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

private struct ActivityView: View {
    private static let pageSize = 20
    private static let refreshIntervalNanoseconds: UInt64 = 5_000_000_000

    @ObservedObject var model: ControlPanelModel
    @State private var analytics: RuntimeAnalyticsPayload?
    @State private var visibleCallCount = pageSize

    private var running: Bool { model.status.loaded && model.status.healthy }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 22) {
                PageHeader(
                    title: L10n.text("Activity"),
                    detail: L10n.text("Recent tool calls and execution details.")
                )

                SettingsSection(L10n.text("Recent calls")) {
                    Text(
                        L10n.text(
                            "Only tool name, source, latency, stages, status, and error code are retained. Arguments, results, commands, and file contents are never stored."
                        )
                    )
                    .font(.system(size: 11.5))
                    .foregroundStyle(.secondary)
                    .padding(.horizontal, 13)
                    .padding(.vertical, 10)

                    if let analytics {
                        let visibleCalls = Array(analytics.recentCalls.prefix(visibleCallCount))
                        if !visibleCalls.isEmpty { RowDivider() }
                        ForEach(Array(visibleCalls.enumerated()), id: \.element.id) { index, call in
                            if index > 0 { RowDivider() }
                            RuntimeActivityCallRow(call: call)
                        }
                        if analytics.recentCalls.isEmpty {
                            emptyAnalytics(L10n.text("No call data yet."))
                        } else if visibleCallCount < analytics.recentCalls.count {
                            RowDivider()
                            Button(L10n.text("Show more")) {
                                visibleCallCount += Self.pageSize
                            }
                            .buttonStyle(.plain)
                            .font(.system(size: 12.5, weight: .medium))
                            .foregroundStyle(.tint)
                            .padding(.horizontal, 13)
                            .padding(.vertical, 12)
                            .frame(maxWidth: .infinity, alignment: .leading)
                        }
                    } else {
                        RowDivider()
                        emptyAnalytics(
                            running
                                ? L10n.text("Runtime analytics are unavailable.")
                                : L10n.text("Start AgentDock to view runtime activity.")
                        )
                    }
                }
            }
            .padding(.horizontal, 28)
            .padding(.top, 24)
            .padding(.bottom, 32)
            .frame(maxWidth: 760, alignment: .leading)
        }
        .task {
            while !Task.isCancelled {
                if running {
                    let nextAnalytics = await model.service.runtimeAnalytics(configuration: model.status.configuration)
                    let currentLatestID = analytics?.recentCalls.first?.id
                    let nextLatestID = nextAnalytics?.recentCalls.first?.id
                    if analytics == nil || nextLatestID != currentLatestID {
                        analytics = nextAnalytics
                    }
                } else if analytics != nil {
                    analytics = nil
                }

                try? await Task.sleep(nanoseconds: Self.refreshIntervalNanoseconds)
            }
        }
    }

    private func emptyAnalytics(_ text: String) -> some View {
        Text(text)
            .font(.system(size: 12.5))
            .foregroundStyle(.secondary)
            .padding(.horizontal, 13)
            .padding(.vertical, 14)
            .frame(maxWidth: .infinity, alignment: .leading)
    }
}

private struct SettingsView: View {
    @ObservedObject var model: ControlPanelModel
    @State private var port = 8765
    @State private var logLevel = "info"
    @State private var analytics: RuntimeAnalyticsPayload?
    @State private var showAdvancedDiagnostics = false
    @State private var showCustomPort = false
    @State private var languagePreference: UILanguagePreference = .system
    @State private var themePreference: UIThemePreference = .system
    @State private var coreAutostart = false
    @State private var menuAutostart = false
    @State private var tunnelMode: TunnelMode = .local
    @State private var serverURL = ""
    @State private var tunnelToken = ""
    @State private var publicEndpointCheckResult: PublicEndpointCheckResult?
    @State private var testedPublicMCPURL: URL?
    @State private var isTestingPublicEndpoint = false

    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            PageHeader(title: L10n.text("Settings"))
                .padding(.horizontal, 28).padding(.top, 24)

            HStack(alignment: .top, spacing: 0) {
                VStack(alignment: .leading, spacing: 4) {
                    ForEach(ControlPanelModel.SettingsPage.allCases) { item in
                        Button {
                            model.settingsPage = item
                        } label: {
                            Text(item.title)
                                .font(.system(size: 13, weight: model.settingsPage == item ? .medium : .regular))
                                .frame(maxWidth: .infinity, alignment: .leading)
                                .padding(.leading, 18)
                                .padding(.trailing, 10)
                                .frame(height: 32)
                                .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                        .foregroundStyle(.primary)
                        .background(
                            RoundedRectangle(cornerRadius: 7, style: .continuous)
                                .fill(
                                    model.settingsPage == item
                                        ? Color.accentColor.opacity(0.14)
                                        : Color.clear
                                )
                        )
                    }
                    Spacer(minLength: 0)
                }
                .id(model.languageRevision)
                .padding(.horizontal, 10)
                .padding(.top, 4)
                .frame(width: 190, alignment: .topLeading)
                .frame(maxHeight: .infinity, alignment: .topLeading)

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
            tunnelMode = (try? model.service.configuredTunnelMode()) ?? .local
            let savedNamedOrigin = model.service.configuredNamedTunnelOrigin()
            if !savedNamedOrigin.isEmpty {
                serverURL = savedNamedOrigin
            } else if tunnelMode == .named {
                serverURL = model.status.configuration?.publicURL ?? ""
            }
            languagePreference = L10n.languagePreference()
            themePreference = AppAppearance.preference()
            coreAutostart = model.status.autostartEnabled
            menuAutostart = model.menuLoginAgent.isEnabled
        }
        .task(id: model.settingsPage) {
            if model.settingsPage == .advancedConnection {
                await model.refreshCloudflaredComponent()
                return
            }
            guard model.settingsPage == .logs else { return }
            analytics = model.status.loaded && model.status.healthy
                ? await model.service.runtimeAnalytics(configuration: model.status.configuration)
                : nil
        }
    }

    @ViewBuilder private var settingsContent: some View {
        switch model.settingsPage {
        case .logs:
            VStack(alignment: .leading, spacing: 20) {
                PageHeader(
                    title: L10n.text("Logs"),
                    detail: L10n.text("View logs and diagnostics for troubleshooting.")
                )
                SettingsSection(L10n.text("Logs")) {
                    SettingsRow(L10n.text("Log level")) {
                        Picker("", selection: $logLevel) {
                            ForEach(["debug", "info", "warn", "error"], id: \.self) { value in
                                Text(logLevelTitle(value)).tag(value)
                            }
                        }
                        .labelsHidden()
                        .frame(width: 120, alignment: .trailing)
                        .onChange(of: logLevel) { value in
                            guard value != model.status.configuration?.logLevel else { return }
                            applyLogLevel(value)
                        }
                    }
                    RowDivider()
                    SettingsRow(L10n.text("Logs directory")) {
                        Button(L10n.text("Open")) { model.openLogs() }.controlSize(.small)
                    }
                    RowDivider()
                    SettingsRow(L10n.text("Configuration directory")) {
                        Button(L10n.text("Open")) { model.openConfiguration() }.controlSize(.small)
                    }
                }

                SettingsSection(L10n.text("Diagnostics")) {
                    DisclosureGroup(isExpanded: $showAdvancedDiagnostics) {
                        VStack(spacing: 0) {
                            SettingsRow(L10n.text("Runtime")) {
                                Text(model.status.loaded && model.status.healthy ? L10n.text("Running") : L10n.text("Stopped"))
                                    .foregroundStyle(.secondary)
                            }
                            RowDivider()
                            SettingsRow(L10n.text("Version")) {
                                Text(model.status.version ?? "—").foregroundStyle(.secondary)
                            }
                            if let analytics {
                                RowDivider()
                                SettingsRow(L10n.text("Total calls")) {
                                    Text("\(analytics.totalCalls)").foregroundStyle(.secondary)
                                }
                                RowDivider()
                                SettingsRow(L10n.text("Failed calls")) {
                                    Text("\(analytics.totalErrors)").foregroundStyle(.secondary)
                                }
                                RowDivider()
                                SettingsRow(L10n.text("Recent P95 latency")) {
                                    Text(RuntimeActivityFormat.duration(recentP95(analytics)))
                                        .font(.system(size: 11.5, design: .monospaced))
                                        .foregroundStyle(.secondary)
                                }
                                RowDivider()
                                SettingsRow(L10n.text("Go heap")) {
                                    Text(RuntimeActivityFormat.bytes(analytics.process.heapAllocBytes)).foregroundStyle(.secondary)
                                }
                                RowDivider()
                                SettingsRow(L10n.text("Goroutines")) {
                                    Text("\(analytics.process.goroutines)").foregroundStyle(.secondary)
                                }
                                RowDivider()
                                SettingsRow(L10n.text("GC cycles")) {
                                    Text("\(analytics.process.gcCycles)").foregroundStyle(.secondary)
                                }
                                RowDivider()
                                SettingsRow(L10n.text("Uptime")) {
                                    Text(RuntimeActivityFormat.uptime(analytics.process.uptimeMS)).foregroundStyle(.secondary)
                                }
                                RowDivider()
                                SettingsRow(L10n.text("Diagnostic information")) {
                                    Button(L10n.text("Copy diagnostics")) { copyDiagnostics(analytics) }
                                        .controlSize(.small)
                                }
                            } else {
                                RowDivider()
                                Text(L10n.text("Runtime analytics are unavailable."))
                                    .font(.system(size: 12.5))
                                    .foregroundStyle(.secondary)
                                    .padding(.horizontal, 13)
                                    .padding(.vertical, 12)
                                    .frame(maxWidth: .infinity, alignment: .leading)
                            }
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.top, 8)
                    } label: {
                        Text(L10n.text("Advanced diagnostics"))
                            .font(.system(size: 13, weight: .medium))
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, 13)
                    .padding(.vertical, 12)
                }

                if let message = model.message {
                    Text(message).font(.system(size: 12)).foregroundStyle(.secondary)
                }
            }
        case .permissions:
            VStack(alignment: .leading, spacing: 20) {
                PageHeader(title: L10n.text("Permissions"), detail: L10n.text("Review macOS permissions required by enabled capabilities."))
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
                PageHeader(title: L10n.text("Startup"), detail: L10n.text("Background service and menu bar startup behavior."))
                SettingsSection(L10n.text("Startup")) {
                    SettingsRow(L10n.text("Core background service")) {
                        Toggle("", isOn: $coreAutostart)
                            .labelsHidden()
                            .toggleStyle(.switch)
                            .onChange(of: coreAutostart) { value in Task { await model.setCoreAutostart(value) } }
                    }
                    RowDivider()
                    SettingsRow(L10n.text("Menu bar app")) {
                        Toggle("", isOn: $menuAutostart)
                            .labelsHidden()
                            .toggleStyle(.switch)
                            .onChange(of: menuAutostart) { value in Task { await model.setMenuAutostart(value) } }
                    }
                }
            }

        case .appearance:
            VStack(alignment: .leading, spacing: 20) {
                PageHeader(
                    title: L10n.text("Appearance"),
                    detail: L10n.text("Customize the theme and interface language.")
                )
                SettingsSection(L10n.text("Appearance")) {
                    SettingsRow(L10n.text("Theme")) {
                        Picker("", selection: $themePreference) {
                            ForEach(UIThemePreference.allCases, id: \.rawValue) { preference in
                                Text(preference.title).tag(preference)
                            }
                        }
                        .labelsHidden()
                        .frame(width: 150, alignment: .trailing)
                        .onChange(of: themePreference) { preference in
                            model.setThemePreference(preference)
                        }
                    }
                    RowDivider()
                    SettingsRow(L10n.text("Interface language")) {
                        Picker("", selection: $languagePreference) {
                            ForEach(UILanguagePreference.allCases, id: \.rawValue) { preference in
                                Text(preference.title).tag(preference)
                            }
                        }
                        .labelsHidden()
                        .frame(width: 150, alignment: .trailing)
                        .onChange(of: languagePreference) { preference in
                            model.setLanguagePreference(preference)
                        }
                    }
                }
            }
        case .about:
            VStack(alignment: .leading, spacing: 20) {
                PageHeader(
                    title: L10n.text("About"),
                    detail: L10n.text("About AgentDock and useful project links.")
                )

                VStack(alignment: .leading, spacing: 8) {
                    AgentDockLogoView(size: 40)
                    Text("AgentDock")
                        .font(.system(size: 22, weight: .semibold))
                    Text(L10n.text("AgentDock lets AI clients securely use capabilities on this device."))
                        .font(.system(size: 13))
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
                .padding(.vertical, 4)

                SettingsSection(L10n.text("Application")) {
                    SettingsRow(L10n.text("Version")) {
                        Text(AppVersion.current)
                            .font(.system(size: 12))
                            .foregroundStyle(.secondary)
                    }
                    RowDivider()
                    SettingsRow(L10n.text("AgentDock update")) {
                        Button(L10n.text("Check for updates")) {
                            model.requestUpdate()
                        }
                        .controlSize(.small)
                    }
                }

                SettingsSection(L10n.text("Resources")) {
                    SettingsRow(L10n.text("Website")) {
                        Link(
                            L10n.text("Open"),
                            destination: URL(string: "https://nexusdock.co/")!
                        )
                        .controlSize(.small)
                    }
                    RowDivider()
                    SettingsRow(L10n.text("Documentation")) {
                        Link(
                            L10n.text("Open"),
                            destination: URL(string: "https://docs.nexusdock.co/agentdock/")!
                        )
                        .controlSize(.small)
                    }
                    RowDivider()
                    SettingsRow("GitHub") {
                        Link(
                            L10n.text("Open"),
                            destination: URL(string: "https://github.com/uvwt/agentdock")!
                        )
                        .controlSize(.small)
                    }
                }
            }

        case .advancedConnection:
            VStack(alignment: .leading, spacing: 20) {
                PageHeader(
                    title: L10n.text("Advanced connection"),
                    detail: L10n.text("Local MCP, access credentials, and direct public access.")
                )

                SettingsSection("Local MCP") {
                    SettingsRow(
                        L10n.text("Local address"),
                        detail: model.status.configuration?.localMCPURL?.absoluteString ?? "—"
                    ) {
                        Button(L10n.text("Copy")) {
                            copy(model.status.configuration?.localMCPURL?.absoluteString)
                        }
                        .controlSize(.small)
                    }
                    RowDivider()
                    DisclosureGroup(isExpanded: $showCustomPort) {
                        VStack(spacing: 0) {
                            SettingsRow(L10n.text("Service port")) {
                                TextField("", value: $port, format: .number)
                                    .textFieldStyle(.roundedBorder)
                                    .frame(width: 100)
                            }
                            RowDivider()
                            HStack(spacing: 16) {
                                Text(L10n.text("Changing the service port will restart AgentDock."))
                                    .font(.system(size: 11.5))
                                    .foregroundStyle(.secondary)
                                Spacer()
                                Button(L10n.text("Apply changes")) {
                                    guard let configuration = model.status.configuration else { return }
                                    let settings = EditableServiceSettings(
                                        port: port,
                                        logLevel: configuration.logLevel,
                                        mcpAppsMode: configuration.mcpAppsMode,
                                        browserEnabled: configuration.browserEnabled,
                                        browserCDPURL: configuration.browserCDPURL,
                                        browserReuseExistingCDP: configuration.browserReuseExistingCDP,
                                        acpEnabled: configuration.acpEnabled,
                                        acpProfiles: configuration.acpProfiles,
                                        acpDefaultProfile: configuration.acpDefaultProfile
                                    )
                                    Task { await model.applySettings(settings) }
                                }
                                .controlSize(.small)
                                .disabled(model.isBusy || port == model.status.configuration?.port)
                            }
                            .padding(.horizontal, 13)
                            .frame(minHeight: 48)
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.top, 8)
                    } label: {
                        Text(L10n.text("Custom port"))
                            .font(.system(size: 13, weight: .medium))
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, 13)
                    .padding(.vertical, 12)
                }

                SettingsSection(L10n.text("Access Credentials")) {
                    SettingsRow(
                        L10n.text("Authentication token"),
                        detail: displayedSecret(model.status.configuration?.authToken)
                    ) {
                        Button(L10n.text("Copy")) {
                            copy(model.status.configuration?.authToken)
                        }
                        .controlSize(.small)
                    }
                    RowDivider()
                    SettingsRow(
                        L10n.text("OAuth password"),
                        detail: displayedSecret(model.status.configuration?.oauthPassword)
                    ) {
                        Button(L10n.text("Copy")) {
                            copy(model.status.configuration?.oauthPassword)
                        }
                        .controlSize(.small)
                    }
                }

                SettingsSection(L10n.text("Cloudflare Tunnel")) {
                    SettingsRow(
                        L10n.text("Optional component"),
                        detail: cloudflaredComponentDetail
                    ) {
                        HStack(spacing: 8) {
                            if model.cloudflaredComponent.ready {
                                Button(L10n.text("Update")) {
                                    Task { await model.runCloudflaredComponentAction("update") }
                                }
                                .controlSize(.small)
                                Button(L10n.text("Uninstall")) {
                                    Task { await model.runCloudflaredComponentAction("uninstall") }
                                }
                                .controlSize(.small)
                            } else {
                                Button(
                                    model.cloudflaredComponent.state == "broken"
                                        ? L10n.text("Repair")
                                        : L10n.text("Install")
                                ) {
                                    Task { await model.runCloudflaredComponentAction("install") }
                                }
                                .controlSize(.small)
                            }
                        }
                        .disabled(model.isBusy)
                    }
                    RowDivider()
                    Text(L10n.text("Used for self-hosted public access. NexusDock remote connections do not require this component."))
                        .font(.system(size: 11.5))
                        .foregroundStyle(.secondary)
                        .padding(.horizontal, 13)
                        .padding(.vertical, 11)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }

                if model.cloudflaredComponent.ready {
                SettingsSection(L10n.text("Public access")) {
                    SettingsRow(
                        L10n.text("Public address"),
                        detail: model.status.configuration?.publicMCPURL?.absoluteString ?? L10n.text("Disabled")
                    ) {
                        HStack(spacing: 8) {
                            if testedPublicMCPURL == model.status.configuration?.publicMCPURL,
                               let result = publicEndpointCheckResult {
                                Text(publicEndpointStatusText(result))
                                    .font(.system(size: 11.5, weight: .medium))
                                    .foregroundStyle(result.isReachable ? Color.green : Color.red)
                                    .help(result.message)
                            }

                            Button(isTestingPublicEndpoint ? L10n.text("Checking") : L10n.text("Test")) {
                                guard let publicMCPURL = model.status.configuration?.publicMCPURL else { return }
                                Task { await testPublicEndpoint(publicMCPURL) }
                            }
                            .controlSize(.small)
                            .disabled(model.status.configuration?.publicMCPURL == nil || isTestingPublicEndpoint)

                            Button(L10n.text("Copy")) {
                                copy(model.status.configuration?.publicMCPURL?.absoluteString)
                            }
                            .controlSize(.small)
                            .disabled(model.status.configuration?.publicMCPURL == nil)
                        }
                    }
                    RowDivider()
                    SettingsRow(
                        L10n.text("Temporary domain"),
                        detail: L10n.text("Generate a temporary public address without configuring a domain.")
                    ) {
                        Button(
                            tunnelMode == .quick
                                ? L10n.text("Regenerate temporary address")
                                : L10n.text("Generate temporary address")
                        ) {
                            Task {
                                await model.applyTunnel(mode: .quick, serverURL: "", tunnelToken: "")
                            }
                        }
                        .controlSize(.small)
                        .disabled(model.isBusy)
                    }
                    RowDivider()
                    VStack(alignment: .leading, spacing: 8) {
                        Text(L10n.text("Fixed domain"))
                            .font(.system(size: 13, weight: .medium))
                        Text(L10n.text("Use your own HTTPS domain with a Cloudflare Tunnel token."))
                            .font(.system(size: 12))
                            .foregroundStyle(.secondary)
                        TextField("https://mcp.example.com", text: $serverURL)
                            .textFieldStyle(.roundedBorder)
                        SecureField(L10n.text("Cloudflare Tunnel Token"), text: $tunnelToken)
                            .textFieldStyle(.roundedBorder)
                        Button(L10n.text("Apply fixed domain")) {
                            Task {
                                await model.applyTunnel(mode: .named, serverURL: serverURL, tunnelToken: tunnelToken)
                                tunnelToken = ""
                            }
                        }
                        .disabled(
                            model.isBusy
                                || serverURL.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
                        )
                    }
                    .padding(13)
                }
                }

                if let message = model.message {
                    Text(message)
                        .font(.system(size: 12))
                        .foregroundStyle(.secondary)

                }
            }
        }
    }

    private var cloudflaredComponentDetail: String {
        if model.cloudflaredComponent.ready {
            if let version = model.cloudflaredComponent.version, !version.isEmpty {
                return L10n.format("Ready · %@", version)
            }
            return L10n.text("Ready")
        }
        if model.cloudflaredComponent.state == "broken" {
            return L10n.text("Needs repair")
        }
        return L10n.text("Not installed")
    }

    private func logLevelTitle(_ value: String) -> String {
        switch value {
        case "debug": return L10n.text("Debug")
        case "warn": return L10n.text("Warning")
        case "error": return L10n.text("Error")
        default: return L10n.text("Info")
        }
    }

    private func applyLogLevel(_ value: String) {
        guard let configuration = model.status.configuration,
              value != configuration.logLevel else { return }
        let settings = EditableServiceSettings(
            port: configuration.port,
            logLevel: value,
            mcpAppsMode: configuration.mcpAppsMode,
            browserEnabled: configuration.browserEnabled,
            browserCDPURL: configuration.browserCDPURL,
            browserReuseExistingCDP: configuration.browserReuseExistingCDP,
            acpEnabled: configuration.acpEnabled,
            acpProfiles: configuration.acpProfiles,
            acpDefaultProfile: configuration.acpDefaultProfile
        )
        Task { await model.applySettings(settings) }
    }

    private func recentP95(_ analytics: RuntimeAnalyticsPayload) -> Double {
        let values = analytics.recentCalls.map(\.durationMS).sorted()
        guard !values.isEmpty else { return 0 }
        let index = max(0, min(values.count - 1, Int(ceil(Double(values.count) * 0.95)) - 1))
        return values[index]
    }

    private func copyDiagnostics(_ analytics: RuntimeAnalyticsPayload) {
        let lines = [
            "AgentDock \(model.status.version ?? "unknown")",
            "runtime=\(model.status.loaded && model.status.healthy ? "running" : "stopped")",
            "total_calls=\(analytics.totalCalls)",
            "total_errors=\(analytics.totalErrors)",
            "recent_p95=\(RuntimeActivityFormat.duration(recentP95(analytics)))",
            "goroutines=\(analytics.process.goroutines)",
            "heap_alloc=\(RuntimeActivityFormat.bytes(analytics.process.heapAllocBytes))",
            "gc_cycles=\(analytics.process.gcCycles)",
            "uptime=\(RuntimeActivityFormat.uptime(analytics.process.uptimeMS))"
        ]
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(lines.joined(separator: "\n"), forType: .string)
    }

    private func displayedSecret(_ value: String?) -> String {
        guard let value, !value.isEmpty else { return "—" }
        return "••••••••••••"
    }

    private func publicEndpointStatusText(_ result: PublicEndpointCheckResult) -> String {
        if result.isReachable {
            guard let latency = result.latencyMilliseconds else { return L10n.text("Reachable") }
            return "\(latency) ms"
        }

        if let statusCode = result.httpStatusCode {
            let failed = L10n.text("Failed")
            if let latency = result.latencyMilliseconds {
                return "HTTP \(statusCode) · \(failed) · \(latency) ms"
            }
            return "HTTP \(statusCode) · \(failed)"
        }

        return L10n.text("Failed")
    }

    private func testPublicEndpoint(_ publicMCPURL: URL) async {
        guard !isTestingPublicEndpoint else { return }
        isTestingPublicEndpoint = true
        publicEndpointCheckResult = nil
        testedPublicMCPURL = nil

        let result = await PublicEndpointChecker().check(publicMCPURL: publicMCPURL)
        guard model.status.configuration?.publicMCPURL == publicMCPURL else {
            isTestingPublicEndpoint = false
            return
        }

        publicEndpointCheckResult = result
        testedPublicMCPURL = publicMCPURL
        isTestingPublicEndpoint = false
    }

    private func copy(_ value: String?) {
        guard let value, !value.isEmpty else { return }
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(value, forType: .string)
    }
}

private extension Notification.Name {
    static let agentDockOpenPermissions = Notification.Name("AgentDockOpenPermissions")
}
