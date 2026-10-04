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

    func update(_ status: ServiceStatus) { self.status = status }

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
        case .connections:
            PlaceholderPage(title: L10n.text("Connections"), detail: L10n.text("Connect AI clients to this device."))
        case .capabilities:
            PlaceholderPage(title: L10n.text("Capabilities"), detail: L10n.text("Configure the capabilities AgentDock provides to AI."))
        case .activity:
            PlaceholderPage(title: L10n.text("Activity"), detail: L10n.text("Runtime status and recent activity appear here."))
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

private struct PlaceholderPage: View {
    let title: String
    let detail: String
    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            PageHeader(title: title, detail: detail)
            Divider()
            Text(L10n.text("This page will be completed after the Home and Settings visual direction is confirmed."))
                .font(.system(size: 13)).foregroundStyle(.secondary)
        }
        .padding(.horizontal, 28).padding(.top, 24)
        .frame(maxWidth: 760, alignment: .leading)
    }
}

private extension Notification.Name {
    static let agentDockOpenPermissions = Notification.Name("AgentDockOpenPermissions")
}
