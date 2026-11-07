package main

import (
	"net"
	"os"
	"path/filepath"
)

// selfURL is how the desk's data owner reaches this coordinator: the HTTP
// listen address, on loopback when it listens on every interface.
func selfURL(listenAddr string) string {
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return "http://127.0.0.1:8080"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

// deskOwnerDir is where the desk's data owner keeps its keys:
// VYOMANAUT_DESK_OWNER_DIR, else a folder in the system temp directory.
func deskOwnerDir() string {
	if d := os.Getenv("VYOMANAUT_DESK_OWNER_DIR"); d != "" {
		return d
	}
	return filepath.Join(os.TempDir(), "vyomanaut-desk-owner")
}
