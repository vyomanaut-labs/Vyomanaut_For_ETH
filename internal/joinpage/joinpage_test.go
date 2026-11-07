package joinpage

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func newTestServer(t *testing.T, cfg Config) *httptest.Server {
	t.Helper()
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "api") })
	srv := httptest.NewServer(Handler(next, cfg))
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, url string, header ...string) (int, string, http.Header) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), resp.Header
}

func TestNetworkInfoUsesConfiguredRelay(t *testing.T) {
	srv := newTestServer(t, Config{PublicURL: "https://abc.trycloudflare.com", RelayAddrs: []string{"bore.pub:41234"}, ChainAPIURL: "https://chain.example"})
	_, body, _ := get(t, srv.URL+"/api/v1/network/info")
	var in Info
	if err := json.Unmarshal([]byte(body), &in); err != nil {
		t.Fatal(err)
	}
	if in.CoordinatorURL != "https://abc.trycloudflare.com" || in.RelayAddrs[0] != "bore.pub:41234" || in.ChainAPIURL != "https://chain.example" {
		t.Fatalf("info = %+v", in)
	}
	if !strings.HasPrefix(in.AppLink, "vyomanaut://join?") || !strings.Contains(in.AppLink, "relay=bore.pub%3A41234") {
		t.Fatalf("app link = %s", in.AppLink)
	}
}

func TestNetworkInfoFallsBackToRequestHost(t *testing.T) {
	srv := newTestServer(t, Config{})
	_, body, _ := get(t, srv.URL+"/api/v1/network/info", "X-Forwarded-Proto", "https")
	var in Info
	_ = json.Unmarshal([]byte(body), &in)
	host := strings.TrimPrefix(srv.URL, "http://")
	if in.CoordinatorURL != "https://"+host {
		t.Fatalf("coordinator = %s", in.CoordinatorURL)
	}
	if in.RelayAddrs[0] != "127.0.0.1:30300" {
		t.Fatalf("relay = %v", in.RelayAddrs)
	}
}

func TestOtherRoutesReachTheAPI(t *testing.T) {
	srv := newTestServer(t, Config{})
	if _, body, _ := get(t, srv.URL+"/api/v1/time"); body != "api" {
		t.Fatalf("body = %q", body)
	}
	if code, _, h := get(t, srv.URL+"/"); code != http.StatusFound || h.Get("Location") != "/join" {
		t.Fatalf("root: %d %s", code, h.Get("Location"))
	}
}

func TestPagesRender(t *testing.T) {
	srv := newTestServer(t, Config{NetworkName: "Demo <b>net</b>", PublicURL: "https://x.example"})
	for _, p := range []string{"/join", "/join/screen", "/join/desk"} {
		code, body, _ := get(t, srv.URL+p)
		if code != http.StatusOK || strings.Contains(body, "<b>net") {
			t.Fatalf("%s: %d unsafe or failed", p, code)
		}
	}
	_, body, _ := get(t, srv.URL+"/join")
	if !strings.Contains(body, "irm https://x.example/join/install.ps1 | iex") || !strings.Contains(body, "intent://join?") {
		t.Fatal("join page is missing the laptop command or the app link")
	}
	if code, body, _ := get(t, srv.URL+"/join/qr.png"); code != http.StatusOK || !strings.HasPrefix(body, "\x89PNG") {
		t.Fatal("qr is not a png")
	}
}

func TestInstallScriptsQuoteValues(t *testing.T) {
	srv := newTestServer(t, Config{PublicURL: "https://x.example", RelayAddrs: []string{"bore.pub:1"}, Mode: "demo"})
	_, sh, _ := get(t, srv.URL+"/join/install.sh")
	for _, want := range []string{"COORD='https://x.example'", "RELAY='bore.pub:1'", ".vyomanaut-join/x-example"} {
		if !strings.Contains(sh, want) {
			t.Fatalf("install.sh missing %q", want)
		}
	}
	_, ps, _ := get(t, srv.URL+"/join/install.ps1")
	if !strings.Contains(ps, "$Coord = 'https://x.example'") {
		t.Fatal("install.ps1 missing coordinator")
	}
	if shellQuote("a'b") != `'a'\''b'` || psQuote("a'b") != "'a''b'" {
		t.Fatal("quoting")
	}
}

