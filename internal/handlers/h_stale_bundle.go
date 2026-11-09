package handlers

import (
	"github.com/gofiber/fiber/v3"

	"github.com/panedrone/go-upnp-player/internal/services"
)

// staleBundleJS refetches index.html past the browser cache (which also
// replaces the cached copy) and reloads the page.
const staleBundleJS = `fetch("/", { cache: "reload" }).finally(() => location.reload())`

// StaleBundle is the static middleware's not-found handler. A browser that
// cached index.html before it was served with no-cache keeps asking for the
// bundle of an old build and shows a blank page; the browser never revalidates
// that copy, so the server cannot fix it directly (2026-11-09). Instead of a
// 404, such a request gets a tiny script that refreshes the cached index.html
// and reloads the page. Anything else is a plain 404.
func StaleBundle(c fiber.Ctx) error {
	if !services.IsStaleBundlePath(c.Path()) {
		return c.SendStatus(fiber.StatusNotFound)
	}
	c.Set(fiber.HeaderContentType, "text/javascript; charset=utf-8")
	c.Set(fiber.HeaderCacheControl, "no-store")
	// 200, not the 404 the static middleware preset: browsers do not run a
	// module script that came with an error status
	return c.Status(fiber.StatusOK).SendString(staleBundleJS)
}
