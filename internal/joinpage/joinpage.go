// Package joinpage lets a stranger join a Vyomanaut network in one scan.
//
// The coordinator serves, on its normal HTTP port:
//
//	GET /                      redirects to /join
//	GET /join                  the page a phone or laptop opens (from the QR)
//	GET /join/screen           the big-screen page with the QR, for the host
//	GET /join/qr.png           the QR code of /join
//	GET /join/desk             the host's join desk (needs the admin key)
//	GET /join/desk/requests    JSON for the desk (X-Admin-API-Key)
//	GET /join/download/{name}  the app and provider downloads
//	GET /join/install.sh       one-line laptop setup, macOS and Linux
//	GET /join/install.ps1      one-line laptop setup, Windows
//	GET /api/v1/network/info   where this network's relay and chain API are
//
// Everything except the desk is public. The pages carry no secret: what
// keeps a network invite-only is the 6-digit code, which only the host sees.
package joinpage

import (
	"crypto/subtle"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	texttemplate "text/template"

	qrcode "github.com/skip2/go-qrcode"
)

//go:embed assets
var assets embed.FS

var (
	pages   = template.Must(template.ParseFS(assets, "assets/*.html"))
	scripts = texttemplate.Must(texttemplate.New("").Delims("[[", "]]").Funcs(texttemplate.FuncMap{
		"sh": shellQuote,
		"ps": psQuote,
	}).ParseFS(assets, "assets/install.*"))
)

// AppFile is the Android app's file name in the downloads folder and on the
// GitHub release.
const AppFile = "vyomanaut-provider-android.apk"

// AppPackage is the Android app's package name, used by the "open in app" link.
const AppPackage = "org.vyomanaut.provider"

// DefaultReleaseBase is where downloads come from when the coordinator has no
// local copy.
const DefaultReleaseBase = "https://github.com/vyomanaut-labs/Vyomanaut_For_ETH/releases/latest/download"

// DefaultRelayPort is the port cmd/relay listens on by default.
const DefaultRelayPort = "30300"

// Config says what the pages tell people. Empty fields fall back to values
// taken from each request.
type Config struct {
	// NetworkName is shown on every page.
	NetworkName string
	// PublicURL is the coordinator's address as others reach it, such as a
	// Cloudflare tunnel URL. Empty = the address the request came in on.
	PublicURL string
	// RelayAddrs are the relay addresses providers dial ("host:port").
	// Empty = the coordinator's host on port 30300.
	RelayAddrs []string
	// ChainAPIURL is the public chain API address, if the network runs one.
	ChainAPIURL string
	// DownloadsDir holds the app and provider files (the release script's
	// dist/ folder). Files missing from it are fetched from ReleaseBase.
	DownloadsDir string
	// ReleaseBase is where missing downloads redirect to.
	ReleaseBase string
	// Mode is the network profile mode passed to `provider run` ("demo").
	Mode string
	// AdminAPIKey opens the desk. Empty = the desk is closed.
	AdminAPIKey string
	// Desk records join codes. nil = the desk shows none.
	Desk *Desk
}

