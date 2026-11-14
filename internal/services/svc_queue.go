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
	if in.State == "STOPPED" || in.State == "NO_MEDIA_PRESENT" || lastTrackPlayedOut(in) {
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

// AtEnd: the position is within 2 s of the end of the track. A start there is a
// start of a track that has already ended.
func AtEnd(position, duration float64) bool {
	return duration > 0 && position >= duration-2
}

// lastTrackPlayedOut: the last track's position has reached its length. The FiiO
// SR11 stays in PLAYING there instead of going STOPPED (2026-11-10), so the end of
// the playlist is taken from the length, which is known, not from the renderer's
// state. Only for the last track: before any other the renderer moves on by itself
// (gapless), and a position near the end is just the last second playing.
// A stream that starts late in the track (ADR-015) can be so short that the renderer
// reports no length: the expected length of the current track stands in then.
func lastTrackPlayedOut(in TickInput) bool {
	dur := in.Duration
	if dur <= 0 {
		dur = in.CurDuration
	}
	return in.State == "PLAYING" && !in.HasNext && dur > 0 && in.Position >= dur-1
}

// StallAfter is how long a PLAYING renderer's position may stand still. Generous
// enough for a stream that is still buffering.
const StallAfter = 8 * time.Second

// Stalled reports a renderer that claims to play while its position is frozen.
// gmrender stays in PLAYING after its stream dies (BeagleBone, 2026-11-09). A
// position frozen at the very end of the track is a track that has ended, not a
// hang: the FiiO SR11 sits like that after the last track (2026-11-10).
func Stalled(state string, frozenFor time.Duration, position, duration float64) bool {
	return state == "PLAYING" && frozenFor >= StallAfter && !AtEnd(position, duration)
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

// FromSample is the first sample of a track stream that starts fromMs into the
// track [start, end): a start at a position is served as a stream beginning there,
// so the renderer never plays the opening first (ADR-015). It stays inside the track.
func FromSample(start, end uint64, sampleRate uint32, fromMs int64) uint64 {
	if fromMs <= 0 || sampleRate <= 0 {
		return start
	}
	s := start + uint64(fromMs)*uint64(sampleRate)/1000
	if s >= end {
		return start
	}
	return s
}

// StopSafe reports whether a renderer takes AVTransport.Stop without harm, judged by
// the model name from its device description. Stock gmrender-resurrect calls itself
// "gmediarender" (the BeagleBone); the FiiO SR11 runs a gmrender build of its own under
// its own name, and Stop hangs it until a power cycle. Anything else is unknown, so it
// is treated like the FiiO. See ADR-002 and ADR-007.
func StopSafe(model string) bool {
	return model == "gmediarender"
}

// A start the renderer refused ("501 Playing failed") is retried by the poll for a
// while before it is reported. The BeagleBone refuses while its USB DAC is not the
// active input of the amplifier: switching the amplifier from coaxial to USB on the
// remote takes the owner a few seconds (ADR-014).
const (
	StartRetryFor   = 10 * time.Second
	StartRetryEvery = 2 * time.Second
)

// RetryStart decides what the poll does with a refused start: retry it now, keep
// waiting, or give up. pending is how long ago the start was first refused, sinceTry
// how long ago it was last tried.
func RetryStart(pending, sinceTry time.Duration) (retry, giveUp bool) {
	if pending >= StartRetryFor {
		return false, true
	}
	return sinceTry >= StartRetryEvery, false
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
// all), no when they are the same build or our own id is unknown. An older version
// never replaces a newer one: with both a setup copy and a go install copy on the
// machine, starting the stale one took the fresh one down (2026-11-13). Equal versions
// of different builds (a local rebuild) still replace.
func ShouldReplace(runningBuild, runningVersion, ours, ourVersion string) bool {
	if ours == "" || runningBuild == ours {
		return false
	}
	return !NewerVersion(ourVersion, runningVersion)
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

// PlausibleStep reports whether a renderer's new stream position can follow from the
// last one without a seek: it does not go back by more than the renderer's rounding.
// gmrender now and then answers with a position far behind where it plays (0:00:00,
// live on the BeagleBone, 2026-11-14); one such answer is not to be acted on. Running
// ahead is left to the racing check (Racing), which needs several polls in a row.
func PlausibleStep(last, now float64) bool {
	return now-last >= -1.5
}
