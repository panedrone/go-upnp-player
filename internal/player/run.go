package player

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/panedrone/go-upnp-player/internal/services"
	"github.com/panedrone/go-upnp-player/internal/stream"
	"github.com/panedrone/go-upnp-player/internal/upnp"
)

// The renderer plays runs, not tracks (ADR-016): the queue from a track on, for as long
// as the format stays the same, as one WAV stream. It never switches streams between
// those tracks, so nothing breaks in its output there. The player keeps the run's
// layout and turns the renderer's position in the stream into a track and a position
// in it.

// runPlan is a run to start or queue.
type runPlan struct {
	q      int       // queue index of the first track
	ids    []string  // the tracks, in order
	starts []float64 // where each track begins in the stream, s (the first at 0)
	durs   []float64 // track lengths per the library, s
	from   float64   // the stream starts this far into the first track, s (ADR-015)
	length float64   // the stream's length, s
	uri    string
	meta   string
}

// The current run, set by a successful start or an advance to the queued run; guarded by mu.
var (
	runQ      int
	runIDs    []string
	runStarts []float64
	runDurs   []float64
	runLen    float64
	runURI    string
	nextRun   runPlan // the run queued with SetNextAVTransportURI (nextURL is its URI)
	streamPos float64 // the renderer's position in the stream at the latest poll, s
)

// planRun lays out the run that starts at queue track i, from seconds into it, and
// registers it for serving.
func planRun(d upnp.Device, i int, from float64) (runPlan, error) {
	mu.Lock()
	ids := append([]string(nil), queue...)
	mu.Unlock()
	if i < 0 || i >= len(ids) {
		return runPlan{}, fmt.Errorf("index %d out of range", i)
	}
	t0 := lookupTrack(ids[i])
	if t0 == nil {
		return runPlan{}, fmt.Errorf("track %s is not in the library anymore", ids[i])
	}
	if t0.SampleRate == 0 {
		return runPlan{}, fmt.Errorf("track %q has no sample rate", t0.Title)
	}
	fromMs := int64(0)
	if from > 0 {
		fromMs = int64(from * 1000)
	}
	first := stream.TrackSpan(t0, fromMs)
	rp := runPlan{q: i, from: float64(first.Start-t0.Start) / float64(t0.SampleRate)}
	var spans []stream.Span
	var data int64
	for j := i; j < len(ids); j++ {
		t := lookupTrack(ids[j])
		if t == nil {
			break
		}
		sp := first
		if j > i {
			sp = stream.TrackSpan(t, 0)
			if !stream.SameFormat(spans[0], sp) {
				break // a new format is a new stream
			}
		}
		size := sp.Layout().DataLen()
		if j > i && data+size > stream.MaxRunData {
			break
		}
		rp.ids = append(rp.ids, ids[j])
		rp.starts = append(rp.starts, rp.length)
		rp.durs = append(rp.durs, t.Duration)
		rp.length += float64(sp.End-sp.Start) / float64(sp.SampleRate)
		data += size
		spans = append(spans, sp)
	}
	run := stream.Run{Spans: spans}
	id := stream.RegisterRun(run)
	mime := services.PickMime(d.SinkProtocols, "audio/wav", "audio/x-wav")
	base := "http://" + d.LocalIP + ":" + strconv.Itoa(mediaPort)
	rp.uri = base + "/media/run/" + id + ".wav?ct=" + url.QueryEscape(mime)
	rp.meta = services.BuildDIDL(services.TrackMeta{
		Title: t0.Title, Artist: t0.Artist, Album: t0.Album, Date: t0.Date, Genre: t0.Genre,
		TrackNumber: t0.Number, Duration: rp.length, SampleRate: t0.SampleRate,
		Bits: t0.Bits, Channels: t0.Channels, Size: run.Layout().TotalLen(), Mime: mime, URL: rp.uri,
		ArtURL: base + "/media/art/" + t0.ID,
	})
	return rp, nil
}

// setRunLocked makes rp the run the renderer plays, from its start.
func setRunLocked(rp runPlan) {
	runQ, runIDs, runStarts, runDurs, runLen, runURI = rp.q, rp.ids, rp.starts, rp.durs, rp.length, rp.uri
	offset = rp.from
	streamPos = 0
	nextURL, nextRun = "", runPlan{}
}

// runCoherentLocked reports whether the queue still holds the run's tracks where the
// run started: an edited playlist may not.
func runCoherentLocked() bool {
	if runQ < 0 || runQ+len(runIDs) > len(queue) {
		return false
	}
	for k, id := range runIDs {
		if queue[runQ+k] != id {
			return false
		}
	}
	return true
}

// locateLocked turns a position in the stream into the run's track k and the position
// in that track.
func locateLocked(p float64) (k int, pos float64) {
	for k+1 < len(runStarts) && p >= runStarts[k+1] {
		k++
	}
	if len(runStarts) == 0 {
		return 0, p + offset
	}
	pos = p - runStarts[k]
	if k == 0 {
		pos += offset
	}
	return k, pos
}

// queueIndexLocked is the queue index of the run's track k, -1 if the playlist no
// longer has it.
func queueIndexLocked(k int) int {
	if k < 0 || k >= len(runIDs) {
		return -1
	}
	if i := runQ + k; i < len(queue) && queue[i] == runIDs[k] {
		return i
	}
	for i, id := range queue {
		if id == runIDs[k] {
			return i
		}
	}
	return -1
}

// streamTimeLocked is where queue track i at pos seconds is in the current stream;
// ok is false when the stream does not hold it.
func streamTimeLocked(i int, pos float64) (float64, bool) {
	if len(runIDs) == 0 || !runCoherentLocked() {
		return 0, false
	}
	k := i - runQ
	if k < 0 || k >= len(runIDs) {
		return 0, false
	}
	if k == 0 {
		if pos < offset {
			return 0, false // before the stream's start (ADR-015)
		}
		return pos - offset, true
	}
	return runStarts[k] + pos, true
}

// retireStreams lets go of the runs the renderer is being moved off: the player's
// current one and the one it reports (gmrender names the queued next as current
// early), never the target. What it fetches of them from now on is silence
// (stream.RetireRun). The returned func takes that back.
func retireStreams(target, reported string) (restore func()) {
	mu.Lock()
	cur := runURI
	mu.Unlock()
	var undo []func()
	seen := map[string]bool{}
	for _, u := range []string{cur, reported} {
		id, ok := runIDOf(u)
		if !ok || seen[id] || u == target {
			continue
		}
		if tid, ok := runIDOf(target); ok && tid == id {
			continue
		}
		seen[id] = true
		undo = append(undo, stream.RetireRun(id))
	}
	return func() {
		for _, f := range undo {
			f()
		}
	}
}

// runIDOf is the run id in a run URL: http://ip:port/media/run/<id>.wav?ct=...
func runIDOf(u string) (string, bool) {
	i := strings.Index(u, "/media/run/")
	if i < 0 {
		return "", false
	}
	id := u[i+len("/media/run/"):]
	if j := strings.IndexAny(id, ".?"); j >= 0 {
		id = id[:j]
	}
	return id, id != ""
}
