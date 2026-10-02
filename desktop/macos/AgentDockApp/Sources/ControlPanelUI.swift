import AppKit

/// AgentDock 桌面控制面板的轻量视觉语义层。
///
/// 两端不共享 UI Runtime，只共享「窗口背景 / 卡片 / 标题 / 主次操作 / 图标语义」。
/// macOS 使用系统动态颜色和 SF Symbols，同时保留稳定的 AgentDock 蓝灰产品色。
enum ControlPanelUI {
    static let sectionSpacing: CGFloat = 18
    static let sidebarWidth: CGFloat = 202

    // 视觉稿使用稳定的蓝灰产品色，而不是跟随用户的系统强调色。
    // 这样 macOS 与 Windows 能保持同一品牌语义，同时仍为深色模式提供单独配色。
    static let accentColor = dynamicColor(
        light: NSColor(calibratedRed: 47.0 / 255.0, green: 111.0 / 255.0, blue: 237.0 / 255.0, alpha: 1),
        dark: NSColor(calibratedRed: 104.0 / 255.0, green: 151.0 / 255.0, blue: 255.0 / 255.0, alpha: 1)
    )
    static let accentSoftColor = dynamicColor(
        light: NSColor(calibratedRed: 232.0 / 255.0, green: 240.0 / 255.0, blue: 255.0 / 255.0, alpha: 1),
        dark: NSColor(calibratedRed: 31.0 / 255.0, green: 49.0 / 255.0, blue: 79.0 / 255.0, alpha: 1)
    )
    static let canvasColor = dynamicColor(
        light: NSColor(calibratedRed: 245.0 / 255.0, green: 248.0 / 255.0, blue: 252.0 / 255.0, alpha: 1),
        dark: NSColor(calibratedRed: 22.0 / 255.0, green: 27.0 / 255.0, blue: 35.0 / 255.0, alpha: 1)
    )
    static let sidebarColor = dynamicColor(
        light: NSColor(calibratedRed: 237.0 / 255.0, green: 243.0 / 255.0, blue: 249.0 / 255.0, alpha: 1),
        dark: NSColor(calibratedRed: 27.0 / 255.0, green: 34.0 / 255.0, blue: 44.0 / 255.0, alpha: 1)
    )
    static let surfaceColor = dynamicColor(
        light: .white,
        dark: NSColor(calibratedRed: 34.0 / 255.0, green: 41.0 / 255.0, blue: 52.0 / 255.0, alpha: 1)
    )
    static let surfaceBorderColor = dynamicColor(
        light: NSColor(calibratedRed: 221.0 / 255.0, green: 229.0 / 255.0, blue: 239.0 / 255.0, alpha: 1),
        dark: NSColor(calibratedRed: 58.0 / 255.0, green: 68.0 / 255.0, blue: 82.0 / 255.0, alpha: 1)
    )

    static func appMark(symbol: String = "shippingbox.fill") -> NSView {
        let imageView = symbolImageView(
            symbol: symbol,
            accessibilityDescription: "AgentDock",
            pointSize: 17,
            weight: .semibold
        )
        imageView.contentTintColor = .white
        imageView.translatesAutoresizingMaskIntoConstraints = false

        let container = NSBox()
        container.boxType = .custom
        container.titlePosition = .noTitle
        container.borderWidth = 0
        container.cornerRadius = 10
        container.fillColor = accentColor
        container.wantsLayer = true
        container.layer?.shadowColor = NSColor.black.cgColor
        container.layer?.shadowOpacity = 0.10
        container.layer?.shadowRadius = 6
        container.layer?.shadowOffset = NSSize(width: 0, height: -2)

        guard let content = container.contentView else { return container }
        content.addSubview(imageView)
        NSLayoutConstraint.activate([
            container.widthAnchor.constraint(equalToConstant: 36),
            container.heightAnchor.constraint(equalToConstant: 36),
            imageView.centerXAnchor.constraint(equalTo: content.centerXAnchor),
            imageView.centerYAnchor.constraint(equalTo: content.centerYAnchor),
            imageView.widthAnchor.constraint(equalToConstant: 20),
            imageView.heightAnchor.constraint(equalToConstant: 20),
        ])
        return container
    }

