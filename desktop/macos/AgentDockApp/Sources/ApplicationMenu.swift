import AppKit

@MainActor
enum ApplicationMenu {
    static func install() {
        let mainMenu = NSMenu(title: "AgentDock")

        let applicationMenuItem = NSMenuItem()
        let applicationMenu = NSMenu(title: "AgentDock")
        applicationMenu.addItem(
            item(
                title: "关于 AgentDock",
                action: #selector(NSApplication.orderFrontStandardAboutPanel(_:)),
                keyEquivalent: ""
            )
        )
        applicationMenu.addItem(.separator())
        applicationMenu.addItem(
            item(
                title: "退出 AgentDock",
                action: #selector(NSApplication.terminate(_:)),
                keyEquivalent: "q"
            )
        )
        applicationMenuItem.submenu = applicationMenu
        mainMenu.addItem(applicationMenuItem)

        let editMenuItem = NSMenuItem()
        let editMenu = NSMenu(title: "编辑")
        editMenu.addItem(item(title: "撤销", action: Selector(("undo:")), keyEquivalent: "z"))
        editMenu.addItem(
            item(
                title: "重做",
                action: Selector(("redo:")),
                keyEquivalent: "z",
                modifiers: [.command, .shift]
            )
        )
        editMenu.addItem(.separator())
        editMenu.addItem(item(title: "剪切", action: #selector(NSText.cut(_:)), keyEquivalent: "x"))
        editMenu.addItem(item(title: "复制", action: #selector(NSText.copy(_:)), keyEquivalent: "c"))
        editMenu.addItem(item(title: "粘贴", action: #selector(NSText.paste(_:)), keyEquivalent: "v"))
        editMenu.addItem(item(title: "全选", action: #selector(NSText.selectAll(_:)), keyEquivalent: "a"))
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
