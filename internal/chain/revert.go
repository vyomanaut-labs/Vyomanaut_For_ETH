package chain

import (
	"encoding/hex"
	"fmt"
	"math/big"
)

// RevertError is returned when a node rejects a call because the contract
// reverted. It matches ErrReverted with errors.Is.
type RevertError struct {
	// Reason is a readable description: the Error(string) text, a Panic code,
	// or the name of one of this repository's custom errors.
	Reason string
	// Data is the raw revert data, if the node supplied it.
	Data []byte
}

func (e *RevertError) Error() string { return "chain: execution reverted: " + e.Reason }

// Unwrap lets errors.Is(err, ErrReverted) succeed.
func (e *RevertError) Unwrap() error { return ErrReverted }

var (
	errorStringSelector = selector("Error(string)")
	panicSelector       = selector("Panic(uint256)")
)

// customErrorSignatures lists every custom error the contracts in /contracts
// can raise, so a revert shows its name instead of four unexplained bytes.
var customErrorSignatures = []string{
	// AuditAnchor
	"ZeroCoordinator()", "NotCoordinator()", "EpochNotIncreasing(uint64,uint64)", "EmptyAnchor()",
	// VyomaEscrow
	"ZeroAddress()", "SameCoordinatorAndGuardian()", "BadTimings()", "NotGuardian()",
	"NotOwnerOrCoordinator()", "Reentrancy()", "ZeroAmount()", "NothingReceived()",
	"TransferFailed()", "BadLists()", "PeriodNotIncreasing(uint64,uint64)",
	"InsufficientCredit(address,uint256,uint256)", "TotalMismatch(uint256,uint256)",
	"UnknownPeriod(uint64)", "AlreadyVetoed(uint64)", "PeriodIsVetoed(uint64)",
	"WindowStillOpen(uint64,uint64)", "WindowClosed(uint64)", "NothingToClaim(uint64,address)",
	"CoordinatorStillActive(uint256)", "NothingToRefund(address)",
}

var customErrorNames = func() map[[selectorLen]byte]string {
	m := make(map[[selectorLen]byte]string, len(customErrorSignatures))
	for _, sig := range customErrorSignatures {
		m[selector(sig)] = sig
	}
	return m
}()

// DescribeRevert turns revert data into a short readable string.
func DescribeRevert(data []byte) string {
	if len(data) < selectorLen {
		return "no reason given"
	}
	var sel [selectorLen]byte
	copy(sel[:], data[:selectorLen])
	body := data[selectorLen:]

	switch sel {
	case errorStringSelector:
		if s, ok := decodeErrorString(body); ok {
			return s
		}
	case panicSelector:
		if len(body) >= wordLen {
			return "panic code 0x" + new(big.Int).SetBytes(body[:wordLen]).Text(hexBase)
		}
	}
	if name, ok := customErrorNames[sel]; ok {
		if len(body) == 0 {
			return name
		}
		return fmt.Sprintf("%s args=0x%s", name, hex.EncodeToString(body))
	}
	return "0x" + hex.EncodeToString(data)
}

const hexBase = 16

// decodeErrorString reads the ABI-encoded string of an Error(string) revert.
func decodeErrorString(body []byte) (string, bool) {
	r := abiReader{data: body}
	off, err := r.uintBits(0, maxOffsetBits)
	if err != nil || off%wordLen != 0 {
		return "", false
	}
	lenWord := int(off) / wordLen
	n, err := r.uintBits(lenWord, maxOffsetBits)
	if err != nil {
		return "", false
	}
	start := (lenWord + 1) * wordLen
	end := start + int(n)
	if end < start || end > len(body) {
		return "", false
	}
	return string(body[start:end]), true
}

const maxOffsetBits = 31
