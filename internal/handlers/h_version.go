package handlers

import "github.com/gofiber/fiber/v3"

// BuildID identifies the running exe; set in main.
var BuildID string

// Version handles GET /api/version: lets a newly started exe tell whether the
// running player is the same build or an older one to replace.
func Version(c fiber.Ctx) error {
	return c.JSON(fiber.Map{"build": BuildID})
}
