# ADR-023: The end of the playlist switches the renderer off, as foobar2000 does

**Status:** Accepted 2026-11-14 (1.1.22). **Open:** the live check on the FiiO
SR11: an album played out, then a click on one of its tracks.

## Context

After an album played out, the FiiO SR11 stood at the very end of its stream: position equal to
the length (37:37 of 37:37), PLAYING or PAUSED_PLAYBACK, and no connection to the player. The
player had paused it there ([ADR-002](adr-002-never-stop-gmrender.md)) and kept its stream and
position in mind.

A click on a track of the same album took the restart-in-place path: the renderer seemed to hold
that stream already, so it got a seek and Play. In that state it takes both without an error and
moves nowhere (tried by hand: a seek to 30:00, then Play for 8 s, the position stayed at 37:37).
The start hung. Only foobar2000 brought it back.

foobar2000 at the end of an album clears its slider and the renderer clicks. Its renderer is
stopped, and foobar2000 holds nothing of it. Its next start is a new stream from STOPPED.

## What was tried and rejected

- **Bringing the renderer back from the end with a sequence found by probing it** (a next URI,
  Play, Pause; local builds only, 2026-11-14). The FiiO behaved differently from one try to the
  next, the sequence failed the owner's test twice, and it was guesswork. Rejected by the owner:
  the played-out album is a switch-off, not a state to recover from.
- **Pause at the end and remember the stream** (up to 1.1.21). This is the case above.

## Decision

- Once the playlist has played out (2 s after the end, as before), the player sends Stop if the
  renderer is playing or paused. It waits until the renderer reports STOPPED. The renderer model
  does not matter.
- Then the player forgets its stream and position. The page shows the slider with no thumb
  (`off` in the state) until the next start.
- The next start comes from STOPPED: the stream is set anew, as on a first start.
- This is an exception to [ADR-002](adr-002-never-stop-gmrender.md). The FiiO hung on Stop over
  music, during playback and from pause in the middle of a track. Here the music has ended.
  foobar2000 sends the FiiO this Stop at the end of every album.

## Consequences

- At the end of an album the renderer may click as its output closes, as with foobar2000. That
  happens after the music.
- `testkit.Renderer` keeps the live behaviour of a played-out gmrender: at the end of its stream
  it ignores seeks and Play, and it takes Stop there without failing the test.
