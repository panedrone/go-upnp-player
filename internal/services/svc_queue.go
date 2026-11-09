package services

import (
	"math"
	"time"
)

// TickAction is what the player should do after the latest renderer poll.
type TickAction int

const (
	TickNone     TickAction = iota
	TickAdvanced            // the renderer moved to the next track by itself (SetNextAVTransportURI)
	TickPlayNext            // the track finished and stopped: start the next one manually
	TickFinished            // the queue is exhausted
)

// TickInput is the state snapshot for DecideTick.
type TickInput struct {
	WantPlaying bool          // the user did not press stop/pause
	State       string        // the renderer's CurrentTransportState
	SinceCmd    time.Duration // since our last Play/Seek command
	TrackURI    string        // what the renderer calls current (GetPositionInfo.TrackURI)
	NextURL     string        // what we set via SetNext, "" if nothing was set
	HasNext     bool          // the queue has a next track

	Duration     float64 // TrackDuration from the renderer: length of what is actually playing
	Position     float64 // RelTime now
	LastPosition float64 // RelTime at the previous poll
	CurDuration  float64 // length of the current queue track per the library
	NextDuration float64 // length of the next queue track per the library
}

// durTolerance is the allowed mismatch between renderer and library durations, s. The renderer
// rounds to whole seconds.
const durTolerance = 1.5

// StartGrace is how long after a command not to trust STOPPED: many renderers
// pass through it while buffering the stream.
const StartGrace = 4 * time.Second

// DecideTick decides on queue advancement. A pure function; all the
// protocol noise (TRANSITIONING, brief STOPPEDs) is filtered out here.
func DecideTick(in TickInput) TickAction {
	if !in.WantPlaying {
		return TickNone
	}
	if in.NextURL != "" && in.TrackURI == in.NextURL && reallyAdvanced(in) {
		return TickAdvanced
	}
	if in.SinceCmd < StartGrace {
		return TickNone
	}
	if in.State == "STOPPED" || in.State == "NO_MEDIA_PRESENT" {
		if in.HasNext {
			return TickPlayNext
		}
		return TickFinished
	}
	return TickNone
}

// reallyAdvanced reports whether the renderer is actually playing next, not merely
// calling it current. gmrender (FiiO SR11, seen live 2026-11-09) switches TrackURI to next
// as soon as GStreamer has pulled the current WAV into its buffer, minutes before
// the track ends; the sound is still the old one, and so are TrackDuration/RelTime.
// Trust the transition if the duration became the next track's duration
// (and differs from the current one) or the position rolled back to the start.
func reallyAdvanced(in TickInput) bool {
	near := func(a, b float64) bool { return math.Abs(a-b) <= durTolerance }
	if in.Duration > 0 && near(in.Duration, in.NextDuration) && !near(in.Duration, in.CurDuration) {
		return true
	}
	// Use position rollback only when durations cannot tell the tracks apart: gmrender
	// itself resets RelTime a couple of seconds back at the moment of the early URI switch.
	// A rollback right after our own backward seek is not a transition either.
	return near(in.CurDuration, in.NextDuration) && in.SinceCmd >= StartGrace && in.LastPosition-in.Position > 5
}
