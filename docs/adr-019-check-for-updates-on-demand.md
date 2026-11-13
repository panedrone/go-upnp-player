# ADR-019: Updates are checked only on a click, and each kind of copy updates its own way

**Status:** Accepted 2026-11-13 (1.1.14); the Linux restart fixed in 1.1.16. **Open:** macOS not
run.
**Overcome:** on Windows end to end (1.1.14 → 1.1.15 by the setup and by go install), on Linux
by go install (1.1.16 → 1.1.17, the owner's VM, 2026-11-13); in tests
(version comparison, the kind of copy, GitHub's answer, go install moving the running exe aside
and back).

## Context

The owner wanted a "Check for updates" button and a way to the download, shown in the status
bar and closable. The player must not contact anything by itself.

## Options and what was rejected

- **Checking on start or on a timer.** Rejected by the owner: the player does not call out on its
  own; the user decides when to ask.
- **A plain download link for every copy.** Rejected: only the setup copy is updated by a file.
  Someone who installed with `go install` updates with `go install`; a zip user picks a file on
  the release page.
- **Showing the go install command to copy.** Rejected in favour of running it: the player
  already knows the module, the version and the linker flags it was built with.
- **Links opening from the page.** Rejected: inside the player's own window
  ([ADR-018](adr-018-own-window-via-webview2.md)) a link opens another WebView2 window, not the
  browser. The backend opens pages in the system browser, and only the URL its last check got
  from GitHub, never one sent by the page.

## Decision

- `GET /api/update` asks `api.github.com/repos/panedrone/go-upnp-player/releases/latest` and
  answers with the latest version, whether it is newer and what to do (`services.OfferUpdate`):
  - **setup** (an `unins000.exe` next to the exe): "Download the setup" opens the setup asset;
    running it replaces the player (the setup stops the running one);
  - **go** (the exe lies in `go env GOBIN`, or `GOPATH\bin`): "Update with go install" runs
    `go install -ldflags "<the running exe's flags>" …/cmd/go-upnp-player@<tag>`. Windows keeps
    a running exe from being overwritten but lets it be renamed, so it is moved aside first and
    put back if the build fails. The new exe is then started with `-no-browser` and takes over
    the port (ADR-012); the open page reloads itself;
  - **page** (the zip, a local build): "Open the release page".
- The restart uses the exe path taken when the process started. On Linux `os.Executable` reads
  `/proc/self/exe`, which follows the file once it is moved aside: the 1.1.15 restart started the
  old exe again, which saw its own build running and quit, and the page waited for ever
  (2026-11-13, a go install copy started from a terminal). The new process gets a session of its
  own (`setsid`), so a Ctrl+C in that terminal does not take it down. If the page has not
  reloaded 30 s after the build, it says the update is installed and asks for a restart by hand.
- Versions compare as three numbers; anything else is never "newer".
- The answer blinks three times in the status bar and stays until closed, by its × or, for
  "You have the latest version", by a click on it (owner: it must not run away by itself).
