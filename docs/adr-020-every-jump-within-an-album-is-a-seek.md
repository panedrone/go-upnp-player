# ADR-020: Every jump within an album is a seek: no new stream, no click on coax

**Status:** Accepted 2026-11-14 (1.1.20). ~~A jump over playing music is a plain seek~~: on the
FiiO that grunted; it is the player's Stop and a start there since
[ADR-022](adr-022-a-jump-is-stop-then-start.md) (the run from the album's start stays).
**Overcome:** live on the BeagleBone over coax (the owner jumped between tracks of an album by
hand: no click); in tests (a start mid-album, then a jump back, sends no Stop and no new stream; the
run's silent lead is served to its first request only; one odd position report moves nothing).

## Context

Clicks on the coaxial output came back on track changes, "not always". Listening on the
BeagleBone with a temporary mark button pressed on each click heard, through 12 random Next / Prev /
clicks on tracks, 2026-11-14:

- **No mark after any of the 10 jumps within the stream** (a `Seek`).
- **A mark 2–3 s after both jumps to a track before the run's start**. Such a jump needs a new
  stream: `Stop → SetAVTransportURI → Play` on the BeagleBone, which closes its sound output; the
  DAC on S/PDIF loses the signal and clicks while it locks again. A run started at the chosen
  track ([ADR-016](adr-016-clicks-between-tracks-on-fiio-coax.md)), so Prev from its first track
  or a click on any track above it was such a jump.
- **A song cut short now and then.** gmrender sometimes answers `GetPositionInfo` with a position
  far behind where it plays (`0:00:00`). Taken at its word, that looked like a stream that
  strayed from the playlist, and the player seeked to the next track: a new connection, a fade,
  and a song cut short (07:31:17, Love On The Run). A test with one such answer reproduced it.

## Options

1. **Leave it:** clicks only on jumps back past the run's start. Rejected by the owner.
2. **Switch the BeagleBone through its gapless path instead of Stop.** Rejected: ADR-013 moved it
   to Stop because the gapless path was the suspected trigger of its racing, and a new stream is
   a new lock on S/PDIF whatever way it comes.
3. **Open the run at the album's first track and seek to the chosen one.** Chosen. A jump anywhere
   within the album is then a seek, which was silent every time.

## Decision

- A start (`planStart`) opens the run at the first of the tracks before the chosen one that go
  on in the same format (up to half the RIFF limit back). The queued next run is not widened: it
  starts where the format changes, as before.
- The run's first request, from the stream's start, gets silence for those earlier tracks, then
  the music, faded in as any stream start is. Every later request, a seek back included, gets
  the music. So the seek after the start (`seekLead`) never lets an earlier song be heard: until
  it lands, the renderer plays silence. gmrender drops a seek in the first moments of a stream
  and, right after a start, still reports the previous stream's position (ADR-005); the first
  build took that for a landed seek and left the renderer in the silent lead. The seek now goes
  once the renderer reports the new stream (its URI and length) playing, and counts as landed
  only when that stream stands at the chosen place; it is repeated until then. Live on the
  BeagleBone the music came about 2 s after Play.
- A start on an album's first track, or with nothing of the same format before it, is as before:
  no lead, a stream that begins at the position ([ADR-015](adr-015-start-at-position-as-a-stream.md)).
- One position report that goes back by more than a second and a half, with no seek, no new
  stream and no command in the last 3 s, is set aside and logged; the same on the next poll is
  taken. Running ahead stays with the racing check ([ADR-007](adr-007-racing-gmrender.md)).

## Consequences

- A start mid-album takes a moment longer: the seek past the silent lead.
- The renderer shows the run's whole length, from the album's first track.
- New streams, and their click on coax, remain at the first Play, at a format change and at a
  jump to another album.
