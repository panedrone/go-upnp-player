package handlers

import (
	"bufio"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v3"

	"go-upnp-player/internal/player"
)

// State — GET /api/state
func State(c fiber.Ctx) error {
	return c.JSON(player.Snapshot())
}

// Events handles GET /api/events: SSE with state snapshots.
func Events(c fiber.Ctx) error {
	c.Set(fiber.HeaderContentType, "text/event-stream")
	c.Set(fiber.HeaderCacheControl, "no-cache")
	c.Set(fiber.HeaderConnection, "keep-alive")
	return c.SendStreamWriter(func(w *bufio.Writer) {
		ch, cancel := player.Subscribe()
		defer cancel()
		keepalive := time.NewTicker(15 * time.Second)
		defer keepalive.Stop()
		send := func() bool {
			data, _ := json.Marshal(player.Snapshot())
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return false
			}
			return w.Flush() == nil
		}
		if !send() {
			return
		}
		for {
			select {
			case <-ch:
				if !send() {
					return
				}
			case <-keepalive.C:
				if _, err := w.WriteString(": ping\n\n"); err != nil || w.Flush() != nil {
					return
				}
			}
		}
	})
}

type queueDTO struct {
	IDs []string `json:"ids"`
}

// SetQueue handles PUT /api/queue {ids}: builds the playlist without touching playback.
func SetQueue(c fiber.Ctx) error {
	var in queueDTO
	if err := c.Bind().Body(&in); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	player.SetQueue(in.IDs)
	return c.SendStatus(fiber.StatusNoContent)
}

// Queue — GET /api/queue
func Queue(c fiber.Ctx) error {
	v, tracks := player.Queue()
	return c.JSON(fiber.Map{"version": v, "tracks": tracks})
}

type playDTO struct {
	IDs   []string `json:"ids"`
	Index int      `json:"index"`
}

// Play handles POST /api/player/play {ids, index}: a new queue.
// Without ids it jumps within the current queue.
func Play(c fiber.Ctx) error {
	var in playDTO
	if err := c.Bind().Body(&in); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	if len(in.IDs) == 0 {
		return result(c, player.PlayIndex(in.Index))
	}
	return result(c, player.PlayQueue(in.IDs, in.Index))
}

func Pause(c fiber.Ctx) error  { return result(c, player.Pause()) }
func Resume(c fiber.Ctx) error { return result(c, player.Resume()) }
func Stop(c fiber.Ctx) error   { return result(c, player.Stop()) }
func Next(c fiber.Ctx) error   { return result(c, player.Next()) }
func Prev(c fiber.Ctx) error   { return result(c, player.Prev()) }

type valueDTO struct {
	Value float64 `json:"value"`
}

// Seek handles POST /api/player/seek {value: seconds}
func Seek(c fiber.Ctx) error {
	var in valueDTO
	if err := c.Bind().Body(&in); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return result(c, player.Seek(in.Value))
}

// Volume — POST /api/player/volume {value: 0..100}
func Volume(c fiber.Ctx) error {
	var in valueDTO
	if err := c.Bind().Body(&in); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return result(c, player.SetVolume(int(in.Value)))
}

// RefreshRenderers — POST /api/renderers/refresh
func RefreshRenderers(c fiber.Ctx) error {
	go player.Discover()
	return c.SendStatus(fiber.StatusAccepted)
}

type selectDTO struct {
	UDN string `json:"udn"`
}

// SelectRenderer — POST /api/renderers/select {udn}
func SelectRenderer(c fiber.Ctx) error {
	var in selectDTO
	if err := c.Bind().Body(&in); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return result(c, player.Select(in.UDN))
}

func result(c fiber.Ctx, err error) error {
	if err != nil {
		return fiber.NewError(fiber.StatusBadGateway, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}
