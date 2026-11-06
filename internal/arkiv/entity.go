package arkiv

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// Address is the Arkiv contract, the same on every Arkiv chain.
var Address = common.HexToAddress("0x4400000000000000000000000000000000000044")

// executeSelector is the selector of execute((uint8,bytes)[]). The unit tests
// check it against keccak256 of executeSignature.
var executeSelector = [4]byte{0x49, 0x65, 0x00, 0x44}

const (
	executeSignature = "execute((uint8,bytes)[])"
	opCreate         = uint8(1)

	// ABI and key-derivation sizes.
	abiWordSize    = 32  // one ABI word
	saltBits       = 128 // uint128 salt
	saltBytes      = saltBits / 8
	predictKeySize = abiWordSize + common.AddressLength + common.AddressLength + 8 + saltBytes
)

// The structs below are only ever handed to the ABI packer, which matches
// fields to ABI component names by their camel-cased spelling ("typeId" is
// TypeId, "operationData" is OperationData), so these names are not free.
//
//nolint:revive,staticcheck // field names must match the ABI component names
type abiAttribute struct {
	Name   [32]byte
	TypeId uint8
	Value  []byte
}

//nolint:revive,staticcheck // field names must match the ABI component names
type abiOp struct {
	Operation     uint8
	OperationData []byte
}

var (
	tUint8   = mustABIType("uint8", nil)
	tUint64  = mustABIType("uint64", nil)
	tUint128 = mustABIType("uint128", nil)
	tAttrs   = mustABIType("tuple[]", []abi.ArgumentMarshaling{
		{Name: "name", Type: "bytes32"},
		{Name: "typeId", Type: "uint8"},
		{Name: "value", Type: "bytes"},
	})
	tOps = mustABIType("tuple[]", []abi.ArgumentMarshaling{
		{Name: "operation", Type: "uint8"},
		{Name: "operationData", Type: "bytes"},
	})
)

// mustABIType builds a fixed, compile-time-known ABI type. It cannot fail for
// the literals above, so a panic here is a programming error caught by any test run.
func mustABIType(t string, comps []abi.ArgumentMarshaling) abi.Type {
	ty, err := abi.NewType(t, "", comps)
	if err != nil {
		panic(fmt.Sprintf("arkiv: ABI type %q: %v", t, err))
	}
	return ty
}

// NewSalt returns a random 128-bit salt. A random salt keeps the entity key
// unpredictable to anyone but the creator.
func NewSalt() (*big.Int, error) {
	return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), saltBits))
}

// EncodeCreate returns the operationData for a create operation:
// a 32-byte tuple offset (0x20) followed by
// abi.encode(salt, expiresAt=0, minLifetimeBlocks, flags, attributes).
//
// expiresAt is always 0, so the entity lives for max(0, now + minLifetimeBlocks)
// blocks (spike A0: 1,296,000 blocks gave exactly 30 days at 2 s per block).
func EncodeCreate(attrs []Attribute, salt *big.Int, minLifetimeBlocks uint64, flags uint8) ([]byte, error) {
	sorted, err := normalize(attrs)
	if err != nil {
		return nil, err
	}
	if salt == nil || salt.Sign() < 0 || salt.BitLen() > saltBits {
		return nil, errors.New("arkiv: salt must be an unsigned 128-bit integer")
	}
	conv := make([]abiAttribute, len(sorted))
	for i, a := range sorted {
		conv[i] = abiAttribute{Name: a.Name, TypeId: a.TypeID, Value: a.Value}
	}
	args := abi.Arguments{{Type: tUint128}, {Type: tUint64}, {Type: tUint64}, {Type: tUint8}, {Type: tAttrs}}
	packed, err := args.Pack(salt, uint64(0), minLifetimeBlocks, flags, conv)
	if err != nil {
		return nil, fmt.Errorf("arkiv: pack create data: %w", err)
	}
	return append(common.LeftPadBytes([]byte{abiWordSize}, abiWordSize), packed...), nil
}

// BuildExecuteCalldata wraps one create operationData as execute([(1, data)]) calldata.
func BuildExecuteCalldata(createData []byte) ([]byte, error) {
	packed, err := abi.Arguments{{Type: tOps}}.Pack([]abiOp{{Operation: opCreate, OperationData: createData}})
	if err != nil {
		return nil, fmt.Errorf("arkiv: pack execute: %w", err)
	}
	return append(append([]byte{}, executeSelector[:]...), packed...), nil
}

// PredictKey is the documented entity-key derivation:
// keccak256(chainId(32) || arkiv(20) || owner(20) || nonce(8) || salt(16)),
// where nonce is the owner's entity counter. Create does not need it (the key
// comes from the receipt log); it exists for tests and diagnostics.
func PredictKey(chainID *big.Int, owner common.Address, nonce uint64, salt *big.Int) common.Hash {
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], nonce)
	buf := make([]byte, 0, predictKeySize)
	buf = append(buf, common.LeftPadBytes(chainID.Bytes(), abiWordSize)...)
	buf = append(buf, Address.Bytes()...)
	buf = append(buf, owner.Bytes()...)
	buf = append(buf, n[:]...)
	buf = append(buf, common.LeftPadBytes(salt.Bytes(), saltBytes)...)
	return keccak(buf)
}
