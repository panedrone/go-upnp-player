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
)
