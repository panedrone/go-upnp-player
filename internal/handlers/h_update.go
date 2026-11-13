package handlers

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/panedrone/go-upnp-player/internal/selfupdate"
)

// Set in main.
var (
	// Installed: this copy was put in place by the Windows setup
	Installed bool
	// OpenURL opens a page in the system browser
	OpenURL func(url string)
	// Restart starts the freshly installed exe, which takes over from this one
	Restart func() error
)

// CheckUpdate handles GET /api/update: the "Check for updates" button. The player asks
// GitHub only here, when the user clicks.
func CheckUpdate(c fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 20*time.Second)
	defer cancel()
	o, err := selfupdate.Check(ctx, AppVersion, Installed)
	if err != nil {
		return fiber.NewError(fiber.StatusBadGateway, err.Error())
	}
	return c.JSON(o)
}

// OpenUpdate handles POST /api/update/open: opens the download or release page from the
// last check in the system browser (a link inside the player's own window would open
// another such window, not the browser).
func OpenUpdate(c fiber.Ctx) error {
	u := selfupdate.OfferedURL()
	if u == "" || OpenURL == nil {
		return fiber.NewError(fiber.StatusConflict, "nothing to open: check for updates first")
	}
	OpenURL(u)
	return c.SendStatus(fiber.StatusNoContent)
}

// InstallUpdate handles POST /api/update/install: a go install copy rebuilds itself at the
// offered version and restarts. The answer comes once the build is done.
func InstallUpdate(c fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Minute)
	defer cancel()
	if err := selfupdate.InstallOffered(ctx); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if Restart != nil {
		go func() {
			time.Sleep(300 * time.Millisecond) // let the answer go out first
			_ = Restart()
		}()
	}
	return c.SendStatus(fiber.StatusNoContent)
}
