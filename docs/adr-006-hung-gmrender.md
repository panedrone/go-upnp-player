# ADR-006: A hung gmrender: detect and report, do not pretend

**Status:** Accepted 2026-11-09 (written down 2026-11-10).
**Overcome:** only partly. The player detects the state and tells the user, but cannot cure it:
the renderer or `gmediarender` has to be restarted by hand. **Open:**

- the trigger is not established;
- the BeagleBone's `501 Playing failed` happens inside GStreamer on the device and was never
  traced. gmediarender's logging is off in its unit. foobar2000 never triggers either failure on
  the same board, so this player's control pattern is the first suspect. The hypotheses and checks
  are in
  [ADR-007](adr-007-racing-gmrender.md#investigation-the-suspected-root-cause-on-the-beaglebone-open).

## Context

Two failure modes, both seen on the BeagleBone on 2026-11-09:

1. **Frozen in PLAYING.** gmrender reports PLAYING with a position that no longer moves. It then
   accepts every command and none of them has any effect.
2. **`501 Playing failed`.** The renderer answers `Play` with this error and does not even request
   the file. The failure happens before any network access, inside GStreamer on the device, most
   likely when it opens the ALSA output.

The same `501` also shows up on a racing renderer ([ADR-007](adr-007-racing-gmrender.md)).

## Options

1. **Trust the renderer's PLAYING and report the start as successful.** Rejected: the UI showed
   "playing" over silence.
2. **Cure it with `Stop`.** Rejected because of the FiiO ([ADR-002](adr-002-never-stop-gmrender.md)).
   It would also not help the frozen state, where commands have no effect at all.
3. **Detect it, report it, and stop making it worse.**

## Decision

Option 3.

- A position unchanged for 8 s while PLAYING is reported as `stalled` (`services.Stalled`). The UI
  shows STALLED and a Play button instead of Pause.
- Play on a stalled renderer goes through a real track start, which reports the failure.
- A switch that fails after three attempts is an error
  ([ADR-003](adr-003-switching-tracks-on-busy-gmrender.md)).
- A failed automatic start of the next track clears the intent to play and shows the error. It is
  no longer retried on every poll: the BeagleBone once got 108 `Play` requests in two minutes.

## Consequences

The user sees the real state and an instruction to restart the renderer instead of a fake
"playing".
