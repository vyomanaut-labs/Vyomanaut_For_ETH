// Package deskowner is a data owner that lives inside the coordinator, so the
// host can store and fetch files from the join desk in a browser, with no
// terminal.
//
// It does exactly what cmd/client does, with the same libraries: the file is
// encrypted and erasure-coded here, on the host's machine, and the shards go
// straight to the providers over p2p. The coordinator's API sees what it sees
// from any other owner. The only shortcut is the join code: in demo mode the
// desk already knows every code the coordinator sends, so registering and
// logging in need no one to type one.
//
// The owner's keys are kept in a file in Dir, readable by this user only.
// They are a demo account belonging to whoever hosts the network, not a
// guest's.
package deskowner

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/client/account"
	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/client/manage"
	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/client/retrieve"
	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/client/upload"
	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/config"
	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/erasure"
	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/p2p"
)

// MaxFileBytes is the largest file the desk accepts. The whole file is held in
// memory while it is encoded, like cmd/client does.
const MaxFileBytes = 25 << 20

// DemoCreditPaise is what "Add demo credit" deposits (₹10,000, as in the guides).
const DemoCreditPaise = 1_000_000

// tokenMaxAge is when the owner logs in again. Owner tokens last 24 hours.
const tokenMaxAge = 20 * time.Hour

const stateFile = "desk-owner.json"

const httpTimeout = 30 * time.Second

// keepJobs is how many finished uploads the desk lists.
const keepJobs = 20

var phonePattern = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)

// CodeSource gives the latest join code sent to a phone since a time. The
// join desk (joinpage.Desk) is one.
type CodeSource interface {
	LatestCode(phone string, since time.Time) (string, bool)
}

// Owner is the desk's data owner. Safe for concurrent use.
type Owner struct {
	baseURL string
	dir     string
	profile config.NetworkProfile
	codes   CodeSource
	http    *http.Client
	now     func() time.Time

	mu    sync.Mutex
	st    *state
	jobs  []*Job
	setup bool // a Setup is running
}

// state is what is saved in Dir.
type state struct {
	Phone        string            `json:"phone"`
	OwnerID      uuid.UUID         `json:"owner_id"`
	PrivateKey   string            `json:"private_key"`   // hex ed25519
	MasterSecret string            `json:"master_secret"` // hex
	Token        string            `json:"token"`
	TokenAt      time.Time         `json:"token_at"`
	Names        map[string]string `json:"names"` // file_id -> name the file was uploaded with
}

// New makes an owner that talks to the coordinator at baseURL (its own
// address, such as http://127.0.0.1:8080) and keeps its keys in dir.
func New(baseURL, dir string, profile config.NetworkProfile, codes CodeSource) *Owner {
	o := &Owner{
		baseURL: strings.TrimRight(baseURL, "/"),
		dir:     dir,
		profile: profile,
		codes:   codes,
		http:    &http.Client{Timeout: httpTimeout},
		now:     time.Now,
	}
	o.st, _ = o.load()
	return o
}

// Status is what the desk shows about the owner.
type Status struct {
	Ready          bool   `json:"ready"`
	Phone          string `json:"phone,omitempty"`
	OwnerID        string `json:"owner_id,omitempty"`
	BalancePaise   int64  `json:"balance_paise"`
	AvailablePaise int64  `json:"available_paise"`
	Error          string `json:"error,omitempty"`
	SettingUp      bool   `json:"setting_up"`
	MaxFileBytes   int    `json:"max_file_bytes"`
}

// Status reports whether the owner is set up, and its balance.
func (o *Owner) Status(ctx context.Context) Status {
	o.mu.Lock()
	st, busy := o.st, o.setup
	o.mu.Unlock()
	s := Status{SettingUp: busy, MaxFileBytes: MaxFileBytes}
	if st == nil {
		return s
	}
	s.Phone, s.OwnerID = st.Phone, st.OwnerID.String()
	token, err := o.token(ctx)
	if err != nil {
		s.Error = err.Error()
		return s
	}
	bal, _, avail, err := manage.NewManager(o.baseURL, token, o.http).Balance(ctx, st.OwnerID)
	if err != nil {
		s.Error = "Cannot read the balance: " + err.Error()
		return s
	}
	s.Ready, s.BalancePaise, s.AvailablePaise = true, bal, avail
	return s
}

// ErrBusy means another setup is already running.
var ErrBusy = errors.New("deskowner: already setting up")

