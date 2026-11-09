// Package upnp is a minimal UPnP AV control point: renderer discovery (SSDP)
// and control (SOAP AVTransport/RenderingControl/ConnectionManager).
package upnp

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	svcAVTransport       = "urn:schemas-upnp-org:service:AVTransport:1"
	svcRenderingControl  = "urn:schemas-upnp-org:service:RenderingControl:1"
	svcConnectionManager = "urn:schemas-upnp-org:service:ConnectionManager:1"
)

// Device is a discovered MediaRenderer.
type Device struct {
	UDN          string `json:"udn"`
	Name         string `json:"name"`
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	Location     string `json:"location"`
	// LocalIP is this machine's address as seen by the renderer; it goes into track URLs.
	LocalIP string `json:"localIp"`
	// IP is the renderer's own address (from LOCATION), for display in the UI.
	IP string `json:"ip"`

	AVTransportURL      string   `json:"-"`
	RenderingControlURL string   `json:"-"`
	ConnectionMgrURL    string   `json:"-"`
	SinkProtocols       []string `json:"sinkProtocols,omitempty"`
}

type xmlRoot struct {
	URLBase string    `xml:"URLBase"`
	Device  xmlDevice `xml:"device"`
}

type xmlDevice struct {
	DeviceType   string       `xml:"deviceType"`
	FriendlyName string       `xml:"friendlyName"`
	Manufacturer string       `xml:"manufacturer"`
	ModelName    string       `xml:"modelName"`
	UDN          string       `xml:"UDN"`
	Services     []xmlService `xml:"serviceList>service"`
	Devices      []xmlDevice  `xml:"deviceList>device"`
}

type xmlService struct {
	ServiceType string `xml:"serviceType"`
	ControlURL  string `xml:"controlURL"`
}

var httpClient = &http.Client{Timeout: 5 * time.Second}

// fetchDevice reads the device description from the LOCATION in the SSDP response.
func fetchDevice(ctx context.Context, location string) (*Device, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device description %s: HTTP %d", location, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var root xmlRoot
	if err := xml.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("device description %s: %w", location, err)
	}
	renderer := findRenderer(&root.Device)
	if renderer == nil {
		return nil, fmt.Errorf("%s: not a MediaRenderer", location)
	}

	base := location
	if root.URLBase != "" {
		base = root.URLBase
	}
	d := &Device{
		UDN:          renderer.UDN,
		Name:         strings.TrimSpace(renderer.FriendlyName),
		Manufacturer: renderer.Manufacturer,
		Model:        renderer.ModelName,
		Location:     location,
		IP:           hostOf(location),
	}
	for _, s := range renderer.Services {
		abs := resolveURL(base, s.ControlURL)
		switch {
		case strings.HasPrefix(s.ServiceType, "urn:schemas-upnp-org:service:AVTransport:"):
			d.AVTransportURL = abs
		case strings.HasPrefix(s.ServiceType, "urn:schemas-upnp-org:service:RenderingControl:"):
			d.RenderingControlURL = abs
		case strings.HasPrefix(s.ServiceType, "urn:schemas-upnp-org:service:ConnectionManager:"):
			d.ConnectionMgrURL = abs
		}
	}
	if d.AVTransportURL == "" {
		return nil, fmt.Errorf("%s: no AVTransport service", location)
	}
	d.LocalIP = localIPFor(location)
	return d, nil
}

func findRenderer(d *xmlDevice) *xmlDevice {
	if strings.Contains(d.DeviceType, ":MediaRenderer:") {
		return d
	}
	for i := range d.Devices {
		if r := findRenderer(&d.Devices[i]); r != nil {
			return r
		}
	}
	return nil
}

func resolveURL(base, ref string) string {
	b, err := url.Parse(base)
	if err != nil {
		return ref
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return b.ResolveReference(r).String()
}

// localIPFor returns the local address that routes to the device.
// A UDP "connection" sends no packets, it only picks a route; this is more reliable
// than guessing from the interface list (VPN, Hyper-V, WSL).
func localIPFor(location string) string {
	u, err := url.Parse(location)
	if err != nil {
		return ""
	}
	host := u.Hostname()
	conn, err := net.Dial("udp4", net.JoinHostPort(host, "1900"))
	if err != nil {
		return ""
	}
	defer func() { _ = conn.Close() }()
	if a, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		return a.IP.String()
	}
	return ""
}

func hostOf(location string) string {
	u, err := url.Parse(location)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
