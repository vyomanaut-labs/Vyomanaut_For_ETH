package main

import (
	"bytes"
	"strings"
	"testing"
)

func noEnv(string) string { return "" }

func TestUsageErrorsExitWithTwo(t *testing.T) {
	cases := [][]string{
		nil,
		{"bogus"},
		{"epoch"},
		{"epoch", "twelve"},
		{"receipt"},
		{"--nonsense", "list"},
	}
	for _, args := range cases {
		var out, errOut bytes.Buffer
		if code := run(args, noEnv, &out, &errOut); code != exitUsage {
			t.Errorf("%v: exit %d, want %d", args, code, exitUsage)
		}
		if errOut.Len() == 0 {
			t.Errorf("%v: nothing was explained", args)
		}
	}
}

func TestAMissingChainEndpointIsExplained(t *testing.T) {
	// A database must be reachable for the chain check to be reached, so test
	// the chain options directly.
	if _, _, err := openChain(options{}); err == nil || !strings.Contains(err.Error(), "--rpc-url") {
		t.Fatalf("err = %v", err)
	}
	if _, _, err := openChain(options{rpcURL: "http://x"}); err == nil || !strings.Contains(err.Error(), "--anchor") {
		t.Fatalf("err = %v", err)
	}
	if _, _, err := openChain(options{rpcURL: "http://x", anchorAddr: "nope"}); err == nil {
		t.Fatal("a bad address must be rejected")
	}
}

func TestEpochArgumentsAreValidated(t *testing.T) {
	for _, ok := range [][]string{{"epoch", "latest"}, {"epoch", "7"}, {"list"}, {"receipt", "abc"}} {
		if _, err := parseCommand(ok); err != nil {
			t.Errorf("%v: %v", ok, err)
		}
	}
}
