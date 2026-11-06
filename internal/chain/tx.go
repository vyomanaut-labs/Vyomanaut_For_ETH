package chain

import (
	"errors"
	"math/big"
)

const eip1559TxType = 0x02

// Tx is an EIP-1559 (type 2) transaction with an empty access list. A nil To
// creates a contract.
type Tx struct {
	ChainID   uint64
	Nonce     uint64
	GasTipCap *big.Int
	GasFeeCap *big.Int
	Gas       uint64
	To        *Address
	Value     *big.Int
	Data      []byte
}

var errTxFields = errors.New("chain: transaction is missing a fee field")

func (t *Tx) payload() ([][]byte, error) {
	if t.GasTipCap == nil || t.GasFeeCap == nil {
		return nil, errTxFields
	}
	to := []byte{}
	if t.To != nil {
		to = t.To[:]
	}
	return [][]byte{
		rlpUint64(t.ChainID),
		rlpUint64(t.Nonce),
		rlpUint(t.GasTipCap),
		rlpUint(t.GasFeeCap),
		rlpUint64(t.Gas),
		rlpString(to),
		rlpUint(t.Value),
		rlpString(t.Data),
		rlpList(), // empty access list
	}, nil
}

// SigningHash is the digest the sender signs.
func (t *Tx) SigningHash() ([hashLen]byte, error) {
	fields, err := t.payload()
	if err != nil {
		return [hashLen]byte{}, err
	}
	return Keccak256([]byte{eip1559TxType}, rlpList(fields...)), nil
}

// Sign returns the raw signed transaction and its hash.
func (t *Tx) Sign(k *Key) (raw []byte, hash [hashLen]byte, err error) {
	digest, err := t.SigningHash()
	if err != nil {
		return nil, hash, err
	}
	sig := k.Sign(digest)
	fields, err := t.payload()
	if err != nil {
		return nil, hash, err
	}
	fields = append(fields,
		rlpUint64(uint64(sig.V)),
		rlpUint(new(big.Int).SetBytes(sig.R[:])),
		rlpUint(new(big.Int).SetBytes(sig.S[:])),
	)
	raw = append([]byte{eip1559TxType}, rlpList(fields...)...)
	return raw, Keccak256(raw), nil
}
