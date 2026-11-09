package handlers

import (
	"github.com/gofiber/fiber/v3"

	"go-upnp-player/internal/config"
	"go-upnp-player/internal/library"
)

// Tree handles GET /api/tree?path= : children of a node ("" means the roots).
func Tree(c fiber.Ctx) error {
	nodes := library.Children(c.Query("path"))
	if nodes == nil {
		nodes = []library.Node{}
	}
	return c.JSON(nodes)
}

// Tracks handles GET /api/tracks?path= : all tracks under a node.
func Tracks(c fiber.Ctx) error {
	tracks := library.TracksUnder(c.Query("path"))
	if tracks == nil {
		tracks = []*library.Track{}
	}
	return c.JSON(tracks)
}

// Search — GET /api/search?q=
func Search(c fiber.Ctx) error {
	tracks := library.Search(c.Query("q"), 2000)
	if tracks == nil {
		tracks = []*library.Track{}
	}
	return c.JSON(tracks)
}

// Rescan handles POST /api/library/rescan (runs in the background).
func Rescan(c fiber.Ctx) error {
	go library.Scan(config.Get().Roots)
	return c.SendStatus(fiber.StatusAccepted)
}

type settingsDTO struct {
	Roots []string `json:"roots"`
}

// GetSettings — GET /api/settings
func GetSettings(c fiber.Ctx) error {
	roots := config.Get().Roots
	if roots == nil {
		roots = []string{}
	}
	return c.JSON(settingsDTO{Roots: roots})
}

// PutSettings handles PUT /api/settings: saves the folders and rescans.
func PutSettings(c fiber.Ctx) error {
	var in settingsDTO
	if err := c.Bind().Body(&in); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	if err := config.Update(func(cfg *config.Config) { cfg.Roots = in.Roots }); err != nil {
		return err
	}
	go library.Scan(in.Roots)
	return c.SendStatus(fiber.StatusNoContent)
}
