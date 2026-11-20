# ADR-032: A silent renderer stays on the list as OFFLINE, and comes back stopped

**Status:** Accepted 2026-11-20 (1.1.49). **Open:** checked in tests only; the live check is
Valera with the amplifier switched from USB to coaxial while playing, and back.

## Context

The two renderers behave differently when the amplifier is switched to another input.

- **FiiO SR11 on USB.** The SR11 is a box of its own: its UPnP stack does not care which input
  the amplifier listens to. It keeps answering SSDP, so it stays on the list, and the player shows
  the red OFFLINE badge when the renderer does not answer the poll.
- **Valera (BeagleBone, gmediarender).** The USB DAC is the board's only sound card. Switching
  the amplifier away takes the card off the bus. If gmediarender was holding it, the board's
  `gmediarender-card-gone` rebuilds the daemon (Valera's ADR-003), and libupnp needs about 10 s to
  advertise again. The Scan switch dropped a device not heard for 5 s (`services.ScanGone`), so
  Valera disappeared from the list and came back. Refresh did the same: it replaced the list with
  whatever answered.

There was a second fault behind the first one. Valera comes back from the rebuild empty, as
`NO_MEDIA_PRESENT`. The player still wanted to play, and `DecideTick` read `NO_MEDIA_PRESENT` as
the end of the stream: `TickPlayNext` started the next track into a card that was not there.

The rebuild itself stays. It is what releases the sound device, and the device left open is what
lost bit-perfect playback (ADR-031). This ADR is only about how the player shows it and what it
does after it.

## Decision

1. **The selected renderer is never dropped from the list.** The Scan switch skips it when it
   drops the devices it has not heard from; Refresh keeps its last known entry when it does not
   answer. The poll still fails, so the row shows OFFLINE, the way the SR11's does. Devices that
   are not selected are dropped as before. To get rid of a dead renderer, select another one.
2. **A device that answers from a new address is taken at that address.** A renderer rebuilt on
   another port would otherwise stay OFFLINE for good.
3. **Back from OFFLINE without the player's stream counts as a Stop that keeps the place**
   (`services.LostTheStream`). The renderer is back STOPPED, `NO_MEDIA_PRESENT`, or holding a
   stream that is not the player's, while the player wanted to play or was paused. The player
   then:
   - sends the renderer nothing;
   - clears the intent to play and forgets the run;
   - keeps the place in `resumeAt`, so the next Play continues where the music was cut;
   - says so in the status bar.

   A renderer that missed a few polls and still plays the player's stream goes on as before.

## Options rejected

- **A longer `ScanGone`.** A longer limit only wins against this one rebuild time, and a device
  that is really gone would stay listed longer. Keeping the selected renderer says what is meant.
- **Starting the track again on return.** The amplifier is listening elsewhere, and Valera may
  have no card at all. Playing into nothing is what the second fault did.
- **Start over, as the Stop button does (ADR-025).** The owner did not press Stop. The cut was
  made by the amplifier, so Play continues at the place.

## Consequences

- With the amplifier on the SR11, Valera stays on the list as OFFLINE, or as an idle renderer
  once it has been rebuilt, and nothing plays by itself.
- Without a rebuild on Valera's side (a live release of the card in place) the renderer would
  never leave the network. That is a matter for the board; the player is ready for either.
