package player

import (
	"github.com/panedrone/go-upnp-player/internal/config"
	"github.com/panedrone/go-upnp-player/internal/library"
	"github.com/panedrone/go-upnp-player/internal/services"
)

// SetDSD changes how DSD files are converted (ADR-028) and saves it in its own file. A DSD
// track that plays is restarted where it is, so the change is heard at once, as the
// equalizer's is: the gain applies to a stream as it opens, and a new rate is a new stream
// format, which needs the library's DSD tracks rebuilt first (from its cache).
//
// While a change is being applied (the rescan, the restart) the state says so: the DSD card
// shows a spinner by its selects.
func SetDSD(s services.DSDSettings) error {
	s = s.Normalize()
	if err := config.SaveDSD(s); err != nil {
		return err
	}
	dsdApplyingAdd(1)
	old := library.DSD()
	library.SetDSD(s)
	if s.Rate == old.Rate {
		defer dsdApplyingAdd(-1)
		return restartDSD()
	}
	go func() {
		defer dsdApplyingAdd(-1)
		library.Scan(config.Get().Roots)
		// the stream the renderer holds is in the old rate: the restart must not reuse it
		mu.Lock()
		staleRun = true
		mu.Unlock()
		setErr(restartDSD())
	}()
	return nil
}

// dsdApplyingAdd counts the DSD changes still being applied (two quick changes overlap).
func dsdApplyingAdd(n int) {
	mu.Lock()
	dsdApplying += n
	mu.Unlock()
}

// restartDSD starts the playing track again where it is, if it is a DSD file.
func restartDSD() error {
	mu.Lock()
	active := wantPlaying && !paused && index >= 0 && index < len(queue)
	pos := position
	id := ""
	if active {
		id = queue[index]
	}
	mu.Unlock()
	if !active {
		return nil
	}
	if t := lookupTrack(id); t == nil || t.DSD == nil {
		return nil
	}
	return Seek(pos)
}
