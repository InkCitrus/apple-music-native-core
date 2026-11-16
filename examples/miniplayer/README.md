# Minimal Go player

This executable imports `player`, `auth` and `native` directly. It does not start the `amcore` daemon, MyGo Music or a browser. Its application name is **Native Core Demo**. The native SDK helper remains a managed dependency of the library.

Build the core's native bundle on your OS first, then put this example beside it:

```sh
python3 scripts/build.py
go build -trimpath -o build/bundle/amcore-demo ./examples/miniplayer
build/bundle/amcore-demo --auth --language zh-CN --track YOUR_CATALOG_SONG_ID
```

Windows: build `build/bundle/amcore-demo.exe` instead. Keep `native/` and the Windows DLLs beside the executable. `--auth` downloads the pinned runtime and opens the client-attributed native account window. Account input never goes through the terminal.

Commands: `play ID`, `pause`, `resume`, `seek SECONDS`, `volume 0..100`, `state`, `stop`, `auth`, `quit`. Initial volume is 15%; use `--volume 0` for a muted check. The source ALAC format and system output rate are displayed separately.

An existing **dedicated native** session may be selected with `--session`, and an existing runtime with `--runtime`. The example never reads browser profiles or Music.app account data. `--json` emits only safe player state for automated checks, without credentials, tokens, signed media URLs or saved audio.

On macOS, build the optional native control window after the native bundle:

```sh
python3 examples/miniplayer/build_macos.py
```

The result is `build/demo/Native Core Demo.app`. The small SwiftUI window controls the direct Go example; it does not use the daemon. Its executable forwards `--session`, `--runtime`, `--track`, `--volume` and `--language` flags to the Go example, so the same dedicated test session can be reused without copying account files.

GUI launchers can also supply `AMCORE_DEMO_SESSION`, `AMCORE_DEMO_RUNTIME`, `AMCORE_DEMO_TRACK`, `AMCORE_DEMO_VOLUME` and `AMCORE_DEMO_LANGUAGE`. These accept the same configuration values as the corresponding flags; they are not account credentials. Explicit flags take precedence. Never put session contents or tokens into launcher configuration.
