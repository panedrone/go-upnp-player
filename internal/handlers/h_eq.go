package handlers

import (
	"github.com/gofiber/fiber/v2"

	"go-upnp-player/internal/config"
	"go-upnp-player/internal/player"
	"go-upnp-player/internal/services"
)

type eqDTO struct {
	Enabled bool      `json:"enabled"`
	Gains   []float64 `json:"gains"`
	Bands   []float64 `json:"bands"`
	MaxGain float64   `json:"maxGain"`
}

// GetEQ — GET /api/eq
func GetEQ(c *fiber.Ctx) error {
	s := config.Get().EQ.Normalize()
	return c.JSON(eqDTO{Enabled: s.Enabled, Gains: s.Gains, Bands: services.EQBands, MaxGain: services.EQMaxGain})
}

// PutEQ — PUT /api/eq {enabled, gains}
func PutEQ(c *fiber.Ctx) error {
	var in eqDTO
	if err := c.BodyParser(&in); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return result(c, player.SetEQ(services.EQSettings{Enabled: in.Enabled, Gains: in.Gains}))
}