// Setup registers the owner with phone, or logs it in again if phone is the
// one it already has. A new phone replaces the saved owner.
func (o *Owner) Setup(ctx context.Context, phone string) error {
	phone = strings.ReplaceAll(strings.TrimSpace(phone), " ", "")
	if len(phone) == 10 && !strings.HasPrefix(phone, "+") {
		phone = "+91" + phone
	}
	if !phonePattern.MatchString(phone) {
		return fmt.Errorf("Enter the phone as +91 and 10 digits.")
	}
	o.mu.Lock()
	if o.setup {
		o.mu.Unlock()
		return ErrBusy
	}
	o.setup = true
	st := o.st
	o.mu.Unlock()
	defer func() { o.mu.Lock(); o.setup = false; o.mu.Unlock() }()

	if st != nil && st.Phone == phone {
		_, err := o.login(ctx, st)
		return err
	}
	return o.register(ctx, phone)
}

// code sends a join code to phone and reads it back from the desk.
func (o *Owner) code(ctx context.Context, phone, purpose string) (string, error) {
	if o.codes == nil {
		return "", errors.New("The desk can read join codes only in demo mode.")
	}
	since := o.now().Add(-time.Second)
	if err := account.SendOTP(ctx, o.baseURL, o.http, phone, purpose); err != nil {
		return "", fmt.Errorf("Could not send a code: %w", err)
	}
	code, ok := o.codes.LatestCode(phone, since)
	if !ok {
		return "", errors.New("The code did not reach the desk. Is the coordinator in demo mode?")
	}
	return code, nil
}

func (o *Owner) register(ctx context.Context, phone string) error {
	code, err := o.code(ctx, phone, account.OTPPurposeOwnerRegister)
	if err != nil {
		return err
	}
	v, err := account.VerifyOTP(ctx, o.baseURL, o.http, phone, code)
	if err != nil {
		return fmt.Errorf("The code was not accepted: %w", err)
	}
	if !v.IsNewEntity {
		return fmt.Errorf("%s is already registered on this network (as a %s) by someone else. Use another number.", phone, roleName(v.Role))
	}
	reg, err := account.RegisterOwner(ctx, o.baseURL, o.http, v.Token)
	if err != nil {
		return fmt.Errorf("Registration failed: %w", err)
	}
	pass := make([]byte, 32)
	if _, err := rand.Read(pass); err != nil {
		return err
	}
	id, err := account.FinalizeIdentity(reg.PublicKey, reg.PrivateKey, reg.OwnerID, []byte(hex.EncodeToString(pass)), o.profile)
	if err != nil {
		return err
	}
	st := &state{
		Phone:        phone,
		OwnerID:      reg.OwnerID,
		PrivateKey:   hex.EncodeToString(reg.PrivateKey),
		MasterSecret: hex.EncodeToString(id.MasterSecret[:]),
		Token:        reg.Token,
		TokenAt:      o.now(),
		Names:        map[string]string{},
	}
	if err := o.save(st); err != nil {
		return err
	}
	o.mu.Lock()
	o.st = st
	o.mu.Unlock()
	// A fresh owner has no credit; give it some so the first upload works. The ledger
	// credits a deposit a little later (up to about a minute), so wait for it: otherwise the
	// first upload fails with "not enough credit".
	if err := o.Deposit(ctx); err == nil {
		o.waitForCredit(ctx)
	}
	return nil
}

// creditWait is how long Setup waits for the starting credit to appear. It stays under the
// desk's own request timeout; if the credit is slower, the desk shows it when it lands.
const creditWait = 40 * time.Second

