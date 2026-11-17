# ADR-029: DSD to PCM at every rate up to 192 kHz, both families

**Status:** Accepted 2026-11-16 (1.1.40). Replaces the Rates point of
[ADR-028](adr-028-dsd-converted-to-pcm-in-the-player.md). **Open:** 48, 96 and 192 kHz not yet
heard on the streamers.

How it works now: [SAD §4](go-upnp-player-sad.md#4-serving-tracks) and [the UI SAD](go-upnp-player-ui-sad.md).

## Context

ADR-028 offered only the 44.1 kHz family (44.1, 88.2, 176.4) for DSD: DSD64…DSD512 are its
multiples, so whole-number decimation was enough, and 192 kHz was called "a resampler for
nothing". That reasoning came from advice (an AI assistant's, Gemini's) that the owner does not
follow. The owner, 2026-11-16: offer every rate foobar2000 offers for DSD up to 192 kHz; he
likes 192.

## Options

1. **Keep the 44.1 kHz family only** (ADR-028). − Not what the owner wants; foobar2000 offers the
   48 kHz rates too.
2. **Every foobar2000 rate up to 192 kHz:** 44.1, 48, 88.2, 96, 176.4, 192. + The owner's choice.
   − A rate of the other family needs a rational resampler.
3. **Above 192 (352.8, 384 and foobar2000's bracketed DSD-rate entries).** − Past the S/PDIF limit
   of the streamers this player is for (ADR-028's context); not offered.

## Decision

Option 2, by the owner.

- **Rates:** 44.1, 48, 88.2, 96, 176.4 and 192 kHz in the DSD card; 176.4 stays the default.
- **Conversion:** within the DSD's own family nothing changed (whole-number decimation). For the
  other family the decimations stop at the same place in the DSD's family (176.4 kHz for 192 from
  DSD64) and a polyphase resampler takes it the rest of the way, 160/147 up or 147/160 down:
  Kaiser-windowed sinc, 120 dB, linear phase, as the rest of the chain. The band kept is up to
  35 kHz, below 90 % of the lower Nyquist frequency of the two rates (19.8 kHz for 48 kHz from
  DSD64). A 3.072 MHz DSD now lands on the chosen rate exactly, no longer on its own family's
  nearest one.
- **Seeking:** a stream starts on a whole cycle of the resampler (160 output samples for 192 from
  DSD64), so a stream started anywhere still gives the same samples as one from the start.
- **Cost (measured 2026-11-16):** DSD128 converts about 26× real time to 192 kHz against 32× to
  176.4 kHz on the test machine.

## Consequences

- A DSD track at 48/96/192 kHz is resampled; to the rest of the player it is still a PCM track of
  the chosen rate.
- The tests cover the resampled rates: a 1 kHz sine keeps its level and phase, the chunked and the
  mid-stream starts give the same samples.
