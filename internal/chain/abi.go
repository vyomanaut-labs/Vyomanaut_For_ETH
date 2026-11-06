package chain

import (
	"fmt"
	"math/big"
)

// A small ABI encoder and decoder covering exactly the types the contracts
// use: uintN, address, bytes32, bool, and dynamic arrays of address or uint256.

const (
	wordLen      = 32
	selectorLen  = 4
	maxUint256Bt = 256
)

type abiArg struct {
	word    []byte // exactly wordLen bytes when !dynamic
	tail    []byte // the encoded tail (length word + elements) when dynamic
	dynamic bool
	err     error
}

func wordOfBig(v *big.Int) abiArg {
	if v == nil || v.Sign() < 0 || v.BitLen() > maxUint256Bt {
		return abiArg{err: fmt.Errorf("%w: integer out of uint256 range", ErrABI)}
	}
	w := make([]byte, wordLen)
	v.FillBytes(w)
	return abiArg{word: w}
}

func uintArg(v *big.Int) abiArg { return wordOfBig(v) }

func uint64Arg(v uint64) abiArg { return wordOfBig(new(big.Int).SetUint64(v)) }

func addrArg(a Address) abiArg {
	w := make([]byte, wordLen)
	copy(w[wordLen-AddressLen:], a[:])
	return abiArg{word: w}
}

func bytes32Arg(b [hashLen]byte) abiArg {
	w := make([]byte, wordLen)
	copy(w, b[:])
	return abiArg{word: w}
}

func addrArrayArg(as []Address) abiArg {
	tail := wordOfBig(new(big.Int).SetUint64(uint64(len(as)))).word
	for _, a := range as {
		tail = append(tail, addrArg(a).word...)
	}
	return abiArg{tail: tail, dynamic: true}
}

func uintArrayArg(vs []*big.Int) abiArg {
	tail := wordOfBig(new(big.Int).SetUint64(uint64(len(vs)))).word
	for _, v := range vs {
		w := wordOfBig(v)
		if w.err != nil {
			return w
		}
		tail = append(tail, w.word...)
	}
	return abiArg{tail: tail, dynamic: true}
}

// selector returns the first four bytes of keccak256(signature).
func selector(signature string) [selectorLen]byte {
	h := Keccak256([]byte(signature))
	var s [selectorLen]byte
	copy(s[:], h[:selectorLen])
	return s
}

// encodeArgs lays out arguments as the ABI head (static words and offsets)
// followed by the tails of the dynamic ones.
func encodeArgs(args []abiArg) ([]byte, error) {
	headLen := wordLen * len(args)
	var head, tails []byte
	for _, a := range args {
		if a.err != nil {
			return nil, a.err
		}
		if !a.dynamic {
			head = append(head, a.word...)
			continue
		}
		head = append(head, wordOfBig(new(big.Int).SetUint64(uint64(headLen+len(tails)))).word...)
		tails = append(tails, a.tail...)
	}
	return append(head, tails...), nil
}

// encodeCall returns selector || encoded arguments for a function signature
// such as "anchor(uint64,bytes32,uint32)".
func encodeCall(signature string, args ...abiArg) ([]byte, error) {
	body, err := encodeArgs(args)
	if err != nil {
		return nil, err
	}
	sel := selector(signature)
	return append(sel[:], body...), nil
}

// abiReader reads fixed-width words from return data.
type abiReader struct{ data []byte }

func (r abiReader) word(i int) ([]byte, error) {
	start := i * wordLen
	if i < 0 || start+wordLen > len(r.data) {
		return nil, fmt.Errorf("%w: return data has %d bytes, wanted word %d", ErrABI, len(r.data), i)
	}
	return r.data[start : start+wordLen], nil
}

func (r abiReader) big(i int) (*big.Int, error) {
	w, err := r.word(i)
	if err != nil {
		return nil, err
	}
	return new(big.Int).SetBytes(w), nil
}

// uintBits reads word i as an unsigned integer that must fit in bits bits.
func (r abiReader) uintBits(i int, bits uint) (uint64, error) {
	v, err := r.big(i)
	if err != nil {
		return 0, err
	}
	if v.BitLen() > int(bits) {
		return 0, fmt.Errorf("%w: value does not fit in %d bits", ErrABI, bits)
	}
	return v.Uint64(), nil
}

func (r abiReader) bytes32(i int) ([hashLen]byte, error) {
	var out [hashLen]byte
	w, err := r.word(i)
	if err != nil {
		return out, err
	}
	copy(out[:], w)
	return out, nil
}

func (r abiReader) address(i int) (Address, error) {
	var a Address
	w, err := r.word(i)
	if err != nil {
		return a, err
	}
	for _, b := range w[:wordLen-AddressLen] {
		if b != 0 {
			return a, fmt.Errorf("%w: address word has non-zero padding", ErrABI)
		}
	}
	copy(a[:], w[wordLen-AddressLen:])
	return a, nil
}

func (r abiReader) boolean(i int) (bool, error) {
	v, err := r.uintBits(i, 1)
	return v == 1, err
}
