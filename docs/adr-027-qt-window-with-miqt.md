# ADR-027: A Qt window (miqt) beside the web page, calling the player directly

**Status:** Accepted 2026-11-15 (1.1.27: the window has what the page has, and the Qt setup is
among the release's assets). **Open:** the update check offers the Qt copy the release page,
not its setup; no Qt build for Linux or macOS is released.

How it is built now: [the Qt player's SAD](go-upnp-player-qt-sad.md).

## Context

The page (React, served by the player, shown in the browser or in a WebView2 window,
[ADR-018](adr-018-own-window-via-webview2.md)) talks to the player over REST and SSE. The owner had
long wanted a native Qt window. The layouts and behaviour the page worked out are the
specification for it. A Qt window in the same process needs no REST: it calls the player's
packages directly, the same way the HTTP handlers do.

## Options

1. **Keep only the page.**
2. **therecipe/qt**, the best-known Go binding. Rejected: abandoned since 2020, on Qt 5.13, and it
   builds badly with today's Go.
3. **mappu/miqt**, Qt 5 and Qt 6, maintained, plain cgo. Taken.

For the window's own settings (where localStorage keeps them for the page):

1. **SQLite, as Firefox does.** Rejected: a database for a few dozen values, plus cgo or a heavy
   pure-Go build.
2. **JSON.** Taken: a section of the config file the player already keeps.

## Decision

- **A separate program**, `cmd/go-upnp-player-qt`, built only with the `qt` build tag. The ordinary
  build, the tests and `go install` know nothing of Qt or cgo.
- **One process holds the whole player.** The window calls the player's packages directly
  (`player.PlayIndex`, `player.Select`, `library.Children`, and so on). A command runs on a
  goroutine, since many wait for the renderer for seconds. The window follows the state through
  `player.Subscribe`, the same wake as the SSE stream ([ADR-026](adr-026-state-as-server-sent-events.md)),
  and redraws on Qt's GUI thread (`mainthread.Start`).
- **The HTTP server still runs.** The renderers fetch the music from it, and the page works from
  any browser at the same time.
- **One player at a time**, this one or the web one. They share the port and the config file. The
  start-up follows `cmd/go-upnp-player`: a later start of another build replaces the running
  player, and a start of the same build brings it to the front.
- **The build on Windows** uses the Qt SDK (MinGW kit) and the MinGW GCC of the same version, as
  miqt's own notes describe. Instead of installing pkg-config, `cli/pkg-config` answers cgo from
  the `.pc` files Qt ships, with Qt's real prefix in place of its build machine's. `windeployqt`
  puts the Qt DLLs next to the exe. On Linux the system's Qt packages and pkg-config do the same.

## Consequences

- The first build compiles miqt's C++ bindings: about 5 minutes. Later builds take seconds.
- The exe is about 26 MB, plus about 40 MB of Qt DLLs and plugins next to it; the setup packs it
  all into about 18 MB.
- The Qt exe carries the player's icon as a Windows resource (`icon.rc`, compiled by MinGW's
  `windres` to `rsrc_windows_amd64.syso`, which Go links by itself); its window's title is
  "go-upnp-player Qt", and its status bar shows the version alone, as the page's does.
- The Qt setup is a program of its own (its AppId, folder `go-upnp-player-qt` and shortcuts
  "go-upnp-player Qt"), beside the web player's setup. Before installing or uninstalling it stops
  a running player through its Quit, as the web setup does. Its uninstaller warns that the
  settings, the playlist and the cache are shared with the web player.
- A new exe path listening on the network gets the Windows firewall prompt once, as the web player
  did. It has to be allowed for private networks, or the renderers cannot fetch the music.
