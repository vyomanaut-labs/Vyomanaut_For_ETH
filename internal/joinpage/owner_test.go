package joinpage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/deskowner"
)

// fakeOwner records what the desk asked it to do.
type fakeOwner struct {
	phone    string
	uploaded string
	data     []byte
	deleted  string
}

func (f *fakeOwner) Status(context.Context) deskowner.Status {
	return deskowner.Status{Ready: f.phone != "", Phone: f.phone, AvailablePaise: 500}
}
func (f *fakeOwner) Setup(_ context.Context, phone string) error {
	if phone == "bad" {
		return errors.New("Enter the phone as +91 and 10 digits.")
	}
	f.phone = phone
	return nil
}
func (f *fakeOwner) Deposit(context.Context) error { return nil }
func (f *fakeOwner) StartUpload(name string, data []byte) (deskowner.Job, error) {
	f.uploaded, f.data = name, data
	return deskowner.Job{ID: "j1", Name: name, SizeBytes: len(data), State: "uploading"}, nil
}
func (f *fakeOwner) Jobs() []deskowner.Job { return nil }
func (f *fakeOwner) Files(context.Context) ([]deskowner.File, error) {
	return []deskowner.File{{FileID: "f1", Name: "cat.png", SizeBytes: 3}}, nil
}
func (f *fakeOwner) Retrieve(_ context.Context, id string) (string, []byte, error) {
	if id != "f1" {
		return "", nil, errors.New("no such file")
	}
	return "cat.png", []byte("png"), nil
}
func (f *fakeOwner) Delete(_ context.Context, id string) error { f.deleted = id; return nil }

func do(t *testing.T, method, url, ctype string, body io.Reader, withKey bool) (int, string, http.Header) {
	t.Helper()
	req, _ := http.NewRequest(method, url, body)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	if withKey {
		req.Header.Set("X-Admin-API-Key", testKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), resp.Header
}

func TestOwnerRoutesNeedTheKey(t *testing.T) {
	srv := newTestServer(t, Config{AdminAPIKey: testKey, Owner: &fakeOwner{}})
	for _, path := range []string{"/join/desk/owner", "/join/desk/owner/files", "/join/desk/owner/files/f1", "/join/desk/chain"} {
		if code, _, _ := do(t, http.MethodGet, srv.URL+path, "", nil, false); code != http.StatusUnauthorized {
			t.Errorf("%s without key: %d, want 401", path, code)
		}
	}
	if code, _, _ := do(t, http.MethodDelete, srv.URL+"/join/desk/owner/files/f1", "", nil, false); code != http.StatusUnauthorized {
		t.Errorf("delete without key: %d", code)
	}
}

func TestOwnerOffWithoutDemoMode(t *testing.T) {
	srv := newTestServer(t, Config{AdminAPIKey: testKey})
	code, body, _ := do(t, http.MethodGet, srv.URL+"/join/desk/owner", "", nil, true)
	if code != http.StatusNotFound || !strings.Contains(body, "demo mode") {
		t.Fatalf("got %d %s", code, body)
	}
}

func TestOwnerSetupHidesItsOwnCodes(t *testing.T) {
	desk := NewDesk(nil, nil)
	o := &fakeOwner{}
	srv := newTestServer(t, Config{AdminAPIKey: testKey, Owner: o, Desk: desk})

	code, body, _ := do(t, http.MethodPost, srv.URL+"/join/desk/owner/setup", "application/json", strings.NewReader(`{"phone":"98765 43210"}`), true)
	if code != http.StatusOK || o.phone != "98765 43210" {
		t.Fatalf("setup: %d %s (phone %q)", code, body, o.phone)
	}
	_ = desk.SendOTP(context.Background(), "+919876543210", "111111")
	_ = desk.SendOTP(context.Background(), "+910000000001", "222222")
	reqs := desk.Recent(context.Background())
	if len(reqs) != 1 || reqs[0].Code != "222222" {
		t.Fatalf("desk should list only the guest's code, got %+v", reqs)
	}

	code, body, _ = do(t, http.MethodPost, srv.URL+"/join/desk/owner/setup", "application/json", strings.NewReader(`{"phone":"bad"}`), true)
	if code != http.StatusBadRequest || !strings.Contains(body, "+91") {
		t.Fatalf("bad phone: %d %s", code, body)
	}
}

func TestOwnerUploadDownloadDelete(t *testing.T) {
	o := &fakeOwner{phone: "+919876543210"}
	srv := newTestServer(t, Config{AdminAPIKey: testKey, Owner: o})

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "holiday photo.jpg")
	_, _ = fw.Write([]byte("jpegbytes"))
	_ = mw.Close()
	code, body, _ := do(t, http.MethodPost, srv.URL+"/join/desk/owner/upload", mw.FormDataContentType(), &buf, true)
	if code != http.StatusAccepted || o.uploaded != "holiday photo.jpg" || string(o.data) != "jpegbytes" {
		t.Fatalf("upload: %d %s (%q %q)", code, body, o.uploaded, o.data)
	}

	code, body, hdr := do(t, http.MethodGet, srv.URL+"/join/desk/owner/files/f1", "", nil, true)
	if code != http.StatusOK || body != "png" || hdr.Get("Content-Type") != "image/png" ||
		!strings.Contains(hdr.Get("Content-Disposition"), `filename=cat.png`) {
		t.Fatalf("download: %d %q %v", code, body, hdr)
	}
	if code, _, _ := do(t, http.MethodGet, srv.URL+"/join/desk/owner/files/nope", "", nil, true); code != http.StatusBadGateway {
		t.Fatalf("missing file: %d", code)
	}

	code, body, _ = do(t, http.MethodGet, srv.URL+"/join/desk/owner/files", "", nil, true)
	var list struct{ Files []deskowner.File }
	if code != http.StatusOK || json.Unmarshal([]byte(body), &list) != nil || len(list.Files) != 1 {
		t.Fatalf("files: %d %s", code, body)
	}

	if code, _, _ := do(t, http.MethodDelete, srv.URL+"/join/desk/owner/files/f1", "", nil, true); code != http.StatusOK || o.deleted != "f1" {
		t.Fatalf("delete: %d (%q)", code, o.deleted)
	}
}

