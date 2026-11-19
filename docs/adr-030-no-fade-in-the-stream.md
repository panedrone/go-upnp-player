# ADR-030: No fade in the stream: the renderer gets the file's own samples

**Status:** Accepted 2026-11-19. Amends [ADR-016](adr-016-clicks-between-tracks-on-fiio-coax.md)
(its 1.1.5 fade-in) and the fade clause of
[ADR-020](adr-020-every-jump-within-an-album-is-a-seek.md). **Open:** the owner's live check that
no new click appeared at a start or a seek, on both renderers and on both outputs.
**Overcome:** in tests; every byte-exact test in `internal/stream` now compares the stream against
the file with nothing switched off.

## Context

Since 1.1.5 the first 10 ms of every stream rose out of silence (`fadeMs`, `Reader.fadeIn`),
against the click heard when a stream started mid-wave on the FiiO's coaxial output
(ADR-016, 2026-11-11). A run's later parts were exempt, so the fade applied at a start and at
every seek that opened a new stream.

It multiplied 441 samples per stream start by a rising gain. That is the only place where the
player altered samples with the equalizer off, and the byte-exact tests had to turn it off
(`noFade`) to compare a stream against its file at all.

The measurement that settles it was made on the BeagleBone on 2026-11-19 (the renderer's own
repository): with the sound device left open after a stream played out, gmrender's `playbin`
resamples the next stream of another rate into the one the device is stuck at — a whole track,
every sample, with no error logged. Next to that, 441 samples are noise, and the owner's
instruction is that the stream is to be the file's own data.

## Options

1. **Keep the fade.** Rejected: it alters samples in the one path that is supposed to alter
   nothing, and the click it was for belongs to S/PDIF relock, which USB does not have (ADR-016).
2. **Remove it; let a start be a step into the wave.** Chosen.
3. **Fade only where the output is coaxial.** Rejected: the player cannot know how a renderer is
   wired, and a setting for it would be a second way to lose bit-perfect silently.

## Decision

`fadeMs`, `Reader.fade`, `Reader.faded`, `Reader.fadeIn` and the run's `opened` flag are gone. A
stream carries the file's samples from its first one. The lead silence of a start in the middle of
an album (ADR-020) stays: it adds samples before the music, it does not change them.

## Consequences

- A stream that opens mid-wave steps into it. On a coaxial output that step may click again, where
  ADR-016 heard it; on USB it cannot.
- `TestStreamStartFadesIn` and `TestRunSeamIsNotFaded` are gone, and the tests that compared bytes
  through `noFade` now hold with no helper: that is the bit-perfect guarantee itself.
- The equalizer and the DSD conversion are untouched: both are asked for, both are off by default
  for PCM.
