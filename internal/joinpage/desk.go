package joinpage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"sync"
	"time"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/api"
)

// deskKeep is how many recent join codes the desk remembers.
const deskKeep = 100

// deskShowFor is how long a code stays on the desk after it was sent. Codes
// expire after 10 minutes (internal/api otpTTL); a few more minutes let the
// operator see that a code was used or ran out.
const deskShowFor = 15 * time.Minute

// codeLifetime mirrors internal/api's otpTTL.
const codeLifetime = 10 * time.Minute

// Desk is an api.OtpSender that remembers each code it hands on, so the
// operator's join desk page can show it. It wraps the real sender (the demo
// delivery log), which still receives every code.
//
// The codes live only in this process's memory: nothing new is written to
// disk or the database.
type Desk struct {
	next api.OtpSender
	db   *sql.DB
	now  func() time.Time

	mu       sync.Mutex
	entries  []deskEntry
	ownPhone string // the desk's own data owner; its codes are not listed
}

type deskEntry struct {
	phone  string
	code   string
	sentAt time.Time
}

// NewDesk wraps next. db may be nil; with a database the desk can also tell
// whether each code has been used.
func NewDesk(next api.OtpSender, db *sql.DB) *Desk {
	return &Desk{next: next, db: db, now: time.Now}
}

var _ api.OtpSender = (*Desk)(nil)

// SendOTP hands the code to the wrapped sender, then records it.
func (d *Desk) SendOTP(ctx context.Context, phoneNumber, code string) error {
	if d.next != nil {
		if err := d.next.SendOTP(ctx, phoneNumber, code); err != nil {
			return err
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.entries = append(d.entries, deskEntry{phone: phoneNumber, code: code, sentAt: d.now()})
	if len(d.entries) > deskKeep {
		d.entries = d.entries[len(d.entries)-deskKeep:]
	}
	return nil
}

// LatestCode returns the newest code sent to phone at or after since. The
// desk's own data owner (internal/deskowner) uses it to register and log in
// without anyone typing a code.
func (d *Desk) LatestCode(phone string, since time.Time) (string, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i := len(d.entries) - 1; i >= 0; i-- {
		e := d.entries[i]
		if e.sentAt.Before(since) {
			break
		}
		if e.phone == phone {
			return e.code, true
		}
	}
	return "", false
}

// hidden reports whether phone belongs to the desk's own data owner, whose
// codes the desk uses itself and does not list.
func (d *Desk) hidden(phone string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.ownPhone != "" && phone == d.ownPhone
}

// HidePhone stops the desk listing codes for phone (the desk's own owner).
func (d *Desk) HidePhone(phone string) {
	d.mu.Lock()
	d.ownPhone = phone
	d.mu.Unlock()
}

// DeskRequest is one row on the join desk.
type DeskRequest struct {
	Phone     string    `json:"phone"`
	Code      string    `json:"code"`
	SentAt    time.Time `json:"sent_at"`
	ExpiresAt time.Time `json:"expires_at"`
	// State is "waiting", "joined" or "expired".
	State string `json:"state"`
}

// Recent returns the codes sent in the last deskShowFor, newest first.
func (d *Desk) Recent(ctx context.Context) []DeskRequest {
	d.mu.Lock()
	snapshot := append([]deskEntry(nil), d.entries...)
	d.mu.Unlock()

	now := d.now()
	out := make([]DeskRequest, 0, len(snapshot))
	for i := len(snapshot) - 1; i >= 0; i-- {
		e := snapshot[i]
		if now.Sub(e.sentAt) > deskShowFor || d.hidden(e.phone) {
			continue
		}
		r := DeskRequest{Phone: e.phone, Code: e.code, SentAt: e.sentAt.UTC(), ExpiresAt: e.sentAt.Add(codeLifetime).UTC(), State: "waiting"}
		switch {
		case d.used(ctx, e.phone, e.code):
			r.State = "joined"
		case now.After(r.ExpiresAt):
			r.State = "expired"
		}
		out = append(out, r)
	}
	return out
}

// used reports whether the code has been consumed. otp_codes keeps only a
// SHA-256 of each code, the same hash internal/api writes.
func (d *Desk) used(ctx context.Context, phone, code string) bool {
	if d.db == nil {
		return false
	}
	hash := sha256.Sum256([]byte(code))
	var consumed bool
	err := d.db.QueryRowContext(ctx,
		`SELECT consumed_at IS NOT NULL FROM otp_codes WHERE phone_number = $1 AND code_hash = $2 ORDER BY created_at DESC LIMIT 1`,
		phone, hash[:]).Scan(&consumed)
	return err == nil && consumed
}