// ConfigFromEnv reads the optional VYOMANAUT_PUBLIC_* settings.
//
//	VYOMANAUT_NETWORK_NAME      name shown on the pages
//	VYOMANAUT_PUBLIC_URL        the coordinator's public address
//	VYOMANAUT_PUBLIC_RELAY      relay address(es) for providers, comma-separated
//	                            (falls back to VYOMANAUT_RELAY_ADDRS)
//	VYOMANAUT_PUBLIC_CHAIN_API  the chain API's public address
//	VYOMANAUT_DOWNLOADS_DIR     folder holding the app and provider files
//	VYOMANAUT_RELEASE_BASE      where missing files are downloaded from
func ConfigFromEnv() Config {
	relays := os.Getenv("VYOMANAUT_PUBLIC_RELAY")
	if relays == "" {
		relays = os.Getenv("VYOMANAUT_RELAY_ADDRS")
	}
	cfg := Config{
		NetworkName:  os.Getenv("VYOMANAUT_NETWORK_NAME"),
		PublicURL:    strings.TrimRight(os.Getenv("VYOMANAUT_PUBLIC_URL"), "/"),
		RelayAddrs:   splitList(relays),
		ChainAPIURL:  strings.TrimRight(os.Getenv("VYOMANAUT_PUBLIC_CHAIN_API"), "/"),
		DownloadsDir: os.Getenv("VYOMANAUT_DOWNLOADS_DIR"),
		ReleaseBase:  strings.TrimRight(os.Getenv("VYOMANAUT_RELEASE_BASE"), "/"),
	}
	return cfg
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Handler serves the join routes and passes every other request to next.
func Handler(next http.Handler, cfg Config) http.Handler {
	cfg.NetworkName = nameUnsafe.ReplaceAllString(cfg.NetworkName, "")
	if strings.TrimSpace(cfg.NetworkName) == "" {
		cfg.NetworkName = "Vyomanaut network"
	}
	if cfg.ReleaseBase == "" {
		cfg.ReleaseBase = DefaultReleaseBase
	}
	if cfg.Mode == "" {
		cfg.Mode = "demo"
	}
	s := &server{cfg: cfg}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/join", http.StatusFound)
	})
	mux.HandleFunc("GET /join", s.page("join.html"))
	mux.HandleFunc("GET /join/screen", s.page("screen.html"))
	mux.HandleFunc("GET /join/desk", s.page("desk.html"))
	mux.HandleFunc("GET /join/desk/requests", s.deskRequests)
	mux.HandleFunc("GET /join/qr.png", s.qr)
	mux.HandleFunc("GET /join/download/{name}", s.download)
	mux.HandleFunc("GET /join/install.sh", s.script("install.sh", "text/x-shellscript; charset=utf-8"))
	mux.HandleFunc("GET /join/install.ps1", s.script("install.ps1", "text/plain; charset=utf-8"))
	mux.HandleFunc("GET /api/v1/network/info", s.infoJSON)
	mux.Handle("/", next)
	return mux
}

type server struct{ cfg Config }

// Info is what GET /api/v1/network/info returns, and what the pages show.
type Info struct {
	NetworkName    string   `json:"network_name"`
	CoordinatorURL string   `json:"coordinator_url"`
	RelayAddrs     []string `json:"relay_addrs"`
	ChainAPIURL    string   `json:"chain_api_url,omitempty"`
	JoinURL        string   `json:"join_url"`
	// AppLink opens the Android app with this network filled in.
	AppLink string `json:"app_link"`
	// AppDownloadURL is the Android app.
	AppDownloadURL string `json:"app_download_url"`
	Mode           string `json:"mode"`
}

var hostPattern = regexp.MustCompile(`^[A-Za-z0-9.\-]+(:[0-9]{1,5})?$|^\[[0-9A-Fa-f:.]+\](:[0-9]{1,5})?$`)

// coordinatorURL is the address others should use for this coordinator.
func (s *server) coordinatorURL(r *http.Request) string {
	if s.cfg.PublicURL != "" {
		return s.cfg.PublicURL
	}
	host := r.Host
	if !hostPattern.MatchString(host) {
		host = "localhost"
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + host
}

func (s *server) networkInfo(r *http.Request) Info {
	coord := s.coordinatorURL(r)
	relays := s.cfg.RelayAddrs
	if len(relays) == 0 {
		if u, err := url.Parse(coord); err == nil && u.Hostname() != "" {
			relays = []string{hostPort(u.Hostname(), DefaultRelayPort)}
		}
	}
	in := Info{
		NetworkName:    s.cfg.NetworkName,
		CoordinatorURL: coord,
		RelayAddrs:     relays,
		ChainAPIURL:    s.cfg.ChainAPIURL,
		JoinURL:        coord + "/join",
		AppDownloadURL: coord + "/join/download/" + AppFile,
		Mode:           s.cfg.Mode,
	}
	in.AppLink = "vyomanaut://join?" + appQuery(in).Encode()
	return in
}

func hostPort(host, port string) string {
	if strings.Contains(host, ":") {
		return "[" + host + "]:" + port
	}
	return host + ":" + port
}

func appQuery(in Info) url.Values {
	q := url.Values{}
	q.Set("coordinator", in.CoordinatorURL)
	if len(in.RelayAddrs) > 0 {
		q.Set("relay", strings.Join(in.RelayAddrs, ","))
	}
	if in.ChainAPIURL != "" {
		q.Set("chain", in.ChainAPIURL)
	}
	q.Set("name", in.NetworkName)
	return q
}

// intentLink is the Chrome-on-Android form of AppLink: it opens the app when
// installed, otherwise it downloads the app.
func intentLink(in Info) string {
	return "intent://join?" + appQuery(in).Encode() +
		"#Intent;scheme=vyomanaut;package=" + AppPackage +
		";S.browser_fallback_url=" + url.QueryEscape(in.AppDownloadURL) + ";end"
}


func (s *server) infoJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(s.networkInfo(r))
}

