# ADR-008: CUE tracks as virtual WAV cut on the player side; seeking without a SEEKTABLE

**Status:** Accepted 2026-11-09 (written down 2026-11-10).
**Overcome:** fully. Seeking was measured before and after the fix.

## Context

A CUE sheet describes tracks inside one FLAC image. The renderer has to play one track of it,
seek within it, and show that track's length.

## Options

1. **Serve the whole image and `Seek` to the track start.** Rejected for two reasons:
   - time-based seeking on renderers is imprecise, so tracks would start early or late;
   - the renderer would show the length of the whole album.
2. **Cut the track on the player side.** The player serves a virtual WAV: a 44-byte header plus
   the PCM of the sample range from the track's `INDEX 01` to the next track's `INDEX 01`.

## Decision

Option 2.

- The next track's pregap stays at the end of the previous track.
- The length is known exactly, so `Content-Length` is honest.
- A Range request is mapped to a sample number. The decoder jumps to the right FLAC frame and
  drops the samples before the target.

**Finding the frame.** With a SEEKTABLE, it is looked up there.

Without a SEEKTABLE, the first version decoded the file from the start, which was too slow
(numbers below). This was replaced 2026-11-09 by bisection over frame headers:

1. jump into the byte range;
2. find the next valid frame header by its sync code and CRC-8;
3. compare its sample number with the target;
4. narrow the range and repeat.

Measured on a 24/192 track:

| | Decode from the start | Bisection |
|---|---|---|
| Range request at 5 minutes | 7.9 s | 0.23 s |
| Track switch (a seek to the end of the current track, [ADR-003](adr-003-switching-tracks-on-busy-gmrender.md)) | ~19 s | ~2 s |

## Consequences

- The bytes are identical to a sequential decode. A test encodes a FLAC without a SEEKTABLE and
  compares the two.
- The same WAV path carries the equalizer ([ADR-009](adr-009-equalizer-on-player-side.md)), so a
  plain FLAC file also goes through WAV while the EQ is on.
- Images other than FLAC are not supported yet ([ADR-001](adr-001-lossless-formats-beyond-flac.md)).
