# ADR-004: Telling a real track advance from gmrender's early reports

**Status:** Accepted 2026-11-09 (written down 2026-11-10).
**Overcome:** fully. The playlist index follows the renderer correctly on both devices.
Update 2026-11-10 (1.0.11): the end of a track is not always STOPPED either. The FiiO SR11 stays
in PLAYING at the very end after the last track. That showed as a false STALLED with "restart
it". The last track now counts as played out when its position reaches its known length
(`lastTrackPlayedOut`), not by waiting for the position to freeze (the owner rejected timing
heuristics for this). A position at the end is never reported as stalled, and the renderer is
paused 2 s after the end of the playlist.

## Context

The poll (once a second, `GetTransportInfo` + `GetPositionInfo`) must notice when the renderer
has moved on to the queued next track by itself, so that the playlist index advances.

On gmrender (seen live on the FiiO SR11, 2026-11-09):

- `TrackURI` switches to the next track as soon as GStreamer has downloaded the current WAV into
  its buffer. That can be minutes before the real transition.
- At the same moment `RelTime` jumps back by a couple of seconds.

## Options

1. **Advance when `TrackURI` changes.** Rejected: it advanced minutes early, and the UI showed the
   next track while the current one was still playing.
2. **Advance on a position jump back.** Rejected as the only signal: the early `RelTime` reset
   looks exactly like it.
3. **Advance when `TrackDuration` becomes the next track's length.** A position jump back counts
   only when both tracks have the same length, because then the length cannot tell them apart.

## Decision

Option 3, in `services.DecideTick`. In the same function:

- STOPPED more than 4 s after the player's last command starts the next track;
- reaching the end of the playlist stops.

## Consequences

- A track switch is likewise confirmed by length plus position near 0 (`SwitchDone`), never by
  the URI alone ([ADR-003](adr-003-switching-tracks-on-busy-gmrender.md)).
- `RelTime` comes in whole seconds, so lengths are compared with a tolerance (`durTolerance`).