// pageData is what the HTML pages are rendered with.
type pageData struct {
	Info
	IntentLink template.URL
	AppLinkURL template.URL
	ShCommand  string
	PsCommand  string
	DeskOpen   bool
}

func (s *server) page(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		in := s.networkInfo(r)
		d := pageData{
			Info: in,
			// Both are built from this server's own values and query-escaped,
			// so they are safe to put in href.
			IntentLink: template.URL(intentLink(in)),
			AppLinkURL: template.URL(in.AppLink),
			ShCommand:  "curl -fsSL " + in.CoordinatorURL + "/join/install.sh | sh",
			PsCommand:  "irm " + in.CoordinatorURL + "/join/install.ps1 | iex",
			DeskOpen:   s.cfg.AdminAPIKey != "",
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if err := pages.ExecuteTemplate(w, name, d); err != nil {
			http.Error(w, "page error", http.StatusInternalServerError)
		}
	}
}

func (s *server) qr(w http.ResponseWriter, r *http.Request) {
	png, err := qrcode.Encode(s.networkInfo(r).JoinURL, qrcode.Medium, 512)
	if err != nil {
		http.Error(w, "qr error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(png)
}

// QRText renders a QR code for the terminal (two rows per character).
func QRText(content string) (string, error) {
	q, err := qrcode.New(content, qrcode.Low)
	if err != nil {
		return "", err
	}
	return q.ToSmallString(false), nil
}

var fileNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._\-]*$`)

func (s *server) download(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !fileNamePattern.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	if s.cfg.DownloadsDir != "" {
		path := filepath.Join(s.cfg.DownloadsDir, name)
		if st, err := os.Stat(path); err == nil && st.Mode().IsRegular() {
			if strings.HasSuffix(name, ".apk") {
				w.Header().Set("Content-Type", "application/vnd.android.package-archive")
			} else {
				w.Header().Set("Content-Type", "application/octet-stream")
			}
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
			http.ServeFile(w, r, path)
			return
		}
	}
	http.Redirect(w, r, s.cfg.ReleaseBase+"/"+url.PathEscape(name), http.StatusFound)
}

// scriptData is what the install scripts are rendered with.
type scriptData struct {
	Info
	Relay string
	// Slug names this network's folder on the laptop, so joining a second
	// network never reuses the first one's registration.
	Slug string
}

// nameUnsafe strips characters that could break out of the quoted strings
// the network name is placed in (HTML, JavaScript, shell, PowerShell).
var nameUnsafe = regexp.MustCompile(`[^\p{L}\p{N} .,&()_+-]`)

var slugUnsafe = regexp.MustCompile(`[^a-z0-9]+`)

func (s *server) script(name, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		in := s.networkInfo(r)
		d := scriptData{Info: in, Relay: strings.Join(in.RelayAddrs, ",")}
		if u, err := url.Parse(in.CoordinatorURL); err == nil {
			d.Slug = strings.Trim(slugUnsafe.ReplaceAllString(strings.ToLower(u.Host), "-"), "-")
		}
		if d.Slug == "" {
			d.Slug = "network"
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "no-store")
		if err := scripts.ExecuteTemplate(w, name, d); err != nil {
			http.Error(w, "script error", http.StatusInternalServerError)
		}
	}
}

// shellQuote quotes s for a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// psQuote quotes s for PowerShell.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func (s *server) deskRequests(w http.ResponseWriter, r *http.Request) {
	key := s.cfg.AdminAPIKey
	got := r.Header.Get("X-Admin-API-Key")
	if key == "" || subtle.ConstantTimeCompare([]byte(got), []byte(key)) != 1 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"wrong admin key"}`))
		return
	}
	var reqs []DeskRequest
	if s.cfg.Desk != nil {
		reqs = s.cfg.Desk.Recent(r.Context())
	}
	if reqs == nil {
		reqs = []DeskRequest{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"enabled": s.cfg.Desk != nil, "requests": reqs})
}
