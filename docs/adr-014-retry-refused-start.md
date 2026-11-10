# ADR-014: A refused start is retried for 10 s, and the page says so

**Status:** Accepted 2026-11-10 (1.0.10, local build).
**Overcome:** worked around by design. The renderer's refusal is a fact of the hardware, and the
player now waits it out instead of failing. **Open:** not yet tried live with the amplifier's input
switched late.

## Context

The BeagleBone answers `Play` with `501 Playing failed` while its USB DAC is not usable. The
everyday case is the owner's: the amplifier (Topping MX3s) is still on its coaxial input when
playback is switched to the BeagleBone. Switching the input to USB on the remote takes a few
seconds. Until then every `Play` is refused.

foobar2000 fails the same way, and in both programs the owner "got slapped" for being a few
seconds early. The same `501` also comes when `Play` follows `Stop` too closely
([ADR-013](adr-013-stop-safe-renderers-start-like-foobar.md)); `startDirect` already retries that
up to 4 times within ~2 s.

## Options

1. **Report the error at once** (the previous behaviour, and foobar2000's). Rejected: it punishes a
   normal sequence of actions.
2. **Keep retrying inside the command for as long as needed.** Rejected:
   - it would hold the command lock, so Stop could not cancel it;
   - the request would hang with no explanation on the page.
3. **Retry in the background for a bounded time and say so on the page.** The owner asked for
   10 s: a minute was too long.

## Decision

Option 3.

- When the renderer has taken the URI but refuses every `Play`, the start becomes pending instead
  of failing. The playlist index, the intent to play and the wanted position are kept.
- The poll retries the start every 2 s (`services.RetryStart`), for at most 10 s
  (`StartRetryEvery`, `StartRetryFor`). The poll does nothing else meanwhile.
- **The page states it honestly, without alarm.** The transport badge shows a gray CONNECTING
  (the owner's choice: RETRYING sounded alarming). The Output card says "Connecting to <renderer>"
  with the attempt number, the seconds left, and the likely cause ("is its output switched on
  (the amplifier input)?").
- **Success:** the track plays from the wanted position, and the note disappears.
- **After 10 s:** a real error ("refused to play N times in 10s …"). The position is kept for the
  next Play.
- **Stop** ends the retries. **Pause** ends them and keeps the position. **Switching renderers**
  drops them.

## Consequences

- A refusal that is not about the DAC, such as a hung renderer
  ([ADR-006](adr-006-hung-gmrender.md)), is reported 10 s later than before.
- Only a refused `Play` is retried. A failed `SetAVTransportURI` or an unreachable renderer is
  still an immediate error.
