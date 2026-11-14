# ADR-022: A jump over playing music is the player's Stop, then a start there

**Status:** Accepted 2026-11-14 (1.1.21).
**Overcome:** live over coax on both renderers: jumps between tracks and the slider are clean on
the FiiO SR11 and fast and clean on the BeagleBone (the owner, 2026-11-14); in tests (the steps
and their order, each sent only after the renderer reported the previous one done).

## Context

1.1.20 made every jump within an album a seek in the stream
([ADR-020](adr-020-every-jump-within-an-album-is-a-seek.md)). On the BeagleBone that was silent;
on the FiiO SR11 a seek over playing music is a run of grunts, the slider's too. The owner then
showed the way by hand: the Stop button stops the FiiO quietly and sets the slider to 0, a click
on any track starts it with no sound of its own.

## What was tried and rejected

- **The plain seek** (1.1.20): grunts on the FiiO.
- **Repeating the start's seek** until the position showed it: the FiiO reports its old position
  for a while after a seek, so it got seek after seek. Fixed: one seek, waited for.
- **Mute around the seek** (`SetMute`): the FiiO's mute does not reach its coaxial output.
- **A 2 s stretch served as silence before the target**: a length guessed from one trace, a
  clock instead of the renderer's word, and a gap for every renderer. Rejected by the owner.
- **Pause, seek, play, each confirmed by polling**: one jerk left. The paused FiiO still holds a
  moment of music and lets it out on Play (as in ADR-016).

## Decision

- A jump to a track, Next/Prev, the slider and the reseek after an equalizer change, while music
  plays, are the player's own Stop followed by a start of the target (`jumpLocked`): the same
  code as the Stop button and a click on a track after it. Stop pauses the renderer, lets the
  stream go to silence and seeks it where it is, so what it holds is silence; the start seeks
  it to the target and plays.
- Every step waits for the renderer to report what it leads to before the next is sent
  (`confirm`): halted and standing still; at the position and standing still, or a request for
  the stream from that position (`stream.OpenedSince`); PLAYING. The BeagleBone, paused, takes a
  seek and fetches from the target but reports its old position until Play: waiting for the
  position alone ran out the polls, some 8 s per jump, in the first build. The
  polls are the only waiting; a step that is never reported gives up after 50 polls and is
  logged.
- While paused, a seek stays a plain seek: nothing is heard.
- Every renderer alike; nothing depends on the model.

## Consequences

- A jump takes a moment longer than a bare seek: the confirmed steps.
- The rule for this project: drive the renderer by what it reports, the way the owner's own
  button presses do, not by guessed delays or data tricks.
