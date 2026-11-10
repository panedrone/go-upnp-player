# ADR-005: Repeating a seek that gmrender ignored at the start of a stream

**Status:** Accepted 2026-11-09 (written down 2026-11-10).
**Overcome:** fully. Switching renderers mid-track and resuming after an update both land on the
right position.

## Context

Two features start a track and then need it at a given position:

- switching renderers while playing: the track continues on the new renderer at the old position;
- resuming after an update or restart of the player (2026-11-10).

gmrender silently ignores a `Seek` sent in the first moments of a stream: it answers OK and keeps
playing from 0. Switching renderers used to restart the track from the beginning because of this.

## Options

1. **One `Seek` right after `Play`.** Rejected: it is often ignored.
2. **A fixed delay, then one `Seek`.** Rejected: how long the renderer needs depends on the
   network and the device, so any fixed delay is either too short or wastes time.
3. **Wait for PLAYING, then repeat the `Seek` until the renderer reports the target position**
   (`seekWhenReady`, up to 6 tries, `services.SeekLanded`).

## Decision

Option 3.

When the renderer already holds the same track, there is no new stream to wait for. A single
`Seek` goes straight to the target ([ADR-003](adr-003-switching-tracks-on-busy-gmrender.md)).

## Consequences

- While paused, a renderer switch only remembers the position (`resumeAt`). The next Play starts
  the track on the new renderer there. Stop clears it ("start over").
- On quit or update while playing or paused, the position is saved as `playlist.position` in
  `config.json`. After the restart, Play continues there through the same path.
- A seek that lands late is one reason racing detection waits for three polls in a row
  ([ADR-007](adr-007-racing-gmrender.md)).
