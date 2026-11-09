# go-upnp-player

![go-upnp-player: library tree, playlist, output devices and the 18-band equalizer](docs/img/screenshot.png)

A player for a local FLAC/CUE library that plays to network UPnP/DLNA renderers
(tested with gmrender-resurrect: a home-made streamer on a BeagleBone Green and a FiiO SR11).
A single exe: a Go/Fiber v3 backend with a React-Bootstrap UI embedded in it, opened in the browser.

- folder tree, search, tracks from `.cue` sheets (single-file image + cue) and plain track files;
- renderer selection, play/pause/stop/seek/next/prev, renderer volume;
- 18-band equalizer: 55 Hz … 20 kHz, ±20 dB, presets; applied by the player itself, in the WAV stream;
- the same page works as a remote control from a phone on the same Wi-Fi.

How it works and why it was built this way: [docs/go-upnp-player-sad.md](docs/go-upnp-player-sad.md);
decisions under discussion: [ADR-001 — lossless formats beyond FLAC](docs/adr-001-lossless-formats-beyond-flac.md).

## Install (Windows)

Requirements: Windows 10/11 x64, a UPnP/DLNA renderer on the same network, a library of FLAC
files and/or FLAC images with `.cue` sheets.

1. Download `go-upnp-player-windows-amd64.zip` from
   [Releases](https://github.com/panedrone/go-upnp-player/releases) and unpack it into any folder,
   e.g. `C:\Programs\go-upnp-player`. Nothing else is installed.
2. Run `go-upnp-player.exe`. The exe is not signed, so on the first start Windows SmartScreen may
   say "Windows protected your PC": click **More info → Run anyway**.
3. Windows Firewall asks about network access: **allow it for private networks**. Renderers
   download tracks from the player themselves; without the rule you get "the command went through,
   but silence".
4. The player opens in the browser at `http://localhost:8090/`. In the **Library folders** card
   enter your music folders (one per line) and press **Save**; the library is scanned.
5. Pick a renderer in the **Output** card (press **Use** on hover), build a playlist with the
   **→** button next to a folder, a cue sheet or a track, and press **▶**.

Optional:

- **Desktop shortcut:** right-click `go-upnp-player.exe` → *Send to → Desktop (create shortcut)*.
  For a nicer icon: shortcut *Properties → Change Icon…* → `icon.ico` from the same folder.
- **Pin to the taskbar:** right-click the shortcut → *Show more options → Pin to taskbar*.
  The player has no window of its own, so it cannot be pinned while running.
- **Phone as a remote:** open `http://<this PC's IP>:8090/` on a phone in the same network.

Settings live in `%AppData%\go-upnp-player\config.json`, the log and the metadata cache in
`%LocalAppData%\go-upnp-player`. To uninstall, delete the program folder and those two folders.

### With Go (any OS)

For those who prefer it, with Go 1.26+:

```bash
go install github.com/panedrone/go-upnp-player/cmd/go-upnp-player@latest
```

The binary lands in `$(go env GOPATH)/bin`. Built this way it is a console program (on Windows a
console window stays open while it runs; close it or press Ctrl+C to stop). Linux and macOS
builds compile but have not been tested.

## Use

- Only one copy runs per machine: starting it again just opens the page. After an update, the
  new exe stops the old one gracefully and takes over; an open page reloads itself.
- The program has no window: stop it with the **⏻ Quit** button on the page (two clicks). It
  pauses the renderer first, then exits.
- Started from cmd or PowerShell, it attaches to that console and prints Fiber's startup banner.
- Command line options:
  - `-root` — library folders separated by `;` (remembered; can be changed later on the page);
  - `-port` — port for the UI, the API and the track streams, 8090 by default;
  - `-no-browser` — do not open the browser.

## Build from source

Requires Go 1.26+. The UI bundle (`web/dist`) is built at design time and committed, so Node is
needed only to change the UI.

```powershell
.\build.ps1        # the exe only: bin\go-upnp-player.exe (no console window)
.\build.ps1 -UI    # rebuild web/dist first (Node 20+), then the exe
```

After a UI change, commit `web/dist` together with the sources.

UI development:

```powershell
go run ./cmd/go-upnp-player -no-browser   # backend on :8090
cd web; npm run dev               # Vite proxying /api and /media to :8090
```

## Icon

`web/public/favicon.svg` is the source (an original drawing: a streamer box with a play badge).
`web/public/favicon-32.png`, `assets/icon-256.png` and `assets/icon.ico` are rendered from it; the
`.ico` is meant for a desktop shortcut. The exe itself has no embedded icon yet: that needs a
Windows resource (`.syso`), e.g. `go-winres simply --icon assets/icon-256.png` in `cmd/go-upnp-player`.
