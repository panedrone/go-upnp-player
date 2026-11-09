// go-upnp-player plays a FLAC/CUE library on UPnP renderers.
// One exe: Fiber serves the UI (React, embedded), the API, and the tracks themselves to renderers.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
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

	"github.com/panedrone/go-upnp-player/internal/bg_workers"
	"github.com/panedrone/go-upnp-player/internal/config"
	"github.com/panedrone/go-upnp-player/internal/handlers"
	"github.com/panedrone/go-upnp-player/internal/library"
	"github.com/panedrone/go-upnp-player/internal/player"
	"github.com/panedrone/go-upnp-player/internal/services"
	"github.com/panedrone/go-upnp-player/internal/stream"
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
	handlers.AppVersion = Version
	player.Build = build
	player.Version = Version
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

	app := fiber.New(fiber.Config{AppName: "go-upnp-player " + Version})
	initRoutes(app)

	go func() {
		<-ctx.Done()
		// Graceful shutdown: pause the renderer (otherwise it plays out its buffer and
		// hits a vanished server), then stop the server. SSE and track streaming
		// are long-lived, so wait for them no longer than 3 s.
		player.Shutdown()
		_ = app.ShutdownWithTimeout(3 * time.Second)
	}()

	log.Printf("go-upnp-player %s: %s", Version, ui)
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
