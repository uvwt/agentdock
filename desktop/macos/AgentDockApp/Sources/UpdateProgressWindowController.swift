import AppKit

@MainActor
final class UpdateProgressWindowController: NSWindowController {
    private let titleLabel = NSTextField(labelWithString: L10n.text("Updating AgentDock"))
    private let versionLabel = NSTextField(labelWithString: "")
    private let statusLabel = NSTextField(wrappingLabelWithString: L10n.text("Checking for updates…"))
    private let progressIndicator = NSProgressIndicator()
    private let detailLabel = NSTextField(wrappingLabelWithString: "")
    private let doneButton = NSButton(title: L10n.text("Done"), target: nil, action: nil)

    init() {
        let window = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: 460, height: 220),
            styleMask: [.titled],
            backing: .buffered,
            defer: false
        )
        window.title = L10n.text("AgentDock Update")
        window.isReleasedWhenClosed = false
        window.center()
        super.init(window: window)
        configureUI()
    }

    required init?(coder: NSCoder) {
        fatalError("init(coder:) has not been implemented")
    }

    func present() {
        showWindow(nil)
        window?.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }

    func presentChecking() {
        resetForUpdate()
        apply(.local(type: .stage, stage: .checking, currentVersion: AppVersion.current))
        present()
    }

    func presentFinishing(currentVersion: String, targetVersion: String) {
        resetForUpdate()
        apply(.local(
            type: .stage,
            stage: .restarting,
            currentVersion: currentVersion,
            targetVersion: targetVersion
        ))
        present()
    }

    func apply(_ event: UpdateProgressEvent) {
        updateVersionText(event)
        switch event.type {
        case .failed:
            showFailure(event.error ?? L10n.text("Update failed"))
        case .completed:
            let targetVersion = event.targetVersion ?? event.currentVersion
            if let targetVersion, AppVersion.display(targetVersion) == AppVersion.current {
                showUpToDate(version: targetVersion)
            } else {
                showCompletion(targetVersion: targetVersion, warning: nil)
            }
        case .stage, .progress:
            guard let stage = event.stage else { return }
            render(stage: stage, event: event)
        }
    }

    func showCompletion(targetVersion: String?, warning: String?) {
        titleLabel.stringValue = L10n.text("AgentDock update completed")
        if let targetVersion, !targetVersion.isEmpty {
            statusLabel.stringValue = L10n.format("Updated to %@", AppVersion.display(targetVersion))
        } else {
            statusLabel.stringValue = L10n.text("Update completed.")
        }
        detailLabel.stringValue = warning ?? ""
        setFinishedProgress(success: true)
        doneButton.isHidden = false
        window?.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }

    func showFailure(_ message: String) {
        titleLabel.stringValue = L10n.text("AgentDock update failed")
        statusLabel.stringValue = L10n.text("Update failed")
        detailLabel.stringValue = message
        setFinishedProgress(success: false)
        doneButton.isHidden = false
        window?.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }

    private func showUpToDate(version: String) {
        titleLabel.stringValue = L10n.text("AgentDock is up to date")
        statusLabel.stringValue = AppVersion.display(version)
        detailLabel.stringValue = ""
        setFinishedProgress(success: true)
        doneButton.isHidden = false
        present()
    }

    private func configureUI() {
        titleLabel.font = .systemFont(ofSize: 20, weight: .semibold)
        versionLabel.textColor = .secondaryLabelColor
        statusLabel.font = .systemFont(ofSize: 13, weight: .medium)
        detailLabel.textColor = .secondaryLabelColor
        detailLabel.lineBreakMode = .byWordWrapping

        progressIndicator.style = .bar
        progressIndicator.controlSize = .regular
        progressIndicator.isIndeterminate = true
        progressIndicator.minValue = 0
        progressIndicator.maxValue = 1

        doneButton.target = self
        doneButton.action = #selector(donePressed)
        doneButton.keyEquivalent = "\r"
        doneButton.isHidden = true

        let buttonRow = NSStackView(views: [NSView(), doneButton])
        buttonRow.orientation = .horizontal
        buttonRow.alignment = .centerY

        let stack = NSStackView(views: [
            titleLabel,
            versionLabel,
            statusLabel,
            progressIndicator,
            detailLabel,
            buttonRow,
        ])
        stack.orientation = .vertical
        stack.alignment = .leading
        stack.spacing = 10
        stack.translatesAutoresizingMaskIntoConstraints = false

        guard let contentView = window?.contentView else { return }
        contentView.addSubview(stack)
        NSLayoutConstraint.activate([
            stack.leadingAnchor.constraint(equalTo: contentView.leadingAnchor, constant: 24),
            stack.trailingAnchor.constraint(equalTo: contentView.trailingAnchor, constant: -24),
            stack.topAnchor.constraint(equalTo: contentView.topAnchor, constant: 22),
            stack.bottomAnchor.constraint(equalTo: contentView.bottomAnchor, constant: -20),
            progressIndicator.widthAnchor.constraint(equalTo: stack.widthAnchor),
            detailLabel.widthAnchor.constraint(equalTo: stack.widthAnchor),
            buttonRow.widthAnchor.constraint(equalTo: stack.widthAnchor),
        ])
    }

    private func resetForUpdate() {
        titleLabel.stringValue = L10n.text("Updating AgentDock")
        versionLabel.stringValue = ""
        statusLabel.stringValue = L10n.text("Checking for updates…")
        detailLabel.stringValue = ""
        doneButton.isHidden = true
        progressIndicator.isIndeterminate = true
        progressIndicator.doubleValue = 0
        progressIndicator.startAnimation(nil)
    }

    private func render(stage: UpdateProgressStage, event: UpdateProgressEvent) {
        switch stage {
        case .checking:
            setIndeterminateStatus(L10n.text("Checking for updates…"))
        case .downloading:
            renderDownload(event)
        case .verifying:
            setIndeterminateStatus(L10n.text("Verifying update package…"))
        case .extracting:
            setIndeterminateStatus(L10n.text("Extracting update…"))
        case .installing:
            setIndeterminateStatus(L10n.text("Installing update…"))
        case .updatingSkills:
            setIndeterminateStatus(L10n.text("Updating core skills…"))
        case .restarting:
            setIndeterminateStatus(L10n.text("Finishing update…"))
        }
    }

    private func renderDownload(_ event: UpdateProgressEvent) {
        statusLabel.stringValue = L10n.text("Downloading update…")
        let bytes = max(event.bytes ?? 0, 0)
        if let total = event.totalBytes, total > 0 {
            progressIndicator.stopAnimation(nil)
            progressIndicator.isIndeterminate = false
            progressIndicator.minValue = 0
            progressIndicator.maxValue = Double(total)
            progressIndicator.doubleValue = min(Double(bytes), Double(total))
            detailLabel.stringValue = L10n.format(
                "%@ / %@",
                formattedBytes(bytes),
                formattedBytes(total)
            )
        } else {
            progressIndicator.isIndeterminate = true
            progressIndicator.startAnimation(nil)
            detailLabel.stringValue = bytes > 0
                ? L10n.format("Downloaded %@", formattedBytes(bytes))
                : ""
        }
    }

    private func setIndeterminateStatus(_ text: String) {
        statusLabel.stringValue = text
        detailLabel.stringValue = ""
        progressIndicator.isIndeterminate = true
        progressIndicator.startAnimation(nil)
    }

    private func setFinishedProgress(success: Bool) {
        progressIndicator.stopAnimation(nil)
        progressIndicator.isIndeterminate = false
        progressIndicator.minValue = 0
        progressIndicator.maxValue = 1
        progressIndicator.doubleValue = success ? 1 : 0
    }

    private func updateVersionText(_ event: UpdateProgressEvent) {
        guard let current = event.currentVersion, !current.isEmpty,
              let target = event.targetVersion, !target.isEmpty else {
            return
        }
        versionLabel.stringValue = L10n.format(
            "Version %@ → %@",
            AppVersion.display(current),
            AppVersion.display(target)
        )
    }

    private func formattedBytes(_ bytes: Int64) -> String {
        ByteCountFormatter.string(fromByteCount: bytes, countStyle: .file)
    }

    @objc private func donePressed() {
        close()
    }
}
