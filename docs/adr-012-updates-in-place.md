# ADR-012: Updating in place: one instance, no stale UI, playback position kept

**Status:** Accepted 2026-11-09; resuming at the saved position added 2026-11-10 (written down
2026-11-10); an older version no longer replaces a newer one, added 2026-11-13 (1.1.17).
**Overcome:** almost fully. **Open:** a browser that cached both the old `index.html` and the old
bundle before the cache fix keeps showing the old UI until a hard reload (Ctrl+F5).

## Context

The player is a single exe, with no console window, started from a shortcut. An update means
running the new exe while the old one is still serving. Each of the problems below appeared on
2026-11-09:

- A shortcut click after an update opened the old, still running player with its old UI.
- After an update the page came up blank. Embedded files have a zero modification time, so the
  browser kept a stale `index.html` that pointed at the previous build's hashed bundle.
- A page left open across an update kept its stale state: expanded tree folders never refetched.
- Play after an update started the track from 0 (fixed 2026-11-10). The slider showed the right
  position, because the renderer reports it, but the new process had forgotten that the track was
  paused.

## Options and what was rejected

**Stopping the player**

- A minimized console window. Rejected 2026-11-09: it sits on the taskbar and is awkward to close.
- **No console.** The exe is built with `-H=windowsgui` and quit from the UI (⏻, two clicks). Chosen.

**Single instance**

- A named mutex. Not needed: a busy port already stops a second copy from working.
- **Bind the port first and talk to whoever holds it.** Chosen.

**Cache**

- `Cache-Control: no-cache` alone. Not enough: with a zero modification time every conditional
  request got a 304.
- **Also drop `If-Modified-Since` for `index.html`.** Chosen.

## Decision

**One instance.** On start the process binds port 8090. If the port is busy and the holder answers
as go-upnp-player, the builds are compared via `/api/version`, a short SHA-256 of the exe file:

- same build: open the browser on the running player and exit;
- different build, or no `/api/version` at all (older builds): call `/api/shutdown` on the
  running player, then take the port as soon as it is free;
- ~~any different build replaces the running one~~ (2026-11-13, 1.1.17): unless the running
  player is a newer version. With a setup copy and a go install copy on one machine
  ([ADR-019](adr-019-check-for-updates-on-demand.md)), starting the stale one took the fresh one
  down. An older copy now shows the running player (its window, or the page) and exits; equal
  versions of different builds, a local rebuild, still replace.

If anything else holds the port, it is an error.

**Shutdown** (Quit, Ctrl+C or a replacement by a new build):

1. pause a playing renderer ([ADR-002](adr-002-never-stop-gmrender.md));
2. if a track was playing or paused, save its position as `playlist.position`;
3. call `app.ShutdownWithTimeout(3s)`. The timeout is needed because track streams never end on
   their own.

After the restart, Play continues the track there ([ADR-005](adr-005-seek-ignored-at-stream-start.md)).
A user Stop, or quitting while stopped, saves no position.

**UI cache**

- `index.html` is served with `Cache-Control: no-cache`, without `If-Modified-Since`.
- A request for a bundle that no longer exists (`/assets/index-*.js`) gets a tiny script instead
  of a 404. The script refetches `index.html` with `cache: "reload"` and reloads the page.

**Open pages:** `/api/state` carries the build id, and a page that sees a different build than the
one it started with reloads itself ([ADR-010](adr-010-ui-polls-state.md)).

## Consequences

- An update is: run the new exe. The old instance hands over the port, the page reloads, and
  playback continues from the same second after Play.
- The project was renamed from upnp-player on 2026-11-09. Files in the old data folders are read
  once as a fallback, so an upgrade loses nothing except the saved column widths.
