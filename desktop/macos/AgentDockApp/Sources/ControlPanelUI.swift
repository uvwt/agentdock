import AppKit

/// AgentDock 桌面控制面板的轻量视觉语义层。
///
/// 两端不共享 UI Runtime，只共享「窗口背景 / 卡片 / 标题 / 主次操作 / 图标语义」。
/// macOS 使用系统动态颜色和 SF Symbols，避免为了跨平台一致性破坏原生深色模式与辅助功能。
enum ControlPanelUI {
    static let sectionSpacing: CGFloat = 16
    static let sidebarWidth: CGFloat = 190

    static func appMark(symbol: String = "shippingbox.fill") -> NSImageView {
        let imageView = symbolImageView(
            symbol: symbol,
            accessibilityDescription: "AgentDock",
            pointSize: 24,
            weight: .semibold
        )
        imageView.contentTintColor = .controlAccentColor
        imageView.widthAnchor.constraint(equalToConstant: 34).isActive = true
        imageView.heightAnchor.constraint(equalToConstant: 34).isActive = true
        return imageView
    }

    static func card(title: String, symbol: String, content: NSView) -> NSBox {
        let card = NSBox()
        card.boxType = .custom
        card.titlePosition = .noTitle
        card.borderColor = .separatorColor
        card.borderWidth = 1
        card.cornerRadius = 12
        card.fillColor = .controlBackgroundColor

        let icon = symbolImageView(
            symbol: symbol,
            accessibilityDescription: title,
            pointSize: 14,
            weight: .medium
        )
        icon.contentTintColor = .secondaryLabelColor
        icon.widthAnchor.constraint(equalToConstant: 18).isActive = true
        icon.heightAnchor.constraint(equalToConstant: 18).isActive = true

        let titleLabel = NSTextField(labelWithString: title)
        titleLabel.font = .systemFont(ofSize: 14, weight: .semibold)

        let header = NSStackView(views: [icon, titleLabel, NSView()])
        header.orientation = .horizontal
        header.alignment = .centerY
        header.spacing = 7

        let stack = NSStackView(views: [header, content])
        stack.orientation = .vertical
        stack.alignment = .leading
        stack.spacing = 13
        stack.translatesAutoresizingMaskIntoConstraints = false

        guard let cardContent = card.contentView else {
            return card
        }
        cardContent.addSubview(stack)

        content.translatesAutoresizingMaskIntoConstraints = false
        NSLayoutConstraint.activate([
            stack.leadingAnchor.constraint(equalTo: cardContent.leadingAnchor, constant: 16),
            stack.trailingAnchor.constraint(equalTo: cardContent.trailingAnchor, constant: -16),
            stack.topAnchor.constraint(equalTo: cardContent.topAnchor, constant: 15),
            stack.bottomAnchor.constraint(equalTo: cardContent.bottomAnchor, constant: -16),
            header.widthAnchor.constraint(equalTo: stack.widthAnchor),
            content.widthAnchor.constraint(equalTo: stack.widthAnchor),
        ])
        return card
    }

    static func configureQuietAction(_ button: NSButton, symbol: String) {
        button.bezelStyle = .inline
        button.image = NSImage(systemSymbolName: symbol, accessibilityDescription: button.title)
        button.imagePosition = .imageLeading
        button.imageScaling = .scaleProportionallyDown
    }

    static func configureSecondaryAction(_ button: NSButton, symbol: String) {
        button.bezelStyle = .rounded
        button.image = NSImage(systemSymbolName: symbol, accessibilityDescription: button.title)
        button.imagePosition = .imageLeading
        button.imageScaling = .scaleProportionallyDown
    }

    static func configurePrimaryAction(_ button: NSButton) {
        button.bezelStyle = .rounded
        button.keyEquivalent = "\r"
    }

    static func pageHeader(title: String, subtitle: String) -> NSStackView {
        let titleLabel = NSTextField(labelWithString: title)
        titleLabel.font = .systemFont(ofSize: 25, weight: .semibold)

        let subtitleLabel = NSTextField(wrappingLabelWithString: subtitle)
        subtitleLabel.font = .systemFont(ofSize: 12.5)
        subtitleLabel.textColor = .secondaryLabelColor

        let stack = NSStackView(views: [titleLabel, subtitleLabel])
        stack.orientation = .vertical
        stack.alignment = .leading
        stack.spacing = 4
        return stack
    }

    static func sidebarButton(
        title: String,
        symbol: String,
        tag: Int,
        target: AnyObject,
        action: Selector
    ) -> NSButton {
        let button = NSButton(title: title, target: target, action: action)
        button.tag = tag
        button.bezelStyle = .inline
        button.image = NSImage(systemSymbolName: symbol, accessibilityDescription: title)
        button.imagePosition = .imageLeading
        button.imageScaling = .scaleProportionallyDown
        button.alignment = .left
        button.font = .systemFont(ofSize: 13, weight: .medium)
        button.contentTintColor = .secondaryLabelColor
        button.wantsLayer = true
        button.layer?.cornerRadius = 8
        button.heightAnchor.constraint(equalToConstant: 34).isActive = true
        return button
    }

    static func setSidebarSelection(_ button: NSButton, selected: Bool) {
        let foregroundColor: NSColor = selected ? .controlAccentColor : .labelColor
        button.contentTintColor = foregroundColor
        button.attributedTitle = NSAttributedString(
            string: button.title,
            attributes: [
                .foregroundColor: foregroundColor,
                .font: NSFont.systemFont(ofSize: 13, weight: .medium),
            ]
        )
        button.layer?.backgroundColor = selected
            ? NSColor.controlAccentColor.withAlphaComponent(0.12).cgColor
            : NSColor.clear.cgColor
    }

    private static func symbolImageView(
        symbol: String,
        accessibilityDescription: String,
        pointSize: CGFloat,
        weight: NSFont.Weight
    ) -> NSImageView {
        let image = NSImage(systemSymbolName: symbol, accessibilityDescription: accessibilityDescription)?
            .withSymbolConfiguration(
                NSImage.SymbolConfiguration(pointSize: pointSize, weight: weight)
            )
        let imageView = NSImageView()
        imageView.image = image
        imageView.imageScaling = .scaleProportionallyDown
        return imageView
    }
}
