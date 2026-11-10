# ADR-007: A racing gmrender: restart it with Stop → SetAVTransportURI → Play

**Status:** Accepted 2026-11-09 in 1.0.4; thresholds lowered in 1.0.5 (written down 2026-11-10).
**Overcome:** worked around, not cured. **Open:**

- the root cause on the BeagleBone is unknown. foobar2000 never triggers it on the same board,
  so the first suspect is this player's control pattern (next-URI switching and long pauses
  instead of Stop). Since 2026-11-10 the BeagleBone is driven like foobar2000
  ([ADR-013](adr-013-stop-safe-renderers-start-like-foobar.md)); whether the racing is gone is
  not yet seen live. The hypotheses and checks are at the end of this ADR;
- the automatic restart is verified by tests only. Live checks showed no false triggers, but no
  real racing episode has happened since the fix.

## Context

The BeagleBone (gmrender) sometimes reports PLAYING with no sound while its position runs far
faster than real time. The queue flies past: tracks are downloaded as fast as the network allows
and never played. In this state it may also answer `Play` with `501 Playing failed`
([ADR-006](adr-006-hung-gmrender.md)).

Seen on 2026-11-09, after switching back to the BeagleBone from the FiiO. The first time the
position ran at 30–40x, the next time at ~5x.

Tried on a healthy device without reproducing it:

- a 60 s pause;
- a restart of the player;
- the switch path itself;
- sample format (all tracks were 44.1 kHz / 16 bit).

What cures it, checked live:

- `Pause` / `Play` does nothing.
- `Stop → SetAVTransportURI → Play` cures it. foobar2000 does exactly this on a new start, and
  that is how the owner recovered the device.
- Advice to reboot the renderer was wrong: the device itself was fine.

## Options

1. **Report it like a hung renderer and ask the user to restart the device.** Rejected: restarting
   the device is not needed, and the user would be told to do the wrong thing.
2. **Pause/Play from the player.** Rejected: proven not to help.
3. **Detect the racing and restart the track with `Stop → SetAVTransportURI → Play`.** This is
   the one deliberate exception to [ADR-002](adr-002-never-stop-gmrender.md). The renderer plays
   nothing anyway, so there is nothing to lose. The hang risk applies to the FiiO, which has never
   raced.

## Decision

Option 3.

**Detection** (`services.Racing`, in the poll): within one track, the position grows by more than
1.5 s per second of wall time plus 1.5 s, on three polls in a row.

- The slack covers `RelTime`'s whole seconds: one second of polling can show 2 s.
- Requiring three polls in a row covers a seek that lands late
  ([ADR-005](adr-005-seek-ignored-at-stream-start.md)).
- 1.0.4 used 3 s per second + 3 s and two polls. Rejected in 1.0.5: it missed the ~5x episode.
  The racing speed is whatever the download allows, so a high threshold misses slow episodes.

**Cure:** `Stop`, then the same playlist track is started again from the position where the
racing began, with a line in the log.

**No loop:** if the renderer races again within 2 minutes of a restart, it is paused and reported
("restart the renderer") instead of being stopped over and over.

## Consequences

- `testkit.Renderer` allows `Stop` only in the racing tests (`StopAllowed`).
- If the root cause is ever found on the device, the detection can stay as a safety net.

## Investigation: the suspected root cause on the BeagleBone (open)

The board's own documentation is the "Valera Jr." engineer's log, a separate repository.

**The audio path.** The BeagleBone has no I2S DAC: McASP is unused and the onboard audio is
removed by device-tree overlays. The path is:

1. gmrender: `playbin` → `alsasink`;
2. ALSA `hw:N,0`, with no dmix (the `N` is written into `/etc/asound.conf` at deploy time);
3. `snd-usb-audio`, which sends isochronous URBs;
4. the AM335x MUSB host controller, fed by DMA;
5. an asynchronous USB DAC: Topping MX3s or SMSL RAW-HA1.

The kernel matters: `4.9.78-ti-r94` and `6.18` are known bad for USB audio on this board;
`5.10.240-bone80` is the one that works.

