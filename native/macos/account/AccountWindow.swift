import AppKit
import SwiftUI

struct AccountStrings {
    let values: [String: String]
    let fallback: [String: String]
    init(language: String) {
        func load(_ name: String) -> [String: String] {
            guard let url = Bundle.main.url(forResource: name, withExtension: "json", subdirectory: "Localizations"),
                  let data = try? Data(contentsOf: url),
                  let strings = try? JSONDecoder().decode([String: String].self, from: data) else { return [:] }
            return strings
        }
        let preferred = language.isEmpty ? Bundle.main.preferredLocalizations.first ?? Locale.preferredLanguages.first ?? "en" : language
        fallback = load("en")
        values = load(preferred.lowercased().hasPrefix("zh") ? "zh-Hans" : "en")
    }
    func text(_ key: String) -> String { values[key] ?? fallback[key] ?? key }
    func format(_ key: String, _ value: String) -> String { String(format: text(key), value) }
}

@MainActor final class AccountModel: ObservableObject {
    @Published var account = ""
    @Published var password = ""
    @Published var code = ""
    @Published var verification = false
    @Published var busy = false
    @Published var success = false
    @Published var showPassword = false
    @Published var failure: String?
    @Published var status = ""
    let preview: Bool
    let appName: String
    let strings: AccountStrings
    init() {
        let args = CommandLine.arguments
        func option(_ name: String) -> String? {
            guard let index = args.firstIndex(of: name), index + 1 < args.count else { return nil }
            return args[index + 1]
        }
        preview = !args.contains("--pipe")
        appName = String((option("--app-name") ?? (preview ? "MyGo Music" : "Apple Music Native Core")).prefix(80))
        strings = AccountStrings(language: option("--language") ?? "")
    }
    var ready: Bool {
        !busy && !success && (verification ? code.count == 6 : !account.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && !password.isEmpty)
    }
    func send(_ event: [String: String]) {
        guard !preview, let data = try? JSONSerialization.data(withJSONObject: event) else { return }
        try? FileHandle.standardOutput.write(contentsOf: data + Data([10]))
    }
    func submit() {
        guard ready else { return }
        if preview { password = ""; verification.toggle(); code = ""; return }
        if verification {
            send(["event": "code", "code": code]); code = ""; status = strings.text("progress_verifying")
        } else {
            send(["event": failure == nil ? "credentials" : "retry", "username": account, "password": password])
            password = ""; status = strings.text("progress_signing_in")
        }
        failure = nil; busy = true
    }
    func accept(_ message: [String: String]) {
        switch message["view"] {
        case "credentials": verification = false; busy = false; failure = nil; password = ""; code = ""
        case "verification": verification = true; busy = false; failure = nil; code = ""
        case "progress":
            busy = true
            switch message["stage"] {
            case "session": status = strings.text("progress_session")
            case "verifying": status = strings.text("progress_verifying")
            default: status = strings.text("progress_signing_in")
            }
        case "failure":
            busy = false; verification = false; password = ""; code = ""
            let allowed = ["network", "account", "terms", "rejected"]
            let reason = allowed.contains(message["stage"] ?? "") ? message["stage"]! : "unavailable"
            failure = strings.text("error_" + reason)
        case "success":
            busy = false; success = true; password = ""; code = ""
            DispatchQueue.main.asyncAfter(deadline: .now() + 1) { [weak self] in
                self?.send(["event": "done"]); NSApp.terminate(nil)
            }
        default: break
        }
    }
    func cancel() { password = ""; code = ""; send(["event": "cancel"]); NSApp.terminate(nil) }
}

