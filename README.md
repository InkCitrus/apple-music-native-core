# Apple Music Native Core

A headless Apple Music playback core for macOS ARM64, Windows x64 and Linux x64. It provides a Go library and a versioned JSON Lines daemon for other applications. No browser process, Electron UI, Python runtime, Music.app automation, or Widevine identity files are needed for playback.

This is an experimental extraction of the MyGo Music lossless prototype. Its architecture is similar to a small library/daemon such as librespot. Authorization and licensed sample decryption still run in Apple's native Android libraries through a compatibility loader; this is **not** a clean-room implementation of Apple's protocol and is not affiliated with Apple.

Applications may embed the Go module or use the standalone daemon and native bundle. The minimal player example demonstrates direct library calls, with an optional native macOS control window.

## Build and run

Requires Go 1.26+, Python 3 for the build scripts, and a C/C++ compiler. Python is not a playback dependency. Run the build on the target OS:

```sh
python3 scripts/build.py
build/bundle/amcore --data-dir ./data setup
build/bundle/amcore --data-dir ./data auth
build/bundle/amcore --data-dir ./data serve
```

Windows uses `build\bundle\amcore.exe`. Install MSYS2 UCRT64 GCC, make, clang and lld first; use `python scripts/build.py --msys C:\msys64`. macOS requires Apple Silicon, macOS 14+, Xcode command line tools and CMake 3.20+. The Linux bundle uses the system C/C++ runtime plus a pinned loader requiring glibc 2.38 or later; ALSA or PulseAudio supplies output. Linux sign-in dialogs require `zenity` or `kdialog` in a graphical session. Windows uses system PowerShell/WinForms; macOS uses a native account connection window, attributed to the embedding application. These dialogs are local to this core, not an Apple-hosted sign-in page.

The build produces `build/bundle/amcore[.exe]` and `build/bundle/native/PLATFORM/`. Keep the directory structure intact. CI builds the same bundles without signing in to any account or fetching Apple's SDK libraries.

For distribution, commit the source used for the build and run `python3 scripts/bundle_sources.py` (Windows: add `--msys C:\msys64`). This adds license texts, a corresponding source archive and SHA-256 build metadata. macOS dependency sources and nested submodules are included in that archive when present, omitting unrelated upstream test data, benchmarks and prebuilt binaries. `dependency-sources/` contains their snapshots; the regular build fetches the same pinned revisions. Windows bundles include the DLLs needed to run without an installed compiler.

`setup` downloads checksum-pinned SDK dependencies separately. Runtime libraries and private sessions are stored in separate `DATA/runtime` and `DATA/session` directories. `auth` asks for account details and verification codes in system dialogs and passes them to the native helper using private pipes. An active subscription and account access to the requested song are required.

## Call from another application

Start one `amcore --data-dir PRIVATE_DIRECTORY serve` child. Write one JSON object per line to its stdin and parse replies by `id` on stdout. Pass `--app-name "Your App" --language zh-CN` to identify the client and select the account-window language. English and Simplified Chinese cover credentials, verification, progress and errors, with English fallback for other languages. The macOS window follows system light/dark appearance and runs only during authorization.

The process listens on no network ports. See [the protocol](docs/PROTOCOL.md) and [Node example](examples/node-client.mjs).

```json
{"id":1,"method":"hello"}
{"id":2,"method":"play","params":{"trackId":"123456789","position":0,"quality":"hires"}}
{"id":3,"method":"pause"}
{"id":4,"method":"seek","params":{"position":90000}}
{"id":5,"method":"volume","params":{"percent":50}}
{"id":6,"method":"resume"}
{"id":7,"method":"state"}
```

`play` acknowledges a buffering job; poll `state` for playback or errors. Positions and durations are milliseconds, volume is 0–100, and zero volume is valid. Control the queue in the embedding application, advancing only after `ended`. The core never silently opens Music.app, launches a browser, plays previews, or advances to another track. One SDK helper is reused across songs and seeks, and is released on logout or process shutdown.

Go applications can use `player.New(player.Config{Native: native.Config{...}})` directly. `catalog`, `stream`, `codec`, `audio`, `native`, `auth` and `service` are separate packages. Custom applications can provide their own sign-in form through `auth.Prompt`, or their own PCM sink through `player.Config.OpenOutput`. This allows application-specific UI without duplicating the decoder or session management.

## Quality and supported scope

The [minimal player](examples/miniplayer/) embeds the Go library directly and supports account connection, playback, pause, seek, volume and state reporting without MyGo Music or the stdio daemon. Native bundles include `amcore-demo[.exe]` for this example.

`hires` chooses the highest advertised ALAC variant. `lossless` chooses ALAC up to 48 kHz. AAC fallback is intentionally absent. Current full playback supports catalog songs and library songs with a catalog mapping; live radio, uploaded/matched-only files, AirPlay enumeration, exclusive audio output, offline downloads and gapless cross-track output are not implemented.

The underlying prototype passed real subscriber ALAC 24-bit/96 kHz decode, playback, pause/resume, volume and a 90-second seek on macOS ARM64, Windows x64 and Linux x64/WSL2. The extracted daemon has also passed a muted real-account regression on macOS: 24-bit/96 kHz source, pause/resume, zero volume, seek while paused and clean stop. The new code builds and passes portable tests on all three native operating systems. A complete real-account playback regression of the new plugin package on Windows/Linux is still required. Source format is reported separately from system output rate; bit-perfect physical DAC output has not been verified. See [validation details](docs/VALIDATION.md).

The previous Windows helper-only idle observation measured about 178 MiB resident memory and 0.026% of one logical CPU over 60 seconds, with stable RSS. That is not the whole plugin or long-running playback. Native libraries/loader dominate memory; this extraction does not claim librespot-level resource use. macOS Intel and Windows/Linux ARM64 have not been ported.

## Tests

```sh
go test -race ./...
python3 scripts/test_native.py
```

Tests use synthesized audio and an explicitly compiled fake SDK. They do not read real account sessions or contact Apple. The fake SDK transport test is available on macOS/Linux; native Windows CI compiles the actual helper and runs the portable Go suites. The Windows helper retains the validated 16 MiB reserved / 8 MiB committed stack and the v8 guest TLS instruction decoder fix. Do not replace it with an arbitrary cross-compiled helper or reduce that stack without repeating real native authorization tests.

## Source and licenses

New orchestration code is MIT licensed. Preserve the notices in [THIRD_PARTY.md](THIRD_PARTY.md), `codec/`, `audio/` and `third_party/`; upstream components retain their own licenses, including libhybris LGPL components. Apple's proprietary SDK binaries are not included in this repository or relicensed by it. The private review repository contains only source, build recipes, locked public dependency metadata and synthesized test fixtures. Account sessions, identifiers, tokens, signed media URLs, decoded subscriber audio and test profiles must not be committed.
