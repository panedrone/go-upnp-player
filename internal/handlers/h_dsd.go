package handlers

import (
	"github.com/gofiber/fiber/v3"

	"github.com/panedrone/go-upnp-player/internal/library"
	"github.com/panedrone/go-upnp-player/internal/player"
	"github.com/panedrone/go-upnp-player/internal/services"
)

type dsdDTO struct {
	services.DSDSettings
	Rates []uint32  `json:"rates"`
	Gains []float64 `json:"gains"`
}

// GetDSD handles GET /api/dsd: how DSD files are converted to PCM, and the choices.
func GetDSD(c fiber.Ctx) error {
	return c.JSON(dsdDTO{DSDSettings: library.DSD(), Rates: services.DSDRates, Gains: services.DSDGains})
}

// PutDSD handles PUT /api/dsd: new conversion settings, heard at once (player.SetDSD).
func PutDSD(c fiber.Ctx) error {
	var in services.DSDSettings
	if err := c.Bind().Body(&in); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return result(c, player.SetDSD(in))
}
