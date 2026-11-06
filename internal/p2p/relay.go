// relay.go completes ARCH §13 Tier 3: the Vyomanaut relay node and the
// provider side of a relay reservation, so a provider that cannot accept
// incoming connections is still reachable.
//
// Why this is needed in practice: Android phones on a ZeroTier VPN never get
// new incoming connections delivered to an app (only replies to connections
// the app opened), and phones on mobile data sit behind carrier-grade NAT.
// Both can open connections out. So the provider keeps one outgoing control
// connection to a relay, and the relay asks it to open a second outgoing
// connection whenever someone wants to reach it.
//
// Wire protocol ("VRL1", extending the two requests nat.go already sends).
// Every connection to a relay starts with the 4-byte prefix "VRL1" and a
// one-byte operation:
//
//	'R' len(1) peerID(len)  reserve a slot for peerID. The relay answers one
//	                        ack byte. On OK the connection stays open as the
//	                        control channel: the relay sends 'P' (ping) and
//	                        'C' + 16-byte token (someone is dialling you); the
//	                        provider sends 'P' (keepalive).
//	'F' len(1) peerID(len)  forward me to peerID (nat.go's DialViaRelay). The
//	                        relay answers one ack byte once the provider has
//	                        connected back (or rejects), then splices bytes.
//	'A' token(16)           the provider connecting back for that token. The
//	                        relay answers one ack byte, then splices bytes.
//
// The relay only copies bytes: the dialer and the provider run the normal
// TLS 1.3 handshake through it (host.go), so peer identity is verified end to
// end and the relay sees only ciphertext. A peer reserving someone else's
// Peer ID therefore cannot read or forge traffic; at worst it receives
// connections whose TLS handshake then fails (a denial of service that signed
// reservations would close; not needed for the demo network).
package p2p

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

const (
	relayOpReserve = 'R'
	relayOpForward = 'F'
	relayOpAccept  = 'A'
	relayMsgDial   = 'C'
	relayMsgPing   = 'P'

	relayTokenLen = 16

	// relayHandshakeTimeout bounds reading a request's header.
	relayHandshakeTimeout = 10 * time.Second
	// relayConnectBackTimeout bounds how long a forward waits for the provider
	// to open its data connection.
	relayConnectBackTimeout = 10 * time.Second
	// relayPingInterval and relayIdleTimeout keep control channels alive
	// through NATs and detect dead ones on both ends.
	relayPingInterval = 30 * time.Second
	relayIdleTimeout  = 3 * relayPingInterval

	relayRetryMin    = 2 * time.Second
	relayRetryMax    = 30 * time.Second
	relayRetryFactor = 2
)

// ── Relay node ───────────────────────────────────────────────────────────────

// RelayServer is a Vyomanaut relay node. The zero value is not usable; use
// NewRelayServer.
type RelayServer struct {
	logf func(format string, args ...any)

	mu           sync.Mutex
	reservations map[PeerID]*relayControl
	pending      map[[relayTokenLen]byte]chan net.Conn
}

type relayControl struct {
	conn    net.Conn
	writeMu sync.Mutex
}

func (c *relayControl) send(b []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(relayHandshakeTimeout))
	_, err := c.conn.Write(b)
	return err
}

// NewRelayServer returns a relay node. logf may be nil.
func NewRelayServer(logf func(format string, args ...any)) *RelayServer {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &RelayServer{
		logf:         logf,
		reservations: make(map[PeerID]*relayControl),
		pending:      make(map[[relayTokenLen]byte]chan net.Conn),
	}
}

// Reservations reports how many providers currently hold a slot.
func (s *RelayServer) Reservations() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.reservations)
}

// Serve accepts connections on ln until it is closed.
func (s *RelayServer) Serve(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go s.handle(conn)
	}
}

func (s *RelayServer) handle(conn net.Conn) {
	_ = conn.SetDeadline(time.Now().Add(relayHandshakeTimeout))
	header := make([]byte, len(relayRequestPrefix)+1)
	if _, err := readFull(conn, header); err != nil || [4]byte(header[:4]) != relayRequestPrefix {
		_ = conn.Close()
		return
	}
	switch header[4] {
	case relayOpReserve:
		id, err := readRelayPeerID(conn)
		if err != nil {
			_ = conn.Close()
			return
		}
		s.reserve(id, conn)
	case relayOpForward:
		id, err := readRelayPeerID(conn)
		if err != nil {
			_ = conn.Close()
			return
		}
		s.forward(id, conn)
	case relayOpAccept:
		var token [relayTokenLen]byte
		if _, err := readFull(conn, token[:]); err != nil {
			_ = conn.Close()
			return
		}
		s.accept(token, conn)
	default:
		_ = conn.Close()
	}
}

