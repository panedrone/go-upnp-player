# ADR-015: A start at a position is a stream that begins there

**Status:** Accepted 2026-11-10 (1.0.10, local build). Supersedes
[ADR-005](adr-005-seek-ignored-at-stream-start.md) for track starts.
**Overcome:** fully in tests. **Open:** the owner's live check of switching back to the
BeagleBone from the FiiO.

## Context

Three things start a track somewhere in its middle:

- switching renderers mid-track;
- Play after an update or a restart of the player;
- the cure of a racing renderer.

Until now every such start played the track from 0 and then seeked
([ADR-005](adr-005-seek-ignored-at-stream-start.md)). On the BeagleBone the opening of the track
was audible for about a second and a half before the jump. A trace of the renderer's RelTime
showed it plainly: PLAYING at 0:00–0:01, then the target position.

## Options

1. **Play, then seek** (as before). Rejected: the opening is heard.
2. **`Pause` from STOPPED** (a silent preroll), then `Seek`, then `Play`. Rejected: gmrender
   refuses `Pause` from a real STOPPED. It only seemed to work in a manual test, while the renderer
   still reported PLAYING right after a `Stop`.
3. **Mute until the seek lands.** Rejected by the owner as a crutch. A renderer left muted would
   also play silence until someone noticed.
4. **Serve a stream that begins at the position.** The URL carries `&from=<ms>`. The player serves
   a WAV of the track from that point to its end, with an honest length. This is the same span
   machinery that cuts CUE tracks out of an image, only with a later start. The renderer plays it
   from its own start, so there is no seek and nothing to hear before the position.

## Decision

Option 4.

**Building the stream**

- `media()` adds `&from=<ms>` for a start past 1 s. Such a stream is always WAV, even for a plain
  FLAC file.
- The DIDL duration is the remaining length.
- `stream.ForTrack` starts the span at `services.FromSample`.

**Reading the renderer**

- The player remembers `offset` (where the current stream starts).
- The poll adds it to the renderer's RelTime and TrackDuration for the position and length shown
  in the UI.
- The advance decision ([ADR-004](adr-004-detecting-track-advance.md)) works on the renderer's own
  figures, with the current track's expected length shortened by the offset.
- On an advance the offset returns to 0: the next track is queued as a plain stream.

**Seeking**

- A user seek at or after the stream's start is relative to it (`Seek(sec − offset)`).
- A seek before the stream's start starts the track again from there. Prev and Seek(0) start a
  plain stream.

## Consequences

- No seek follows a start any more. `seekWhenReady` and the check against a stale RelTime are gone.
  ADR-005's repeated seek was a workaround for exactly that seek.
- A seek backwards before the stream's start restarts the track. On the FiiO that goes through
  its next-URI path ([ADR-003](adr-003-switching-tracks-on-busy-gmrender.md)).
- A plain FLAC file started at a position plays as WAV until the next track. The samples are
  identical, and a test compares the stream with the tail of the whole one.
