package services

import (
	"math"
	"strings"
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

// StallAfter is how long a PLAYING renderer's position may stand still. Generous
// enough for a stream that is still buffering.
const StallAfter = 8 * time.Second

// Stalled reports a renderer that claims to play while its position is frozen.
// gmrender stays in PLAYING after its stream dies (BeagleBone, 2026-11-09).
func Stalled(state string, frozenFor time.Duration) bool {
	return state == "PLAYING" && frozenFor >= StallAfter
}

// RacingTicks is how many polls in a row must see a racing position before the
// renderer is restarted: a single jump can be a seek that landed late.
const RacingTicks = 3

// Racing reports a renderer whose position within one track runs ahead of the
// clock. gmrender gets into this with nothing pacing it: it reports PLAYING, races
// through the queue and plays no sound; Pause/Play does not cure it, only
// Stop + SetAVTransportURI + Play does (BeagleBone, 2026-11-09). The speed is
// whatever the download allows: 30–40x once, ~5x another time, so the margin is
// small — RelTime is in whole seconds, so a 1 s poll can see 2 s, never 3.
func Racing(state string, advanced float64, elapsed time.Duration) bool {
	return state == "PLAYING" && elapsed > 0 && advanced > 1.5*elapsed.Seconds()+1.5
}

// SwitchPlan says how to move a renderer to another track.
type SwitchPlan struct {
	ViaNext bool    // SetNextAVTransportURI + seek to the end instead of SetAVTransportURI
	SeekTo  float64 // where to seek the current track, seconds
	Resume  bool    // the renderer is paused: send Play after the seek
}

// PlanSwitch picks the switching method from the renderer state and the length
// of what it is playing. gmrender ignores SetAVTransportURI unless STOPPED, and
// Stop hangs the FiiO SR11, so a playing or paused renderer goes through its
// gapless path. Without a known length there is nowhere to seek: fall back to
// SetAVTransportURI.
func PlanSwitch(state string, curDuration float64) SwitchPlan {
	busy := state == "PLAYING" || state == "PAUSED_PLAYBACK"
	if !busy || curDuration < 3 {
		return SwitchPlan{}
	}
	return SwitchPlan{
		ViaNext: true,
		SeekTo:  curDuration - 1,
		Resume:  state == "PAUSED_PLAYBACK",
	}
}

// SwitchDone reports whether the renderer now plays the target track: the length
// it reports matches (it rounds to whole seconds) and playback is near the start.
// The length alone is not enough: replaying the current track, or a track of the
// same length, matched at once while the old track still sat at its last second
// (seen live 2026-11-09).
func SwitchDone(rendererDur, rendererPos, targetDur float64) bool {
	return rendererDur > 0 && math.Abs(rendererDur-targetDur) <= durTolerance && rendererPos < 10
}

// ShouldReplace decides whether a newly started exe replaces the player that is
// already running: yes when the builds differ (an older one has no build id at
// all), no when they are the same build or our own id is unknown.
func ShouldReplace(running, ours string) bool {
	return ours != "" && running != ours
}

// SeekLanded reports whether the renderer's position is where we seeked to.
// The window allows for the time between the Seek and the position query and
// for renderers that seek to the nearest frame or keyframe.
func SeekLanded(pos, target float64) bool {
	return pos >= target-2 && pos <= target+5
}

// IsIndexPath reports whether a request path serves the UI's index.html.
func IsIndexPath(p string) bool {
	return p == "" || p == "/" || p == "/index.html"
}

// SameLength compares a renderer-reported length (whole seconds) with a library one.
func SameLength(rendererDur, libDur float64) bool {
	return rendererDur > 0 && math.Abs(rendererDur-libDur) <= durTolerance
}

// IsStaleBundlePath reports whether a missing static path looks like the UI
// bundle of another build (Vite names it assets/index-<hash>.js).
func IsStaleBundlePath(p string) bool {
	return strings.HasPrefix(p, "/assets/index-") && strings.HasSuffix(p, ".js")
}
