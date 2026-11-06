package p2p

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// testLogf logs through t until the test ends, then drops lines: relay
// goroutines can outlive a test, and t.Logf after the end panics.
func testLogf(t *testing.T) func(string, ...any) {
	var mu sync.Mutex
	done := false
	t.Cleanup(func() { mu.Lock(); done = true; mu.Unlock() })
	return func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		if !done {
			t.Logf(format, args...)
		}
	}
}

// startTestRelay runs a RelayServer on an OS-assigned loopback port.
func startTestRelay(t *testing.T) (*RelayServer, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	relay := NewRelayServer(testLogf(t))
	go func() { _ = relay.Serve(ln) }()
	return relay, ln.Addr().String()
}

// newUnreachableHost builds a host with NO listener: like an Android provider,
// nothing can connect to it directly.
func newUnreachableHost(t *testing.T) Host {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	h, err := NewHost(HostConfig{PrivateKey: priv})
	if err != nil {
		t.Fatalf("NewHost: %v", err)
	}
	t.Cleanup(func() { _ = h.Close() })
	return h
}

func waitForReservations(t *testing.T, relay *RelayServer, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for relay.Reservations() != want {
		if time.Now().After(deadline) {
			t.Fatalf("relay has %d reservations, want %d", relay.Reservations(), want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestRelayEndToEnd is the whole Tier 3 path: a provider with no listener
// reserves a slot, and a dialer reaches its stream handler through the relay
// with the ordinary Connect + NewStream calls, TLS verified end to end.
func TestRelayEndToEnd(t *testing.T) {
	relay, relayAddr := startTestRelay(t)
	provider := newUnreachableHost(t)
	dialer := newUnreachableHost(t)

	const echo ProtocolID = "/vyomanaut/test-echo/1.0.0"
	provider.SetStreamHandler(echo, func(s Stream) {
		defer func() { _ = s.Close() }()
		if s.RemotePeer() != dialer.PeerID() {
			t.Errorf("provider sees remote %s, want %s", s.RemotePeer(), dialer.PeerID())
		}
		buf := make([]byte, 5)
		if _, err := io.ReadFull(s, buf); err != nil {
			return
		}
		_, _ = s.Write(buf)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = ServeViaRelay(ctx, provider, relayAddr, testLogf(t)) }()
	waitForReservations(t, relay, 1)

	addr, err := RelayMultiaddr(relayAddr, provider.PeerID())
	if err != nil {
		t.Fatalf("RelayMultiaddr: %v", err)
	}
	opCtx, opCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer opCancel()
	if err := dialer.Connect(opCtx, provider.PeerID(), []Multiaddr{addr}); err != nil {
		t.Fatalf("Connect via relay: %v", err)
	}
	// Several streams, as audits and uploads each open their own.
	for i := 0; i < 3; i++ {
		s, err := dialer.NewStream(opCtx, provider.PeerID(), echo)
		if err != nil {
			t.Fatalf("NewStream %d via relay: %v", i, err)
		}
		if _, err := s.Write([]byte("hello")); err != nil {
			t.Fatalf("Write: %v", err)
		}
		got := make([]byte, 5)
		if _, err := io.ReadFull(s, got); err != nil {
			t.Fatalf("Read: %v", err)
		}
		if string(got) != "hello" {
			t.Errorf("echo = %q, want hello", got)
		}
		_ = s.Close()
	}
}

// TestRelayRejectsUnknownTarget: a forward to a peer with no reservation fails
// fast instead of hanging the dialer.
func TestRelayRejectsUnknownTarget(t *testing.T) {
	_, relayAddr := startTestRelay(t)
	dialer := newUnreachableHost(t)
	absent := newUnreachableHost(t)
	addr, err := RelayMultiaddr(relayAddr, absent.PeerID())
	if err != nil {
		t.Fatalf("RelayMultiaddr: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	if err := dialer.Connect(ctx, absent.PeerID(), []Multiaddr{addr}); err == nil {
		t.Fatal("Connect succeeded to a peer with no reservation")
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("rejection took %s; want it to be immediate", time.Since(start))
	}
}

// TestRelayIdentityIsVerifiedEndToEnd: a peer that reserves someone else's
// Peer ID gets the connection, but the dialer's TLS check refuses it.
func TestRelayIdentityIsVerifiedEndToEnd(t *testing.T) {
	relay, relayAddr := startTestRelay(t)
	impostor := newUnreachableHost(t)
	victim := newUnreachableHost(t)
	dialer := newUnreachableHost(t)

	// The impostor reserves the victim's ID by hand, then serves with its own key.
	conn, err := net.Dial("tcp", relayAddr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	req := append(append([]byte{}, relayRequestPrefix[:]...), relayOpReserve, byte(len(victim.PeerID())))
	req = append(req, []byte(victim.PeerID())...)
	if _, err := conn.Write(req); err != nil {
		t.Fatalf("Write: %v", err)
	}
	ack := make([]byte, 1)
	if _, err := io.ReadFull(conn, ack); err != nil || ack[0] != negotiationAckOK {
		t.Fatalf("reservation ack = %v, %v", ack, err)
	}
	waitForReservations(t, relay, 1)
	go func() {
		msg := make([]byte, 1+relayTokenLen)
		if _, err := io.ReadFull(conn, msg); err != nil || msg[0] != relayMsgDial {
			return
		}
		var token [relayTokenLen]byte
		copy(token[:], msg[1:])
		impl, ok := impostor.(*host)
		if !ok {
			return
		}
		impl.connectBack(context.Background(), relayAddr, token)
	}()

	addr, err := RelayMultiaddr(relayAddr, victim.PeerID())
	if err != nil {
		t.Fatalf("RelayMultiaddr: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := dialer.Connect(ctx, victim.PeerID(), []Multiaddr{addr}); err == nil {
		t.Fatal("Connect accepted an impostor holding the victim's reservation")
	}
}

// TestRelayReservationSurvivesRelayRestart: the provider reconnects by itself.
func TestRelayReservationSurvivesRelayRestart(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	relayAddr := ln.Addr().String()
	first := NewRelayServer(testLogf(t))
	go func() { _ = first.Serve(ln) }()

	provider := newUnreachableHost(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = ServeViaRelay(ctx, provider, relayAddr, testLogf(t)) }()
	waitForReservations(t, first, 1)

	// Restart the relay on the same port: closing the listener does not close
	// the control connection, so drop it by closing the reservation.
	_ = ln.Close()
	first.mu.Lock()
	for id, ctl := range first.reservations {
		_ = ctl.conn.Close()
		delete(first.reservations, id)
	}
	first.mu.Unlock()

	ln2, err := net.Listen("tcp", relayAddr)
	if err != nil {
		t.Skipf("could not re-listen on %s: %v", relayAddr, err)
	}
	t.Cleanup(func() { _ = ln2.Close() })
	second := NewRelayServer(testLogf(t))
	go func() { _ = second.Serve(ln2) }()

	deadline := time.Now().Add(relayRetryMin + 5*time.Second)
	for second.Reservations() != 1 {
		if time.Now().After(deadline) {
			t.Fatal("provider did not reserve again after the relay restarted")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
