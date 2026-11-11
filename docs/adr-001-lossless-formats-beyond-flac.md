# ADR-001: Lossless formats beyond FLAC (DSF/DSD, WAV, AIFF, WavPack, APE, ALAC)

**Status:** Proposed (2026-11-09). Nothing implemented yet. Open: check what the FiiO SR11
actually does with DSF pass-through (native DSD or conversion to PCM inside GStreamer);
pick a pure-Go decoder for WavPack/ALAC or leave them pass-through only; APE stays
pass-through only (no pure-Go decoder).

## Context

The player handles FLAC only: plain FLAC files are streamed as is, CUE tracks are cut from a
FLAC image and served as WAV (SAD §4). Libraries also contain other formats that carry the
original samples exactly:

| Format | Extension | Kind | In the test library |
|---|---|---|---|
| DSD (Sony DSF, Philips DSDIFF) | `.dsf`, `.dff` | 1-bit DSD, not PCM | 43 DSF |
| WAV, AIFF | `.wav`, `.aif(f)` | uncompressed PCM | 1 WAV |
| WavPack | `.wv` | lossless PCM | 1 |
| Monkey's Audio | `.ape` | lossless PCM | 0 |
| ALAC | `.m4a` | lossless PCM in MP4 (the same container is also used for lossy AAC) | 0 |
| TTA | `.tta` | lossless PCM | 0 |

What the renderers declare in `ConnectionManager.GetProtocolInfo` (Sink), 2026-11-09:

| MIME | FiiO SR11 | BeagleBone |
|---|---|---|
| `audio/x-dsd`, `application/x-gst-av-dsf` | yes | — |
| `audio/wav`, `audio/x-wav`, `audio/aiff` | yes | yes (`audio/x-wav`, `audio/aiff`) |
| `audio/x-wavpack` | yes | yes |
| `application/x-ape` | yes | — |
| `audio/alac`, `audio/x-alac` | yes | yes |
| `audio/x-tta` | yes | yes |

"Bit-perfect" here means the renderer receives the original PCM samples unchanged: true for
any lossless PCM codec as long as nobody resamples or applies the EQ. DSD is a different
signal (1-bit, 2.8 MHz and up). Through UPnP/HTTP to a GStreamer renderer it is very likely
converted to PCM on the renderer, so bit-perfect DSD cannot be promised here, only "DSD files
play".

**Out of scope for good:** lossy and mobile formats — MP3, AAC (including lossy `.m4a`),
Ogg Vorbis, Opus, WMA. The library scan must not list them, and an `.m4a` is accepted only if
its codec is ALAC. The reason is the owner's (2026-11-12): the player is for people who bought a
network streamer to take the computer out of the audio path (galvanic or optical isolation) and
want the original samples on it; sending lossy files over such a link makes no sense. The README
says so instead of "FLAC only".

## Options

1. **Pass-through.** Serve the original file and let the renderer decode it, when the renderer
   declares the format's MIME (the way plain FLAC already works).
   - \+ no decoders in the player; bit-perfect for lossless PCM; DSF reaches the FiiO as is.
   - − no CUE slicing of such images; the EQ cannot apply; depends on each renderer
     (the BeagleBone has no DSD and no APE).
2. **Decode in the player to WAV**, the same path as CUE tracks.
   - \+ CUE images in these formats, the EQ, uniform behaviour on every renderer.
   - − needs a pure-Go decoder per format: WAV/AIFF are trivial; ALAC and WavPack have
     third-party Go decoders of uneven maturity; APE has none. DSD needs DSD→PCM conversion
     (a decimation filter the player would own), which is not bit-perfect by definition.
3. **External ffmpeg** for decoding.
   - \+ every format at once.
   - − a second binary next to the single exe and a process per stream; rejected as it breaks
     the "one exe, nothing to install" property.

## Decision (proposed)

A hybrid of 1 and 2, decided per format:

- **WAV, AIFF:** decode in the player (option 2) — trivial, gives CUE images and the EQ.
- **WavPack, ALAC, TTA:** pass-through to renderers that declare the MIME (option 1). Move to
  in-player decoding only after a pure-Go decoder passes a sample-exact comparison with the
  reference decoder.
- **APE:** pass-through only, and only to renderers that declare it (FiiO); hidden for the
  others.
- **DSF/DFF:** pass-through only to renderers that declare DSD (FiiO); hidden for the others.
  No in-player DSD→PCM in the first step. Not presented as bit-perfect.
- When the EQ is on, pass-through tracks play without it, and the UI says so.
- A format a renderer cannot take is not offered for that renderer (greyed out in the tree),
  rather than failing on Play.

## Consequences

- The library scan learns the new extensions and reads their duration and format (headers
  only, no decoding at scan time); `.m4a` needs a codec check.
- `media()` picks pass-through vs WAV per track and per renderer, using the Sink list it
  already reads for MIME selection.
- To be verified before this ADR is accepted: DSF pass-through on the FiiO (does it play, how
  does seeking behave, is the output DSD or PCM), and a WavPack file on both renderers.
