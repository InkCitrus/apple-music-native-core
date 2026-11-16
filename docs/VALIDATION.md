# Validation scope

The following checks describe the extraction, rather than assuming that a successful earlier prototype proves the new integration.

| Check | Result |
| --- | --- |
| Go suites, including race checks | Passed locally on macOS and in native macOS ARM64, Windows x64 and Linux x64 CI |
| Synthetic ALAC decoded PCM equality | Passed for 16-bit/44.1 kHz, 24-bit/96 kHz and 24-bit/192 kHz fixtures |
| Synthetic encrypted HLS and sample-accurate seek | Passed; no subscriber audio used |
| Production C++ helper with a compiled fake SDK | Passed on macOS/Linux, including repeated keys, large binary messages and cancellation |
| Extracted daemon, real macOS session | Passed with output muted: 24-bit/96 kHz ALAC source, pause/resume, volume, 90-second seek while paused, stop and clean exit |
| New plugin adapter | Persistent JSONL process, platform selection, library/catalog mapping, zero volume, stale-request suppression, sanitized failures and logout covered by synthetic host tests |

The macOS real-account run used a dedicated existing native session through `--session`; it did not copy account files into the repository or save audio. The system output rate was 48 kHz while the source was 96 kHz. This confirms the lossless source/decode path and control behavior, not bit-perfect hardware output or a fresh sign-in through the new system dialogs.

Windows/Linux real-account ALAC playback passed in the preceding prototype. The extracted daemon and complete plugin package still need that real-account regression on the target systems. CI deliberately uses no account or Apple SDK libraries, so it cannot establish service authorization, real output-device behavior or long-running resource use.

The earlier Windows helper-only idle observation was approximately 178 MiB resident memory and 0.026% of one logical CPU over 60 seconds. This is a baseline for the native runtime, not a measurement of the extracted complete plugin or an assurance of librespot-like memory use.
