package main

import "testing"

func TestSelfURL(t *testing.T) {
	for in, want := range map[string]string{
		":8080":          "http://127.0.0.1:8080",
		"0.0.0.0:9000":   "http://127.0.0.1:9000",
		"10.0.0.5:8080":  "http://10.0.0.5:8080",
		"[::]:8080":      "http://127.0.0.1:8080",
		"not an address": "http://127.0.0.1:8080",
	} {
		if got := selfURL(in); got != want {
			t.Errorf("selfURL(%q) = %q, want %q", in, got, want)
		}
	}
}
