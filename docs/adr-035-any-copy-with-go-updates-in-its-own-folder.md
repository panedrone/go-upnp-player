# ADR-035: Any copy with Go at hand updates itself with go install, in its own folder

**Status:** Accepted 2026-11-27 (1.1.63). **Open:** the owner's update of `bin\go-upnp-player.exe`
(a local build) from the page, 1.1.63 → the next release.
**Overcome:** in tests (`TestUpdateKindOf`, `TestGoInstall`).

## Context

ADR-019 updated by go install only a copy lying in `go env GOBIN` (or `GOPATH\bin`); a local
build or an unpacked zip got the release page. The owner runs a local build
(`D:\work\go-upnp-player\bin\go-upnp-player.exe`, the taskbar pin) and expected "Update" to
update it, not to open GitHub.

## Decision

- A copy that the setup did not install and that finds Go (`go env` answers) is offered
  "Update with go install", wherever its exe lies.
- The build runs with `GOBIN` set to the running exe's folder, so the new exe replaces that very
  file (moved aside first, as before) and the restart starts it. The Go bin folder is no longer
  asked where the copy lives; it only tells that Go is there.
- A setup copy still takes the new setup; a copy without Go still gets the release page.

## Rejected

- **Only the Go bin folder (ADR-019).** A local build or a zip with Go on the machine could have
  updated itself and was sent to a web page instead.
- **Building into GOPATH\bin and copying over.** Two copies of the player on disk, and a second
  exe the user never asked for.

## Consequences

The exe must keep its name, `go-upnp-player(.exe)`: go install names the build after the
module's command, and a renamed copy is put back with "go install left no …".
