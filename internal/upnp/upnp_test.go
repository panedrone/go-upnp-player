package upnp

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type soapServer struct {
	*httptest.Server
	mu     sync.Mutex
	action string
	body   string
}

// newSOAP answers every action with the given status and response body.
func newSOAP(t *testing.T, status int, resp string) *soapServer {
	t.Helper()
	s := &soapServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.action, s.body = r.Header.Get("SOAPACTION"), string(b)
		s.mu.Unlock()
		w.WriteHeader(status)
		_, _ = io.WriteString(w, resp)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *soapServer) device() Device {
	return Device{AVTransportURL: s.URL, RenderingControlURL: s.URL, ConnectionMgrURL: s.URL}
}

func response(action, inner string) string {
	return `<s:Envelope xmlns:s="x"><s:Body><u:` + action + `Response xmlns:u="y">` + inner + `</u:` + action + `Response></s:Body></s:Envelope>`
}

func TestActionsSendWhatTheSpecAsks(t *testing.T) {
	ctx := context.Background()
	s := newSOAP(t, 200, response("X", ""))
	d := s.device()
	cases := []struct {
		name, action string
		want         []string
		call         func() error
	}{
		{"seturi", "AVTransport:1#SetAVTransportURI", []string{"<CurrentURI>http://a?x=1&amp;y=2</CurrentURI>", "<CurrentURIMetaData>&lt;m/&gt;</CurrentURIMetaData>"}, func() error { return SetURI(ctx, d, "http://a?x=1&y=2", "<m/>") }},
		{"setnext", "AVTransport:1#SetNextAVTransportURI", []string{"<NextURI>u</NextURI>"}, func() error { return SetNextURI(ctx, d, "u", "") }},
		{"play", "AVTransport:1#Play", []string{"<InstanceID>0</InstanceID><Speed>1</Speed>"}, func() error { return Play(ctx, d) }},
		{"pause", "AVTransport:1#Pause", []string{"<InstanceID>0</InstanceID>"}, func() error { return Pause(ctx, d) }},
		{"stop", "AVTransport:1#Stop", nil, func() error { return Stop(ctx, d) }},
		{"seek", "AVTransport:1#Seek", []string{"<Unit>REL_TIME</Unit><Target>0:01:05</Target>"}, func() error { return Seek(ctx, d, 65) }},
		{"volume", "RenderingControl:1#SetVolume", []string{"<Channel>Master</Channel><DesiredVolume>7</DesiredVolume>"}, func() error { return SetVolume(ctx, d, 7) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := c.call(); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(s.action, c.action) {
				t.Fatalf("SOAPACTION %s", s.action)
			}
			for _, w := range c.want {
				if !strings.Contains(s.body, w) {
					t.Fatalf("%s not in %s", w, s.body)
				}
			}
		})
	}
}

func TestReadingActions(t *testing.T) {
	ctx := context.Background()
	d := newSOAP(t, 200, response("GetTransportInfo", `<CurrentTransportState>PLAYING</CurrentTransportState><CurrentTransportStatus>OK</CurrentTransportStatus>`)).device()
	if ti, err := GetTransportInfo(ctx, d); err != nil || ti.State != "PLAYING" || ti.Status != "OK" {
		t.Fatalf("%+v %v", ti, err)
	}
	d = newSOAP(t, 200, response("GetPositionInfo", `<TrackURI>http://a&amp;b</TrackURI><TrackDuration>0:03:00</TrackDuration><RelTime>0:00:10</RelTime>`)).device()
	if pi, err := GetPositionInfo(ctx, d); err != nil || pi.TrackURI != "http://a&b" || pi.Duration != 180 || pi.Position != 10 {
		t.Fatalf("%+v %v", pi, err)
	}
	d = newSOAP(t, 200, response("GetVolume", `<CurrentVolume> 42 </CurrentVolume>`)).device()
	if v, err := GetVolume(ctx, d); err != nil || v != 42 {
		t.Fatalf("%d %v", v, err)
	}
	d = newSOAP(t, 200, response("GetProtocolInfo", `<Sink>a, b,,c</Sink>`)).device()
	if got := getSinkProtocols(ctx, &d); len(got) != 3 || got[1] != "b" {
		t.Fatalf("%v", got)
	}
}

