# ADR-017: The renderer's old position after a seek is not taken for where it plays

**Status:** Accepted 2026-11-11 (1.1.6). **Open:** the owner's live check of clicks in the
playlist and the seek slider on both renderers; the FiiO's reports after a seek were not traced.
**Overcome:** in tests: the BeagleBone's trace is replayed through the `player/seams.go` function
variables (`seek_test.go`), and every case fails without the fix.

## Context

Since [ADR-016](adr-016-clicks-between-tracks-on-fiio-coax.md) a click on a track of the playing
run, Next and Prev are a `Seek` in its stream. The owner reported both a click in the playlist
landing on the wrong track and the seek slider jumping.

A one-off probe polled the BeagleBone every 100 ms while the player was driven through its API
(2026-11-11):

- `Seek` answers OK at once, but `GetPositionInfo.RelTime` stays at the old place until the seek
  is done: ~0.5 s within a track, over a second for a jump of minutes in a 38-minute run.
- A click on "Visions" (track 17) while "Alone" (15) played: the next poll still saw 5:25 in the
  stream, i.e. track 15. Not the track after the clicked one, so the poll took it for a stream
  that had strayed from an edited playlist and started track 18, "Hope".
- A seek from 0:08 to 2:00: the state went 120 → 8 → 120.
- Next skipped a track (owner, same day). The tracks of a run start at fractions of a second in
  its stream, while seek targets and `RelTime` are whole seconds, both cut down: a jump to the
  next track landed up to a second before it, the poll read that as the end of the playing track,
  took the stream for one that had strayed and started the track after the next.
- After a real start the renderer reports the new URI with the old stream's `TrackDuration` for
  ~0.5 s (not acted on: the advance check already needs the length to match).

## Options

1. **Hold the slider in the page until the polled value comes near the chosen one** (what the
   page did since 2026-11-10: two matching polls, a margin, a 5 s cap). Rejected: it guesses in
   the UI about what the backend says, it does nothing for a click in the playlist, and it made
   the knob jump on its own when a guess failed. The backend is the one source of truth for the
   page.
2. **The poll ignores positions for a fixed time after every seek.** Rejected: a jump of minutes
   takes longer than a short seek, and a fixed pause also hides the real position after the
   seek has landed.
3. **The poll takes the seek target as the position until the renderer reports one after it**
   (at most `SeekLandWithin`, 5 s; past that the renderer is taken at its word, it may have
   dropped the seek). Chosen.

## Decision

Option 3: `services.SeekLanded` decides, the player's poll holds the stream position at the target
meanwhile. For the whole seconds, `services.TrackInStream` takes a position less than 1.5 s
before the playing track's start as its start. Sending the seek target with a fraction was not
chosen: not every renderer takes one (`services.FormatSeekTarget`), and rounding the target up
would cut up to a second off the track's opening. The page shows the backend's state as it is; its slider hands back to the poll at the
first state asked for after the command answered.
