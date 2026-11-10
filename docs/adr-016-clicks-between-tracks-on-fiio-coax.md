# ADR-016: One stream for the tracks of one format, as foobar2000 sends it

**Status:** Accepted 2026-11-10 (1.1.4, local build). **Open:** the owner's live check on the
FiiO's coaxial output; the BeagleBone with runs; the owner's live check of a switch from the
BeagleBone back to the FiiO. A trace on 2026-11-11 showed what clicked there: the FiiO, left
paused, let out the moment of music it held before the switch, and the new stream started
mid-wave. Fixed in 1.1.5 (a left renderer holds silence; every stream fades in over 10 ms):
the owner heard no clicks there on 2026-11-11.
**Overcome:** in tests: the run is the tracks' PCM back to back, the equalizer carries over a seam
between files, Next/Prev within a run are seeks.

## Context

On the FiiO SR11's coaxial output a loud click is heard at track boundaries. Its USB output to the
same DAC chain is silent there.

What was measured on 2026-11-10 (a CUE album, equalizer on, so WAV streams):

- **The served audio is clean at the boundaries.** The last samples of a track and the first of
  the next differ by 1–7 out of 32768, near silence on both sides. No step in the data.
- **The player answers at once.** The first bytes of a track stream go out in 3–5 ms.
- **The FiiO opens the next stream late.** A trace of its transport and of its connections to the
  player: it read the current stream to the end 5 s before the track ended and opened the next URI
  only at the last second. The transport stayed PLAYING throughout: its gapless path, started too
  late for its output buffer.

So the renderer's output runs dry for a moment at the boundary. Over S/PDIF the clock travels with
the signal: the receiving DAC loses lock and clicks. Over USB the DAC keeps its own clock and plays
the moment of no data as silence.

foobar2000 does not click on the same FiiO. Its "UPnP MediaRenderer Output" decodes and joins the
tracks itself and sends the renderer one endless stream: the renderer never switches.

## Options

1. **Mute around the boundary.** Rejected: it cannot help. The samples at the measured boundaries
   are silence already, and the click still comes; it is the break in the S/PDIF signal that
   clicks, not the audio in it. The owner also called muting undesirable here.
2. **Leave it to the renderer.** Rejected: the owner wants the clicks gone.
3. **One stream per format, as foobar2000 does.** Chosen. The owner's first impression was that
   it reads as a crutch; it was taken once it turned out to be what foobar2000 does.

## Decision

The renderer gets **runs**: the playlist from the chosen track on, for as long as sample rate,
channels and bits stay the same, as one WAV (`/media/run/<id>.wav`). A run ends where the format
changes (a new format is a new stream anyway, the DAC relocks there) or before the RIFF size field
would overflow. The next run is queued with `SetNextAVTransportURI`, as tracks were.

- The player turns the renderer's position in the stream into the track and its position.
- Next, Prev, a click on a track of the run and a seek within it are a `Seek` in the stream: no
  stream switch, nothing breaks in the output.
- Tracks of one CUE image are decoded as one range. Between two files the equalizer's filters
  carry over instead of starting cold.
- A switch to a track outside the run goes through the renderer's gapless path, which plays the
  last second of the current stream first: the end of the run, another song. The player lets
  that stream go before the switch, and serves silence for the rest of it
  ([ADR-003](adr-003-switching-tracks-on-busy-gmrender.md)).
- Every run is WAV decoded by the player; a FLAC is no longer passed through as is. That is what
  foobar2000 sends too, and the equalizer applies everywhere.

## Consequences

- The renderer shows the run's first track as its title for the whole run, and the run's length.
- A playlist edited while a run plays: the run goes on, and where its next track is not the
  playlist's next, the player starts the playlist's one.
- ADR-004 (detecting an advance) and ADR-003 (switching a busy renderer) now apply between runs.
