package testkit

import (
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/panedrone/go-upnp-player/internal/services"
	"github.com/panedrone/go-upnp-player/internal/upnp"
)

// Call is one SOAP action the renderer received.
type Call struct {
	Action string
	Args   map[string]string
}

// Renderer is a fake gmrender on a local HTTP server: it takes
// SetAVTransportURI only from STOPPED, moves to the queued next URI when seeked
// to the end of the current track, and fails the test on AVTransport.Stop,
// which hangs the real FiiO SR11.
type Renderer struct {
	Device upnp.Device

	t   testing.TB
	srv *httptest.Server

	mu         sync.Mutex
	calls      []Call
	state      string
	uri        string
	next       string
	pos        float64
	volume     int
	lengths    map[string]float64
	fail       map[string]bool
	ignoreSeek int
	noAdvance  bool
}

// NewRenderer starts a fake renderer; it stops with the test.
func NewRenderer(t testing.TB, udn string) *Renderer {
	t.Helper()
	r := &Renderer{t: t, state: "STOPPED", volume: 40, lengths: map[string]float64{}, fail: map[string]bool{}}
	r.srv = httptest.NewServer(http.HandlerFunc(r.serve))
	t.Cleanup(r.srv.Close)
	r.Device = upnp.Device{
		UDN: udn, Name: udn, LocalIP: "127.0.0.1",
		AVTransportURL:      r.srv.URL + "/avt",
		RenderingControlURL: r.srv.URL + "/rc",
		ConnectionMgrURL:    r.srv.URL + "/cm",
		SinkProtocols:       []string{"http-get:*:audio/x-wav:*", "http-get:*:audio/flac:*"},
	}
	return r
}

// Close takes the renderer off the network.
func (r *Renderer) Close() { r.srv.Close() }

// With runs fn under the renderer's lock, for setting up a state.
func (r *Renderer) With(fn func(s *RendererState)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := RendererState{State: r.state, URI: r.uri, Next: r.next, Pos: r.pos, Volume: r.volume, IgnoreSeeks: r.ignoreSeek, NoAdvance: r.noAdvance}
	fn(&s)
	r.state, r.uri, r.next, r.pos, r.volume, r.ignoreSeek, r.noAdvance = s.State, s.URI, s.Next, s.Pos, s.Volume, s.IgnoreSeeks, s.NoAdvance
}

// RendererState is the part of the fake a test may set or read.
type RendererState struct {
	State, URI, Next string
	Pos              float64
	Volume           int
	IgnoreSeeks      int  // this many Seeks are silently dropped, like gmrender on a fresh stream
	NoAdvance        bool // a seek to the end does not move to the next URI
}

// Get returns the current state.
func (r *Renderer) Get() RendererState {
	var out RendererState
	r.With(func(s *RendererState) { out = *s })
	return out
}

// SetLength sets the length a track reports, by track ID.
func (r *Renderer) SetLength(id string, sec float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lengths[id] = sec
}

// Fail makes an action answer with a UPnP fault.
func (r *Renderer) Fail(action string, on bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fail[action] = on
}

// Count returns how many times an action was received.
func (r *Renderer) Count(action string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, c := range r.calls {
		if c.Action == action {
			n++
		}
	}
	return n
}

// Last returns the arguments of the latest call of an action (nil if none).
func (r *Renderer) Last(action string) map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.calls) - 1; i >= 0; i-- {
		if r.calls[i].Action == action {
			return r.calls[i].Args
		}
	}
	return nil
}

var argRe = regexp.MustCompile(`<(\w+)>([^<]*)</\w+>`)

func (r *Renderer) serve(w http.ResponseWriter, req *http.Request) {
	body, _ := io.ReadAll(req.Body)
	soap := strings.Trim(req.Header.Get("SOAPACTION"), `"`)
	action := soap[strings.LastIndex(soap, "#")+1:]
	args := map[string]string{}
	for _, m := range argRe.FindAllStringSubmatch(string(body), -1) {
		args[m[1]] = html.UnescapeString(m[2])
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, Call{Action: action, Args: args})
	if r.fail[action] {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `<Envelope><Body><Fault><detail><UPnPError><errorCode>501</errorCode><errorDescription>Action failed</errorDescription></UPnPError></detail></Fault></Body></Envelope>`)
		return
	}
	out := map[string]string{}
	switch action {
	case "SetAVTransportURI":
		if r.state == "STOPPED" || r.state == "NO_MEDIA_PRESENT" {
			r.uri, r.pos = args["CurrentURI"], 0
		}
	case "SetNextAVTransportURI":
		r.next = args["NextURI"]
	case "Play":
		r.state = "PLAYING"
	case "Pause":
		r.state = "PAUSED_PLAYBACK"
	case "Stop":
		r.t.Errorf("AVTransport.Stop sent: it hangs the FiiO SR11 until a power cycle")
	case "Seek":
		if r.ignoreSeek > 0 {
			r.ignoreSeek--
			break
		}
		r.pos = services.ParseHMS(args["Target"])
		if d := r.length(r.uri); !r.noAdvance && r.next != "" && d > 0 && r.pos >= d-1.5 {
			r.uri, r.next, r.pos = r.next, "", 0
		}
	case "GetTransportInfo":
		out["CurrentTransportState"] = r.state
		out["CurrentTransportStatus"] = "OK"
	case "GetPositionInfo":
		out["TrackURI"] = html.EscapeString(r.uri)
		out["TrackDuration"] = services.FormatHMS(r.length(r.uri))
		out["RelTime"] = services.FormatHMS(r.pos)
	case "GetVolume":
		out["CurrentVolume"] = strconv.Itoa(r.volume)
	case "SetVolume":
		r.volume, _ = strconv.Atoi(args["DesiredVolume"])
	case "GetProtocolInfo":
		out["Sink"] = "http-get:*:audio/x-wav:*, http-get:*:audio/flac:*"
	}
	var b strings.Builder
	b.WriteString(`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:` + action + `Response xmlns:u="x">`)
	for k, v := range out {
		b.WriteString("<" + k + ">" + v + "</" + k + ">")
	}
	b.WriteString(`</u:` + action + `Response></s:Body></s:Envelope>`)
	_, _ = io.WriteString(w, b.String())
}

// length looks a URI up by the track ID in it: .../<id>.wav?ct=...
func (r *Renderer) length(uri string) float64 {
	id := uri[strings.LastIndex(uri, "/")+1:]
	if i := strings.IndexAny(id, ".?"); i >= 0 {
		id = id[:i]
	}
	return r.lengths[id]
}
