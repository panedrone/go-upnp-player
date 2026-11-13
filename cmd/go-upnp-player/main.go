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

	"github.com/panedrone/go-upnp-player/cmd/go-upnp-player/server"
	"github.com/panedrone/go-upnp-player/cmd/go-upnp-player/window"
	"github.com/panedrone/go-upnp-player/internal/bg_workers"
	"github.com/panedrone/go-upnp-player/internal/config"
	"github.com/panedrone/go-upnp-player/internal/handlers"
	"github.com/panedrone/go-upnp-player/internal/library"
	"github.com/panedrone/go-upnp-player/internal/player"
	"github.com/panedrone/go-upnp-player/internal/selfupdate"
	"github.com/panedrone/go-upnp-player/internal/services"
	"github.com/panedrone/go-upnp-player/internal/stream"
)

func init() {
	// WebView2 runs its window on the thread that created it: keep main on one OS thread
	runtime.LockOSThread()
}

func main() {
	port := flag.Int("port", 8090, "HTTP port for the UI, API and media (must be reachable from renderers)")
	roots := flag.String("root", "", "music library folders separated by ';' (saved to config)")
	noBrowser := flag.Bool("no-browser", false, "do not open the UI in a browser on start")
	inWindow := flag.Bool("window", false, "show the UI in a window of its own instead of the browser (Windows; the default for an installed copy)")
	inBrowser := flag.Bool("browser", false, "show the UI in the browser even in an installed copy")
	flag.Parse()
	useWindow := !*noBrowser && !*inBrowser && (*inWindow || installed())

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
	build := server.BuildID()
	handlers.BuildID = build
	handlers.AppVersion = server.Version
	player.Build = build
	player.Version = server.Version
	addr := ":" + strconv.Itoa(*port)
	ln, err := net.Listen("tcp4", addr)
	if err != nil {
		running, theirs := server.RunningBuild(ui)
		if !running {
			setupLogFile() // the port belongs to some other program: no log of ours to keep
			log.Fatalf("listen: %v", err)
		}
		if !services.ShouldReplace(theirs, build) {
			// the shortcut as "show the player": its window, or else the browser
			if useWindow && server.ShowRunningWindow(ui) {
				return
			}
			if !*noBrowser {
				openBrowser(ui)
			}
			return
		}
		setupLogFile()
		log.Printf("replacing running build %q with %q", theirs, build)
		ln, err = server.TakeOver(ui, addr)
		if err != nil {
			log.Fatalf("listen after replacing the running player: %v", err)
		}
	} else {
		setupLogFile()
	}

	selfupdate.RemoveLeftover()
	handlers.Installed = installed()
	handlers.OpenURL = openBrowser
	handlers.Restart = func() error { return selfupdate.Restart(os.Args[1:]) }

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

	app := fiber.New(fiber.Config{AppName: "go-upnp-player " + server.Version})
	server.InitRoutes(app)

	go func() {
		<-ctx.Done()
		// Graceful shutdown: pause the renderer (otherwise it plays out its buffer and
		// hits a vanished server), then stop the server. Track streams
		// are long-lived, so wait for them no longer than 3 s.
		player.Shutdown()
		_ = app.ShutdownWithTimeout(3 * time.Second)
	}()

	log.Printf("go-upnp-player %s: %s", server.Version, ui)
	// Fiber's startup banner for those who start the player from a console; a
	// shortcut start has no console, so there is nothing to print it to.
	listen := func() error {
		return app.Listener(ln, fiber.ListenConfig{DisableStartupMessage: !server.HasConsole()})
	}
	if !useWindow {
		if !*noBrowser {
			openBrowser(ui)
		}
		if err := listen(); err != nil {
			log.Fatalf("server: %v", err)
		}
		return
	}

	// Own window: the server runs aside, the window owns the main thread (WebView2 needs
	// it). Closing the window quits like the Quit button; Quit, Ctrl+C or a takeover by a
	// new build close the window.
	served := make(chan error, 1)
	go func() { served <- listen() }()
	go func() {
		<-ctx.Done()
		window.Close()
	}()
	handlers.ShowWindow = window.Show
	if !window.Run(ui, "go-upnp-player") {
		log.Printf("no WebView2 on this machine: the UI opens in the browser")
		openBrowser(ui)
	}
	stop() // the window was closed: the same path as Quit
	if err := <-served; err != nil {
		log.Fatalf("server: %v", err)
	}
}

// installed: this exe was put here by the Windows installer, whose uninstaller lies next
// to it. An installed copy opens its own window; one from go install or the zip keeps the
// browser.
func installed() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(filepath.Dir(exe), "unins000.exe"))
	return err == nil
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
	// Appended, not recreated: a new build replacing a running one used to wipe the
	// old process's last lines, including how it shut down (2026-11-10). Started
	// over once it passes 1 MB.
	p := filepath.Join(dir, "go-upnp-player.log")
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if st, err := os.Stat(p); err == nil && st.Size() > 1<<20 {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(p, flags, 0o644)
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
