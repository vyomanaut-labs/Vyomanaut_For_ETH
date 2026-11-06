package main

import (
	"testing"
	"time"
)

func TestRTOForDirectProvidersIsUnchanged(t *testing.T) {
	if got := rtoFor(true, 40, 5, 0, false, false); got != 60*time.Millisecond {
		t.Errorf("own samples: rto = %s, want 60ms (avg + 4*var)", got)
	}
	if got := rtoFor(false, 0, 0, 30, true, false); got != 30*time.Millisecond {
		t.Errorf("pool median: rto = %s, want 30ms", got)
	}
	if got := rtoFor(false, 0, 0, 0, false, false); got != bootstrapFallbackRTO {
		t.Errorf("no data: rto = %s, want %s", got, bootstrapFallbackRTO)
	}
}

// The live failure: a phone behind the relay with fewer than 5 samples was
// held to the PC pool's few-millisecond median.
func TestRTOForRelayedProviderIgnoresThePoolMedian(t *testing.T) {
	if got := rtoFor(false, 0, 0, 30, true, true); got != bootstrapFallbackRTO {
		t.Errorf("relayed, few samples: rto = %s, want %s", got, bootstrapFallbackRTO)
	}
}

func TestRTOForRelayedProviderHasAFloor(t *testing.T) {
	if got := rtoFor(true, 900, 50, 0, false, true); got != relayMinRTO {
		t.Errorf("relayed, own window 1.1s: rto = %s, want floor %s", got, relayMinRTO)
	}
	if got := rtoFor(true, 4000, 1000, 0, false, true); got != 8*time.Second {
		t.Errorf("relayed, own window 8s: rto = %s, want 8s", got)
	}
}
