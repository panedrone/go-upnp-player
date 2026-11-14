# ADR-002: Never send AVTransport.Stop to a renderer

**Status:** Accepted 2026-11-09 (written down 2026-11-10). Amended by
[ADR-013](adr-013-stop-safe-renderers-start-like-foobar.md) (2026-11-10): renderers known to survive
`Stop` (stock gmrender, the BeagleBone) now get it; every other renderer still never does.
Amended by [ADR-023](adr-023-the-end-of-the-playlist-switches-off.md) (2026-11-14): after the
playlist has played out, the renderer gets `Stop` as from foobar2000; the music has ended.
**Overcome:** fully. Nothing in normal operation needs `Stop`. One deliberate exception: the cure
for a racing renderer ([ADR-007](adr-007-racing-gmrender.md)), which plays nothing at that moment.

## Context

On `AVTransport.Stop` during playback the FiiO SR11 (gmrender-resurrect) took its whole UPnP
service down until a power cycle. This was a live incident on 2026-11-09. It happened again from
pause, so pausing first does not make `Stop` safe.

`Stop` looked necessary in two places:

- before `SetAVTransportURI`, because gmrender applies a new URI only from STOPPED
  (see [ADR-003](adr-003-switching-tracks-on-busy-gmrender.md));
- for the Stop button.

## Options

1. `Stop` before `SetAVTransportURI` and for the Stop button. Rejected: it hangs the FiiO.
2. `Pause → Stop → SetAVTransportURI`. Rejected: it hangs the FiiO from pause too.
3. **No `Stop` at all.** The Stop button sends `Pause` and clears the intent to play. Tracks are
   switched without leaving PLAYING/PAUSED ([ADR-003](adr-003-switching-tracks-on-busy-gmrender.md)).

## Decision

Option 3.

- The Stop button sends `Pause`, sets `wantPlaying = false` and drops any remembered resume
  position. The next Play starts the track over.
- When the user switches renderers, the old renderer gets `Pause`.
- On quit or update, a playing renderer gets `Pause`.

## Consequences

- A "stopped" renderer actually sits in PAUSED_PLAYBACK. The player's own intent
  (`wantPlaying`) is what tells stopped from paused.
- `testkit.Renderer` fails any test that sends `Stop`. The racing-renderer tests are the only ones
  that allow it explicitly. This keeps the rule enforced without the hardware.
