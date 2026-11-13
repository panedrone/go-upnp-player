# ADR-018: The installed player shows its UI in a window of its own (WebView2)

**Status:** Accepted 2026-11-13 (1.1.14). **Open:** the window is not tried on a Windows without
the WebView2 runtime (the code falls back to the browser there, untested).
**Overcome:** checked live on the owner's PC with `-window`: the window opens, a second start brings it to the
front and exits, closing the window quits the player.

## Context

The player had no window: the UI opened in the default browser. Windows users who install a
program expect a window of its own, an entry in the taskbar and a close button that closes it.
The owner asked for this for the installed version only; the zip and `go install` keep the
browser.

## Options and what was rejected

- **React Native (for Windows).** Rejected: it draws native components, not HTML, so the React
  Bootstrap UI would have to be rewritten.
- **Electron or NW.js**, as Postman and pgAdmin do. Rejected: they bring their own Chromium and
  Node.js, hundreds of megabytes, for a page that already works in any browser engine.
- **Wails** (Go with a web UI in a native window). Rejected for now: a framework with its own build
  and project layout, more than one window needs.
- **`github.com/jchv/go-webview2`.** Chosen: pure Go, no cgo, uses the WebView2 runtime that
  Windows 10/11 already have; the UI is the same page at `http://localhost:8090/`, unchanged.

**What the close button does.** Owner, 2026-11-13: close closes, minimize minimizes. Closing the
window takes the Quit path (renderer paused, position saved, ADR-002, ADR-012). Hiding into the
tray with the music playing on was not wanted.

**Which copy opens a window.** An installed copy is recognised by the uninstaller `unins000.exe`
next to the exe, so the installer passes no arguments: a taskbar pin made from the running window
starts the exe without arguments and still gets the window. `-window` and `-browser` override.

## Decision

- `cmd/go-upnp-player/window` (Windows; elsewhere a stub that always says "no window") runs the
  WebView2 window on the main OS thread (`runtime.LockOSThread` in `init`); the HTTP server runs
  aside. WebView2 data lives in `%LocalAppData%\go-upnp-player\webview2`.
- Closing the window cancels the main context, the same as Quit; Quit, Ctrl+C or a takeover by a
  new build close the window.
- A second start of the same build posts `/api/window/show`; the running player restores and
  raises its window (the second process first allows it the foreground). 404 means it runs in the
  browser, and the second start opens the page as before.
- No WebView2 runtime: logged, and the browser opens instead.
- The window comes back where it was closed (added 1.1.15): its restored rectangle and whether it
  was maximized go to `config.json` (`window`) on close, by the close button (read before
  go-webview2 destroys the window) or by Quit and takeovers. A rectangle no monitor shows any more
  is dropped, and the window opens centred.
- The window and its taskbar button get the icon from a copy of `assets/icon.ico` embedded into
  the exe (`window/icon.ico`, `go:embed`), at the sizes Windows asks for. The exe file itself
  still has no icon resource, so Explorer shows it without one; the shortcuts carry `icon.ico`.