func TestChainSectionShowsReferenceWhenChainsOff(t *testing.T) {
	srv := newTestServer(t, Config{AdminAPIKey: testKey})
	code, body, _ := do(t, http.MethodGet, srv.URL+"/join/desk/chain", "", nil, true)
	var d struct {
		Chains []ChainSection
		Events []ChainEvent
	}
	if code != http.StatusOK || json.Unmarshal([]byte(body), &d) != nil {
		t.Fatalf("chain: %d %s", code, body)
	}
	if len(d.Chains) != 2 || d.Chains[0].Live || d.Chains[0].ChainID != refChainID || d.Chains[1].ChainID != arkivChainID {
		t.Fatalf("chains = %+v", d.Chains)
	}
	if got := d.Chains[0].Links[0].URL; got != refExplorer+"/address/"+refEscrow {
		t.Fatalf("escrow link = %s", got)
	}
	if !strings.Contains(d.Chains[1].Query, "vyomanaut-demo") || d.Events == nil {
		t.Fatalf("arkiv = %+v, events = %v", d.Chains[1], d.Events)
	}
}

func TestChainSectionUsesThisNetworksAddresses(t *testing.T) {
	c := ChainConfig{WhitechainLive: true, ChainID: 99, Explorer: "https://x.example", Escrow: "0xE", ArkivLive: true, ArkivWriter: "0xW", ArkivAppTag: "mynet"}
	s := c.sections()
	if !s[0].Live || s[0].ChainID != 99 || s[0].Links[0].URL != "https://x.example/address/0xE" {
		t.Fatalf("whitechain = %+v", s[0])
	}
	if !s[1].Live || s[1].Links[0].URL != arkivExplorer+"/address/0xW" || !strings.Contains(s[1].Query, "mynet") {
		t.Fatalf("arkiv = %+v", s[1])
	}
}

func TestLatestCode(t *testing.T) {
	d := NewDesk(nil, nil)
	now := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	d.now = func() time.Time { return now }
	_ = d.SendOTP(context.Background(), "+911", "111111")
	now = now.Add(time.Minute)
	_ = d.SendOTP(context.Background(), "+912", "222222")
	_ = d.SendOTP(context.Background(), "+911", "333333")
	if c, ok := d.LatestCode("+911", now.Add(-time.Second)); !ok || c != "333333" {
		t.Fatalf("got %q %v", c, ok)
	}
	if _, ok := d.LatestCode("+911", now.Add(time.Second)); ok {
		t.Fatal("a code older than since must not be returned")
	}
	if _, ok := d.LatestCode("+913", now.Add(-time.Hour)); ok {
		t.Fatal("unknown phone")
	}
}
