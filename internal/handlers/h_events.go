package handlers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/panedrone/go-upnp-player/internal/player"
)

// keepalive: a comment line this often keeps proxies and the browser from taking an idle
// stream for a dead one.
const keepalive = 15 * time.Second

// Events handles GET /api/events: the player state as Server-Sent Events (ADR-026). The
// first event is the state now; then one event per change, the same JSON as
// GET /api/state. Identical snapshots are not sent twice.
func Events(c fiber.Ctx) error {
	c.Set(fiber.HeaderContentType, "text/event-stream")
	c.Set(fiber.HeaderCacheControl, "no-cache")
	c.Set("X-Accel-Buffering", "no")
	return c.SendStreamWriter(func(w *bufio.Writer) {
		wake, done, cancel := player.Subscribe()
		defer cancel()
		tick := time.NewTicker(keepalive)
		defer tick.Stop()
		var last []byte
		send := func() bool {
			b, err := json.Marshal(player.Snapshot())
			if err != nil || bytes.Equal(b, last) {
				return true
			}
			last = b
			if _, err := w.WriteString("data: "); err != nil {
				return false
			}
			_, _ = w.Write(b)
			_, _ = w.WriteString("\n\n")
			return w.Flush() == nil // an error: the page went away
		}
		if !send() {
			return
		}
		for {
			select {
			case <-done:
				return
			case <-wake:
				if !send() {
					return
				}
			case <-tick.C:
				if _, err := w.WriteString(": keepalive\n\n"); err != nil || w.Flush() != nil {
					return
				}
			}
		}
	})
}
