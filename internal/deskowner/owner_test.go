package deskowner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/client/upload"
	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/config"
)

func TestSetupRejectsBadPhones(t *testing.T) {
	o := New("http://127.0.0.1:1", t.TempDir(), config.SelectProfile("demo"), nil)
	for _, p := range []string{"", "12345", "+0123456789", "phone"} {
		if err := o.Setup(context.Background(), p); err == nil || !strings.Contains(err.Error(), "+91") {
			t.Errorf("Setup(%q) = %v, want a phone format error", p, err)
		}
	}
}

func TestSetupWithoutDeskCodes(t *testing.T) {
	o := New("http://127.0.0.1:1", t.TempDir(), config.SelectProfile("demo"), nil)
	if err := o.Setup(context.Background(), "98765 43210"); err == nil || !strings.Contains(err.Error(), "demo mode") {
		t.Fatalf("got %v", err)
	}
}

func TestNotSetUp(t *testing.T) {
	o := New("http://127.0.0.1:1", t.TempDir(), config.SelectProfile("demo"), nil)
	if s := o.Status(context.Background()); s.Ready || s.Phone != "" {
		t.Fatalf("status = %+v", s)
	}
	if _, err := o.Files(context.Background()); !errors.Is(err, ErrNotSetUp) {
		t.Fatalf("Files: %v", err)
	}
	if _, err := o.StartUpload("a.txt", []byte("x")); !errors.Is(err, ErrNotSetUp) {
		t.Fatalf("StartUpload: %v", err)
	}
}

func TestStartUploadChecksSize(t *testing.T) {
	o := New("http://127.0.0.1:1", t.TempDir(), config.SelectProfile("demo"), nil)
	if _, err := o.StartUpload("a", nil); err == nil {
		t.Fatal("empty file accepted")
	}
	if _, err := o.StartUpload("a", make([]byte, MaxFileBytes+1)); err == nil || !strings.Contains(err.Error(), "MB") {
		t.Fatalf("big file: %v", err)
	}
}

func TestStateSurvivesRestartAndIsPrivate(t *testing.T) {
	dir := t.TempDir()
	o := New("http://127.0.0.1:1", dir, config.SelectProfile("demo"), nil)
	st := &state{Phone: "+919876543210", OwnerID: uuid.New(), PrivateKey: "aa", MasterSecret: "bb", Names: map[string]string{"f": "x.txt"}}
	if err := o.save(st); err != nil {
		t.Fatal(err)
	}
	again := New("http://127.0.0.1:1", dir, config.SelectProfile("demo"), nil)
	if again.st == nil || again.st.OwnerID != st.OwnerID || again.st.Names["f"] != "x.txt" {
		t.Fatalf("reloaded %+v", again.st)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(dir, stateFile))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("state file mode %v (%v)", info.Mode().Perm(), err)
		}
	}
	// Damaged keys are reported, not used.
	if _, _, _, err := again.keys(); err == nil {
		t.Fatal("short keys accepted")
	}
}

func TestCleanName(t *testing.T) {
	for in, want := range map[string]string{
		"photo.jpg":              "photo.jpg",
		"../../etc/passwd":       "passwd",
		`C:\Users\a\b.txt`:       "b.txt",
		"":                       "file",
		"bad\x00name\n.txt":      "badname.txt",
		strings.Repeat("a", 300): strings.Repeat("a", 120),
	} {
		if got := cleanName(in); got != want {
			t.Errorf("cleanName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUploadErrorIsPlain(t *testing.T) {
	if got := uploadError(fmt.Errorf("x: %w", upload.ErrInsufficientEscrow)); !strings.Contains(got, "credit") {
		t.Errorf("escrow: %s", got)
	}
	if got := uploadError(fmt.Errorf("x: %w", upload.ErrNetworkNotReady)); !strings.Contains(got, "providers") {
		t.Errorf("not ready: %s", got)
	}
}
