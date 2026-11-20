# go-upnp-player — decisions and renderer quirks (ADR)

Why the player is built the way the SAD describes ([go-upnp-player-sad.md](go-upnp-player-sad.md),
the UI: [go-upnp-player-ui-sad.md](go-upnp-player-ui-sad.md)), what was rejected and which renderer
quirks forced it.

Each ADR's status line says how far the problem is overcome and what is still open.

| ADR | Topic |
|---|---|
| [001](adr-001-lossless-formats-beyond-flac.md) | Lossless formats beyond FLAC (DSF/DSD, WAV, WavPack, APE, ALAC) |
| [002](adr-002-never-stop-gmrender.md) | ~~Never send `Stop`: it hangs the FiiO SR11~~ (superseded by ADR-025) |
| [003](adr-003-switching-tracks-on-busy-gmrender.md) | Switching tracks on a busy gmrender through its gapless path |
| [004](adr-004-detecting-track-advance.md) | Telling a real track advance from gmrender's early reports |
| [005](adr-005-seek-ignored-at-stream-start.md) | Repeating a seek that gmrender ignored at the start of a stream |
| [006](adr-006-hung-gmrender.md) | A hung gmrender (frozen position, `501 Playing failed`) |
| [007](adr-007-racing-gmrender.md) | A racing gmrender: no sound, position running ahead |
| [008](adr-008-cue-tracks-as-virtual-wav.md) | CUE tracks as virtual WAV; seeking without a SEEKTABLE |
| [009](adr-009-equalizer-on-player-side.md) | Equalizer on the player side, soft limiter instead of preamp |
| [010](adr-010-ui-polls-state.md) | ~~The page polls `/api/state` instead of a push channel~~ (superseded by ADR-026) |
| [011](adr-011-library-tree-navigation.md) | The library tree is for choosing, the playlist is for playing |
| [012](adr-012-updates-in-place.md) | Updating in place: one instance, no stale UI, position kept |
| [013](adr-013-stop-safe-renderers-start-like-foobar.md) | Renderers that survive Stop are driven the way foobar2000 drives them |
| [014](adr-014-retry-refused-start.md) | A refused start is retried for 10 s, and the page says so |
| [015](adr-015-start-at-position-as-a-stream.md) | A start at a position is a stream that begins there |
| [016](adr-016-clicks-between-tracks-on-fiio-coax.md) | One stream for the tracks of one format, as foobar2000 sends it: no clicks between tracks |
| [017](adr-017-old-position-after-seek.md) | The renderer's old position after a seek is not taken for where it plays |
| [018](adr-018-own-window-via-webview2.md) | The installed player shows its UI in a window of its own (WebView2) |
| [019](adr-019-check-for-updates-on-demand.md) | Updates are checked only on a click, and each kind of copy updates its own way |
| [020](adr-020-every-jump-within-an-album-is-a-seek.md) | Every jump within an album is a seek: no new stream, no click on coax |
| [021](adr-021-keep-the-sound-output-open.md) | Stop keeps the renderer's sound output open |
| [022](adr-022-a-jump-is-stop-then-start.md) | A jump over playing music: paused on silence, then restarted in place |
| [023](adr-023-the-end-of-the-playlist-switches-off.md) | The end of the playlist switches the renderer off, as foobar2000 does |
| [024](adr-024-a-start-takes-what-the-renderer-holds.md) | A start goes by what the renderer holds: from STOPPED, no lead and no seek |
| [025](adr-025-stop-is-stop.md) | Stop is Stop, on every renderer; transitions between tracks are their own |
| [026](adr-026-state-as-server-sent-events.md) | The page gets the state as Server-Sent Events |
| [027](adr-027-qt-window-with-miqt.md) | A Qt window (miqt) beside the web page, calling the player directly |
| [028](adr-028-dsd-converted-to-pcm-in-the-player.md) | DSD files are converted to PCM in the player, up to 176.4 kHz |
| [029](adr-029-dsd-every-rate-up-to-192.md) | DSD to PCM at every rate up to 192 kHz, both families |
| [030](adr-030-no-fade-in-the-stream.md) | No fade in the stream: the renderer gets the file's own samples |
| [031](adr-031-a-run-ends-in-silence-so-stop-arrives-live.md) | A run ends in a tail of silence, so Stop reaches a live stream |
| [032](adr-032-a-silent-renderer-stays-listed-offline.md) | A silent renderer stays on the list as OFFLINE, and comes back stopped |
| [033](adr-033-equalizer-center.md) | Center: the equalizer's curve moved down to an average of 0 dB, on request |
