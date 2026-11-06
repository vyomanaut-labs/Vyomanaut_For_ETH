package chainctl

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

// Identity is what the data owner's client left on disk that this tool needs:
// who the owner is and the current session token. The key material in the same
// file is never read.
type Identity struct {
	OwnerID uuid.UUID
	Token   string
}

// ErrNoIdentity means the data directory holds no registered owner.
var ErrNoIdentity = errors.New("chainctl: no owner is registered in that data directory; run the client's register first")

// ReadIdentity reads <dataDir>/identity.json, as written by the owner client.
func ReadIdentity(dataDir string) (Identity, error) {
	raw, err := os.ReadFile(filepath.Join(dataDir, "identity.json")) //nolint:gosec // an operator-chosen directory
	if errors.Is(err, os.ErrNotExist) {
		return Identity{}, ErrNoIdentity
	}
	if err != nil {
		return Identity{}, fmt.Errorf("read the identity file: %w", err)
	}
	var rec struct {
		OwnerID string `json:"owner_id"`
		Token   string `json:"token"`
	}
	if err := json.Unmarshal(raw, &rec); err != nil {
		return Identity{}, fmt.Errorf("the identity file is not readable: %w", err)
	}
	id, err := uuid.Parse(rec.OwnerID)
	if err != nil || rec.Token == "" {
		return Identity{}, ErrNoIdentity
	}
	return Identity{OwnerID: id, Token: rec.Token}, nil
}
