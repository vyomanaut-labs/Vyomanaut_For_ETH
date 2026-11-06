package main

import (
	"fmt"
	"net"
	"strings"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/p2p"
)

// parseRelayAddrs turns --relay-addrs into "host:port" relay nodes. Each entry
// may be host:port or a direct multiaddr such as /ip4/10.0.0.1/tcp/30300.
// An empty flag means no relays.
func parseRelayAddrs(flagValue string) ([]string, error) {
	var relays []string
	for _, entry := range strings.Split(flagValue, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		hostport := entry
		if strings.HasPrefix(entry, "/") {
			addr, err := p2p.ParseMultiaddr(entry)
			if err != nil {
				return nil, fmt.Errorf("%q: %w", entry, err)
			}
			hp, ok := addr.HostPort()
			if !ok || addr.Network() != "tcp" {
				return nil, fmt.Errorf("%q: a relay node must be a direct tcp address", entry)
			}
			hostport = hp
		}
		host, port, err := net.SplitHostPort(hostport)
		if err != nil || host == "" || port == "" {
			return nil, fmt.Errorf("%q: want host:port", entry)
		}
		relays = append(relays, hostport)
	}
	return relays, nil
}