func TestDownloadServesLocalFileOrRedirects(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, AppFile), []byte("apk"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := newTestServer(t, Config{DownloadsDir: dir, ReleaseBase: "https://rel.example"})
	code, body, h := get(t, srv.URL+"/join/download/"+AppFile)
	if code != http.StatusOK || body != "apk" || h.Get("Content-Type") != "application/vnd.android.package-archive" {
		t.Fatalf("apk: %d %q %s", code, body, h.Get("Content-Type"))
	}
	code, _, h = get(t, srv.URL+"/join/download/provider-linux-amd64")
	if code != http.StatusFound || h.Get("Location") != "https://rel.example/provider-linux-amd64" {
		t.Fatalf("redirect: %d %s", code, h.Get("Location"))
	}
	if code, _, _ = get(t, srv.URL+"/join/download/..%2Fsecret"); code != http.StatusNotFound {
		t.Fatalf("traversal: %d", code)
	}
}

type recordSender struct{ got []string }

func (r *recordSender) SendOTP(_ context.Context, phone, code string) error {
	r.got = append(r.got, phone+" "+code)
	return nil
}

func TestDeskNeedsKeyAndShowsCodes(t *testing.T) {
	inner := &recordSender{}
	desk := NewDesk(inner, nil)
	now := time.Date(2026, 11, 7, 1, 0, 0, 0, time.UTC)
	desk.now = func() time.Time { return now }
	_ = desk.SendOTP(context.Background(), "+919876500001", "123456")
	if len(inner.got) != 1 {
		t.Fatal("wrapped sender not called")
	}
	srv := newTestServer(t, Config{AdminAPIKey: testKey, Desk: desk})
	if code, _, _ := get(t, srv.URL+"/join/desk/requests"); code != http.StatusUnauthorized {
		t.Fatalf("no key: %d", code)
	}
	if code, _, _ := get(t, srv.URL+"/join/desk/requests", "X-Admin-API-Key", strings.Repeat("0", 64)); code != http.StatusUnauthorized {
		t.Fatalf("wrong key: %d", code)
	}
	_, body, _ := get(t, srv.URL+"/join/desk/requests", "X-Admin-API-Key", testKey)
	if !strings.Contains(body, `"code":"123456"`) || !strings.Contains(body, `"state":"waiting"`) {
		t.Fatalf("desk body = %s", body)
	}
	now = now.Add(11 * time.Minute)
	if got := desk.Recent(context.Background()); got[0].State != "expired" {
		t.Fatalf("state after 11 min = %s", got[0].State)
	}
	now = now.Add(5 * time.Minute)
	if got := desk.Recent(context.Background()); len(got) != 0 {
		t.Fatal("old code still shown")
	}
}

func TestDeskClosedWithoutKey(t *testing.T) {
	srv := newTestServer(t, Config{Desk: NewDesk(nil, nil)})
	if code, _, _ := get(t, srv.URL+"/join/desk/requests", "X-Admin-API-Key", ""); code != http.StatusUnauthorized {
		t.Fatalf("empty key opened the desk: %d", code)
	}
}

func TestNetworkInfoCanBeReadFromAnotherSite(t *testing.T) {
	srv := newTestServer(t, Config{})
	_, _, hdr := get(t, srv.URL+"/api/v1/network/info")
	if hdr.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("CORS header = %q", hdr.Get("Access-Control-Allow-Origin"))
	}
	// Nothing private may be opened to other sites.
	for _, path := range []string{"/join/desk/requests", "/join/desk/owner", "/join/desk/chain"} {
		if _, _, h := get(t, srv.URL+path); h.Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("%s must not send a CORS header", path)
		}
	}
}