func (s *RelayServer) reserve(id PeerID, conn net.Conn) {
	ctl := &relayControl{conn: conn}
	s.mu.Lock()
	old, replacing := s.reservations[id]
	if !replacing && len(s.reservations) >= relayMaxConcurrentReservations {
		s.mu.Unlock()
		_, _ = conn.Write([]byte{negotiationAckReject})
		_ = conn.Close()
		s.logf("[RELAY] reservation for %s rejected: %d-slot capacity reached", id, relayMaxConcurrentReservations)
		return
	}
	s.reservations[id] = ctl
	s.mu.Unlock()
	if replacing {
		// The provider reconnected (for example after a network change); the
		// newest control channel wins.
		_ = old.conn.Close()
	}
	if err := ctl.send([]byte{negotiationAckOK}); err != nil {
		s.dropReservation(id, ctl)
		return
	}
	s.logf("[RELAY] %s reserved a slot from %s", id, conn.RemoteAddr())

	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(relayPingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if ctl.send([]byte{relayMsgPing}) != nil {
					_ = conn.Close()
					return
				}
			}
		}
	}()
	buf := make([]byte, 1)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(relayIdleTimeout))
		if _, err := conn.Read(buf); err != nil {
			break
		}
	}
	close(done)
	s.dropReservation(id, ctl)
	s.logf("[RELAY] %s released its slot", id)
}

func (s *RelayServer) dropReservation(id PeerID, ctl *relayControl) {
	s.mu.Lock()
	if s.reservations[id] == ctl {
		delete(s.reservations, id)
	}
	s.mu.Unlock()
	_ = ctl.conn.Close()
}

func (s *RelayServer) forward(target PeerID, conn net.Conn) {
	s.mu.Lock()
	ctl := s.reservations[target]
	s.mu.Unlock()
	if ctl == nil {
		_, _ = conn.Write([]byte{negotiationAckReject})
		_ = conn.Close()
		s.logf("[RELAY] forward to %s from %s: no reservation", target, conn.RemoteAddr())
		return
	}

	var token [relayTokenLen]byte
	if _, err := rand.Read(token[:]); err != nil {
		_ = conn.Close()
		return
	}
	back := make(chan net.Conn, 1)
	s.mu.Lock()
	s.pending[token] = back
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.pending, token)
		s.mu.Unlock()
	}()

	if err := ctl.send(append([]byte{relayMsgDial}, token[:]...)); err != nil {
		_, _ = conn.Write([]byte{negotiationAckReject})
		_ = conn.Close()
		return
	}
	var data net.Conn
	select {
	case data = <-back:
	case <-time.After(relayConnectBackTimeout):
		_, _ = conn.Write([]byte{negotiationAckReject})
		_ = conn.Close()
		s.logf("[RELAY] forward to %s: provider did not connect back in %s", target, relayConnectBackTimeout)
		return
	}
	if _, err := conn.Write([]byte{negotiationAckOK}); err != nil {
		_ = conn.Close()
		_ = data.Close()
		return
	}
	if _, err := data.Write([]byte{negotiationAckOK}); err != nil {
		_ = conn.Close()
		_ = data.Close()
		return
	}
	_ = conn.SetDeadline(time.Time{})
	_ = data.SetDeadline(time.Time{})
	splice(conn, data)
}

func (s *RelayServer) accept(token [relayTokenLen]byte, conn net.Conn) {
	s.mu.Lock()
	back, ok := s.pending[token]
	delete(s.pending, token)
	s.mu.Unlock()
	if !ok {
		_, _ = conn.Write([]byte{negotiationAckReject})
		_ = conn.Close()
		return
	}
	back <- conn // buffered: never blocks
}

// splice copies bytes both ways until either side closes, then closes both.
func splice(a, b net.Conn) {
	var once sync.Once
	closeBoth := func() {
		once.Do(func() {
			_ = a.Close()
			_ = b.Close()
		})
	}
	go func() {
		_, _ = io.Copy(a, b)
		closeBoth()
	}()
	_, _ = io.Copy(b, a)
	closeBoth()
}

func readRelayPeerID(conn net.Conn) (PeerID, error) {
	n := make([]byte, 1)
	if _, err := readFull(conn, n); err != nil {
		return "", err
	}
	if n[0] == 0 {
		return "", errors.New("p2p: relay: empty peer ID")
	}
	id := make([]byte, n[0])
	if _, err := readFull(conn, id); err != nil {
		return "", err
	}
	return PeerID(id), nil
}

