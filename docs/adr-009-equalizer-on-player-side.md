# ADR-009: Equalizer on the player side, foobar2000's band grid, soft limiter instead of preamp

**Status:** Accepted 2026-11-09 (written down 2026-11-10).
**Overcome:** fully. Loudness was measured on a real track.

## Context

The renderers offer volume only: gmrender's UPnP RenderingControl has no equalizer. The owner's
own EQ curves live in foobar2000.

## Options and what was rejected

**Where the EQ runs**

- On the renderer: impossible, there is nothing to control.
- **In the player:** the EQ is applied to the WAV stream the renderer pulls. Chosen.

**Bands**

- 10 bands, 31 Hz to 16 kHz, ±12 dB. Rejected 2026-11-09 as not enough.
- **18 half-octave bands, the same grid as foobar2000:** 55·2^(n/2), from 55 Hz to ~19.9 kHz,
  ±20 dB. Chosen.

**Filter Q**

- Q = 2.87, the textbook value for a half octave. Rejected: it leaves dips between adjacent bands.
- **Q = 2.0** (peaking biquads from the RBJ Audio EQ Cookbook). Chosen.

**Overload**

- **Preamp equal to minus the largest boost.** Rejected 2026-11-09. A +18 dB treble boost became
  "the whole track −18 dB, treble unchanged": near silence instead of sparkle.
- **No overall attenuation, a soft limiter instead.** The signal is linear up to −1 dBFS; above
  that, peaks are rounded off with tanh and never exceed full scale. Chosen. Measured on
  "Never Say Die" with the same settings: loudness +1.6 dB instead of −18 dB.

**Presets**

- Built-in Hard and Soft presets. Replaced 2026-11-09.
- **Load and save foobar2000 `.feq` files** (one integer dB per line, 18 lines, CRLF), done
  entirely in the browser. Chosen.
  - A file with a different number of values is refused, not remapped.
  - A saved set for a different band grid is reset to zero, not remapped.

## Decision

As chosen above.

- A band is skipped only above 0.475·fs, so the top band works at 44.1 kHz too.
- The settings live in an `atomic.Pointer`. Open streams pick them up from the next FLAC frame.

## Consequences

- The renderer buffers several seconds. After an EQ change the player re-requests the stream from
  the current position (`Seek`).
- Turning the EQ on or off for a plain FLAC track changes the stream type (FLAC or WAV), so that
  track is restarted at the same position.
- With the EQ on, the stream is no longer bit-perfect. Pass-through formats in
  [ADR-001](adr-001-lossless-formats-beyond-flac.md) would play without the EQ.
