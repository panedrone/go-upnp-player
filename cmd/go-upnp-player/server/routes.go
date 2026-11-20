// Package server is what main wires together: the route table, the one-instance
// check with the takeover of an older build, the console check and the version.
package server

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/static"
	"github.com/panedrone/go-upnp-player/internal/handlers"
	"github.com/panedrone/go-upnp-player/internal/services"
	"github.com/panedrone/go-upnp-player/web"
)

func InitRoutes(app *fiber.App) {
	api := app.Group("/api")
	api.Get("/state", handlers.State)
	api.Get("/events", handlers.Events)
	api.Get("/version", handlers.Version)
	api.Get("/queue", handlers.Queue)
	api.Put("/queue", handlers.SetQueue)
	api.Get("/tree", handlers.Tree)
	api.Put("/starred", handlers.PutStarred)
	api.Get("/tracks", handlers.Tracks)
	api.Get("/track/:id/properties", handlers.TrackProperties)
	api.Get("/search", handlers.Search)
	api.Post("/library/rescan", handlers.Rescan)
	api.Get("/settings", handlers.GetSettings)
	api.Put("/settings", handlers.PutSettings)
	api.Post("/renderers/refresh", handlers.RefreshRenderers)
	api.Put("/renderers/auto", handlers.AutoScanRenderers)
	api.Post("/renderers/select", handlers.SelectRenderer)
	api.Post("/player/play", handlers.Play)
	api.Post("/player/pause", handlers.Pause)
	api.Post("/player/resume", handlers.Resume)
	api.Post("/player/stop", handlers.Stop)
	api.Post("/player/next", handlers.Next)
	api.Post("/player/prev", handlers.Prev)
	api.Post("/player/seek", handlers.Seek)
	api.Post("/player/volume", handlers.Volume)
	api.Post("/shutdown", handlers.Shutdown)
	api.Post("/window/show", handlers.Window)
	api.Get("/update", handlers.CheckUpdate)
	api.Post("/update/open", handlers.OpenUpdate)
	api.Post("/update/install", handlers.InstallUpdate)
	api.Get("/eq", handlers.GetEQ)
	api.Put("/eq", handlers.PutEQ)
	api.Post("/eq/center", handlers.CenterEQ)
	api.Get("/dsd", handlers.GetDSD)
	api.Put("/dsd", handlers.PutDSD)

	// Fiber v3 registers HEAD for GET routes automatically (DisableHeadAutoRegister
	// is off); renderers send HEAD before GET
	app.Get("/media/track/:file", handlers.MediaTrack)
	app.Get("/media/run/:file", handlers.MediaRun)
	app.Get("/media/art/:id", handlers.MediaArt)

	// the embedded UI; the filesystem middleware of v2 is static in v3.
	// index.html must be revalidated on every load: embedded files carry a zero
	// modification time, the browser kept a stale index.html that pointed at the
	// previous build's hashed bundle, and after an update the page came up blank
	// (2026-11-09). The hashed bundles themselves may be cached.
	// With -web the files come from disk; the middleware then keeps no open
	// file handlers, so a fresh npm run build shows on the next reload.
	cacheFor := time.Duration(0) // the middleware's default
	if web.FromDisk() {
		cacheFor = -1
	}
	app.Get("/*", handlers.FreshIndex, static.New("", static.Config{
		FS:            web.Dist(),
		IndexNames:    []string{"index.html"},
		CacheDuration: cacheFor,
		ModifyResponse: func(c fiber.Ctx) error {
			if services.IsIndexPath(c.Path()) {
				c.Set(fiber.HeaderCacheControl, "no-cache")
			}
			return nil
		},
		NotFoundHandler: handlers.StaleBundle,
	}))
}
