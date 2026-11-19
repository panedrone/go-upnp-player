# ADR-031: A run ends in a tail of silence, so Stop reaches a live stream

**Status:** Accepted 2026-11-19. Amends [ADR-016](adr-016-clicks-between-tracks-on-fiio-coax.md)
(the next-URI queueing between runs) and [ADR-025](adr-025-stop-is-stop.md) (which Stop ends a
stream). **Open:** the owner's live check on the BeagleBone that an album following one of another
rate now opens the device at its own rate (`/proc/asound/card1/pcm0p/sub0/hw_params`), and that no
click appeared at the end of an album.
**Overcome:** in tests. Measured on the board beforehand: see below.

## Context

Measured on the BeagleBone on 2026-11-19 (over SSH, the renderer's own repository has the log):

- While a stream plays, `/proc/asound/card1/pcm0p/sub0/status` says `RUNNING` and the USB
  interrupt rate is 8076/s — the DAC's own 125 µs packet interval, nothing the player causes.
- **When a stream runs out on its own, gmrender does not release the device.** The PCM stays
  `RUNNING` for as long as the process lives, `aplay` on `hw:1,0` answers `Device or resource
  busy`, and the interrupts keep flowing with nothing playing. Tried against that state and none
  of it helped: `Pause`, `Play`, `Stop`, `Seek`, `SetAVTransportURI` with an empty URI. Only a
  restart of the service closes it.
- **A stream of another rate is then resampled into the rate the device is stuck at.** A 48 kHz
  file on a device left open at 44.1 kHz played at `rate: 44100`, the DAC reporting
  `Momentary freq = 44100 Hz`, with no error logged anywhere. On a free device the same file
  opens at `rate: 48000`, `Momentary freq = 47999 Hz`.
- `Stop` sent **while the stream still plays** closes the device properly (`closed`, 0
  interrupts).

So a run that plays to its last byte costs bit-perfect playback for every stream of another rate
after it — a whole track resampled, every sample, silently. Two paths led there:

1. The player's `Stop` at the end of a stream (`switchOff`, ADR-025) arrives after the stream has
   run out, where it no longer helps.
2. The next run was queued with `SetNextAVTransportURI` (ADR-016). A run ends where the format
   changes, so the renderer's own gapless path moved to a stream of a **different rate** while
   holding the device open — the resampling case exactly.

foobar2000 never hits this: it sends the renderer one stream of unknown length for the whole
session and ends it with its own `Stop`, over a live stream.

## Options

1. **Patch gmrender to release the device at the end of a stream.** Rejected by the owner: the
   board's binaries are not ours to patch.
2. **Stop a second or two before the stream ends.** Rejected: it cuts the end of the music, and
   the renderer reports its position in whole seconds, so how much it cuts is not even known.
3. **Serve a stream of unknown length, as foobar2000 does, and end it with Stop.** Rejected for
   now: runs, Range requests and seeking all rest on a known length (ADR-016), and the device
   would then stay open around the clock.
4. **End a run with a tail of silence and act when the music, not the stream, has played out.**
   Chosen: the stream is still live when `Stop` arrives, and not a sample of music is lost.

## Decision

- **A run carries `Tail`:** `stream.TailSeconds` (3 s) of digital silence after the music, counted
  in the WAV header, the Content-Length and the DIDL duration — it is part of the stream the
  renderer is told to expect. Three seconds because the renderer rounds its position to whole
  seconds and one poll has to fall inside the tail.
- **The tick acts on the music, not the stream.** `TickInput.MusicLength` is the music of the
  stream the renderer holds, and `services.MusicPlayedOut` is true a second past it. It ends a run
  exactly as `STOPPED` did, with a next run (`TickPlayNext`, which starts it with Stop →
  SetAVTransportURI → Play, ADR-025) or without (`TickFinished` → `switchOff`).
- **No wait at the end of a playlist in that case.** The 2 s of `haltDelay`, there because up to
  half a second of music could be left, is skipped when the tail is already playing.
- **A next run is queued only when its format matches** (`queueNext`), which happens solely where
  one album had to be split over the RIFF size limit. Every other next run is left to the tick.
- The player's own lengths are kept apart: `runPlan.length` and `runLen` are the music,
  `runPlan.stream` and `runStream` the whole stream. What is compared against a renderer-reported
  duration uses the latter.

## Consequences

- Three seconds of silence play at the end of every run. Where the tail is reached, the device is
  released, so the next stream of another rate opens at its own rate.
- Gapless between runs is gone except for a split album — but a run ends where the format changes,
  and a renderer relocks there anyway (ADR-016). The gap is now the player's Stop and start.
- An album's DIDL duration is 3 s longer than its music. The UI shows the music: track positions
  come from the player's own layout, not from that figure.
- A renderer that reports no length for a short stream (ADR-015, the half-second case) is driven
  by the tail as well: its position reaches the tail and the tick ends the run there.
