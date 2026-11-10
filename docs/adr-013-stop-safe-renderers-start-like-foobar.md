# ADR-013: Renderers that survive Stop are driven the way foobar2000 drives them

**Status:** Accepted 2026-11-10 (1.0.9, local build). Amends
[ADR-002](adr-002-never-stop-gmrender.md) and [ADR-003](adr-003-switching-tracks-on-busy-gmrender.md)
for those renderers only.
**Overcome:** looks so, not yet confirmed. First live use (2026-11-10, a few minutes): the owner
heard no fault, and the player's log has no racing episode. The same session found a side effect,
fixed in 1.0.10: switching back sometimes started the track from 0 (see Consequences). **Open:**

- the old trigger has not been repeated deliberately: switch to the FiiO, wait several minutes,
  switch back;
- one short session is not proof for a fault that came and went.

The racing detector ([ADR-007](adr-007-racing-gmrender.md)) stays as a safety net. If an episode
still happens, the log will show it.

## Context

foobar2000 drives the same BeagleBone flawlessly. It knows nothing about the board except that it
is a network renderer, and it always starts playback with `Stop → SetAVTransportURI → Play`.

This player instead:

- never sent `Stop` (because of the FiiO, [ADR-002](adr-002-never-stop-gmrender.md));
- switched a busy renderer through the next-URI path with a seek to the last second
  ([ADR-003](adr-003-switching-tracks-on-busy-gmrender.md));
- left a renderer paused, pipeline and HTTP connection idle, while another one played.

The racing appeared exactly after that pattern ([ADR-007](adr-007-racing-gmrender.md)).

## Options

1. **Keep one behaviour for all renderers.** Rejected: it keeps the suspected trigger on the one
   renderer that shows the fault.
2. **Send `Stop` to every renderer.** Rejected: it hangs the FiiO SR11.
3. **Decide per renderer** whether `Stop` is safe, and drive the safe ones like foobar2000.

## Decision

Option 3.

`services.StopSafe` looks at the model name from the device description:

- **`gmediarender`** (stock gmrender-resurrect, the BeagleBone) is Stop-safe.
- **The FiiO SR11** runs its own gmrender build under its own model name. It is not Stop-safe.
- **Any other model** is unknown and treated like the FiiO.

On a Stop-safe renderer:

- **Switching tracks while it is busy:** `Stop → SetAVTransportURI → Play`, plus a seek when a
  position is wanted. No next-URI switch, no seek to the end.
- **The Stop button, and leaving it for another renderer:** a real `Stop` instead of `Pause`, so it
  never sits paused with an idle pipeline.

Kept as before:

- gapless playback within the playlist: `SetNextAVTransportURI` for the following track;
- the user's Pause is still `Pause`.

## Consequences

- A forced track change on the BeagleBone can have a short gap, as it has in foobar2000. Natural
  advances stay gapless.
- `testkit.Renderer` fails a test on `Stop` unless the test declares the fake Stop-safe. The FiiO
  rule stays enforced everywhere else.
- **A Play right after Stop can be refused** (found in 1.0.9 live, 2026-11-10). The BeagleBone
  answered `501 Playing failed`, most likely because its ALSA device was still being released.
  The failed start skipped the seek, and the next Play started the track from 0, which looked
  random to the owner. Fixed in 1.0.10:
  - after `Stop`, the player waits up to 2 s for STOPPED;
  - `Play` is tried up to 4 times, 0.7 s apart;
  - a start that still fails keeps the wanted position for the next Play;
  - a seek that never lands is logged.
