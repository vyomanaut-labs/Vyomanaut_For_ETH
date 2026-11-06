// Command relay runs a Vyomanaut relay node (ARCH §13 Tier 3, internal/p2p
// relay.go).
//
// Providers that cannot accept incoming connections keep a reservation on a
// relay by connecting out to it, and advertise
// /ip4/<relay>/tcp/<port>/p2p-circuit/p2p/<their Peer ID>. The coordinator and
// clients then reach them through the relay with the normal p2p calls. Android
// phones need this: Android never delivers new incoming VPN (ZeroTier)
// connections to an app, and mobile data is behind carrier-grade NAT.
//
// The relay only copies bytes; TLS runs end to end between the two peers.
//
// Usage:
//
//	relay --listen :30300
//
// Run it where every provider and the coordinator can reach it, for the demo
// on the coordinator machine. Providers pass --relay-addrs=<that ip>:30300.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/p2p"
)

const statusInterval = time.Minute

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "relay: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("relay", flag.ContinueOnError)
	listen := fs.String("listen", ":30300", "TCP address to listen on")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", *listen, err)
	}
	server := p2p.NewRelayServer(log.Printf)
	log.Printf("[RELAY] listening on %s", ln.Addr())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		ticker := time.NewTicker(statusInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				_ = ln.Close()
				return
			case <-ticker.C:
				log.Printf("[RELAY] %d provider(s) reachable through this relay", server.Reservations())
			}
		}
	}()

	// Serve only returns when the listener fails or is closed by the signal above.
	if err := server.Serve(ln); !errors.Is(err, net.ErrClosed) {
		return err
	}
	log.Printf("[RELAY] stopped")
	return nil
}
