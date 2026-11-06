package arkiv

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
)

// ErrBadKey means the wallet key file is missing, unreadable, too open, or not
// a secp256k1 key. Its message never contains key material.
var ErrBadKey = errors.New("arkiv: bad wallet key file")

const groupOtherRW = 0o066

// LoadKeyFile reads a wallet key file: 32 bytes of hex on one line (optional
// 0x prefix), the format `arkiv-spike gen` writes. On Unix the file must not be
// readable or writable by group or others (chmod 600).
func LoadKeyFile(path string) (*ecdsa.PrivateKey, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadKey, err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&groupOtherRW != 0 {
		return nil, fmt.Errorf("%w: %s is accessible by other users; run chmod 600", ErrBadKey, path)
	}
	raw, err := os.ReadFile(path) //nolint:gosec // path is operator configuration
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadKey, err)
	}
	s := strings.TrimSpace(string(raw))
	s = strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	key, err := crypto.HexToECDSA(s)
	if err != nil {
		return nil, fmt.Errorf("%w: expected 32 hex-encoded bytes", ErrBadKey)
	}
	return key, nil
}

// ReadSecretFile reads a one-line secret (such as an RPC access key) from a
// file, with the same permission rule as LoadKeyFile.
func ReadSecretFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("arkiv: secret file: %w", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&groupOtherRW != 0 {
		return "", fmt.Errorf("arkiv: %s is accessible by other users; run chmod 600", path)
	}
	raw, err := os.ReadFile(path) //nolint:gosec // path is operator configuration
	if err != nil {
		return "", fmt.Errorf("arkiv: secret file: %w", err)
	}
	return strings.TrimSpace(string(raw)), nil
}