// ── Provider side ────────────────────────────────────────────────────────────

// ServeViaRelay keeps a reservation for self on the relay at relayAddr
// ("host:port") and serves every connection forwarded through it on h, exactly
// as if it had arrived on h's own listener. It reconnects with backoff when the
// control channel drops and returns only when ctx is cancelled. logf may be nil.
//
// h must be a Host created by NewHost.
func ServeViaRelay(ctx context.Context, h Host, relayAddr string, logf func(format string, args ...any)) error {
	impl, ok := h.(*host)
	if !ok {
		return fmt.Errorf("p2p.ServeViaRelay: host must be created by NewHost")
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	backoff := relayRetryMin
	for ctx.Err() == nil {
		started := time.Now()
		err := impl.holdRelayReservation(ctx, relayAddr, logf)
		if ctx.Err() != nil {
			break
		}
		if time.Since(started) > relayIdleTimeout {
			backoff = relayRetryMin // it had been working; retry promptly
		}
		logf("[RELAY] reservation on %s lost (%v); retrying in %s", relayAddr, err, backoff)
		select {
		case <-ctx.Done():
		case <-time.After(backoff):
		}
		backoff = min(backoff*relayRetryFactor, relayRetryMax)
	}
	return ctx.Err()
}

func (h *host) holdRelayReservation(ctx context.Context, relayAddr string, logf func(string, ...any)) error {
	var d net.Dialer
	dialCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	conn, err := d.DialContext(dialCtx, "tcp", relayAddr)
	cancel()
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer func() { _ = conn.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	req := append(append([]byte{}, relayRequestPrefix[:]...), relayOpReserve, byte(len(h.peerID)))
	req = append(req, []byte(h.peerID)...)
	_ = conn.SetDeadline(time.Now().Add(relayHandshakeTimeout))
	if _, err := conn.Write(req); err != nil {
		return fmt.Errorf("write reservation: %w", err)
	}
	ack := make([]byte, 1)
	if _, err := readFull(conn, ack); err != nil {
		return fmt.Errorf("read reservation ack: %w", err)
	}
	if ack[0] != negotiationAckOK {
		return fmt.Errorf("relay rejected the reservation (it may be full)")
	}
	_ = conn.SetDeadline(time.Time{})
	logf("[RELAY] reserved a slot on %s; reachable through it", relayAddr)

	var writeMu sync.Mutex
	keepalive := time.NewTicker(relayPingInterval)
	defer keepalive.Stop()
	go func() {
		for range keepalive.C {
			writeMu.Lock()
			_ = conn.SetWriteDeadline(time.Now().Add(relayHandshakeTimeout))
			_, err := conn.Write([]byte{relayMsgPing})
			writeMu.Unlock()
			if err != nil {
				_ = conn.Close()
				return
			}
		}
	}()

	msg := make([]byte, 1)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(relayIdleTimeout))
		if _, err := readFull(conn, msg); err != nil {
			return fmt.Errorf("control channel: %w", err)
		}
		switch msg[0] {
		case relayMsgPing:
		case relayMsgDial:
			var token [relayTokenLen]byte
			if _, err := readFull(conn, token[:]); err != nil {
				return fmt.Errorf("control channel: %w", err)
			}
			go h.connectBack(ctx, relayAddr, token)
		default:
			return fmt.Errorf("control channel: unexpected message 0x%02x", msg[0])
		}
	}
}

// connectBack opens the data connection the relay asked for and serves it.
func (h *host) connectBack(ctx context.Context, relayAddr string, token [relayTokenLen]byte) {
	var d net.Dialer
	dialCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	conn, err := d.DialContext(dialCtx, "tcp", relayAddr)
	cancel()
	if err != nil {
		return
	}
	_ = conn.SetDeadline(time.Now().Add(relayHandshakeTimeout))
	req := append(append([]byte{}, relayRequestPrefix[:]...), relayOpAccept)
	req = append(req, token[:]...)
	if _, err := conn.Write(req); err != nil {
		_ = conn.Close()
		return
	}
	ack := make([]byte, 1)
	if _, err := readFull(conn, ack); err != nil || ack[0] != negotiationAckOK {
		_ = conn.Close()
		return
	}
	_ = conn.SetDeadline(time.Time{})
	// From here the bytes are the dialer's: run the server side of the normal
	// TLS + protocol negotiation, exactly like a connection from h's listener.
	h.serveConn(tls.Server(conn, h.serverTLSConfig()))
}
