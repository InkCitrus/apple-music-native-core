# Stdio protocol v1

Launch `amcore --data-dir PRIVATE_DIR --app-name "Your App" --language en serve`. The client name labels the account connection window; it never impersonates an Apple-hosted page. `zh-CN` selects Simplified Chinese and other unsupported languages fall back to English. Keep stdin open. UTF-8 JSON Lines, maximum request line 64 KiB. A request has a positive integer `id`, `method`, and optional object `params`. Replies may arrive out of order; always match `id`. Await control replies when command order matters. There is no HTTP/WebSocket server.

Success: `{"id":1,"ok":true,"result":{...}}`.
Failure: `{"id":1,"ok":false,"error":{"code":"signIn","message":"Sign in using the native account window"}}`.
Error codes: `invalid`, `unsupported`, `setup`, `signIn`, `busy`, `unavailable`. Diagnostics, private paths, signed media URLs, credentials and native error text do not appear in ordinary replies.

| Method | Parameters | Result |
| --- | --- | --- |
| `hello` | none | protocol version, core version, formats, `liveRadio` capability, device IDs |
| `doctor` | none | helper/session presence, without account values |
| `setup` | none | download and verify runtime dependencies |
| `auth` | none | open local account/password/OTP dialogs and persist native session |
| `session` | none | private `developerToken`, `mediaUserToken` for the embedding application's catalog API |
| `play` | `trackId` (catalog ID), `position` (ms), optional `quality`, optional `nextTrackId` (catalog ID) | accept a new buffering job, replacing old playback |
| `prepareRadio` | `stationId` (Apple Music station ID) | verify a playable live segment before updating a queue |
| `playRadio` | `stationId` (Apple Music station ID) | verify or reuse a prepared segment and start the broadcast |
| `state` | none | current state |
| `pause`, `resume` | none | change output state |
| `seek` | `position` (ms), optional `quality` | seek in the current song and retain pause and volume |
| `volume` | `percent` (finite 0–100) | change volume, including mute |
| `quality` | `quality`: `hires`, `lossless` or `aac` | quality used for the next play/seek; `aac` requires `hello.formats` to include `aac` |
| `devices` | none | system default output |
| `stop` | none | discard queued PCM; keep the SDK helper for reuse |
| `logout` | none | stop audio, release helper, remove only the dedicated account directory |
| `close` | none | acknowledge and shut down all owned processes |

`session` explicitly exports subscriber tokens over the caller's private pipe so an embedding application can use catalog/library APIs. Never log that reply, send it to untrusted peers, or expose this daemon through an unauthenticated network bridge. Passwords and OTPs never go through the daemon API. Embedders wanting their own account form use the `auth.Prompt` Go interface and supply credentials directly to the native helper API.

State example:

```json
{"status":"playing","trackId":"123456789","position":12500,"duration":240000,"volume":50,"format":{"sampleRate":96000,"channels":2,"bits":24,"framesPerPacket":4096},"outputRate":48000}
```

Statuses: `idle`, `buffering`, `playing`, `paused`, `ended`, `error`. `buffering` is active playback intent and is not an end-of-track signal. `error` includes a safe error category. `format` describes the decoded source; `outputRate` reports the backend's available output-rate measurement, which is not proof of end-to-end device format or bit-perfect output.

Native outputs may add `outputStats` with `underruns`, `queuedFrames`, and `capacityFrames`. Frames use the source sample rate. The underrun counter records an empty output queue while active, excluding intentional pause, cancellation and normal end-of-input. These are local output-health counters, without credentials, URLs or audio samples; they do not prove bit-perfect output or replace a listening check. Output capacity is approximately two seconds at each source sample rate. At most one encrypted HLS segment is prefetched ahead of the current segment, and it is discarded on cancellation.

There is one decoder and one native key context in use at a time. The embedding application owns queue order. Poll state; after `ended`, choose the next track explicitly. No audio is written to disk. Incoming EOF cancels outstanding work and terminates owned helpers and dialogs.

`nextTrackId` is an optional hint. Once the current song has buffered audio, the core can prepare the next song's playlist and up to 8 MiB of encrypted startup data in memory using the current quality. It uses this data only for a fresh, matching `play` from position zero. Changing track or quality can discard the prepared data. The hint does not advance the queue or open a second decoder or key context. Older protocol v1 cores ignore the hint.

When `hello.liveRadio` is true, pass one of the following station IDs to `prepareRadio` and `playRadio`. Preparation leaves the current playback job running. The core can reuse the verified first segment for up to 20 seconds. A live state has `live: true`, the station ID in `trackId`, and `duration: 0`. `position` is elapsed system audio output. Live seeking returns `unsupported`. The public TuneIn broadcasts require no account token and use the station's own AAC quality.

| Station | ID |
| --- | --- |
| Apple Music 1 | `ra.978194965` |
| Apple Music Hits | `ra.1498155548` |
| Apple Music Country | `ra.1498157166` |
| Apple Music Club | `ra.1740613859` |
| Apple Música Uno | `ra.1740613864` |
| Apple Music Chill | `ra.1740614260` |