**The decisive fact: foobar2000 never sees it.** foobar2000 drives the same board as a plain
network renderer and plays flawlessly. So the board, the DAC and the kernel can do the job.
Suspicion falls first on what this player asks of gmrender that foobar2000 never does.

| | foobar2000 | this player |
|---|---|---|
| Stream | one stream for the whole session (its UPnP output sends one WAV/FLAC of unknown length) | a separate HTTP stream per track, with `Content-Length` and Range requests |
| New start | `Stop → SetAVTransportURI → Play` | `Stop` is never sent ([ADR-002](adr-002-never-stop-gmrender.md)) |
| Track changes | none inside the stream | `SetNextAVTransportURI` (gapless, `playbin`'s about-to-finish) |
| Switching tracks | — | a seek to the last second of the current track ([ADR-003](adr-003-switching-tracks-on-busy-gmrender.md)) |
| Leaving the renderer | Stop | `Pause` for as long as the other renderer plays; the paused stream's HTTP connection sits idle |

**Hypothesis 1 (first).** gmrender's GStreamer 1.8.3 breaks on this player's pattern. The
observed trigger is exactly that pattern: switching back after the board sat paused while the FiiO
played, then the next-URI path with a seek near the end of a track, on a pipeline whose HTTP
connection had been idle for minutes.

- A broken segment or clock after that would explain a position running ahead with no sound.
- Only a full pipeline restart cures it, and that is precisely foobar2000's way of starting.
- Not tried yet: a pause of several minutes. Only 60 s was tried, and it did not trigger it.

**Hypothesis 2 (less likely now).** The USB DAC drops off the bus while gmrender holds the PCM
open:

- writes fail, and `alsasink` drops the data and carries on, which looks like racing;
- re-enumeration under another card index leaves `hw:N,0` naming nothing, which would explain
  `501 Playing failed` ([ADR-006](adr-006-hung-gmrender.md)).

But a USB fault would sooner or later hit foobar2000 too. The `dmesg` check below rules it in or
out in a minute.

**The fix that follows from hypothesis 1** is done in
[ADR-013](adr-013-stop-safe-renderers-start-like-foobar.md) (2026-11-10). The BeagleBone is now
driven the way foobar2000 drives it: `Stop → SetAVTransportURI → Play` for a switch, and a real
`Stop` instead of a long pause. Whether that ends the racing is still to be seen live.

**What to check, in this order, right when it races** (before curing it; over SSH):

1. **Did the DAC leave the bus?** This is the key check.
   - `dmesg | tail -60`: look for `USB disconnect`, `new high-speed USB device`, `reset`,
     `cannot submit urb`, errors -71, -110 or -19, and `musb` or `cppi41` lines.
   - `cat /proc/asound/cards`: is the DAC still there, and under which index?
   - `cat /etc/asound.conf`: does `hw:N,0` still match that index?
2. **What does ALSA itself report?**
   - `cat /proc/asound/cardN/pcm0p/sub0/status`: read it twice, one second apart. While playing,
     `hw_ptr` should grow by the sample rate; `state` should be RUNNING. Missing or CLOSED means
     the handle is dead.
   - `cat /proc/asound/cardN/stream0`: `Status: Running` and `Momentary freq`.
3. **Kernel and power management.**
   - `uname -r` must be `5.10.240-bone80`.
   - `cat /sys/bus/usb/devices/1-1/power/control` and `.../power/runtime_status`. `auto` means
     autosuspend is allowed. `echo on > .../power/control` rules it out as a test.
4. **Whether the USB chain works without gmrender.**
   1. `systemctl stop gmediarender`;
   2. `valera_rate_check.py`. Ratio 1.0000 means USB and the kernel are fine and the fault is in
      GStreamer or gmrender. A failure means it is below ALSA;
   3. `systemctl start gmediarender`.
5. **gmrender's own log.** Logging is off in the unit. Add `--logfile=/var/log/gmediarender.log`
   to `ExecStart`, so the next episode leaves a trace.

The racing detector in the player logs every episode ("plays ahead of the clock … restarting").
Comparing those times with `dmesg` on the board shows whether each episode lines up with a USB
event.
