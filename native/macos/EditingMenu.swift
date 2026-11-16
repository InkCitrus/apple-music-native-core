import AppKit

// NSHostingView in a manually launched AppKit application does not install
// SwiftUI's Edit commands. Route the standard shortcuts to the field editor.
@MainActor enum NativeEditingMenu {
    static func install(appName: String, language: String) {
        func load(_ name: String) -> [String: String] {
            guard let url = Bundle.main.url(forResource: name, withExtension: "json", subdirectory: "Localizations"),
                  let data = try? Data(contentsOf: url),
                  let values = try? JSONDecoder().decode([String: String].self, from: data) else { return [:] }
            return values
        }
        let preferred = language.isEmpty ? Bundle.main.preferredLocalizations.first ?? Locale.preferredLanguages.first ?? "en" : language
        let fallback = load("en"), values = load(preferred.lowercased().hasPrefix("zh") ? "zh-Hans" : "en")
        func title(_ key: String, _ english: String) -> String { values[key] ?? fallback[key] ?? english }

        let bar = NSMenu()
        let app = NSMenu(title: appName)
        app.addItem(withTitle: String(format: title("menu_quit", "Quit %@"), appName), action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
        let appItem = bar.addItem(withTitle: appName, action: nil, keyEquivalent: "")
        appItem.submenu = app

        let edit = NSMenu(title: title("menu_edit", "Edit"))
        edit.addItem(withTitle: title("menu_undo", "Undo"), action: NSSelectorFromString("undo:"), keyEquivalent: "z")
        let redo = edit.addItem(withTitle: title("menu_redo", "Redo"), action: NSSelectorFromString("redo:"), keyEquivalent: "z")
        redo.keyEquivalentModifierMask = [.command, .shift]
        edit.addItem(.separator())
        edit.addItem(withTitle: title("menu_cut", "Cut"), action: #selector(NSText.cut(_:)), keyEquivalent: "x")
        edit.addItem(withTitle: title("menu_copy", "Copy"), action: #selector(NSText.copy(_:)), keyEquivalent: "c")
        edit.addItem(withTitle: title("menu_paste", "Paste"), action: #selector(NSText.paste(_:)), keyEquivalent: "v")
        edit.addItem(withTitle: title("menu_select_all", "Select All"), action: #selector(NSText.selectAll(_:)), keyEquivalent: "a")
        let editItem = bar.addItem(withTitle: edit.title, action: nil, keyEquivalent: "")
        editItem.submenu = edit
        NSApp.mainMenu = bar
    }
}
