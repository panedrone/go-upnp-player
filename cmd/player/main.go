// go-upnp-player plays a FLAC/CUE library on UPnP renderers.
// One exe: Fiber serves the UI (React, embedded), the API, and the tracks themselves to renderers.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
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

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/static"

	"go-upnp-player/internal/bg_workers"
	"go-upnp-player/internal/config"
	"go-upnp-player/internal/handlers"
	"go-upnp-player/internal/library"
	"go-upnp-player/internal/player"
	"go-upnp-player/internal/services"
	"go-upnp-player/internal/stream"
	"go-upnp-player/web"
)

func main() {
	port := flag.Int("port", 8090, "HTTP port for the UI, API and media (must be reachable from renderers)")
	roots := flag.String("root", "", "music library folders separated by ';' (saved to config)")
	noBrowser := flag.Bool("no-browser", false, "do not open the UI in a browser on start")
	flag.Parse()

	// The exe is built without a console (-H=windowsgui): there is nowhere to see
	// stderr, so the log goes to a file, overwritten on start. It is opened only
	// once this copy knows it will serve: a second copy that merely opens the
	// browser used to truncate the running player's log (2026-11-09).
	ui := fmt.Sprintf("http://localhost:%d/", *port)

	// One instance per machine: if the port is taken by our own player of the same
	// build, just open its UI and exit (so the shortcut also works as "show the
	// player"). A different build (an older exe still running after an update) is
	// shut down gracefully and replaced; otherwise the shortcut kept opening the old
	// UI (2026-11-09). Taken by something else: error. The check runs before any
	// writes to the config.
	build := buildID()
	handlers.BuildID = build
	player.Build = build
	addr := ":" + strconv.Itoa(*port)
	ln, err := net.Listen("tcp4", addr)
	if err != nil {
		running, theirs := runningBuild(ui)
		if !running {
			setupLogFile() // the port belongs to some other program: no log of ours to keep
			log.Fatalf("listen: %v", err)
		}
		if !services.ShouldReplace(theirs, build) {
			if !*noBrowser {
				openBrowser(ui)
			}
			return
		}
		setupLogFile()
		log.Printf("replacing running build %q with %q", theirs, build)
		ln, err = takeOver(ui, addr)
		if err != nil {
			log.Fatalf("listen after replacing the running player: %v", err)
		}
	} else {
		setupLogFile()
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

	app := fiber.New()
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
	// Fiber's startup banner for those who start the player from a console; a
	// shortcut start has no console, so there is nothing to print it to.
	if err := app.Listener(ln, fiber.ListenConfig{DisableStartupMessage: !hasConsole()}); err != nil {
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
	api.Get("/version", handlers.Version)
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

	// Fiber v3 registers HEAD for GET routes automatically (DisableHeadAutoRegister
	// is off); renderers send HEAD before GET
	app.Get("/media/track/:file", handlers.MediaTrack)
	app.Get("/media/art/:id", handlers.MediaArt)

	// the embedded UI; the filesystem middleware of v2 is static in v3.
	// index.html must be revalidated on every load: embedded files carry a zero
	// modification time, the browser kept a stale index.html that pointed at the
	// previous build's hashed bundle, and after an update the page came up blank
	// (2026-11-09). The hashed bundles themselves may be cached.
	app.Get("/*", static.New("", static.Config{
		FS:         web.Dist(),
		IndexNames: []string{"index.html"},
		ModifyResponse: func(c fiber.Ctx) error {
			if services.IsIndexPath(c.Path()) {
				c.Set(fiber.HeaderCacheControl, "no-cache")
			}
			return nil
		},
		NotFoundHandler: handlers.StaleBundle,
	}))
}

// buildID identifies this exe: a short hash of the executable file, so every
// build differs and copies of the same build match.
func buildID() string {
	p, err := os.Executable()
	if err != nil {
		return ""
	}
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// runningBuild reports whether the port is answered by go-upnp-player and which
// build it is ("" for builds before /api/version existed).
func runningBuild(ui string) (running bool, build string) {
	if !alreadyRunning(ui) {
		return false, ""
	}
	c := http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(ui + "api/version")
	if err != nil {
		return true, ""
	}
	defer func() { _ = resp.Body.Close() }()
	var v struct {
		Build string `json:"build"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&v) != nil {
		return true, ""
	}
	return true, v.Build
}

// takeOver asks the running player to quit (the same graceful path as the Quit
// button: renderer paused first) and binds the port once it is free.
func takeOver(ui, addr string) (net.Listener, error) {
	c := http.Client{Timeout: 3 * time.Second}
	if resp, err := c.Post(ui+"api/shutdown", "application/json", nil); err == nil {
		_ = resp.Body.Close()
	}
	var lastErr error
	for range 40 { // up to ~10 s: its shutdown waits up to 3 s for open streams
		ln, err := net.Listen("tcp4", addr)
		if err == nil {
			return ln, nil
		}
		lastErr = err
		time.Sleep(250 * time.Millisecond)
	}
	return nil, lastErr
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