    static func card(title: String, symbol: String, content: NSView) -> NSBox {
        let card = NSBox()
        card.boxType = .custom
        card.titlePosition = .noTitle
        card.borderColor = surfaceBorderColor
        card.borderWidth = 1
        card.cornerRadius = 15
        card.fillColor = surfaceColor
        card.wantsLayer = true
        card.layer?.shadowColor = NSColor.black.cgColor
        card.layer?.shadowOpacity = 0.045
        card.layer?.shadowRadius = 10
        card.layer?.shadowOffset = NSSize(width: 0, height: -3)

        let icon = symbolImageView(
            symbol: symbol,
            accessibilityDescription: title,
            pointSize: 13,
            weight: .semibold
        )
        icon.contentTintColor = accentColor
        icon.translatesAutoresizingMaskIntoConstraints = false

        let iconContainer = NSBox()
        iconContainer.boxType = .custom
        iconContainer.titlePosition = .noTitle
        iconContainer.borderWidth = 0
        iconContainer.cornerRadius = 8
        iconContainer.fillColor = accentSoftColor
        iconContainer.translatesAutoresizingMaskIntoConstraints = false
        iconContainer.contentView?.addSubview(icon)
        if let iconContent = iconContainer.contentView {
            NSLayoutConstraint.activate([
                icon.centerXAnchor.constraint(equalTo: iconContent.centerXAnchor),
                icon.centerYAnchor.constraint(equalTo: iconContent.centerYAnchor),
                icon.widthAnchor.constraint(equalToConstant: 16),
                icon.heightAnchor.constraint(equalToConstant: 16),
            ])
        }
        NSLayoutConstraint.activate([
            iconContainer.widthAnchor.constraint(equalToConstant: 30),
            iconContainer.heightAnchor.constraint(equalToConstant: 30),
        ])

        let titleLabel = NSTextField(labelWithString: title)
        titleLabel.font = .systemFont(ofSize: 14.5, weight: .semibold)

        let header = NSStackView(views: [iconContainer, titleLabel, NSView()])
        header.orientation = .horizontal
        header.alignment = .centerY
        header.spacing = 9

        let stack = NSStackView(views: [header, content])
        stack.orientation = .vertical
        stack.alignment = .leading
        stack.spacing = 15
        stack.translatesAutoresizingMaskIntoConstraints = false

        guard let cardContent = card.contentView else {
            return card
        }
        cardContent.addSubview(stack)

        content.translatesAutoresizingMaskIntoConstraints = false
        NSLayoutConstraint.activate([
            stack.leadingAnchor.constraint(equalTo: cardContent.leadingAnchor, constant: 18),
            stack.trailingAnchor.constraint(equalTo: cardContent.trailingAnchor, constant: -18),
            stack.topAnchor.constraint(equalTo: cardContent.topAnchor, constant: 17),
            stack.bottomAnchor.constraint(equalTo: cardContent.bottomAnchor, constant: -18),
            header.widthAnchor.constraint(equalTo: stack.widthAnchor),
            content.widthAnchor.constraint(equalTo: stack.widthAnchor),
        ])
        return card
    }

    static func hero(content: NSView) -> NSBox {
        let hero = NSBox()
        hero.boxType = .custom
        hero.titlePosition = .noTitle
        hero.borderColor = accentColor.withAlphaComponent(0.16)
        hero.borderWidth = 1
        hero.cornerRadius = 17
        hero.fillColor = accentSoftColor.withAlphaComponent(0.72)
        hero.wantsLayer = true
        hero.layer?.shadowColor = NSColor.black.cgColor
        hero.layer?.shadowOpacity = 0.05
        hero.layer?.shadowRadius = 12
        hero.layer?.shadowOffset = NSSize(width: 0, height: -4)

        guard let heroContent = hero.contentView else { return hero }
        content.translatesAutoresizingMaskIntoConstraints = false
        heroContent.addSubview(content)
        NSLayoutConstraint.activate([
            content.leadingAnchor.constraint(equalTo: heroContent.leadingAnchor, constant: 22),
            content.trailingAnchor.constraint(equalTo: heroContent.trailingAnchor, constant: -22),
            content.topAnchor.constraint(equalTo: heroContent.topAnchor, constant: 20),
            content.bottomAnchor.constraint(equalTo: heroContent.bottomAnchor, constant: -20),
        ])
        return hero
    }

