import AppKit
import SwiftUI

struct SourceFormat: Decodable { var sampleRate: Int; var bits: Int; var channels: Int }
struct PlaybackState: Decodable {
    var status = "idle"
    var position: Int64 = 0
    var duration: Int64 = 0
    var volume: Double = 15
    var format: SourceFormat?
    var outputRate: Double?
    enum CodingKeys: String, CodingKey { case status, position, duration, volume, format, outputRate }
}
@MainActor final class PlayerModel: ObservableObject {
    @Published var track = "1783644617"
    @Published var state = PlaybackState()
    @Published var position = 0.0
    @Published var volume = 15.0
    @Published var editingPosition = false
    @Published var message = ""
    @Published var alive = false
    let chinese: Bool
    let launchArguments: [String]
    private var process: Process?
    private var input: FileHandle?
    private var output = Data()
    init() {
        var args = Array(CommandLine.arguments.dropFirst())
        let environment = ProcessInfo.processInfo.environment
        for (flag, name) in [("--session", "AMCORE_DEMO_SESSION"), ("--runtime", "AMCORE_DEMO_RUNTIME"), ("--track", "AMCORE_DEMO_TRACK"), ("--volume", "AMCORE_DEMO_VOLUME"), ("--language", "AMCORE_DEMO_LANGUAGE")] {
            if !args.contains(flag), let value = environment[name], !value.isEmpty { args += [flag, value] }
        }
        launchArguments = args
        func option(_ name: String) -> String? { guard let n = args.firstIndex(of: name), n + 1 < args.count else { return nil }; return args[n + 1] }
        chinese = (option("--language") ?? Locale.preferredLanguages.first ?? "en").lowercased().hasPrefix("zh")
        if let value = option("--track") { track = value }
        if let value = option("--volume"), let level = Double(value) { volume = level }
    }
    func text(_ en: String, _ zh: String) -> String { chinese ? zh : en }
    var statusText: String {
        switch state.status {
        case "idle": return text("Ready", "就绪")
        case "buffering": return text("Buffering", "缓冲中")
        case "playing": return text("Playing", "播放中")
        case "paused": return text("Paused", "已暂停")
        case "ended": return text("Ended", "已结束")
        case "error": return text("Playback error", "播放错误")
        default: return state.status
        }
    }
    var active: Bool { ["playing", "buffering", "paused"].contains(state.status) }
    var canPlay: Bool { alive && !track.isEmpty && track.count <= 24 && track.allSatisfy { "0123456789".contains($0) } }
    func start() {
        guard process == nil else { return }
        let child = Process(), stdin = Pipe(), stdout = Pipe()
        let folder = Bundle.main.bundleURL.appendingPathComponent("Contents/MacOS")
        child.executableURL = folder.appendingPathComponent("amcore-demo")
        child.arguments = ["--json", "--language", chinese ? "zh-CN" : "en", "--bundle", Bundle.main.bundleURL.appendingPathComponent("Contents/Resources/native/darwin-arm64").path] + launchArguments
        child.standardInput = stdin; child.standardOutput = stdout; child.standardError = FileHandle.nullDevice
        stdout.fileHandleForReading.readabilityHandler = { [weak self] handle in
            let data = handle.availableData; guard !data.isEmpty else { return }
            Task { @MainActor in self?.receive(data) }
        }
        child.terminationHandler = { [weak self] _ in
            stdout.fileHandleForReading.readabilityHandler = nil
            Task { @MainActor in self?.alive = false }
        }
        do { try child.run(); process = child; input = stdin.fileHandleForWriting; alive = true }
        catch { message = text("The player could not start.", "播放器未能启动。") }
    }
    private func receive(_ data: Data) {
        output.append(data)
        while let end = output.firstIndex(of: 10) {
            let line = output.prefix(upTo: end); output.removeSubrange(...end)
            guard let event = try? JSONSerialization.jsonObject(with: line) as? [String: Any] else { continue }
            if event["event"] as? String == "state", let value = event["value"], let data = try? JSONSerialization.data(withJSONObject: value), let next = try? JSONDecoder().decode(PlaybackState.self, from: data) {
                state = next
                if !editingPosition { position = Double(next.position) / 1000 }
                if next.status == "error" { message = text("Playback failed. Check account access and runtime setup.", "播放失败，请检查账号授权及运行库。") }
            } else if event["event"] as? String == "error" {
                message = event["value"] as? String == "signIn" ? text("Connect your Apple Music account first.", "请先连接 Apple Music 账号。") : text("The operation could not be completed.", "操作未能完成。")
            } else if event["event"] as? String == "authorized" { message = text("Account connected.", "账号已连接。") }
        }
        if output.count > 65536 { output = Data() }
    }
    func send(_ command: String) {
        guard let input else { return }
        message = ""
        do { try input.write(contentsOf: Data((command + "\n").utf8)) }
        catch { message = text("The player connection closed.", "播放器连接已关闭。") }
    }
    func stop() { input?.closeFile(); input = nil; if process?.isRunning == true { process?.interrupt() }; process = nil }
    func clock(_ seconds: Double) -> String { let n = max(0, Int(seconds)); return String(format: "%d:%02d", n / 60, n % 60) }
}
struct PlayerView: View {
    @ObservedObject var model: PlayerModel
    var body: some View {
        VStack(alignment: .leading, spacing: 20) {
            HStack {
                VStack(alignment: .leading, spacing: 4) {
                    Text("Native Core Demo").font(.system(size: 22, weight: .semibold))
                    Text(model.text("Direct Go library example", "直接调用 Go 库的独立示例")).font(.callout).foregroundStyle(.secondary)
                }
                Spacer()
                Button(model.text("Connect account", "连接账号")) { model.send("auth") }.disabled(!model.alive)
            }
            Divider()
            VStack(alignment: .leading, spacing: 8) {
                Text(model.text("Apple Music catalog song ID", "Apple Music 曲库歌曲 ID")).font(.caption).foregroundStyle(.secondary)
                HStack {
                    TextField("1783644617", text: $model.track).textFieldStyle(.roundedBorder).onSubmit { if model.canPlay { model.send("play " + model.track) } }
                    Button(model.text("Play", "播放")) { model.send("play " + model.track) }.buttonStyle(.borderedProminent).disabled(!model.canPlay)
                }
            }
            VStack(spacing: 5) {
                Slider(value: $model.position, in: 0...max(1, Double(model.state.duration) / 1000), onEditingChanged: { editing in
                    model.editingPosition = editing
                    if !editing { model.send("seek " + String(model.position)) }
                }).disabled(!model.active)
                HStack { Text(model.clock(model.position)); Spacer(); Text(model.clock(Double(model.state.duration) / 1000)) }.font(.caption.monospacedDigit()).foregroundStyle(.secondary)
            }
            HStack {
                Button(model.state.status == "paused" ? model.text("Resume", "继续") : model.text("Pause", "暂停")) { model.send(model.state.status == "paused" ? "resume" : "pause") }.disabled(!model.active)
                Button(model.text("Stop", "停止")) { model.send("stop") }.disabled(!model.active)
                Spacer()
                Image(systemName: "speaker.wave.2").foregroundStyle(.secondary)
                Slider(value: $model.volume, in: 0...100, onEditingChanged: { editing in if !editing { model.send("volume " + String(model.volume)) } }).frame(width: 110)
                Text(String(format: "%.0f%%", model.volume)).font(.caption.monospacedDigit()).frame(width: 34, alignment: .trailing)
            }
            HStack {
                VStack(alignment: .leading, spacing: 5) {
                    Text(model.text("Decoded source", "解码源格式")).font(.caption).foregroundStyle(.secondary)
                    if let format = model.state.format { Text(String(format: "ALAC · %d-bit / %.1f kHz", format.bits, Double(format.sampleRate) / 1000)).font(.system(.body, design: .monospaced)) }
                    else { Text("—") }
                }
                Spacer()
                VStack(alignment: .trailing, spacing: 5) {
                    Text(model.text("System output", "系统输出")).font(.caption).foregroundStyle(.secondary)
                    Text(model.state.outputRate.map { String(format: "%.1f kHz", $0 / 1000) } ?? "—").font(.system(.body, design: .monospaced))
                }
            }.padding(14).background(RoundedRectangle(cornerRadius: 9).fill(Color.primary.opacity(0.04)))
            Text(model.message.isEmpty ? model.text("Playback: ", "播放状态：") + model.statusText : model.message)
                .font(.caption).foregroundStyle(.secondary).lineLimit(2).frame(height: 26, alignment: .top)
            Text(model.text("Source quality and system output are reported separately. Exclusive output is not enabled.", "源格式与系统输出分别显示，当前未使用独占输出。"))
                .font(.caption2).foregroundStyle(.tertiary)
        }.padding(26).frame(width: 540)
            .onAppear { model.start() }
    }
}
@MainActor final class PlayerDelegate: NSObject, NSApplicationDelegate, NSWindowDelegate {
    let model = PlayerModel()
    var window: NSWindow?
    func applicationDidFinishLaunching(_ notification: Notification) {
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 540, height: 480), styleMask: [.titled, .closable, .miniaturizable], backing: .buffered, defer: false)
        window.title = "Native Core Demo"
        window.contentView = NSHostingView(rootView: PlayerView(model: model))
        window.delegate = self; window.isReleasedWhenClosed = false
        window.center(); window.makeKeyAndOrderFront(nil); NSApp.activate(ignoringOtherApps: true)
        self.window = window
    }
    func windowShouldClose(_ sender: NSWindow) -> Bool { model.stop(); NSApp.terminate(nil); return true }
}
@main struct PlayerApplication {
    @MainActor static func main() {
        let app = NSApplication.shared; let delegate = PlayerDelegate()
        app.setActivationPolicy(.regular); app.delegate = delegate
        withExtendedLifetime(delegate) { app.run() }
    }
}
