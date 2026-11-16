# ADR-028: DSD files are converted to PCM in the player, up to 176.4 kHz

**Status:** Accepted 2026-11-15 (1.1.35). Heard on the BeagleBone 2026-11-16 (through an
Amanero-type card, coax into the SMSL RAW-HA1): plays. **Open:** not yet heard on the FiiO SR11.
DSD images with a cue sheet, SACD `.iso` images,
DST-compressed `.dff` and multichannel DSD are not played.

How it works now: [SAD §4](go-upnp-player-sad.md#4-serving-tracks) and §7.

## Context

[ADR-001](adr-001-lossless-formats-beyond-flac.md) proposed DSF/DFF as pass-through, offered only
to renderers that declare DSD (the FiiO SR11), with no conversion in the player. The library holds
43 DSF files (DSD64 and DSD128, stereo) and one SACD `.iso`.

The owner's view (2026-11-15): the player does not cater for DACs that take DSD only over USB. The
streamers it is for feed a DAC over S/PDIF (coax), which carries PCM up to 192 kHz: neither native
DSD nor DoP (DSD packed into PCM frames, which needs a DAC that unpacks it and a renderer that
passes the frames on untouched, no volume, no resampling) gets there. foobar2000 plays such files
to the same chain through foo_input_sacd's DSD → PCM, with these settings: PCM at 88.2 or
176.4 kHz, +0…+3 dB rather than its +6 (which clips loud masters), the multistage converter.

## Options

1. **Pass-through to renderers that declare DSD** (ADR-001's proposal). − Only the FiiO; what it
   does with DSD is its own business (very likely PCM inside GStreamer, at a rate we do not
   choose); no EQ; nothing on the BeagleBone.
2. **DoP.** − As above: a DAC that unpacks it, and gmrender's volume or any resampling turns it
   into noise. Rejected by the owner: "we do not support devices reachable only over USB".
3. **DSD → PCM in the player.** + Every renderer, the EQ, seeking and the one-stream runs
   (ADR-016) as for FLAC; the rate and the gain are ours. − Not bit-perfect by nature (DSD has no
   PCM samples to keep); a converter of our own.

## Decision

Option 3.

- **Rates:** 44.1, 88.2 or 176.4 kHz, chosen in the DSD card; 176.4 by default (the owner's
  choice, 2026-11-15: it sounds better to him than 88.2). Only the 44.1 kHz
  family: DSD64…DSD512 are its multiples, so the conversion is whole-number decimation, no
  resampling. 192 kHz would need a resampler for nothing (the streamers' S/PDIF ends at 192). A
  DSD file of the 48 kHz family (3.072 MHz) lands on its own family's nearest rate.
- **Gain:** 0, +3 or +6 dB; +6 by default, as in foobar2000 (the owner's choice, 2026-11-15).
  The SACD's 0 dB is 50 % modulation, −6 dBFS in PCM: +6 brings it to full scale and clips loud
  masters, 0 never clips. Clipped samples are counted and logged once per stream.
- **Converter (the owner left it to me):** pure Go, in `services`. A FIR straight on the bits
  through per-byte tables, decimating by 8; FIR halvings down to the PCM rate; a last low-pass.
  Linear phase, Kaiser-windowed sinc, 120 dB stopbands, the band kept up to 35 kHz (below 20 kHz
  at 44.1): above it there is only the modulator's noise, which DSD pushes up there, and which
  the amplifier's tweeters do not need. This is foobar2000's "multistage" idea; its "direct, 30 kHz
  low-pass" was not copied, as 35 kHz keeps the band of a 88.2/176.4 PCM master. float64
  throughout, 24-bit out. About 19× real time for DSD128 on the test machine.
- **Seeking:** a stream that starts at a sample starts converting a little earlier and drops the
  filters' run-in and delay: it gives the same samples as a stream played from the start.
- **Files:** `.dsf` (Sony) and `.dff` (Philips DSDIFF), mono or stereo, plain DSD; tags and cover
  from their ID3v2 tag. No DST, no multichannel, no `.iso`, no cue sheets over DSD for now.
- **Settings:** on the server in a file of their own, `dsd.json` beside `config.json`. A change
  restarts a playing DSD track where it is, so it is heard at once (at first it applied only from
  the next stream, and the owner heard nothing until he changed albums). A new rate first rescans
  the library (from its cache), since the rate is part of each DSD track.

## Consequences

- A DSD track is a PCM track to the rest of the player: its rate, 24 bits, its length in PCM
  samples. The EQ, the fade-in, runs and seeking needed nothing new.
- The player cannot call DSD playback bit-perfect; the README says DSD plays converted.
- ADR-001's DSD line is superseded; its other formats stay as proposed there.
- The Qt player (postponed) plays DSD with the default settings and has no DSD card.