func TestFaultsBecomeTypedErrors(t *testing.T) {
	ctx := context.Background()
	fault := `<Envelope><Body><Fault><detail><UPnPError><errorCode>701</errorCode><errorDescription>Transition not available</errorDescription></UPnPError></detail></Fault></Body></Envelope>`
	d := newSOAP(t, 500, fault).device()
	err := Play(ctx, d)
	var se *SOAPError
	if !errors.As(err, &se) || se.Code != "701" || se.HTTPStatus != 500 || !strings.Contains(err.Error(), "Transition") {
		t.Fatalf("%v", err)
	}
	if _, err := GetTransportInfo(ctx, d); err == nil {
		t.Fatal("want error")
	}
	if _, err := GetPositionInfo(ctx, d); err == nil {
		t.Fatal("want error")
	}
	if _, err := GetVolume(ctx, d); err == nil {
		t.Fatal("want error")
	}
	if got := getSinkProtocols(ctx, &d); got != nil {
		t.Fatalf("%v", got)
	}
	d = newSOAP(t, 500, "not xml").device()
	if !errors.As(Play(ctx, d), &se) || se.Code != "?" {
		t.Fatalf("%+v", se)
	}
}

func TestCallTransportErrors(t *testing.T) {
	ctx := context.Background()
	if err := Play(ctx, Device{}); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("%v", err)
	}
	if err := Play(ctx, Device{AVTransportURL: "http://127.0.0.1:1/"}); err == nil {
		t.Fatal("want a connection error")
	}
	if err := Play(ctx, Device{AVTransportURL: "::bad"}); err == nil {
		t.Fatal("want a URL error")
	}
	if _, err := parseResponse([]byte("<a><b"), "a"); err == nil {
		t.Fatal("want an XML error")
	}
	got, err := parseResponse([]byte(`<R><A>1<x>skip</x></A><B/></R><C>after</C>`), "R")
	if err != nil || got["A"] != "1" || len(got) != 2 {
		t.Fatalf("%v %v", got, err)
	}
	got, err = parseResponse([]byte(`<Other/>`), "R")
	if err != nil || len(got) != 0 {
		t.Fatalf("%v %v", got, err)
	}
}

const description = `<root><device>
  <deviceType>urn:schemas-upnp-org:device:MediaServer:1</deviceType>
  <deviceList><device>
    <deviceType>urn:schemas-upnp-org:device:MediaRenderer:1</deviceType>
    <friendlyName> Streamer </friendlyName>
    <manufacturer>Acme</manufacturer>
    <modelName>M1</modelName>
    <UDN>uuid:1</UDN>
    <serviceList>
      <service><serviceType>urn:schemas-upnp-org:service:AVTransport:2</serviceType><controlURL>/ctl/avt</controlURL></service>
      <service><serviceType>urn:schemas-upnp-org:service:RenderingControl:1</serviceType><controlURL>ctl/rc</controlURL></service>
      <service><serviceType>urn:schemas-upnp-org:service:ConnectionManager:1</serviceType><controlURL>http://other:9/cm</controlURL></service>
    </serviceList>
  </device></deviceList>
</device></root>`

func page(t *testing.T, status int, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/dir/desc.xml"
}

func TestFetchDeviceFindsNestedRenderer(t *testing.T) {
	loc := page(t, 200, description)
	base := strings.TrimSuffix(loc, "/dir/desc.xml")
	d, err := fetchDevice(context.Background(), loc)
	if err != nil {
		t.Fatal(err)
	}
	if d.UDN != "uuid:1" || d.Name != "Streamer" || d.Model != "M1" || d.IP != "127.0.0.1" || d.LocalIP != "127.0.0.1" {
		t.Fatalf("%+v", d)
	}
	if d.AVTransportURL != base+"/ctl/avt" || d.RenderingControlURL != base+"/dir/ctl/rc" || d.ConnectionMgrURL != "http://other:9/cm" {
		t.Fatalf("%+v", d)
	}
}

