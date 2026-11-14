# ADR-025: Stop is Stop, on every renderer; transitions between tracks are their own

**Status:** Accepted 2026-11-14 (1.1.23). Supersedes [ADR-002](adr-002-never-stop-gmrender.md),
[ADR-003](adr-003-switching-tracks-on-busy-gmrender.md),
[ADR-013](adr-013-stop-safe-renderers-start-like-foobar.md) and
[ADR-021](adr-021-keep-the-sound-output-open.md). **Open:** the live check on the FiiO SR11,
the renderer that hung on `Stop` over music on 2026-11-09 (ADR-002); and on both renderers, a
click on coax where a new stream opens after `Stop`, as with foobar2000.

## Context

The Stop button and the transitions between tracks shared one code path, and one flag decided
both (`keepOutputOpen`, ADR-021). Neither renderer got a real `Stop`:

- The Stop button paused the renderer. The page then showed PAUSED, once it began to show what
  the renderer reports.
- A new stream on a busy renderer went through the gapless path: the target as the next URI, then
  a seek to the end of the current stream.

After ADR-024 such new streams became common. The BeagleBone hung on that path three times in a
row on 2026-11-14 (`did not switch … it may be hung`): it reported PLAYING with the position
standing at 5:10. ADR-021 had named exactly this as its signal to reconsider.

The owner: the Stop button and the internal transitions are different things and must not be
mixed. Stop must be sent, on every renderer alike. And there is no need to hammer a renderer: ask
its state first.

## Decision

- **Stop button.** It sends `AVTransport.Stop` to every renderer and clears the intent to play.
- **A new stream on a busy renderer.** `Stop` → `SetAVTransportURI` → `Play`, on every renderer,
  as foobar2000 starts it. The gapless next-URI switch, its retries and its "did not switch"
  error are gone.
- **Every `Stop` the player sends** (`stopRenderer`) goes the same way:
  - the renderer's state is asked first, and one already STOPPED (or holding nothing) gets no
    command;
  - otherwise exactly one `Stop`, never repeated;
  - then only polls, until the renderer reports STOPPED.
  The same applies to the Stop button, the old renderer at a switch of renderers, the end of the
  playlist (ADR-023), the restart of a racing renderer (ADR-007) and shutdown.
- **A jump within the stream the renderer holds** (Next/Prev, a click on a track of the run, the
  slider) stays a transition of its own, not the Stop button (`quiet`, ADR-022). The renderer is
  paused, its stream is let go to silence, and it is seeked where it stands. Then it is restarted
  in place. Its output stays open, and the player's intent to play is not touched.
- **No model list.** `services.StopSafe` is gone.

## Consequences

- The page shows STOPPED after the Stop button on every renderer, because that is what the
  renderer reports.
- A start after `Stop`, and a new stream on a busy renderer, open the renderer's sound output
  anew. On the BeagleBone the DAC on coax clicked there (ADR-021), as it does with foobar2000.
- The FiiO gets `Stop` over music again. In the 2026-11-09 incidents its UPnP service went down
  until a power cycle. The record does not say how many Stops it got then, or in which state.
  Now it gets at most one, and only when it is not already stopped.
- `testkit.Renderer` no longer fails a test on `Stop`.
