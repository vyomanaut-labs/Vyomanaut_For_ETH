//go:build vyoandroid

package main

import (
	"context"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

// Android has no /etc/resolv.conf, so Go's own resolver (this binary is built
// with CGO off) falls back to [::1]:53, where nothing listens, and every
// hostname lookup fails. That breaks coordinators and relays given by name,
// such as a Cloudflare tunnel or bore.pub. The app passes the phone's current
// DNS servers in VYOMANAUT_DNS_SERVERS; public resolvers are the fallback.
const dnsServersEnv = "VYOMANAUT_DNS_SERVERS"

var fallbackDNSServers = []string{"8.8.8.8", "1.1.1.1"}

const dnsDialTimeout = 3 * time.Second

// androidCACerts is where Android keeps its trusted certificate authorities.
// Go looks only in the usual Linux places, so HTTPS (a Cloudflare tunnel)
// failed with "certificate signed by unknown authority". crypto/x509 reads
// SSL_CERT_DIR when it first verifies a certificate, after init has run.
const androidCACerts = "/system/etc/security/cacerts"

func init() {
	if os.Getenv("SSL_CERT_DIR") == "" {
		if _, err := os.Stat(androidCACerts); err == nil {
			_ = os.Setenv("SSL_CERT_DIR", androidCACerts)
		}
	}
	if _, err := os.Stat("/etc/resolv.conf"); err == nil {
		return
	}
	servers := dnsServers(os.Getenv(dnsServersEnv))
	// A UDP dial never fails, so "try the next server on error" cannot work.
	// Go's resolver retries each query several times (2 attempts x 2 default
	// servers); each retry dials again, and gets the next server in turn.
	var next atomic.Uint32
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: dnsDialTimeout}
			s := servers[int(next.Add(1)-1)%len(servers)]
			return d.DialContext(ctx, network, s)
		},
	}
}

// dnsServers turns "10.0.0.1, 2001:db8::1" into host:53 addresses, phone's
// servers first, then the public fallbacks.
func dnsServers(env string) []string {
	var out []string
	for _, s := range append(strings.Split(env, ","), fallbackDNSServers...) {
		s = strings.TrimSpace(s)
		if s == "" || net.ParseIP(strings.Trim(s, "[]")) == nil {
			continue
		}
		out = append(out, net.JoinHostPort(strings.Trim(s, "[]"), "53"))
	}
	return out
}
