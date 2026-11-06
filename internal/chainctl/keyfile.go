package chainctl

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/chain"
)

const (
	keyBytes   = 32
	keyFileDir = 0o700
	keyFileOne = 0o600
	maxTries   = 8
)

// ErrKeyFileExists means a key file is already there and was not overwritten.
var ErrKeyFileExists = errors.New("chainctl: that key file already exists; it is not overwritten")

// NewKeyFile makes a fresh wallet key and writes it to path, readable by the
// owner only. It never overwrites an existing file: losing a key loses the
// wallet. It returns the wallet's address.
func NewKeyFile(path string) (chain.Address, error) {
	var key *chain.Key
	var raw [keyBytes]byte
	for i := 0; i < maxTries && key == nil; i++ {
		if _, err := rand.Read(raw[:]); err != nil {
			return chain.Address{}, fmt.Errorf("random: %w", err)
		}
		if k, err := chain.ParseKeyHex(hex.EncodeToString(raw[:])); err == nil {
			key = k
		}
	}
	if key == nil {
		return chain.Address{}, errors.New("chainctl: could not make a valid key")
	}
	if err := os.MkdirAll(filepath.Dir(path), keyFileDir); err != nil {
		return chain.Address{}, fmt.Errorf("create the key directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, keyFileOne) //nolint:gosec // an operator-chosen path
	if errors.Is(err, os.ErrExist) {
		return chain.Address{}, ErrKeyFileExists
	}
	if err != nil {
		return chain.Address{}, fmt.Errorf("create the key file: %w", err)
	}
	_, werr := f.WriteString(hex.EncodeToString(raw[:]) + "\n")
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	clear(raw[:])
	if werr != nil {
		_ = os.Remove(path)
		return chain.Address{}, fmt.Errorf("write the key file: %w", werr)
	}
	return key.Address(), nil
}
