# ADR-033: Center: the equalizer's curve moved down to an average of 0 dB, on request

**Status:** Accepted 2026-11-20. **Open:** the owner's listening check of a centred curve.
**Overcome:** measured on a loud DSD track (below), and in tests (`TestCenterEQ*`, the `equalizer`
step of the endpoint walk).

## Context

Measured on 2026-11-20 on Metallica "Orion" (DSF, DSD128 → 176.4 kHz / 24 bit), the stream
exactly as the player makes it:

| | DSD gain | peak | RMS | samples above −1 dBFS | seconds with the limiter at work |
|---|---|---|---|---|---|
| equalizer off | +6 | −0.99 dBFS | −20.6 dBFS | 1 | 1 of 504 |
| the owner's curve (sliders up to +11 on the treble) | +6 | 0 dBFS | −14.5 dBFS | 247 874 | 399 of 504 |
| the owner's curve | +3 | 0 dBFS | −17.5 dBFS | 19 270 | 325 of 504 |
| the same curve centred (−4 dB) | +3 | −1.66 dBFS | −25.4 dBFS | 0 | 0 of 504 |

The DSD conversion is clean. The overload is the equalizer: with no preamp (ADR-009) a boost
reaches the soft limiter, which rounds off peaks on loud passages. The sliders understate the
boost: with Q = 2 the half-octave bands overlap, and three neighbours at +11 make a curve of about
+21 dB.

## Options

1. **An automatic preamp** (the stream lowered behind the sliders). Rejected on 2026-11-09
   (ADR-009): the music fell by that much with nothing on screen saying so.
2. **Lower the DSD gain.** Helps DSD a little (the third row), does nothing for PCM files, and the
   conversion was not what clipped.
3. **Fit: lower every slider by the curve's peak** (its frequency response at 44.1–192 kHz up to
   20 kHz). Built, measured and removed the same day: for the owner's curve it moved every slider
   down by 21, the middle pinned at −20, and Orion came out at −24.8 dBFS peak and −50.3 dBFS RMS.
   No limiter, but 30 dB quieter and sliders that "no equalizer anywhere looks like" (the owner).
   Music has little energy at 14–20 kHz, so a fit by the curve's peak leaves 24 dB unused.
4. **Center: lower every slider by the curve's average.** Chosen: the shape stays natural, the
   loss is a few decibels, and on the measured track the limiter has nothing left to do.

## Decision

`services.CenterEQ` lowers every band by the average of the 18 gains, rounded to a whole decibel
(the sliders' step); a curve averaging 0 or below is left alone, and a band stops at −20.
`POST /api/eq/center` saves the result and answers it. The page shows it as Center among the
presets. It moves the sliders, once, on request: what changed is visible and can be undone by hand.

## Consequences

- A treble- or bass-heavy curve loses its average boost; the music is that much quieter, made up
  on the amplifier.
- Center does not guarantee that the limiter stays idle: a loud master and a steep curve can still
  reach it. It is the cheap, natural-looking step; a gentler curve is the rest.
