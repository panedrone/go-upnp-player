package upnp

import (
	"bufio"
	"bytes"
	"context"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

var ssdpAddr = &net.UDPAddr{IP: net.IPv4(239, 255, 255, 250), Port: 1900}

// listLocalIPv4s is replaceable so that tests search over the loopback only.
var listLocalIPv4s = localIPv4s

// searchTargets are what we search for. Some renderers respond only to their own type,
// others only to ssdp:all, so we send both.
var searchTargets = []string{
	"urn:schemas-upnp-org:device:MediaRenderer:1",
	"ssdp:all",
}

// Discover sends M-SEARCH from all IPv4 interfaces and collects the renderers
// that respond within timeout.
func Discover(ctx context.Context, timeout time.Duration) ([]Device, error) {
	locations := make(map[string]struct{})
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, ip := range listLocalIPv4s() {
		wg.Add(1)
		go func(ip net.IP) {
			defer wg.Done()
			for _, loc := range searchFrom(ip, timeout) {
				mu.Lock()
				locations[loc] = struct{}{}
				mu.Unlock()
			}
		}(ip)
	}
	wg.Wait()

	devices := make(map[string]Device)
	var dwg sync.WaitGroup
	for loc := range locations {
		dwg.Add(1)
		go func(loc string) {
			defer dwg.Done()
			d, err := fetchDevice(ctx, loc)
			if err != nil {
				return // not a renderer or unreachable: not our concern
			}
			d.SinkProtocols = getSinkProtocols(ctx, d)
			mu.Lock()
			devices[d.UDN] = *d
			mu.Unlock()
		}(loc)
	}
	dwg.Wait()

	out := make([]Device, 0, len(devices))
	for _, d := range devices {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func searchFrom(ip net.IP, timeout time.Duration) []string {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: ip, Port: 0})
	if err != nil {
		return nil
	}
	defer func() { _ = conn.Close() }()

	mx := int(timeout.Seconds())
	if mx < 1 {
		mx = 1
	}
	for _, st := range searchTargets {
		msg := "M-SEARCH * HTTP/1.1\r\n" +
			"HOST: 239.255.255.250:1900\r\n" +
			"MAN: \"ssdp:discover\"\r\n" +
			"MX: " + itoa(mx) + "\r\n" +
			"ST: " + st + "\r\n\r\n"
		for range 2 { // UDP gets lost, so send twice
			_, _ = conn.WriteToUDP([]byte(msg), ssdpAddr)
		}
	}

	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	var out []string
	buf := make([]byte, 4096)
	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			return out
		}
		resp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(buf[:n])), nil)
		if err != nil {
			continue
		}
		_ = resp.Body.Close()
		loc := resp.Header.Get("Location")
		if loc == "" {
			continue
		}
		st := resp.Header.Get("St")
		// on ssdp:all every service responds; only devices are of interest
		if strings.Contains(st, ":service:") && !strings.Contains(st, "AVTransport") {
			continue
		}
		out = append(out, loc)
	}
}

func localIPv4s() []net.IP {
	var out []net.IP
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 || ifc.Flags&net.FlagMulticast == 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok {
				if v4 := ipn.IP.To4(); v4 != nil && !v4.IsLinkLocalUnicast() {
					out = append(out, v4)
				}
			}
		}
	}
	return out
}

func itoa(n int) string {
	var b [20]byte
	i := len(b)
	for {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
		if n == 0 {
			return string(b[i:])
		}
	}
}
