# ADR-010: The page polls /api/state instead of a push channel

**Status:** Accepted 2026-11-09 (written down 2026-11-10).
**Overcome:** fully, nothing open.

## Context

The page has to show the renderer's state and position. The backend itself learns them by polling
the renderer once a second ([ADR-004](adr-004-detecting-track-advance.md)), so nothing new can
appear faster than that.

## Options

1. **SSE** (`GET /api/events`): the server pushes a state snapshot on every change. This was the
   first implementation. Dropped 2026-11-09 at the owner's request: a long-lived channel hides
   what the page was shown and when, and needs keepalives.
2. **WebSocket.** Considered and rejected: it is the same kind of long-lived channel, two-way
   traffic is not needed, and it would add a Fiber dependency plus reconnection code.
3. **Plain REST polling:** `GET /api/state` once a second, plus once more right after every
   command, so a click shows its result at once.

## Decision

Option 3.

- One request at a time: a refresh asked for while a request is in flight runs right after it.
- The same reply carries the build id, which the page uses to reload itself after an update
  ([ADR-012](adr-012-updates-in-place.md)).

## Consequences

- Every reading is an ordinary entry in the browser's network log.
- A completed library scan is detected by a scan counter (`library.scans`), not by watching
  `scanning` go false. A scan served from the metadata cache can start and end between two polls
  and never be seen running.
