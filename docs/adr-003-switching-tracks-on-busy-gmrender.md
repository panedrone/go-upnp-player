# ADR-003: Switching tracks on a busy gmrender through its gapless path

**Status:** Accepted 2026-11-09 (written down 2026-11-10). The 2026-11-10 change to restarting
the same track (straight to the target position) is part of it. Since 2026-11-10 it applies only to
renderers that are not Stop-safe (the FiiO); Stop-safe ones are switched with `Stop` instead
([ADR-013](adr-013-stop-safe-renderers-start-like-foobar.md)).
**Overcome:** fully. Track switching works on both renderers (FiiO SR11, BeagleBone), and a
failed switch is reported instead of being treated as a success.

## Context

Both renderers run gmrender-resurrect. While it is PLAYING or PAUSED, gmrender ignores
`SetAVTransportURI` and keeps playing the old song. It accepts a new URI only from STOPPED, and
`Stop` is ruled out ([ADR-002](adr-002-never-stop-gmrender.md)).

gmrender does follow `SetNextAVTransportURI`: when the current track ends, it moves to the queued
"next" by itself.

## What was tried live and rejected (2026-11-09)

- `SetAVTransportURI` while playing: ignored, the old song goes on.
- `Play` to resume, then `SetAVTransportURI`: ignored the same way.
- `Pause → Stop → SetAVTransportURI`: hangs the FiiO ([ADR-002](adr-002-never-stop-gmrender.md)).
- Restarting the same track with `Seek(0)` and then seeking to the wanted position: the opening
  of the track was audible for a moment. Replaced 2026-11-10 by a single seek to the target.

## Decision

`services.PlanSwitch` picks the method from the transport state:

- **STOPPED or no media:** `SetAVTransportURI(target)`, then `Play`.
- **PLAYING or PAUSED:**
  1. `SetNextAVTransportURI(target)`;
  2. `Seek` to the last second of the current track;
  3. `Play` if paused.

  The renderer then moves to the target through its gapless path. The player waits until the
  renderer reports the target's length near position 0 (`SwitchDone`) before it queues the
  following track.
- **The renderer already has the requested track** (same URI and the same audible length):
  `Seek` straight to the start position, plus `Play` if paused.

If the renderer had already pulled a previously queued "next" into its buffer, it lands on that
track instead. The switch is then repeated, up to three attempts. If no attempt works, the player
reports "did not switch … it may be hung" instead of claiming the start succeeded
([ADR-006](adr-006-hung-gmrender.md)).

Right after a track starts, `SetNextAVTransportURI` is sent for the next playlist entry (gapless
playback). If a renderer does not support it, the poll starts the next track itself.

## Consequences

- A switch on a busy renderer costs a seek to the end of the current track. With a FLAC that has
  no SEEKTABLE this used to take ~19 s; it now takes ~2 s
  ([ADR-008](adr-008-cue-tracks-as-virtual-wav.md)).
- The URI alone does not prove which track is playing ([ADR-004](adr-004-detecting-track-advance.md)),
  which is why the length is checked too.
- `testkit.Renderer` reproduces this behaviour: `SetAVTransportURI` applies only from STOPPED, and
  a seek to the end moves to the queued next URI.
