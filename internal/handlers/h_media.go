package handlers

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/panedrone/go-upnp-player/internal/library"
	"github.com/panedrone/go-upnp-player/internal/services"
	"github.com/panedrone/go-upnp-player/internal/stream"
)

// MediaTrack handles GET/HEAD /media/track/:file (<id>.wav|.flac?ct=mime[&from=ms]). The renderer
// fetches it. from: the stream starts that many milliseconds into the track (a start at a
// position, ADR-015).
func MediaTrack(c fiber.Ctx) error {
	ext := filepath.Ext(c.Params("file"))
	t := library.Get(strings.TrimSuffix(c.Params("file"), ext))
	if t == nil {
		return fiber.ErrNotFound
	}
	fromMs, _ := strconv.ParseInt(c.Query("from"), 10, 64)
	return serveSource(c, stream.ForTrack(t, ext == ".wav", fromMs))
}

// MediaRun handles GET/HEAD /media/run/:file (<run id>.wav?ct=mime): several tracks of one
// format as one WAV, registered by the player (ADR-016).
func MediaRun(c fiber.Ctx) error {
	src, ok := stream.ForRun(strings.TrimSuffix(c.Params("file"), filepath.Ext(c.Params("file"))), c.IP())
	if !ok {
		return fiber.ErrNotFound
	}
	return serveSource(c, src)
}

// serveSource answers a media request with Range support.
func serveSource(c fiber.Ctx, src stream.Source) error {
	start, end, partial, err := services.ParseRange(c.Get(fiber.HeaderRange), src.Size)
	if errors.Is(err, services.ErrRangeNotSatisfiable) {
		c.Set(fiber.HeaderContentRange, fmt.Sprintf("bytes */%d", src.Size))
		return c.SendStatus(fiber.StatusRequestedRangeNotSatisfiable)
	}

	ct := c.Query("ct")
	if ct == "" {
		ct = "application/octet-stream"
	}
	c.Set(fiber.HeaderContentType, ct)
	c.Set(fiber.HeaderAcceptRanges, "bytes")
	c.Set("transferMode.dlna.org", "Streaming")
	c.Set("contentFeatures.dlna.org", services.DLNAFeatures)
	length := end - start + 1
	if partial {
		c.Status(fiber.StatusPartialContent)
		c.Set(fiber.HeaderContentRange, fmt.Sprintf("bytes %d-%d/%d", start, end, src.Size))
	}
	if c.Method() == fiber.MethodHead {
		c.Response().Header.SetContentLength(int(length))
		c.Response().SkipBody = true
		return nil
	}
	r, err := src.Open(start)
	if err != nil {
		return err
	}
	c.RequestCtx().SetBodyStream(limitCloser{io.LimitReader(r, length), r}, int(length))
	return nil
}

type limitCloser struct {
	io.Reader
	c io.Closer
}

func (l limitCloser) Close() error { return l.c.Close() }

// MediaArt handles GET /media/art/:id: cover art for the renderer and the UI.
func MediaArt(c fiber.Ctx) error {
	t := library.Get(c.Params("id"))
	if t == nil {
		return fiber.ErrNotFound
	}
	art := library.FindArt(t)
	if art == nil {
		return fiber.ErrNotFound
	}
	c.Set(fiber.HeaderCacheControl, "max-age=3600")
	c.Set(fiber.HeaderContentType, art.Mime)
	if art.Path != "" {
		data, err := os.ReadFile(art.Path)
		if err != nil {
			return fiber.ErrNotFound
		}
		return c.Send(data)
	}
	return c.Send(art.Data)
}
