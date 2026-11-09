package upnp

import (
	"context"
	"strconv"
	"strings"

	"upnp-player/internal/services"
)

var instance0 = Arg{"InstanceID", "0"}

// SetURI — SetAVTransportURI.
func SetURI(ctx context.Context, d Device, uri, meta string) error {
	_, err := call(ctx, d.AVTransportURL, svcAVTransport, "SetAVTransportURI",
		[]Arg{instance0, {"CurrentURI", uri}, {"CurrentURIMetaData", meta}})
	return err
}

// SetNextURI is SetNextAVTransportURI (gapless). Not all renderers support it.
func SetNextURI(ctx context.Context, d Device, uri, meta string) error {
	_, err := call(ctx, d.AVTransportURL, svcAVTransport, "SetNextAVTransportURI",
		[]Arg{instance0, {"NextURI", uri}, {"NextURIMetaData", meta}})
	return err
}

func Play(ctx context.Context, d Device) error {
	_, err := call(ctx, d.AVTransportURL, svcAVTransport, "Play", []Arg{instance0, {"Speed", "1"}})
	return err
}

func Pause(ctx context.Context, d Device) error {
	_, err := call(ctx, d.AVTransportURL, svcAVTransport, "Pause", []Arg{instance0})
	return err
}

func Stop(ctx context.Context, d Device) error {
	_, err := call(ctx, d.AVTransportURL, svcAVTransport, "Stop", []Arg{instance0})
	return err
}

// Seek seeks by time from the start of the track.
func Seek(ctx context.Context, d Device, sec float64) error {
	_, err := call(ctx, d.AVTransportURL, svcAVTransport, "Seek",
		[]Arg{instance0, {"Unit", "REL_TIME"}, {"Target", services.FormatSeekTarget(sec)}})
	return err
}

// TransportInfo — GetTransportInfo.
type TransportInfo struct {
	State  string
	Status string
}

func GetTransportInfo(ctx context.Context, d Device) (TransportInfo, error) {
	r, err := call(ctx, d.AVTransportURL, svcAVTransport, "GetTransportInfo", []Arg{instance0})
	if err != nil {
		return TransportInfo{}, err
	}
	return TransportInfo{State: r["CurrentTransportState"], Status: r["CurrentTransportStatus"]}, nil
}

// PositionInfo — GetPositionInfo.
type PositionInfo struct {
	TrackURI string
	Duration float64
	Position float64
}

func GetPositionInfo(ctx context.Context, d Device) (PositionInfo, error) {
	r, err := call(ctx, d.AVTransportURL, svcAVTransport, "GetPositionInfo", []Arg{instance0})
	if err != nil {
		return PositionInfo{}, err
	}
	return PositionInfo{
		TrackURI: r["TrackURI"],
		Duration: services.ParseHMS(r["TrackDuration"]),
		Position: services.ParseHMS(r["RelTime"]),
	}, nil
}

func GetVolume(ctx context.Context, d Device) (int, error) {
	r, err := call(ctx, d.RenderingControlURL, svcRenderingControl, "GetVolume",
		[]Arg{instance0, {"Channel", "Master"}})
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(r["CurrentVolume"]))
}

func SetVolume(ctx context.Context, d Device, v int) error {
	_, err := call(ctx, d.RenderingControlURL, svcRenderingControl, "SetVolume",
		[]Arg{instance0, {"Channel", "Master"}, {"DesiredVolume", strconv.Itoa(v)}})
	return err
}

// getSinkProtocols returns the formats the renderer declares (ConnectionManager.GetProtocolInfo).
func getSinkProtocols(ctx context.Context, d *Device) []string {
	r, err := call(ctx, d.ConnectionMgrURL, svcConnectionManager, "GetProtocolInfo", nil)
	if err != nil {
		return nil
	}
	var out []string
	for _, p := range strings.Split(r["Sink"], ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
