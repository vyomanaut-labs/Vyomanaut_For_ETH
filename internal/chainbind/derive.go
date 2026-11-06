package chainbind

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"

	"github.com/google/uuid"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/chain"
)

const (
	deriveLabel   = "vyomanaut/demo-payout-wallet/v1"
	maxDeriveTrys = 16
	minSeedLen    = 16
)

// ErrSeedTooShort means the secret is too short to derive keys from.
var ErrSeedTooShort = errors.New("chainbind: the derivation secret is too short")

// DeriveProviderKey derives the demo payout key for a provider from the
// coordinator's secret. The same secret and provider always give the same key;
// without the secret the key cannot be computed. It is custodial by design.
func DeriveProviderKey(secret []byte, provider uuid.UUID) (*chain.Key, error) {
	if len(secret) < minSeedLen {
		return nil, ErrSeedTooShort
	}
	// An HMAC output is a valid secp256k1 key except with negligible
	// probability; a counter makes the loop certain to finish.
	for counter := uint32(0); counter < maxDeriveTrys; counter++ {
		mac := hmac.New(sha256.New, secret)
		mac.Write([]byte(deriveLabel))
		mac.Write(provider[:])
		_ = binary.Write(mac, binary.BigEndian, counter)
		if k, err := chain.ParseKeyHex(hex.EncodeToString(mac.Sum(nil))); err == nil {
			return k, nil
		}
	}
	return nil, errors.New("chainbind: could not derive a key")
}

// DeriveProviderWallet is the address of DeriveProviderKey.
func DeriveProviderWallet(secret []byte, provider uuid.UUID) (chain.Address, error) {
	k, err := DeriveProviderKey(secret, provider)
	if err != nil {
		return chain.Address{}, err
	}
	return k.Address(), nil
}
