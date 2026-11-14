# ADR-026: The page gets the state as Server-Sent Events

**Status:** Accepted 2026-11-14 (1.1.26). Supersedes
[ADR-010](adr-010-ui-polls-state.md). **Open:** a live evening with the page open, in the browser
and in the window (WebView2): the badge and slider follow the renderer, and the page reconnects
after a player restart or update.

## Context

ADR-010 replaced the first push channel (SSE, `GET /api/events`) with plain polling of
`GET /api/state`, once a second and after every command. It was dropped on 2026-11-09 at the
owner's request while the clicks and grunts on track changes were being hunted. A long-lived
channel hid which state the page was shown and when, and every reading had to be a visible
request.

That hunt is over (ADR-022 – ADR-025). The owner asked to go back to a push channel and to do it
cleanly.

## Options

1. **Keep polling** (ADR-010). One request a second while the page is open, most of them
   returning the same state.
2. **WebSocket.** Rejected again, for the same reasons as in ADR-010: two-way traffic is not
   needed, and it would add a dependency and reconnection code.
3. **Server-Sent Events.** Taken. A standard browser API (`EventSource`) that reconnects by
   itself. Every event is plain JSON, visible in the browser's network log (EventStream).

## Decision

- **`GET /api/events`** (`text/event-stream`) streams the state.
  - The first event is the state now.
  - After that, one event per change: the same JSON as `GET /api/state`.
  - A snapshot equal to the last one sent is not sent again.
  - A `: keepalive` comment goes out every 15 s.
- **The player wakes the streams** (`player.Notify`) after every poll of the renderer, once a
  second, and after every command that went through the API.
- **At shutdown the streams end** (`player.EndStreams`), so they do not hold the server's
  graceful stop for its whole timeout.
- **The page** reads the stream with `EventSource`. Its connection state is the stream's.
  After every command the page also asks `GET /api/state` once. That reading is what hands a
  held slider back to the backend (`useHeld`), and a paused player sends no event that could do
  it.
- **`GET /api/state` stays** for those one-shot reads, for tests and for debugging.

## Consequences

- Nothing reaches the page faster than the backend's own once-a-second poll of the renderer. The
  stream removes the empty requests, not that delay.
- A player replaced by another build is noticed on the first event after the reconnect, by its
  build id. The page reloads itself, as before (ADR-012).
