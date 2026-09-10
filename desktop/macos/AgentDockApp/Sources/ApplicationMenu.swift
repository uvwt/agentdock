import AppKit

@MainActor
enum ApplicationMenu {
    static func install() {
        let mainMenu = NSMenu(title: "AgentDock")

        let applicationMenuItem = NSMenuItem()
        let applicationMenu = NSMenu(title: "AgentDock")
        applicationMenu.addItem(
            item(
                title: L10n.text("About AgentDock"),
                action: #selector(NSApplication.orderFrontStandardAboutPanel(_:)),
                keyEquivalent: ""
            )
        )
        applicationMenu.addItem(.separator())
        applicationMenu.addItem(
            item(
                title: L10n.text("Quit AgentDock"),
                action: #selector(NSApplication.terminate(_:)),
                keyEquivalent: "q"
            )
        )
        applicationMenuItem.submenu = applicationMenu
        mainMenu.addItem(applicationMenuItem)

        let editMenuItem = NSMenuItem()
        let editMenu = NSMenu(title: L10n.text("Edit"))
        editMenu.addItem(item(title: L10n.text("Undo"), action: Selector(("undo:")), keyEquivalent: "z"))
        editMenu.addItem(
            item(
                title: L10n.text("Redo"),
                action: Selector(("redo:")),
                keyEquivalent: "z",
                modifiers: [.command, .shift]
            )
        )
        editMenu.addItem(.separator())
        editMenu.addItem(item(title: L10n.text("Cut"), action: #selector(NSText.cut(_:)), keyEquivalent: "x"))
        editMenu.addItem(item(title: L10n.text("Copy"), action: #selector(NSText.copy(_:)), keyEquivalent: "c"))
        editMenu.addItem(item(title: L10n.text("Paste"), action: #selector(NSText.paste(_:)), keyEquivalent: "v"))
        editMenu.addItem(item(title: L10n.text("Select All"), action: #selector(NSText.selectAll(_:)), keyEquivalent: "a"))
        editMenuItem.submenu = editMenu
        mainMenu.addItem(editMenuItem)

        NSApp.mainMenu = mainMenu
    }

    private static func item(
        title: String,
        action: Selector,
        keyEquivalent: String,
        modifiers: NSEvent.ModifierFlags = [.command]
    ) -> NSMenuItem {
        let menuItem = NSMenuItem(title: title, action: action, keyEquivalent: keyEquivalent)
        menuItem.target = nil
        menuItem.keyEquivalentModifierMask = modifiers
        return menuItem
    }
}