struct AccountView: View {
    @ObservedObject var model: AccountModel
    @FocusState private var focus: Field?
    private enum Field { case account, password, code }
    private var text: AccountStrings { model.strings }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 12) {
                Image(systemName: model.success ? "checkmark" : "link")
                    .font(.system(size: 19, weight: .medium)).foregroundStyle(Color.accentColor)
                    .frame(width: 40, height: 40)
                    .background(RoundedRectangle(cornerRadius: 10).fill(Color.accentColor.opacity(0.10)))
                VStack(alignment: .leading, spacing: 3) {
                    Text(model.appName).font(.system(size: 11, weight: .medium)).foregroundStyle(.secondary)
                    Text(text.text(model.success ? "success_title" : model.verification ? "verify_title" : "connect_title"))
                        .font(.system(size: 19, weight: .semibold))
                }
                Spacer(minLength: 0)
            }.frame(height: 42).padding(.bottom, 12)

            Text(model.success ? text.format("success_description", model.appName) : model.verification ? text.text("verify_description") : text.format("description", model.appName))
                .font(.system(size: 12)).foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true).frame(height: 32, alignment: .top)
                .padding(.bottom, 14)
            Divider()

            VStack(alignment: .leading, spacing: 15) {
                if model.success { Spacer() }
                else if model.verification {
                    Text(text.text("code_label")).font(.system(size: 12, weight: .medium))
                    TextField(text.text("code_placeholder"), text: $model.code)
                        .font(.system(size: 22, weight: .medium, design: .monospaced))
                        .tracking(6).multilineTextAlignment(.center)
                        .textFieldStyle(.plain).focused($focus, equals: .code)
                        .padding(12).background(fieldBackground)
                        .onChange(of: model.code) { _, value in
                            model.code = String(value.filter { "0123456789".contains($0) }.prefix(6))
                        }
                    Text(text.text("code_hint")).font(.system(size: 11)).foregroundStyle(.secondary)
                } else {
                    VStack(alignment: .leading, spacing: 6) {
                        Text(text.text("account_label")).font(.system(size: 12, weight: .medium))
                        TextField(text.text("account_placeholder"), text: $model.account)
                            .textFieldStyle(.plain).focused($focus, equals: .account)
                            .padding(.horizontal, 11).frame(height: 37).background(fieldBackground)
                            .onSubmit { focus = .password }
                    }
                    VStack(alignment: .leading, spacing: 6) {
                        Text(text.text("password_label")).font(.system(size: 12, weight: .medium))
                        HStack(spacing: 10) {
                            Group {
                                if model.showPassword { TextField(text.text("password_placeholder"), text: $model.password) }
                                else { SecureField(text.text("password_placeholder"), text: $model.password) }
                            }.textFieldStyle(.plain).focused($focus, equals: .password)
                            Button { model.showPassword.toggle(); focus = .password } label: {
                                Image(systemName: model.showPassword ? "eye.slash" : "eye").foregroundStyle(.secondary)
                            }.buttonStyle(.plain).help(text.text(model.showPassword ? "hide_password" : "show_password"))
                                .accessibilityLabel(text.text(model.showPassword ? "hide_password" : "show_password"))
                        }.padding(.horizontal, 11).frame(height: 37).background(fieldBackground)
                    }
                }
                Spacer(minLength: 0)
            }.font(.system(size: 13)).frame(height: 143).padding(.top, 18)
                .disabled(model.busy || model.success)

            Group {
                if let failure = model.failure {
                    Label(failure, systemImage: "exclamationmark.circle").foregroundStyle(.red)
                } else if model.busy {
                    HStack(spacing: 8) { ProgressView().controlSize(.small); Text(model.status).foregroundStyle(.secondary) }
                } else { Text(" ") }
            }.font(.system(size: 11)).lineLimit(2).frame(height: 38)

            HStack {
                Button(text.text("cancel")) { model.cancel() }.keyboardShortcut(.cancelAction)
                Spacer()
                Button { model.submit() } label: {
                    Text(text.text(model.success ? "connected" : model.busy ? "waiting" : model.verification ? "verify" : model.failure == nil ? "connect" : "retry"))
                        .frame(minWidth: 78)
                }.buttonStyle(.borderedProminent).keyboardShortcut(.defaultAction).disabled(!model.ready)
            }.controlSize(.large)

            Text(text.format("provider", model.appName)).font(.system(size: 10)).foregroundStyle(.tertiary)
                .padding(.top, 21)
        }
        .padding(26).frame(width: 460, height: 466)
        .background(Color(nsColor: .windowBackgroundColor))
        .onAppear { focus = .account }
        .onChange(of: model.verification) { _, value in focus = value ? .code : .password }
    }
    private var fieldBackground: some View {
        RoundedRectangle(cornerRadius: 7).fill(Color(nsColor: .textBackgroundColor))
            .overlay(RoundedRectangle(cornerRadius: 7).strokeBorder(Color.primary.opacity(0.15), lineWidth: 1))
    }
}

@MainActor final class AccountDelegate: NSObject, NSApplicationDelegate, NSWindowDelegate {
    let model = AccountModel()
    var window: NSWindow?
    func applicationDidFinishLaunching(_ notification: Notification) {
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 460, height: 466), styleMask: [.titled, .closable], backing: .buffered, defer: false)
        window.title = model.strings.format("window_title", model.appName)
        window.contentView = NSHostingView(rootView: AccountView(model: model))
        window.delegate = self; window.isReleasedWhenClosed = false
        window.center(); window.makeKeyAndOrderFront(nil); NSApp.activate(ignoringOtherApps: true)
        self.window = window
        guard !model.preview else { return }
        DispatchQueue.global(qos: .userInitiated).async { [weak self] in
            while let line = readLine() {
                guard let data = line.data(using: .utf8), let message = try? JSONSerialization.jsonObject(with: data) as? [String: String] else { continue }
                DispatchQueue.main.async { self?.model.accept(message) }
            }
            DispatchQueue.main.async { NSApp.terminate(nil) }
        }
    }
    func windowShouldClose(_ sender: NSWindow) -> Bool { model.cancel(); return true }
}

@main struct AccountApplication {
    @MainActor static func main() {
        let application = NSApplication.shared
        let delegate = AccountDelegate()
        application.setActivationPolicy(.accessory)
        application.delegate = delegate
        withExtendedLifetime(delegate) { application.run() }
    }
}
