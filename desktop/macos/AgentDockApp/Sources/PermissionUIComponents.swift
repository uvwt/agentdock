import AppKit

final class TopAlignedStackView: NSStackView {
    override var isFlipped: Bool { true }
}

enum PermissionUI {
    static func statusLabel(_ text: String = L10n.text("Not checked")) -> NSTextField {
        let label = NSTextField(labelWithString: text)
        label.font = .systemFont(ofSize: 12, weight: .medium)
        label.alignment = .right
        label.setContentHuggingPriority(.required, for: .horizontal)
        return label
    }

    static func detailLabel(_ text: String) -> NSTextField {
        let label = NSTextField(wrappingLabelWithString: text)
        label.font = .systemFont(ofSize: 11.5)
        label.textColor = .secondaryLabelColor
        label.maximumNumberOfLines = 0
        return label
    }

    static func separator() -> NSBox {
        let box = NSBox()
        box.boxType = .separator
        return box
    }

    static func applyColor(to label: NSTextField, granted: Bool?, attention: Bool = false) {
        if attention {
            label.textColor = .systemOrange
        } else if granted == true {
            label.textColor = .systemGreen
        } else if granted == false {
            label.textColor = .systemRed
        } else {
            label.textColor = .secondaryLabelColor
        }
    }
}
