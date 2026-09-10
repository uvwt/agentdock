import AppKit
import Foundation

@MainActor
final class DesktopPermissionsWindowController: NSWindowController {
    private let contentStack = TopAlignedStackView()
    private var statusLabels: [DesktopPermissionKind: NSTextField] = [:]
    private lazy var fileAccessWindow = FileAccessPermissionsWindowController()

    init() {
        let window = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: 660, height: 610),
            styleMask: [.titled, .closable, .miniaturizable, .resizable],
            backing: .buffered,
            defer: false
        )
        window.title = L10n.text("AgentDock Permission Check")
        window.isReleasedWhenClosed = false
        window.minSize = NSSize(width: 600, height: 520)
        window.center()
        super.init(window: window)
        configureUI()
    }

    required init?(coder: NSCoder) {
        fatalError("init(coder:) has not been implemented")
    }

    func present() {
        refresh()
        showWindow(nil)
        window?.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }

    private func configureUI() {
        guard let contentView = window?.contentView else { return }

        let scrollView = NSScrollView()
        scrollView.hasVerticalScroller = true
        scrollView.autohidesScrollers = true
        scrollView.drawsBackground = false
        scrollView.translatesAutoresizingMaskIntoConstraints = false
        contentView.addSubview(scrollView)

        contentStack.orientation = .vertical
        contentStack.alignment = .leading
        contentStack.spacing = 12
        contentStack.edgeInsets = NSEdgeInsets(top: 22, left: 26, bottom: 22, right: 26)
        contentStack.translatesAutoresizingMaskIntoConstraints = false
        scrollView.documentView = contentStack

        let title = NSTextField(labelWithString: L10n.text("System permissions"))
        title.font = .systemFont(ofSize: 22, weight: .semibold)
        let intro = PermissionUI.detailLabel(
            L10n.text("AgentDock checks permissions directly. Grant only the permissions required by the features you use; you do not need to enable everything at once.")
        )
        intro.widthAnchor.constraint(equalToConstant: 580).isActive = true
        contentStack.addArrangedSubview(title)
        contentStack.addArrangedSubview(intro)
        contentStack.addArrangedSubview(PermissionUI.separator())

        for kind in DesktopPermissionKind.allCases {
            contentStack.addArrangedSubview(makePermissionRow(kind))
            contentStack.addArrangedSubview(PermissionUI.separator())
        }

        let appManagementTitle = NSTextField(labelWithString: L10n.text("App Management"))
        appManagementTitle.font = .systemFont(ofSize: 13, weight: .medium)
        let appManagementDetail = PermissionUI.detailLabel(
            L10n.text("Used to update AgentDock or manage other applications.")
        )
        appManagementDetail.widthAnchor.constraint(equalToConstant: 380).isActive = true
        let appManagementText = NSStackView(views: [appManagementTitle, appManagementDetail])
        appManagementText.orientation = .vertical
        appManagementText.alignment = .leading
        appManagementText.spacing = 3
        let appManagementButton = NSButton(
            title: L10n.text("Open App Management settings"),
            target: self,
            action: #selector(openAppManagementSettings)
        )
        appManagementButton.bezelStyle = .rounded
        let appManagementRow = NSStackView(views: [appManagementText, NSView(), appManagementButton])
        appManagementRow.orientation = .horizontal
        appManagementRow.alignment = .centerY
        appManagementRow.spacing = 8
        appManagementRow.widthAnchor.constraint(equalToConstant: 580).isActive = true
        contentStack.addArrangedSubview(appManagementRow)
        contentStack.addArrangedSubview(PermissionUI.separator())

        let filesTitle = NSTextField(labelWithString: L10n.text("Files and Folders"))
        filesTitle.font = .systemFont(ofSize: 15, weight: .semibold)
        let filesDetail = PermissionUI.detailLabel(
            L10n.text("Check whether AgentDock can access Desktop, Documents, Downloads, and other folders you select.")
        )
        filesDetail.widthAnchor.constraint(equalToConstant: 580).isActive = true
        let filesButton = NSButton(title: L10n.text("Check file access…"), target: self, action: #selector(openFileAccess))
        filesButton.bezelStyle = .rounded
        let filesRow = NSStackView(views: [filesTitle, NSView(), filesButton])
        filesRow.orientation = .horizontal
        filesRow.alignment = .centerY
        filesRow.widthAnchor.constraint(equalToConstant: 580).isActive = true
        contentStack.addArrangedSubview(filesRow)
        contentStack.addArrangedSubview(filesDetail)

        let refreshButton = NSButton(title: L10n.text("Refresh"), target: self, action: #selector(refreshPressed))
        refreshButton.bezelStyle = .rounded
        let footer = NSStackView(views: [NSView(), refreshButton])
        footer.orientation = .horizontal
        footer.widthAnchor.constraint(equalToConstant: 580).isActive = true
        contentStack.addArrangedSubview(footer)

        NSLayoutConstraint.activate([
            scrollView.leadingAnchor.constraint(equalTo: contentView.leadingAnchor),
            scrollView.trailingAnchor.constraint(equalTo: contentView.trailingAnchor),
            scrollView.topAnchor.constraint(equalTo: contentView.topAnchor),
            scrollView.bottomAnchor.constraint(equalTo: contentView.bottomAnchor),
            contentStack.widthAnchor.constraint(equalTo: scrollView.contentView.widthAnchor),
        ])
    }

    private func makePermissionRow(_ kind: DesktopPermissionKind) -> NSView {
        let title = NSTextField(labelWithString: kind.title)
        title.font = .systemFont(ofSize: 13, weight: .medium)
        let detail = PermissionUI.detailLabel(kind.detail)
        detail.widthAnchor.constraint(equalToConstant: 330).isActive = true
        let text = NSStackView(views: [title, detail])
        text.orientation = .vertical
        text.alignment = .leading
        text.spacing = 3

        let status = PermissionUI.statusLabel()
        status.widthAnchor.constraint(equalToConstant: 72).isActive = true
        statusLabels[kind] = status

        let request = NSButton(title: L10n.text("Request access"), target: self, action: #selector(requestPermission(_:)))
        request.bezelStyle = .rounded
        request.tag = kind.rawValue
        let settings = NSButton(title: L10n.text("Open settings"), target: self, action: #selector(openPermissionSettings(_:)))
        settings.bezelStyle = .rounded
        settings.tag = kind.rawValue

        let row = NSStackView(views: [text, NSView(), status, request, settings])
        row.orientation = .horizontal
        row.alignment = .centerY
        row.spacing = 8
        row.widthAnchor.constraint(equalToConstant: 580).isActive = true
        return row
    }

    private func refresh() {
        let snapshot = DesktopPermissionChecker.snapshot()
        for kind in DesktopPermissionKind.allCases {
            let state = snapshot[kind]
            guard let label = statusLabels[kind] else { continue }
            label.stringValue = state.title
            switch state {
            case .granted:
                PermissionUI.applyColor(to: label, granted: true)
            case .notGranted:
                PermissionUI.applyColor(to: label, granted: false)
            case .notDetermined:
                PermissionUI.applyColor(to: label, granted: nil, attention: true)
            case .unavailable:
                PermissionUI.applyColor(to: label, granted: nil)
            }
        }
    }

    @objc private func refreshPressed() {
        refresh()
    }

    @objc private func requestPermission(_ sender: NSButton) {
        guard let kind = DesktopPermissionKind(rawValue: sender.tag) else { return }
        DesktopPermissionChecker.request(kind)
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.8) { [weak self] in
            self?.refresh()
        }
    }

    @objc private func openPermissionSettings(_ sender: NSButton) {
        guard let kind = DesktopPermissionKind(rawValue: sender.tag) else { return }
        DesktopPermissionChecker.openSettings(for: kind)
    }

    @objc private func openFileAccess() {
        fileAccessWindow.present()
    }

    @objc private func openAppManagementSettings() {
        DesktopPermissionChecker.openAppManagementSettings()
    }
}
