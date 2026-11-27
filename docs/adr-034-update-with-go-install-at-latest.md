# ADR-034: Update with go install builds @latest, and the page knows nothing of it

**Status:** Accepted 2026-11-27 (1.1.62). **Open:** the owner's update of a go install copy from
the page, 1.1.62 → the next release.
**Overcome:** in tests (`TestGoInstall`, `TestOfferUpdate`).

## Context

A go install copy updated itself with `go install …/cmd/go-upnp-player@<tag>`, the tag GitHub's
latest release named at the check, and the offer carried that command to the page, which showed
it as the button's tooltip. With v1.1.60 the Go module proxy and checksum database kept answering
"not found" for the tag (they failed to reach GitHub when first asked), so the update could not
be built at all; v1.1.61 was released only to give them a new tag to fetch.

## Decision

- The backend builds `…/cmd/go-upnp-player@latest`, with the running exe's linker flags
  (`selfupdate.GoInstall`). The tag the check saw only decides whether an update is offered.
- The page knows nothing of how the update is made: the offer has no command any more, and
  "Update with go install" sends a bare `POST /api/update/install`. Its messages name no version.

## Rejected

- **The pinned tag (ADR-019).** It ties the build to one tag the proxy may not have yet, and the
  page shows a command it has nothing to do with.
- **`GOPROXY=direct` for the update.** Needs git on the user's machine and skips the checksum
  database; not asked for.

## Consequences

`@latest` is what the proxy knows: right after a release it may still answer with the previous
version, and the update then rebuilds that one. Clicking again later gets the new one.
