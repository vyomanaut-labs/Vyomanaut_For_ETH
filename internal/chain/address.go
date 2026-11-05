package chain

import (
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	// AddressLen is the length of an account address in bytes.
	AddressLen = 20
	// checksumUpperFrom is the hash nibble value from which EIP-55 upper-cases a letter.
	checksumUpperFrom = 8
	nibbleMask        = 0x0f
	nibbleShift       = 4
)

// Address is a 20-byte account or contract address.
type Address [AddressLen]byte

// ParseAddress reads a hex address with or without a 0x prefix. It does not
// require or check an EIP-55 checksum.
func ParseAddress(s string) (Address, error) {
	var a Address
	raw, err := hex.DecodeString(strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X"))
	if err != nil || len(raw) != AddressLen {
		return a, fmt.Errorf("%w: %q", ErrBadAddress, s)
	}
	copy(a[:], raw)
	return a, nil
}

// MustParseAddress is ParseAddress for constants; it panics on bad input.
func MustParseAddress(s string) Address {
	a, err := ParseAddress(s)
	if err != nil {
		panic(err)
	}
	return a
}

// Hex returns the lower-case 0x form.
func (a Address) Hex() string { return "0x" + hex.EncodeToString(a[:]) }

// IsZero reports whether the address is all zero bytes.
func (a Address) IsZero() bool { return a == Address{} }

// String returns the EIP-55 mixed-case checksum form.
func (a Address) String() string {
	lower := hex.EncodeToString(a[:])
	digest := Keccak256([]byte(lower))
	out := []byte(lower)
	for i, c := range out {
		if c < 'a' || c > 'f' {
			continue
		}
		nibble := digest[i/2]
		if i%2 == 0 {
			nibble >>= nibbleShift
		}
		if nibble&nibbleMask >= checksumUpperFrom {
			out[i] = c - 'a' + 'A'
		}
	}
	return "0x" + string(out)
}
