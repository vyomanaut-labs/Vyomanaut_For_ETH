package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func noEnv(string) string { return "" }

func TestUsageErrorsExitWithTwo(t *testing.T) {
	cases := [][]string{
		nil,
		{"bogus"},
		{"wallet"},
		{"wallet", "bogus"},
		{"wallet", "new"},
		{"wallet", "bind"},
		{"deposit", "--amount", "abc"},
		{"deposit", "--amount", "1"},
		{"claim"},
		{"claim", "--period", "1", "--account", "nope"},
		{"refund"},
		{"balance"},
		{"period"},
		{"period", "0"},
	}
	for _, args := range cases {
		var out, errOut bytes.Buffer
		if code := run(args, noEnv, &out, &errOut); code != exitUsage {
			t.Errorf("%v: exit %d, want %d (stderr %q)", args, code, exitUsage, errOut.String())
		}
		if errOut.Len() == 0 {
			t.Errorf("%v: nothing was explained", args)
		}
	}
}

func TestHelpExitsCleanly(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"help"}, noEnv, &out, &errOut); code != exitOK || !strings.Contains(out.String(), "wallet new") {
		t.Fatalf("help: exit %d, out %q", code, out.String())
	}
}

func TestWalletNewThenShowRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "me.key")
	var out, errOut bytes.Buffer
	if code := run([]string{"wallet", "new", "--out", path}, noEnv, &out, &errOut); code != exitOK {
		t.Fatalf("new: exit %d: %s", code, errOut.String())
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "0x") == false || strings.Contains(out.String(), "never share") == false {
		t.Fatalf("new output: %q", out.String())
	}
	var shown bytes.Buffer
	if code := run([]string{"wallet", "show", "--key-file", path}, noEnv, &shown, &errOut); code != exitOK {
		t.Fatalf("show: exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), strings.TrimSpace(shown.String())) {
		t.Fatalf("show printed %q but new printed %q", shown.String(), out.String())
	}
	// A second new must refuse and leave the key alone.
	var o2, e2 bytes.Buffer
	if code := run([]string{"wallet", "new", "--out", path}, noEnv, &o2, &e2); code != exitFailed || !strings.Contains(e2.String(), "not overwritten") {
		t.Fatalf("second new: exit %d: %s", code, e2.String())
	}
}

func TestAMissingChainEndpointIsExplained(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"balance", "--address", "0x0000000000000000000000000000000000000001"}, noEnv, &out, &errOut)
	if code != exitFailed || !strings.Contains(errOut.String(), "--rpc-url") {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
}
