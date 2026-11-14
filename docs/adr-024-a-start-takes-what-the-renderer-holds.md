# ADR-024: A start goes by what the renderer holds: from STOPPED, no lead and no seek

**Status:** Accepted 2026-11-14 (1.1.22). Supersedes the start of
[ADR-020](adr-020-every-jump-within-an-album-is-a-seek.md). **Open:** the live check on the FiiO
SR11, a start mid-album from STOPPED. On the BeagleBone a jump back past the stream's start clicks
on coax again (below).

## Context

ADR-020 opened every start mid-album at the album's first track, with a silent lead, and then
seeked to the chosen track. That way a later jump anywhere within the album was a seek in the same
stream. After [ADR-023](adr-023-the-end-of-the-playlist-switches-off.md) a played-out FiiO stands
in STOPPED, so every next start took that path.

The owner heard it: from STOPPED, Play went to something else first, then back to the track's
start, then played. The FiiO grunts on a seek over playback, and the seek past the lead was one.

Pausing in the lead before that seek did not help. The FiiO reports its position in whole seconds,
so a renderer still playing looks still between polls.

The owner's point: the renderer reports STOPPED, and that settles it. It holds nothing to seek
in, and nothing of the player's needs restoring there.

## What was tried and rejected

- **The seek past the lead over playback** (1.1.20 – 1.1.21). It grunted on the FiiO.
- **Pause in the lead, then seek, then Play, each "confirmed"** (local, 2026-11-14). It grunted.
  The pause lasted under 0.3 s, and the confirmation could not tell.
- **"Still" counted only over more than a second of reports.** Rejected by the owner: rules of
  this kind were made before and took long to undo.

## Decision

- What the renderer reports decides the start (`heldRun`):
  - **It holds the player's current stream** (busy, its URI and length), and the chosen track is
    in it: a restart in place there. That is the owner's Stop-then-click flow
    ([ADR-022](adr-022-a-jump-is-stop-then-start.md)). A stream Stop let go to silence is music
    again (`stream.KeepRun`).
  - **Otherwise** (STOPPED, nothing, another stream): a stream that begins at the chosen track,
    or at the position in it ([ADR-015](adr-015-start-at-position-as-a-stream.md)). It is started
    with `SetAVTransportURI` and `Play` from STOPPED, or through the gapless path from a busy
    renderer. No lead, no seek.
- `planStart` with its walk back to the album's first track, and the seek past the lead
  (`seekLead`), are gone.

## Consequences

- A start from STOPPED is what foobar2000 does: the track, from its start.
- Jumps to a later track of the stream, and the slider, stay restarts in place.
- A jump back past the stream's start (Prev from its first track, a click on a track above it)
  needs a new stream. On the BeagleBone that closes the sound output, and the DAC on coax clicks.
  ADR-020 had removed that click. It is back for this one kind of jump.
