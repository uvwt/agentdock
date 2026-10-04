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

    private var configuration: ServiceConfiguration? { model.status.configuration }
    private var nexusDevice: NexusDeviceStatus { model.nexusDevice }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 22) {
                PageHeader(
                    title: L10n.text("Connections"),
                    detail: L10n.text("Connect AI clients to this device.")
                )

                SettingsSection(L10n.text("Local connection")) {
                    SettingsRow("Local MCP", detail: configuration?.localMCPURL?.absoluteString ?? "—") {
                        Button(L10n.text("Copy")) {
                            copy(configuration?.localMCPURL?.absoluteString)
                        }
                        .controlSize(.small)
                        .disabled(configuration?.localMCPURL == nil)
                    }
                    RowDivider()
                    SettingsRow("Public MCP", detail: configuration?.publicMCPURL?.absoluteString ?? L10n.text("Disabled")) {
                        Button(L10n.text("Copy")) {
                            copy(configuration?.publicMCPURL?.absoluteString)
                        }
                        .controlSize(.small)
                        .disabled(configuration?.publicMCPURL == nil)
                    }
                }

                SettingsSection(L10n.text("Remote connection")) {
                    SettingsRow(
                        "NexusDock",
                        detail: nexusDevice.paired ? nexusDevice.endpoint : L10n.text("Not configured")
                    ) {
                        StatusPill(text: nexusText, active: nexusActive)
                    }
                }
            }
            .padding(.horizontal, 28)
            .padding(.top, 24)
            .padding(.bottom, 32)
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

    private var configuration: ServiceConfiguration? { model.status.configuration }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 22) {
                PageHeader(
                    title: L10n.text("Capabilities"),
                    detail: L10n.text("Configure the capabilities AgentDock provides to AI.")
                )

                SettingsSection(L10n.text("Available capabilities")) {
                    SettingsRow(L10n.text("Browser"), detail: browserDetail) {
                        StatusPill(
                            text: capabilityState(configuration?.browserEnabled),
                            active: configuration?.browserEnabled == true
                        )
                    }
                    RowDivider()
                    SettingsRow("Coding Agent", detail: codingAgentDetail) {
                        StatusPill(
                            text: capabilityState(configuration?.acpEnabled),
                            active: configuration?.acpEnabled == true
                        )
                    }
                    RowDivider()
                    SettingsRow("MCP Apps", detail: L10n.text("Interactive MCP app presentation")) {
                        Text(mcpAppsMode)
                            .font(.system(size: 12))
                            .foregroundStyle(.secondary)
                    }
                }
            }
            .padding(.horizontal, 28)
            .padding(.top, 24)
            .padding(.bottom, 32)
            .frame(maxWidth: 760, alignment: .leading)
        }
    }

    private func capabilityState(_ enabled: Bool?) -> String {
        guard let enabled else { return L10n.text("Unavailable") }
        return enabled ? L10n.text("Enabled") : L10n.text("Disabled")
    }

    private var browserDetail: String {
        guard let configuration else { return L10n.text("Browser automation and web operations") }
        guard configuration.browserEnabled else { return L10n.text("Browser automation and web operations") }
        if configuration.browserReuseExistingCDP { return L10n.text("Prefer an existing local browser") }
        if !configuration.browserCDPURL.isEmpty { return L10n.text("Use the configured CDP browser") }
        return L10n.text("Use an isolated browser")
    }

    private var codingAgentDetail: String {
        guard let configuration else { return L10n.text("No profiles configured") }
        let enabledCount = configuration.acpProfiles.filter(\.enabled).count
        guard enabledCount > 0 else { return L10n.text("No profiles configured") }
        return L10n.format(
            "%d enabled · default %@",
            enabledCount,
            configuration.acpDefaultProfile
        )
    }

    private var mcpAppsMode: String {
        guard let mode = configuration?.mcpAppsMode else { return "—" }
        switch mode {
        case .full: return L10n.text("Full")
        case .compact: return L10n.text("Compact")
        case .off: return L10n.text("Off")
        }
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
                }
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
                    SettingsRow(L10n.text("Core background service")) { StatusPill(text: model.status.autostartEnabled ? L10n.text("Enabled") : L10n.text("Disabled"), active: model.status.autostartEnabled) }
                    RowDivider()
                    SettingsRow(L10n.text("Menu bar app")) { Text(L10n.text("Managed by macOS")).font(.system(size: 12)).foregroundStyle(.secondary) }
                }
            }
        case .credentials:
            VStack(alignment: .leading, spacing: 20) {
                PageHeader(title: "Access Credentials", detail: L10n.text("Credentials remain managed by the existing AgentDock configuration."))
                SettingsSection(L10n.text("Credentials")) {
                    SettingsRow(L10n.text("Authentication token")) { Text(L10n.text("Protected")).font(.system(size: 12)).foregroundStyle(.secondary) }
                    RowDivider()
                    SettingsRow(L10n.text("OAuth password")) { Text(L10n.text("Protected")).font(.system(size: 12)).foregroundStyle(.secondary) }
                }
            }
        }
    }
}

private extension Notification.Name {
    static let agentDockOpenPermissions = Notification.Name("AgentDockOpenPermissions")
}
