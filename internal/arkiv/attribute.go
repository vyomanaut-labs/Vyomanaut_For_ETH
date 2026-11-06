package arkiv

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"sort"

	"github.com/ethereum/go-ethereum/common"
)

// Attribute type ids (low 7 bits of typeId). Only the types this feature uses.
const (
	TypeU64   uint8 = 3
	TypeBytes uint8 = 7
	TypeStr   uint8 = 8

	indexedBit uint8 = 0x80
)

// Protocol limits (https://docs.arkiv.network/reference/limits/) and the one
// operational cap we add.
const (
	MaxAttributes = 32
	MaxNameBytes  = 32
	MaxStrBytes   = 128
	// MaxBytesTotal is the protocol limit for all bytes attributes in one create.
	MaxBytesTotal = 128 * 1024
	// VerifiedMaxBytes is the largest payload actually created on Rabdi in
	// spike A0 (100 KiB, about 6.0 M gas). Publishers stay at or below it.
	VerifiedMaxBytes = 100 * 1024
)

// Attribute is one named, typed value on an entity. Build it with NewStr,
// NewU64 or NewBytes so the limits are checked.
type Attribute struct {
	Name   [32]byte
	TypeID uint8
	Value  []byte
}

// NameString returns the attribute name without its zero padding.
func (a Attribute) NameString() string {
	return string(bytes.TrimRight(a.Name[:], "\x00"))
}

func nameBytes(name string) ([32]byte, error) {
	var out [32]byte
	if name == "" || len(name) > MaxNameBytes {
		return out, fmt.Errorf("arkiv: attribute name %q must be 1 to %d bytes", name, MaxNameBytes)
	}
	if name[0] == '$' {
		return out, fmt.Errorf("arkiv: attribute name %q: names starting with $ are reserved", name)
	}
	for i := 0; i < len(name); i++ {
		if name[i] == 0 || name[i] >= 0x80 {
			return out, fmt.Errorf("arkiv: attribute name %q must be ASCII without NUL", name)
		}
	}
	copy(out[:], name)
	return out, nil
}

// NewStr builds a string attribute (at most MaxStrBytes bytes of UTF-8).
func NewStr(name, value string, indexed bool) (Attribute, error) {
	n, err := nameBytes(name)
	if err != nil {
		return Attribute{}, err
	}
	if len(value) > MaxStrBytes {
		return Attribute{}, fmt.Errorf("arkiv: attribute %q: string is %d bytes, limit %d", name, len(value), MaxStrBytes)
	}
	t := TypeStr
	if indexed {
		t |= indexedBit
	}
	return Attribute{Name: n, TypeID: t, Value: []byte(value)}, nil
}

// NewU64 builds an unsigned 64-bit attribute (32-byte big-endian value).
func NewU64(name string, value uint64, indexed bool) (Attribute, error) {
	n, err := nameBytes(name)
	if err != nil {
		return Attribute{}, err
	}
	t := TypeU64
	if indexed {
		t |= indexedBit
	}
	return Attribute{Name: n, TypeID: t, Value: common.LeftPadBytes(new(big.Int).SetUint64(value).Bytes(), abiWordSize)}, nil
}

// NewBytes builds a bytes attribute. Bytes attributes cannot be indexed.
func NewBytes(name string, value []byte) (Attribute, error) {
	n, err := nameBytes(name)
	if err != nil {
		return Attribute{}, err
	}
	return Attribute{Name: n, TypeID: TypeBytes, Value: value}, nil
}

// normalize returns a sorted copy of attrs after checking the protocol rules:
// 1 to MaxAttributes attributes, strictly ascending unique names, bytes
// attributes not indexed and at most MaxBytesTotal bytes in total.
func normalize(attrs []Attribute) ([]Attribute, error) {
	if len(attrs) == 0 {
		return nil, errors.New("arkiv: an entity needs at least one attribute")
	}
	if len(attrs) > MaxAttributes {
		return nil, fmt.Errorf("arkiv: %d attributes, limit %d", len(attrs), MaxAttributes)
	}
	out := make([]Attribute, len(attrs))
	copy(out, attrs)
	sort.Slice(out, func(i, j int) bool { return bytes.Compare(out[i].Name[:], out[j].Name[:]) < 0 })
	total := 0
	for i, a := range out {
		if i > 0 && a.Name == out[i-1].Name {
			return nil, fmt.Errorf("arkiv: duplicate attribute %q", a.NameString())
		}
		if a.TypeID&^indexedBit == TypeBytes {
			if a.TypeID&indexedBit != 0 {
				return nil, fmt.Errorf("arkiv: attribute %q: bytes cannot be indexed", a.NameString())
			}
			total += len(a.Value)
		}
	}
	if total > MaxBytesTotal {
		return nil, fmt.Errorf("arkiv: %d bytes of bytes attributes, limit %d", total, MaxBytesTotal)
	}
	return out, nil
}
