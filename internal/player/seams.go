package player

import (
	"github.com/panedrone/go-upnp-player/internal/services"
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

// keepOutputOpen: a renderer that survives Stop is still driven like one that does not
// (ADR-002): Stop pauses it on silence and a start goes through its gapless path, so it
// never closes its sound output. On the BeagleBone that opening is where the DAC on the
// coaxial output clicked, about 2 s after Play from Stop (ADR-021). The racing this path
// was once suspected of (ADR-007, ADR-013) is watched for by the racing check; false
// goes back to Stop, as foobar2000 drives it.
var keepOutputOpen = true

// usesStop is services.StopSafe unless the output is kept open.
func usesStop(model string) bool {
	return !keepOutputOpen && services.StopSafe(model)
}
