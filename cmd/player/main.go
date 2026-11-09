// go-upnp-player plays a FLAC/CUE library on UPnP renderers.
// One exe: Fiber serves the UI (React, embedded), the API, and the tracks themselves to renderers.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"

	"go-upnp-player/internal/bg_workers"
	"go-upnp-player/internal/config"
	"go-upnp-player/internal/handlers"
	"go-upnp-player/internal/library"
	"go-upnp-player/internal/player"
	"go-upnp-player/internal/stream"
	"go-upnp-player/web"
)

func main() {
	port := flag.Int("port", 8090, "HTTP port for the UI, API and media (must be reachable from renderers)")
	roots := flag.String("root", "", "music library folders separated by ';' (saved to config)")
	noBrowser := flag.Bool("no-browser", false, "do not open the UI in a browser on start")
	flag.Parse()

	// The exe is built without a console (-H=windowsgui): there is nowhere to see stderr,
	// so the log goes to a file (overwritten on every start).
	setupLogFile()

	ui := fmt.Sprintf("http://localhost:%d/", *port)

	// One instance per machine: if the port is taken by our own player, just open its
	// UI and exit (so the shortcut also works as "show the player"). Taken by something else:
	// error. The check runs before any writes to the config.
	ln, err := net.Listen("tcp4", ":"+strconv.Itoa(*port))
	if err != nil {
		if alreadyRunning(ui) {
			if !*noBrowser {
				openBrowser(ui)
			}
			return
		}
		log.Fatalf("listen: %v", err)
	}

	if _, err := config.Load(); err != nil {
		log.Printf("config: %v", err)
	}
	if *roots != "" {
		var list []string
		for _, r := range strings.Split(*roots, ";") {
			if r = strings.TrimSpace(r); r != "" {
				list = append(list, r)
			}
		}
		if err := config.Update(func(c *config.Config) { c.Roots = list }); err != nil {
			log.Printf("config: %v", err)
		}
	}

	player.Init(*port)
	stream.SetEQ(config.Get().EQ)
	go library.Scan(config.Get().Roots)
	go player.Discover()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go bg_workers.RunPlayerPoll(ctx)
	// the Quit button in the UI takes the same path as Ctrl+C
	handlers.RequestShutdown = stop

	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	initRoutes(app)

	go func() {
		<-ctx.Done()
		// Graceful shutdown: pause the renderer (otherwise it plays out its buffer and
		// hits a vanished server), then stop the server. SSE and track streaming
		// are long-lived, so wait for them no longer than 3 s.
		player.Shutdown()
		_ = app.ShutdownWithTimeout(3 * time.Second)
	}()

	log.Printf("go-upnp-player: %s", ui)
	if !*noBrowser {
		openBrowser(ui)
	}
	if err := app.Listener(ln); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func setupLogFile() {
	dir, err := os.UserCacheDir()
	if err != nil {
		return
	}
	dir = filepath.Join(dir, "go-upnp-player")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	f, err := os.Create(filepath.Join(dir, "go-upnp-player.log"))
	if err != nil {
		return
	}
	log.SetOutput(f)
}

func initRoutes(app *fiber.App) {
	api := app.Group("/api")
	api.Get("/state", handlers.State)
	api.Get("/events", handlers.Events)
	api.Get("/queue", handlers.Queue)
	api.Put("/queue", handlers.SetQueue)
	api.Get("/tree", handlers.Tree)
	api.Get("/tracks", handlers.Tracks)
	api.Get("/search", handlers.Search)
	api.Post("/library/rescan", handlers.Rescan)
	api.Get("/settings", handlers.GetSettings)
	api.Put("/settings", handlers.PutSettings)
	api.Post("/renderers/refresh", handlers.RefreshRenderers)
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
	api.Get("/eq", handlers.GetEQ)
	api.Put("/eq", handlers.PutEQ)

	// Get in Fiber also registers HEAD; renderers send HEAD before GET
	app.Get("/media/track/:file", handlers.MediaTrack)
	app.Get("/media/art/:id", handlers.MediaArt)

	app.Use("/", filesystem.New(filesystem.Config{
		Root:         http.FS(web.Dist()),
		Index:        "index.html",
		NotFoundFile: "index.html",
	}))
}

// alreadyRunning reports whether the port is answered by go-upnp-player itself (its /api/state).
func alreadyRunning(ui string) bool {
	c := http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(ui + "api/state")
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	var st struct {
		Renderers json.RawMessage `json:"renderers"`
		Library   json.RawMessage `json:"library"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&st) != nil {
		return false
	}
	return st.Library != nil
}

func openBrowser(u string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	case "darwin":
		cmd = exec.Command("open", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	_ = cmd.Start()
}
