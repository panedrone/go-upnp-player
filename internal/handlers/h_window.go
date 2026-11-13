package handlers

import "github.com/gofiber/fiber/v3"

// ShowWindow brings the player's own window to the front; false when it runs in the
// browser. Set in main.
var ShowWindow func() bool

// Window handles POST /api/window/show: a second start of the player asks the running one
// to show its window. 404: no window, the caller opens the browser instead.
func Window(c fiber.Ctx) error {
	if ShowWindow == nil || !ShowWindow() {
		return c.SendStatus(fiber.StatusNotFound)
	}
	return c.SendStatus(fiber.StatusNoContent)
}
