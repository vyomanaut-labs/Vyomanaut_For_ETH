package main

import (
	"reflect"
	"testing"
)

func TestParseRelayAddrs(t *testing.T) {
	got, err := parseRelayAddrs(" 10.35.114.52:30300 , /ip4/10.0.0.9/tcp/30301 ,")
	if err != nil {
		t.Fatalf("parseRelayAddrs: %v", err)
	}
	want := []string{"10.35.114.52:30300", "10.0.0.9:30301"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseRelayAddrs = %v, want %v", got, want)
	}
}

func TestParseRelayAddrsEmptyMeansNone(t *testing.T) {
	got, err := parseRelayAddrs("")
	if err != nil || len(got) != 0 {
		t.Errorf("parseRelayAddrs(\"\") = %v, %v; want no relays", got, err)
	}
}

func TestParseRelayAddrsRejectsBadEntries(t *testing.T) {
	for _, bad := range []string{"10.0.0.1", "/ip4/10.0.0.1/udp/30300/quic-v1", "/p2p-circuit/p2p/X", ":30300"} {
		if _, err := parseRelayAddrs(bad); err == nil {
			t.Errorf("parseRelayAddrs(%q) accepted a bad relay", bad)
		}
	}
}
