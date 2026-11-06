package chainctl

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// decimalBase is the base amounts are written in.
const decimalBase = 10

// TokenDecimals is the number of decimals of the USDT used here.
const TokenDecimals = 6

// ErrBadAmount means an amount could not be read.
var ErrBadAmount = errors.New("chainctl: bad amount")

// ParseAmount reads a decimal amount such as "1", "2.5" or "0.000001" into
// token base units, exactly. It refuses signs, exponents and more decimals
// than the token has, rather than rounding money.
func ParseAmount(s string) (*big.Int, error) {
	s = strings.TrimSpace(s)
	whole, frac, _ := strings.Cut(s, ".")
	if whole == "" && frac == "" {
		return nil, fmt.Errorf("%w: %q", ErrBadAmount, s)
	}
	if len(frac) > TokenDecimals {
		return nil, fmt.Errorf("%w: %q has more than %d decimals", ErrBadAmount, s, TokenDecimals)
	}
	for _, part := range []string{whole, frac} {
		for _, c := range part {
			if c < '0' || c > '9' {
				return nil, fmt.Errorf("%w: %q", ErrBadAmount, s)
			}
		}
	}
	frac += strings.Repeat("0", TokenDecimals-len(frac))
	v, ok := new(big.Int).SetString(whole+frac, decimalBase)
	if !ok || v.Sign() <= 0 {
		return nil, fmt.Errorf("%w: %q must be greater than zero", ErrBadAmount, s)
	}
	return v, nil
}

// FormatAmount shows token base units as a decimal, trailing zeros trimmed.
func FormatAmount(v *big.Int) string { return FormatUnits(v, TokenDecimals) }

// FormatUnits shows v, counted in units of 10^-decimals, as a decimal with
// trailing zeros trimmed.
func FormatUnits(v *big.Int, decimals int) string {
	if v == nil {
		return "0"
	}
	neg := v.Sign() < 0
	abs := new(big.Int).Abs(v).String()
	if len(abs) <= decimals {
		abs = strings.Repeat("0", decimals-len(abs)+1) + abs
	}
	whole, frac := abs[:len(abs)-decimals], strings.TrimRight(abs[len(abs)-decimals:], "0")
	out := whole
	if frac != "" {
		out += "." + frac
	}
	if neg {
		out = "-" + out
	}
	return out
}

// GasDecimals is the number of decimals of WBT, the gas token.
const GasDecimals = 18

// ExplorerTx is a block-explorer link for a transaction, or "" without a base.
func ExplorerTx(base string, hash [32]byte) string {
	if base == "" {
		return ""
	}
	return fmt.Sprintf("%s/tx/0x%x", strings.TrimRight(base, "/"), hash[:])
}

// ExplorerAddress is a block-explorer link for an address, or "" without a base.
func ExplorerAddress(base string, a [20]byte) string {
	if base == "" {
		return ""
	}
	return fmt.Sprintf("%s/address/0x%x", strings.TrimRight(base, "/"), a[:])
}
