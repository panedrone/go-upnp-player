package player

import (
	"time"

	"github.com/panedrone/go-upnp-player/internal/library"
	"github.com/panedrone/go-upnp-player/internal/upnp"
)

// Replaceable dependencies. DiscoverRenderers is exported so that endpoint tests
// can put a fake renderer on the "network" without SSDP.
var (
	DiscoverRenderers = upnp.Discover

	lookupTrack = library.Get
	sleep       = time.Sleep

	// What the renderer reports and the seeks sent to it: tests replay through these
	// what a real one answers, e.g. its old position for a while after a seek.
	getTransportInfo = upnp.GetTransportInfo
	getPositionInfo  = upnp.GetPositionInfo
	seekRenderer     = upnp.Seek
)