    static func summaryTile(title: String, symbol: String, status: NSTextField) -> NSBox {
        let tile = NSBox()
        tile.boxType = .custom
        tile.titlePosition = .noTitle
        tile.borderColor = surfaceBorderColor
        tile.borderWidth = 1
        tile.cornerRadius = 14
        tile.fillColor = surfaceColor
        tile.wantsLayer = true
        tile.layer?.shadowColor = NSColor.black.cgColor
        tile.layer?.shadowOpacity = 0.03
        tile.layer?.shadowRadius = 7
        tile.layer?.shadowOffset = NSSize(width: 0, height: -2)

        let icon = symbolImageView(
            symbol: symbol,
            accessibilityDescription: title,
            pointSize: 16,
            weight: .semibold
        )
        icon.contentTintColor = accentColor
        icon.widthAnchor.constraint(equalToConstant: 22).isActive = true
        icon.heightAnchor.constraint(equalToConstant: 22).isActive = true

        let titleLabel = NSTextField(labelWithString: title)
        titleLabel.font = .systemFont(ofSize: 12.5, weight: .semibold)

        status.font = .systemFont(ofSize: 11.5, weight: .medium)
        status.textColor = .secondaryLabelColor
        status.lineBreakMode = .byTruncatingTail

        let stack = NSStackView(views: [icon, titleLabel, status])
        stack.orientation = .vertical
        stack.alignment = .leading
        stack.spacing = 7
        stack.translatesAutoresizingMaskIntoConstraints = false

        guard let tileContent = tile.contentView else { return tile }
        tileContent.addSubview(stack)
        NSLayoutConstraint.activate([
            tile.heightAnchor.constraint(greaterThanOrEqualToConstant: 112),
            stack.leadingAnchor.constraint(equalTo: tileContent.leadingAnchor, constant: 15),
            stack.trailingAnchor.constraint(equalTo: tileContent.trailingAnchor, constant: -15),
            stack.topAnchor.constraint(equalTo: tileContent.topAnchor, constant: 14),
            stack.bottomAnchor.constraint(lessThanOrEqualTo: tileContent.bottomAnchor, constant: -14),
        ])
        return tile
    }

    static func configureQuietAction(_ button: NSButton, symbol: String) {
        button.bezelStyle = .inline
        button.image = NSImage(systemSymbolName: symbol, accessibilityDescription: button.title)
        button.imagePosition = .imageLeading
        button.imageScaling = .scaleProportionallyDown
    }

    static func configureSecondaryAction(_ button: NSButton, symbol: String) {
        button.bezelStyle = .rounded
        button.controlSize = .large
        button.image = NSImage(systemSymbolName: symbol, accessibilityDescription: button.title)
        button.imagePosition = .imageLeading
        button.imageScaling = .scaleProportionallyDown
    }

    static func configurePrimaryAction(_ button: NSButton) {
        button.bezelStyle = .rounded
        button.controlSize = .large
        button.bezelColor = accentColor
        button.contentTintColor = .white
        button.keyEquivalent = "\r"
    }

    static func pageHeader(title: String, subtitle: String) -> NSStackView {
        let titleLabel = NSTextField(labelWithString: title)
        titleLabel.font = .systemFont(ofSize: 27, weight: .semibold)

        let subtitleLabel = NSTextField(wrappingLabelWithString: subtitle)
        subtitleLabel.font = .systemFont(ofSize: 13)
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
        button.contentTintColor = .labelColor
        button.wantsLayer = true
        button.layer?.cornerRadius = 9
        button.heightAnchor.constraint(equalToConstant: 38).isActive = true
        return button
    }

    static func setSidebarSelection(_ button: NSButton, selected: Bool) {
        let foregroundColor: NSColor = selected ? accentColor : .labelColor
        button.contentTintColor = foregroundColor
        button.attributedTitle = NSAttributedString(
            string: button.title,
            attributes: [
                .foregroundColor: foregroundColor,
                .font: NSFont.systemFont(ofSize: 13, weight: .medium),
            ]
        )
        button.layer?.backgroundColor = (selected ? accentSoftColor : NSColor.clear).cgColor
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

    private static func dynamicColor(light: NSColor, dark: NSColor) -> NSColor {
        NSColor(name: nil) { appearance in
            appearance.bestMatch(from: [.darkAqua, .aqua]) == .darkAqua ? dark : light
        }
    }
}
