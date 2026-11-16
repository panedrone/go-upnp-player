package player

import (
	"testing"
	"time"

	"github.com/panedrone/go-upnp-player/internal/library"
	"github.com/panedrone/go-upnp-player/internal/services"
)

// The state says a DSD change is being applied until it is: a gain change returns applied, a
// rate change goes on in the background (rescan, restart) and clears the flag when done.
func TestSetDSDApplying(t *testing.T) {
	resetState()
	t.Cleanup(resetState)
	old := library.DSD()
	t.Cleanup(func() { library.SetDSD(old) })

	if err := SetDSD(services.DSDSettings{Rate: old.Rate, GainDB: 0}); err != nil {
		t.Fatal(err)
	}
	if Snapshot().DSDApplying {
		t.Fatal("a gain change is still applying after it returned")
	}

	rate := uint32(192000)
	if old.Rate == rate {
		rate = 176400
	}
	if err := SetDSD(services.DSDSettings{Rate: rate, GainDB: 0}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for Snapshot().DSDApplying {
		if time.Now().After(deadline) {
			t.Fatal("a rate change never finished applying")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
