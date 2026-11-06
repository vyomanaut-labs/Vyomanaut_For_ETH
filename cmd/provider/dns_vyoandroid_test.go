//go:build vyoandroid

package main

import (
	"reflect"
	"testing"
)

func TestDNSServersPhoneFirstThenFallback(t *testing.T) {
	got := dnsServers(" 10.0.0.1, fe80::1 ,bad,")
	want := []string{"10.0.0.1:53", "[fe80::1]:53", "8.8.8.8:53", "1.1.1.1:53"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}
