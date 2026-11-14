# ADR-021: Stop keeps the renderer's sound output open

**Status:** ~~Accepted 2026-11-14 (1.1.20)~~ superseded by [ADR-025](adr-025-stop-is-stop.md)
(2026-11-14): its open item came true, the BeagleBone hung three times on the path without
Stop; `keepOutputOpen` is gone. Accepted 2026-11-14 (1.1.20). **Open:** evenings of listening with no racing
episode in the log ([ADR-007](adr-007-racing-gmrender.md)), long pauses after Stop included; an
episode is the signal to reconsider.
**Overcome:** live on the BeagleBone over coax, 3 of 3 Stop → Play with no click (the owner,
2026-11-14); before the change every such start clicked.

## Context

After [ADR-020](adr-020-every-jump-within-an-album-is-a-seek.md) jumps within an album were
silent, but every Play after Stop clicked on the coaxial output. Delaying the start's seek by
3 s moved nothing: the click came about 2 s after Play, before the seek and before any music.
That is when the BeagleBone, its buffer filled, opens its sound output after a Stop closed it; the
DAC on S/PDIF locks again and clicks. A lead of ±1 LSB instead of digital zero did not help
either, so it is not a DAC muting on silence.

## Options

1. **Leave Stop a real Stop** ([ADR-013](adr-013-stop-safe-renderers-start-like-foobar.md)).
   Rejected: the owner presses Stop and Play himself sooner or later; every such Play clicked.
2. **Keep the output open: Stop pauses on silence, a start does not send Stop.** Chosen.
   It is how the FiiO, which cannot take Stop, was driven all along (ADR-002, ADR-003).

## Decision

- A renderer that survives Stop is driven like one that does not (`keepOutputOpen`): Stop is
  `Pause`, the stream is let go so that what the renderer holds is silence, and it is seeked
  where it is. A start from there is a seek in the same stream and `Play` when the album is the
  same, and the gapless path otherwise; no `Stop`, so the output stays open.
- This takes back ADR-013 for the BeagleBone, which moved it to Stop because the gapless path and
  an idle paused pipeline were suspected of its racing. The racing check stays and logs any
  episode; one is the signal to reconsider.

## Consequences

- A click is left where the output does open: the first Play after the renderer is switched on
  or the player is started, and a new stream (another album or format) if the renderer closes
  its output between streams.