// waitForCredit polls the balance until some credit is available, or creditWait passes.
func (o *Owner) waitForCredit(ctx context.Context) {
	st, _, _, err := o.keys()
	if err != nil {
		return
	}
	token, err := o.token(ctx)
	if err != nil {
		return
	}
	m := manage.NewManager(o.baseURL, token, o.http)
	deadline := time.Now().Add(creditWait)
	for time.Now().Before(deadline) {
		if _, _, avail, err := m.Balance(ctx, st.OwnerID); err == nil && avail > 0 {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

// login gets a new token for the saved owner.
func (o *Owner) login(ctx context.Context, st *state) (string, error) {
	code, err := o.code(ctx, st.Phone, account.OTPPurposeLogin)
	if err != nil {
		return "", err
	}
	v, err := account.VerifyOTP(ctx, o.baseURL, o.http, st.Phone, code)
	if err != nil {
		return "", fmt.Errorf("Logging in failed: %w", err)
	}
	if v.IsNewEntity || v.EntityID != st.OwnerID {
		// The network was reset (its database wiped): the saved owner is gone.
		o.forget()
		return "", errors.New("This network no longer knows the saved data owner (its database was reset). Set it up again.")
	}
	o.mu.Lock()
	if o.st != nil {
		o.st.Token, o.st.TokenAt = v.Token, o.now()
		_ = o.save(o.st)
	}
	o.mu.Unlock()
	return v.Token, nil
}

func (o *Owner) forget() {
	o.mu.Lock()
	o.st = nil
	o.mu.Unlock()
	_ = os.Remove(filepath.Join(o.dir, stateFile))
}

// ErrNotSetUp means Setup has not run.
var ErrNotSetUp = errors.New("Set up the data owner first.")

// token returns a current token, logging in again when the saved one is old.
func (o *Owner) token(ctx context.Context) (string, error) {
	o.mu.Lock()
	st := o.st
	if st == nil {
		o.mu.Unlock()
		return "", ErrNotSetUp
	}
	token, at := st.Token, st.TokenAt
	o.mu.Unlock()
	if token != "" && o.now().Sub(at) < tokenMaxAge {
		return token, nil
	}
	return o.login(ctx, st)
}

// keys unlocks the saved keys.
func (o *Owner) keys() (st *state, priv ed25519.PrivateKey, master [32]byte, err error) {
	o.mu.Lock()
	st = o.st
	o.mu.Unlock()
	if st == nil {
		return nil, nil, master, ErrNotSetUp
	}
	pk, err := hex.DecodeString(st.PrivateKey)
	if err != nil || len(pk) != ed25519.PrivateKeySize {
		return nil, nil, master, errors.New("the saved keys are damaged; set the data owner up again")
	}
	ms, err := hex.DecodeString(st.MasterSecret)
	if err != nil || len(ms) != len(master) {
		return nil, nil, master, errors.New("the saved keys are damaged; set the data owner up again")
	}
	copy(master[:], ms)
	return st, ed25519.PrivateKey(pk), master, nil
}

// Deposit adds DemoCreditPaise of demo credit.
func (o *Owner) Deposit(ctx context.Context) error {
	st, _, _, err := o.keys()
	if err != nil {
		return err
	}
	token, err := o.token(ctx)
	if err != nil {
		return err
	}
	reqID, err := uuid.NewV7()
	if err != nil {
		return err
	}
	_, err = manage.NewManager(o.baseURL, token, o.http).Deposit(ctx, st.OwnerID, reqID, DemoCreditPaise)
	return err
}

// File is one stored file.
type File struct {
	FileID       string    `json:"file_id"`
	Name         string    `json:"name"`
	SizeBytes    int64     `json:"size_bytes"`
	UploadedAt   time.Time `json:"uploaded_at"`
	Availability string    `json:"availability"`
	MonthlyPaise int64     `json:"monthly_cost_paise"`
}

// Files lists the owner's files, newest first.
func (o *Owner) Files(ctx context.Context) ([]File, error) {
	st, _, master, err := o.keys()
	if err != nil {
		return nil, err
	}
	token, err := o.token(ctx)
	if err != nil {
		return nil, err
	}
	entries, err := manage.NewManager(o.baseURL, token, o.http).ListFiles(ctx, master, st.OwnerID)
	if err != nil {
		return nil, err
	}
	names := o.names()
	out := make([]File, 0, len(entries))
	for _, e := range entries {
		f := File{FileID: e.FileID.String(), Name: e.DisplayName, SizeBytes: e.SizeBytes, UploadedAt: e.UploadedAt,
			Availability: e.AvailabilityLabel, MonthlyPaise: e.MonthlyCostPaise}
		if n := names[f.FileID]; n != "" {
			f.Name = n
		}
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UploadedAt.After(out[j].UploadedAt) })
	return out, nil
}

func (o *Owner) names() map[string]string {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := map[string]string{}
	if o.st != nil {
		for k, v := range o.st.Names {
			out[k] = v
		}
	}
	return out
}

// Retrieve fetches a file back from the providers and decodes it.
func (o *Owner) Retrieve(ctx context.Context, fileID string) (name string, data []byte, err error) {
	id, err := uuid.Parse(fileID)
	if err != nil {
		return "", nil, errors.New("not a file id")
	}
	st, priv, master, err := o.keys()
	if err != nil {
		return "", nil, err
	}
	token, err := o.token(ctx)
	if err != nil {
		return "", nil, err
	}
	host, engine, err := o.hostAndEngine(priv)
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = host.Close() }()
	data, err = retrieve.NewOrchestrator(o.baseURL, token, o.http, host, engine, o.profile).RetrieveFile(ctx, master, st.OwnerID, id)
	if err != nil {
		return "", nil, err
	}
	name = o.names()[fileID]
	if name == "" {
		name = fileID
	}
	return name, data, nil
}

// Delete removes a file from the network.
func (o *Owner) Delete(ctx context.Context, fileID string) error {
	id, err := uuid.Parse(fileID)
	if err != nil {
		return errors.New("not a file id")
	}
	token, err := o.token(ctx)
	if err != nil {
		return err
	}
	if _, err := manage.NewManager(o.baseURL, token, o.http).DeleteFile(ctx, id); err != nil {
		return err
	}
	o.mu.Lock()
	if o.st != nil {
		delete(o.st.Names, fileID)
		_ = o.save(o.st)
	}
	o.mu.Unlock()
	return nil
}

func (o *Owner) hostAndEngine(priv ed25519.PrivateKey) (p2p.Host, *erasure.Engine, error) {
	host, err := p2p.NewHost(p2p.HostConfig{PrivateKey: priv})
	if err != nil {
		return nil, nil, err
	}
	engine, err := erasure.NewEngine(o.profile)
	if err != nil {
		_ = host.Close()
		return nil, nil, err
	}
	return host, engine, nil
}

// Job is one upload started from the desk.
type Job struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	SizeBytes  int        `json:"size_bytes"`
	State      string     `json:"state"` // "uploading", "done" or "failed"
	FileID     string     `json:"file_id,omitempty"`
	Error      string     `json:"error,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// StartUpload uploads data in the background and returns at once. Jobs
// reports how it went.
func (o *Owner) StartUpload(name string, data []byte) (Job, error) {
	if len(data) == 0 {
		return Job{}, errors.New("The file is empty.")
	}
	if len(data) > MaxFileBytes {
		return Job{}, fmt.Errorf("The file is larger than %d MB.", MaxFileBytes>>20)
	}
	if _, _, _, err := o.keys(); err != nil {
		return Job{}, err
	}
	name = cleanName(name)
	j := &Job{ID: uuid.NewString(), Name: name, SizeBytes: len(data), State: "uploading", StartedAt: o.now().UTC()}
	o.mu.Lock()
	o.jobs = append(o.jobs, j)
	if len(o.jobs) > keepJobs {
		o.jobs = o.jobs[len(o.jobs)-keepJobs:]
	}
	snapshot := *j
	o.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		fileID, err := o.upload(ctx, data)
		o.mu.Lock()
		defer o.mu.Unlock()
		done := o.now().UTC()
		j.FinishedAt = &done
		if err != nil {
			j.State, j.Error = "failed", uploadError(err)
			return
		}
		j.State, j.FileID = "done", fileID.String()
		if o.st != nil {
			if o.st.Names == nil {
				o.st.Names = map[string]string{}
			}
			o.st.Names[j.FileID] = name
			_ = o.save(o.st)
		}
	}()
	return snapshot, nil
}

func (o *Owner) upload(ctx context.Context, data []byte) (uuid.UUID, error) {
	st, priv, master, err := o.keys()
	if err != nil {
		return uuid.Nil, err
	}
	token, err := o.token(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	host, engine, err := o.hostAndEngine(priv)
	if err != nil {
		return uuid.Nil, err
	}
	defer func() { _ = host.Close() }()
	sessions := filepath.Join(o.dir, "upload-sessions")
	orch := upload.NewOrchestrator(o.baseURL, token, o.http, host, engine, o.profile, priv, sessions)
	return orch.UploadFile(ctx, master, st.OwnerID, data)
}

// uploadError says in plain words why an upload failed.
func uploadError(err error) string {
	switch {
	case errors.Is(err, upload.ErrInsufficientEscrow):
		return "Not enough credit. Press \"Add demo credit\" and try again."
	case errors.Is(err, upload.ErrNetworkNotReady):
		return "Not enough providers are online yet. Wait until at least 5 devices are active, then try again."
	default:
		return err.Error()
	}
}

// Jobs lists recent uploads, newest first.
func (o *Owner) Jobs() []Job {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]Job, 0, len(o.jobs))
	for i := len(o.jobs) - 1; i >= 0; i-- {
		out = append(out, *o.jobs[i])
	}
	return out
}

var nameUnsafe = regexp.MustCompile(`[\x00-\x1f\x7f/\\]`)

func cleanName(name string) string {
	// Keep only the last path part, whichever slash the browser's OS uses.
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSpace(nameUnsafe.ReplaceAllString(name, ""))
	if name == "" || name == "." {
		name = "file"
	}
	if len(name) > 120 {
		name = name[:120]
	}
	return name
}

func roleName(role string) string {
	if role == "" {
		return "user"
	}
	return role
}

func (o *Owner) load() (*state, error) {
	b, err := os.ReadFile(filepath.Join(o.dir, stateFile))
	if err != nil {
		return nil, err
	}
	var st state
	if err := json.Unmarshal(b, &st); err != nil || st.OwnerID == uuid.Nil {
		return nil, errors.New("deskowner: unreadable state")
	}
	return &st, nil
}

// save writes st. The caller may hold o.mu.
func (o *Owner) save(st *state) error {
	if err := os.MkdirAll(o.dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(o.dir, stateFile+".tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(o.dir, stateFile))
}
