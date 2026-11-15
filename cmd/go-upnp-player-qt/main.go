//go:build qt

// Command go-upnp-player-qt is the player with a Qt window (miqt) in place of the web page:
// the window calls the player's Go packages directly, in the same process. The HTTP server
// still runs: the renderers fetch the music from it, and the web page works too. Built only
// with the qt tag, so that the ordinary build, the tests and go install need no Qt and no cgo:
//
//	go build -tags qt -ldflags "-s -w -H=windowsgui" -o bin/go-upnp-player-qt.exe ./cmd/go-upnp-player-qt
//
// with PKG_CONFIG pointing to cli/pkg-config (see there) and C:\Qt\...\bin on PATH to run it.
//
// One player at a time, this one or the web one: they share the port and the config file. The
// start-up below follows cmd/go-upnp-player: a later start replaces a running player of
// another build, and a start of the same build brings the running one to the front.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v3"
	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/panedrone/go-upnp-player/cmd/go-upnp-player/server"
	"github.com/panedrone/go-upnp-player/internal/bg_workers"
	"github.com/panedrone/go-upnp-player/internal/config"
	"github.com/panedrone/go-upnp-player/internal/handlers"
	"github.com/panedrone/go-upnp-player/internal/library"
	"github.com/panedrone/go-upnp-player/internal/player"
	"github.com/panedrone/go-upnp-player/internal/services"
	"github.com/panedrone/go-upnp-player/internal/stream"
)

func main() {
	// an application of its own: its settings, playlist, library cache and log are apart
	// from the web player's, in go-upnp-player-qt folders
	config.UseAppDir("go-upnp-player-qt")
	port := flag.Int("port", 8090, "HTTP port for the media (must be reachable from renderers), the API and the web page")
	flag.Parse()
	ui := fmt.Sprintf("http://localhost:%d/", *port)

	build := server.BuildID()
	handlers.BuildID = build
	handlers.AppVersion = server.Version
	player.Build = build
	player.Version = server.Version
	addr := ":" + strconv.Itoa(*port)
	ln, err := net.Listen("tcp4", addr)
	if err != nil {
		running, theirs, theirVersion := server.RunningBuild(ui)
		if !running {
			setupLogFile()
			log.Fatalf("listen: %v", err)
		}
		if !services.ShouldReplace(theirs, theirVersion, build, server.Version) {
			server.ShowRunningWindow(ui) // the shortcut as "show the player"
			return
		}
		setupLogFile()
		log.Printf("replacing running build %q with %q", theirs, build)
		if ln, err = server.TakeOver(ui, addr); err != nil {
			log.Fatalf("listen after replacing the running player: %v", err)
		}
	} else {
		setupLogFile()
	}

	if _, err := config.Load(); err != nil {
		log.Printf("config: %v", err)
	}
	player.Init(*port)
	stream.SetEQ(config.Get().EQ)
	go library.Scan(config.Get().Roots)
	go player.Discover()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go bg_workers.RunPlayerPoll(ctx)
	handlers.RequestShutdown = stop // the web page's Quit takes the same path

	app := fiber.New(fiber.Config{AppName: "go-upnp-player Qt " + server.Version})
	server.InitRoutes(app)
	served := make(chan error, 1)
	go func() { served <- app.Listener(ln, fiber.ListenConfig{DisableStartupMessage: true}) }()
	log.Printf("go-upnp-player %s (Qt): %s", server.Version, ui)

	qt.NewQApplication(os.Args)
	// the window's icon: icon.ico lies next to the exe (the installer and windeployqt's folder)
	if exe, err := os.Executable(); err == nil {
		if ico := filepath.Join(filepath.Dir(exe), "icon.ico"); fileExists(ico) {
			qt.QGuiApplication_SetWindowIcon(qt.NewQIcon4(ico))
		}
	}
	w := newWindow(stop)
	handlers.ShowWindow = func() bool {
		mainthread.Start(w.raise)
		return true
	}
	go func() {
		<-ctx.Done()
		// Quit from the web page, Ctrl+C or a takeover by a new build: the window goes
		mainthread.Start(qt.QCoreApplication_Quit)
	}()
	qt.QApplication_Exec()
	w.saveLayout() // a quit from elsewhere (the page, a takeover) skips the window's close

	// the window was closed, or the application was told to quit: shut down as the web
	// player does, the renderer stopped first
	stop()
	player.Shutdown()
	player.EndStreams()
	_ = app.ShutdownWithTimeout(3 * time.Second)
	if err := <-served; err != nil {
		log.Printf("server: %v", err)
	}
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// setupLogFile is cmd/go-upnp-player's, in the Qt player's own folder: appended, started
// over past 1 MB.
func setupLogFile() {
	dir := config.CacheDir()
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	p := filepath.Join(dir, "go-upnp-player-qt.log")
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
