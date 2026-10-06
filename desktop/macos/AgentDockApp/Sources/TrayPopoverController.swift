import AppKit
import SwiftUI

@MainActor
final class TrayPopoverController {
    private let model = TrayPopoverModel()
    private let popover = NSPopover()

    init(
        onOpenAgentDock: @escaping () -> Void,
        onOpenSettings: @escaping () -> Void,
        onRunServiceAction: @escaping (String) -> Void,
        onShowUpdateProgress: @escaping () -> Void
    ) {
        popover.behavior = .transient
        popover.animates = true
        popover.contentSize = NSSize(width: 350, height: 292)
        popover.contentViewController = NSHostingController(
            rootView: TrayPopoverView(
                model: model,
                onOpenAgentDock: onOpenAgentDock,
                onOpenSettings: onOpenSettings,
                onRunServiceAction: onRunServiceAction,
                onShowUpdateProgress: onShowUpdateProgress
            )
        )
    }

    func update(status: ServiceStatus, isUpdating: Bool, isCheckingForUpdate: Bool) {
        model.status = status
        model.isUpdating = isUpdating
        model.isCheckingForUpdate = isCheckingForUpdate
    }

    func toggle(relativeTo button: NSStatusBarButton) {
        if popover.isShown {
            popover.performClose(nil)
            return
        }
        show(relativeTo: button)
    }

    func show(relativeTo button: NSStatusBarButton) {
        guard !popover.isShown else { return }
        popover.show(relativeTo: button.bounds, of: button, preferredEdge: .minY)
    }

    func close() {
        popover.performClose(nil)
    }
}

@MainActor
private final class TrayPopoverModel: ObservableObject {
    @Published var status: ServiceStatus = .missing
    @Published var isUpdating = false
    @Published var isCheckingForUpdate = false
}

private struct TrayPopoverView: View {
    @ObservedObject var model: TrayPopoverModel
    let onOpenAgentDock: () -> Void
    let onOpenSettings: () -> Void
    let onRunServiceAction: (String) -> Void
    let onShowUpdateProgress: () -> Void

    private var statusTitle: String {
        if model.isCheckingForUpdate {
            return L10n.text("Checking for updates…")
        }
        if model.isUpdating {
            return L10n.text("Updating…")
        }
        if !model.status.installed { return L10n.text("Not installed") }
        if model.status.healthy { return L10n.text("Running normally") }
        if model.status.requiresApproval { return L10n.text("Background permission required") }
        if model.status.loaded { return L10n.text("Service error") }
        return L10n.text("Stopped")
    }

    private var statusColor: Color {
        if model.isUpdating || model.isCheckingForUpdate { return .accentColor }
        if model.status.healthy { return .green }
        if model.status.loaded || model.status.requiresApproval { return .orange }
        return .secondary
    }

    private var subtitle: String {
        if model.isCheckingForUpdate {
            return L10n.text("Checking for updates…")
        }
        if model.isUpdating {
            return L10n.text("Updating AgentDock…")
        }
        if model.status.healthy {
            return L10n.text("AgentDock is running")
        }
        if model.status.loaded {
            return L10n.text("AgentDock needs attention")
        }
        return model.status.installed
            ? L10n.text("AgentDock is stopped")
            : L10n.text("Set up AgentDock to get started.")
    }

    private var coreText: String {
        if model.status.healthy { return L10n.text("Running") }
        if model.status.loaded { return L10n.text("Unavailable") }
        return L10n.text("Stopped")
    }

    private var nexusText: String {
        switch model.status.nexusConnection {
        case .connected: return L10n.text("Connected")
        case .disconnected: return L10n.text("Not connected")
        case .configurationError: return L10n.text("Unavailable")
        case .unconfigured: return L10n.text("Not configured")
        }
    }

    private var serviceAction: String {
        model.status.loaded ? "restart" : "start"
    }

    private var serviceActionTitle: String {
        model.status.loaded
            ? L10n.text("Restart AgentDock")
            : L10n.text("Start AgentDock")
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack(spacing: 11) {
                AgentDockLogoView(size: 38)
                VStack(alignment: .leading, spacing: 2) {
                    Text("AgentDock")
                        .font(.system(size: 16.5, weight: .semibold))
                    Text(subtitle)
                        .font(.system(size: 11.5))
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
                Spacer(minLength: 8)
                HStack(spacing: 5) {
                    Circle()
                        .fill(statusColor)
                        .frame(width: 6, height: 6)
                    Text(statusTitle)
                        .font(.system(size: 11, weight: .semibold))
                }
                .padding(.horizontal, 8)
                .padding(.vertical, 5)
                .background(statusColor.opacity(0.12), in: Capsule())
            }

            VStack(spacing: 0) {
                statusRow(symbol: "cpu", title: L10n.text("Core service"), value: coreText)
                    .padding(.horizontal, 12)
                    .padding(.vertical, 10)
                Divider()
                    .padding(.leading, 36)
                statusRow(symbol: "point.3.connected.trianglepath.dotted", title: "NexusDock", value: nexusText)
                    .padding(.horizontal, 12)
                    .padding(.vertical, 10)
            }
            .background(
                RoundedRectangle(cornerRadius: 12, style: .continuous)
                    .fill(Color(nsColor: .controlBackgroundColor))
            )
            .overlay(
                RoundedRectangle(cornerRadius: 12, style: .continuous)
                    .stroke(Color(nsColor: .separatorColor).opacity(0.6), lineWidth: 0.5)
            )

            Button {
                if model.isUpdating {
                    onShowUpdateProgress()
                } else {
                    onOpenAgentDock()
                }
            } label: {
                Text(model.isUpdating
                     ? L10n.text("Show update progress")
                     : model.status.installed ? L10n.text("Open AgentDock") : L10n.text("Set up AgentDock…"))
                    .frame(maxWidth: .infinity)
            }
            .buttonStyle(.borderedProminent)
            .controlSize(.large)

            HStack(spacing: 8) {
                Button(serviceActionTitle) {
                    onRunServiceAction(serviceAction)
                }
                .frame(maxWidth: .infinity)
                .disabled(
                    model.isUpdating ||
                    !model.status.installed ||
                    model.status.requiresApproval
                )

                Button(L10n.text("Settings")) {
                    onOpenSettings()
                }
                .frame(maxWidth: .infinity)
                .disabled(model.isUpdating)
            }
            .controlSize(.regular)

            if let version = model.status.version, !version.isEmpty {
                Text("AgentDock \(version)")
                    .font(.system(size: 10.5))
                    .foregroundStyle(.tertiary)
                    .frame(maxWidth: .infinity)
            }
        }
        .padding(16)
        .frame(width: 350)
    }

    private func statusRow(symbol: String, title: String, value: String) -> some View {
        HStack(spacing: 10) {
            Image(systemName: symbol)
                .font(.system(size: 13))
                .foregroundStyle(.secondary)
                .frame(width: 14)
            Text(title)
                .font(.system(size: 12.5))
            Spacer()
            Text(value)
                .font(.system(size: 11.5, weight: .medium))
                .foregroundStyle(.secondary)
        }
    }
}