func TestFetchDeviceURLBase(t *testing.T) {
	body := strings.Replace(description, "<root>", "<root><URLBase>http://base:1/x/</URLBase>", 1)
	d, err := fetchDevice(context.Background(), page(t, 200, body))
	if err != nil || d.RenderingControlURL != "http://base:1/x/ctl/rc" {
		t.Fatalf("%+v %v", d, err)
	}
}

func TestFetchDeviceRejects(t *testing.T) {
	ctx := context.Background()
	noAVT := strings.ReplaceAll(description, "AVTransport", "Other")
	server := strings.ReplaceAll(description, "MediaRenderer", "Light")
	for name, loc := range map[string]string{
		"status":    page(t, 404, ""),
		"xml":       page(t, 200, "<root><device>"),
		"renderer":  page(t, 200, server),
		"transport": page(t, 200, noAVT),
		"refused":   "http://127.0.0.1:1/x",
		"url":       "::bad",
	} {
		if d, err := fetchDevice(ctx, loc); err == nil {
			t.Fatalf("%s: accepted %+v", name, d)
		}
	}
}

func TestSmallHelpers(t *testing.T) {
	if got := itoa(0); got != "0" {
		t.Fatal(got)
	}
	if got := itoa(1234); got != "1234" {
		t.Fatal(got)
	}
	if hostOf("::bad") != "" || localIPFor("::bad") != "" {
		t.Fatal("a bad URL has no host")
	}
	if got := resolveURL("::bad", "x"); got != "x" {
		t.Fatal(got)
	}
	if got := resolveURL("http://a/", "::bad"); got != "::bad" {
		t.Fatal(got)
	}
	for _, ip := range localIPv4s() {
		if ip.IsLoopback() || ip.To4() == nil {
			t.Fatalf("%v", ip)
		}
	}
}

// ssdpResponder plays the multicast group on the loopback.
func ssdpResponder(t *testing.T, replies ...string) *net.UDPAddr {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	go func() {
		buf := make([]byte, 2048)
		for {
			_, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			for _, r := range replies {
				_, _ = conn.WriteToUDP([]byte(r), from)
			}
		}
	}()
	return conn.LocalAddr().(*net.UDPAddr)
}

func ssdpReply(st, loc string) string {
	return "HTTP/1.1 200 OK\r\nST: " + st + "\r\nLOCATION: " + loc + "\r\n\r\n"
}

func TestDiscoverOverLoopback(t *testing.T) {
	good := page(t, 200, description)
	notRenderer := page(t, 200, strings.ReplaceAll(description, "MediaRenderer", "Light"))
	oldAddr, oldList := ssdpAddr, listLocalIPv4s
	t.Cleanup(func() { ssdpAddr, listLocalIPv4s = oldAddr, oldList })
	ssdpAddr = ssdpResponder(t,
		ssdpReply("urn:schemas-upnp-org:device:MediaRenderer:1", good),
		ssdpReply("urn:schemas-upnp-org:device:Basic:1", notRenderer),
		ssdpReply("urn:schemas-upnp-org:service:Other:1", "http://ignored/"),
		"HTTP/1.1 200 OK\r\nST: x\r\n\r\n", // no LOCATION
		"garbage",
	)
	listLocalIPv4s = func() []net.IP { return []net.IP{net.IPv4(127, 0, 0, 1)} }

	found, err := Discover(context.Background(), 300*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].UDN != "uuid:1" {
		t.Fatalf("%+v", found)
	}
}

func TestSearchFromUnusableAddress(t *testing.T) {
	if got := searchFrom(net.IPv4(203, 0, 113, 1), time.Millisecond); got != nil {
		t.Fatalf("%v", got)
	}
}
