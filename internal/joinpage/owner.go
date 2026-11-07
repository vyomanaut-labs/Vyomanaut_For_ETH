package joinpage

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/deskowner"
)

// DeskOwner is the desk's built-in data owner (internal/deskowner.Owner).
type DeskOwner interface {
	Status(ctx context.Context) deskowner.Status
	Setup(ctx context.Context, phone string) error
	Deposit(ctx context.Context) error
	StartUpload(name string, data []byte) (deskowner.Job, error)
	Jobs() []deskowner.Job
	Files(ctx context.Context) ([]deskowner.File, error)
	Retrieve(ctx context.Context, fileID string) (string, []byte, error)
	Delete(ctx context.Context, fileID string) error
}

var _ DeskOwner = (*deskowner.Owner)(nil)

// ownerTimeout bounds the quick owner calls (not uploads, which run in the
// background, and not retrieve, which fetches shards).
const ownerTimeout = 45 * time.Second

const retrieveTimeout = 5 * time.Minute

func (s *server) adminOK(w http.ResponseWriter, r *http.Request) bool {
	key := s.cfg.AdminAPIKey
	got := r.Header.Get("X-Admin-API-Key")
	if key == "" || subtle.ConstantTimeCompare([]byte(got), []byte(key)) != 1 {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "wrong admin key"})
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

// owner wraps a desk owner handler: admin key, and a clear answer when the
// coordinator has no built-in owner (not demo mode).
func (s *server) owner(h func(http.ResponseWriter, *http.Request, DeskOwner)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.adminOK(w, r) {
			return
		}
		if s.cfg.Owner == nil {
			writeJSON(w, http.StatusNotFound, map[string]any{"enabled": false, "error": "The built-in data owner runs only in demo mode."})
			return
		}
		h(w, r, s.cfg.Owner)
	}
}

func (s *server) hideOwnPhone(st deskowner.Status) {
	if s.cfg.Desk != nil && st.Phone != "" {
		s.cfg.Desk.HidePhone(normalizePhone(st.Phone))
	}
}

func (s *server) ownerStatus(w http.ResponseWriter, r *http.Request, o DeskOwner) {
	ctx, cancel := context.WithTimeout(r.Context(), ownerTimeout)
	defer cancel()
	st := o.Status(ctx)
	s.hideOwnPhone(st)
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "status": st, "jobs": o.Jobs()})
}

func (s *server) ownerSetup(w http.ResponseWriter, r *http.Request, o DeskOwner) {
	var body struct {
		Phone string `json:"phone"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("send {\"phone\": \"+91...\"}"))
		return
	}
	if s.cfg.Desk != nil {
		// Hide the codes this setup is about to send, before they are sent.
		s.cfg.Desk.HidePhone(normalizePhone(body.Phone))
	}
	ctx, cancel := context.WithTimeout(r.Context(), ownerTimeout)
	defer cancel()
	if err := o.Setup(ctx, body.Phone); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, deskowner.ErrBusy) {
			status = http.StatusConflict
		}
		writeErr(w, status, err)
		return
	}
	st := o.Status(ctx)
	s.hideOwnPhone(st)
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "status": st, "jobs": o.Jobs()})
}

func normalizePhone(p string) string {
	out := make([]rune, 0, len(p))
	for _, c := range p {
		if c != ' ' {
			out = append(out, c)
		}
	}
	if len(out) == 10 && out[0] != '+' {
		return "+91" + string(out)
	}
	return string(out)
}

func (s *server) ownerDeposit(w http.ResponseWriter, r *http.Request, o DeskOwner) {
	ctx, cancel := context.WithTimeout(r.Context(), ownerTimeout)
	defer cancel()
	if err := o.Deposit(ctx); err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": o.Status(ctx)})
}

func (s *server) ownerUpload(w http.ResponseWriter, r *http.Request, o DeskOwner) {
	r.Body = http.MaxBytesReader(w, r.Body, deskowner.MaxFileBytes+(1<<20))
	f, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("Choose a file up to %d MB.", deskowner.MaxFileBytes>>20))
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, deskowner.MaxFileBytes+1))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	job, err := o.StartUpload(hdr.Filename, data)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func (s *server) ownerFiles(w http.ResponseWriter, r *http.Request, o DeskOwner) {
	ctx, cancel := context.WithTimeout(r.Context(), ownerTimeout)
	defer cancel()
	files, err := o.Files(ctx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	if files == nil {
		files = []deskowner.File{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files})
}

func (s *server) ownerRetrieve(w http.ResponseWriter, r *http.Request, o DeskOwner) {
	ctx, cancel := context.WithTimeout(r.Context(), retrieveTimeout)
	defer cancel()
	started := time.Now()
	name, data, err := o.Retrieve(ctx, r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	ctype := mime.TypeByExtension(extOf(name))
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("X-Retrieve-Ms", fmt.Sprint(time.Since(started).Milliseconds()))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

func extOf(name string) string {
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '.' {
			return name[i:]
		}
	}
	return ""
}

func (s *server) ownerDelete(w http.ResponseWriter, r *http.Request, o DeskOwner) {
	ctx, cancel := context.WithTimeout(r.Context(), ownerTimeout)
	defer cancel()
	if err := o.Delete(ctx, r.PathValue("id")); err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": r.PathValue("id")})
}
