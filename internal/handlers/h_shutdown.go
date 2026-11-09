package handlers

import "github.com/gofiber/fiber/v3"

// RequestShutdown starts a graceful process shutdown; set in main.
var RequestShutdown func()

// Shutdown handles POST /api/shutdown: the Quit button in the UI. The response is sent at once,
// the shutdown follows.
func Shutdown(c fiber.Ctx) error {
	if RequestShutdown == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "shutdown is not available")
	}
	go RequestShutdown()
	return c.SendStatus(fiber.StatusAccepted)
}
