package p2p

import (
	"testing"
	"time"
)

func TestRelayMultiaddrNamingItsRelay(t *testing.T) {
	const target = "12D3KooWTargetPeer"
	m, err := ParseMultiaddr("/ip4/10.35.114.52/tcp/30300/p2p-circuit/p2p/" + target)
	if err != nil {
		t.Fatalf("ParseMultiaddr: %v", err)
	}
	if !m.IsRelay() {
		t.Fatal("IsRelay = false, want true")
	}
	if _, ok := m.HostPort(); ok {
		t.Error("HostPort ok = true: a relay address must not be dialled as if the relay were the target")
	}
	relay, peer, ok := RelayRoute(m)
	if !ok || relay != "10.35.114.52:30300" || peer != target {
		t.Errorf("RelayRoute = (%q, %q, %v), want (10.35.114.52:30300, %s, true)", relay, peer, ok, target)
	}
}

func TestRelayMultiaddrWithRelayPeerID(t *testing.T) {
	m, err := ParseMultiaddr("/ip4/10.0.0.1/tcp/30300/p2p/12D3KooWRelay/p2p-circuit/p2p/12D3KooWTarget")
	if err != nil {
		t.Fatalf("ParseMultiaddr: %v", err)
	}
	if relay, peer, ok := RelayRoute(m); !ok || relay != "10.0.0.1:30300" || peer != "12D3KooWTarget" {
		t.Errorf("RelayRoute = (%q, %q, %v)", relay, peer, ok)
	}
}

func TestBareCircuitAddressNamesNoRelay(t *testing.T) {
	m, err := ParseMultiaddr("/p2p-circuit/p2p/12D3KooWTarget")
	if err != nil {
		t.Fatalf("ParseMultiaddr: %v", err)
	}
	if _, _, ok := RelayRoute(m); ok {
		t.Error("RelayRoute ok = true for an address that names no relay")
	}
}

func TestMalformedRelayAddressesAreRejected(t *testing.T) {
	for _, s := range []string{
		"/ip4/10.0.0.1/tcp/30300/p2p-circuit",
		"/ip4/10.0.0.1/tcp/30300/p2p-circuit/p2p",
		"/ip4/10.0.0.1/udp/30300/quic-v1/p2p-circuit/p2p/X",
		"/ip4/10.0.0.1/p2p-circuit/p2p/X",
	} {
		if _, err := ParseMultiaddr(s); err == nil {
			t.Errorf("ParseMultiaddr(%q) succeeded, want an error", s)
		}
	}
}

func TestRelayMultiaddrBuilder(t *testing.T) {
	m, err := RelayMultiaddr("10.35.114.52:30300", "12D3KooWSelf")
	if err != nil {
		t.Fatalf("RelayMultiaddr: %v", err)
	}
	if m.String() != "/ip4/10.35.114.52/tcp/30300/p2p-circuit/p2p/12D3KooWSelf" {
		t.Errorf("RelayMultiaddr = %s", m)
	}
	if m, err := RelayMultiaddr("relay.example:30300", "X"); err != nil || m.String() != "/dns4/relay.example/tcp/30300/p2p-circuit/p2p/X" {
		t.Errorf("RelayMultiaddr(dns) = %v, %v", m, err)
	}
	if _, err := RelayMultiaddr("no-port", "X"); err == nil {
		t.Error("RelayMultiaddr accepted an address without a port")
	}
}

func TestDirectAddressesAreUnchanged(t *testing.T) {
	m, err := ParseMultiaddr("/ip4/10.35.114.7/tcp/30303/p2p/12D3KooWX")
	if err != nil {
		t.Fatalf("ParseMultiaddr: %v", err)
	}
	if hp, ok := m.HostPort(); !ok || hp != "10.35.114.7:30303" || m.IsRelay() {
		t.Errorf("direct address parsed as (%q, %v, relay=%v)", hp, ok, m.IsRelay())
	}
}

func TestPrefersRelay(t *testing.T) {
	relay, _ := ParseMultiaddr("/ip4/10.0.0.1/tcp/30300/p2p-circuit/p2p/X")
	direct, _ := ParseMultiaddr("/ip4/10.0.0.7/tcp/30303")
	bare, _ := ParseMultiaddr("/p2p-circuit/p2p/X")
	if !PrefersRelay([]Multiaddr{relay, direct}) {
		t.Error("relay listed first: PrefersRelay = false")
	}
	if PrefersRelay([]Multiaddr{direct, relay}) || PrefersRelay(nil) || PrefersRelay([]Multiaddr{bare}) {
		t.Error("PrefersRelay = true for a direct-first, empty or relay-less list")
	}
}

func TestTransferTimeout(t *testing.T) {
	relay, _ := ParseMultiaddr("/ip4/10.0.0.1/tcp/30300/p2p-circuit/p2p/X")
	direct, _ := ParseMultiaddr("/ip4/10.0.0.7/tcp/30303")
	if got := TransferTimeout([]Multiaddr{direct}, 5*time.Second); got != 5*time.Second {
		t.Errorf("direct: %s, want 5s unchanged", got)
	}
	if got := TransferTimeout([]Multiaddr{relay, direct}, 5*time.Second); got != RelayTransferTimeout {
		t.Errorf("relayed: %s, want %s", got, RelayTransferTimeout)
	}
	if got := TransferTimeout([]Multiaddr{relay}, time.Minute); got != time.Minute {
		t.Errorf("relayed with a longer budget: %s, want it kept", got)
	}
}
