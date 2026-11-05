package chain

import "math/big"

// A minimal RLP encoder: byte strings, unsigned integers and lists. Items
// passed to rlpList must already be encoded.

const (
	rlpShortString = 0x80
	rlpShortList   = 0xc0
	rlpMaxShort    = 55
	rlpLongString  = 0xb7
	rlpLongList    = 0xf7
)

func rlpString(b []byte) []byte {
	if len(b) == 1 && b[0] < rlpShortString {
		return []byte{b[0]}
	}
	return append(rlpHeader(len(b), rlpShortString, rlpLongString), b...)
}

func rlpUint(v *big.Int) []byte {
	if v == nil || v.Sign() == 0 {
		return []byte{rlpShortString}
	}
	return rlpString(v.Bytes())
}

func rlpUint64(v uint64) []byte { return rlpUint(new(big.Int).SetUint64(v)) }

func rlpList(items ...[]byte) []byte {
	var body []byte
	for _, it := range items {
		body = append(body, it...)
	}
	return append(rlpHeader(len(body), rlpShortList, rlpLongList), body...)
}

func rlpHeader(n int, short, long byte) []byte {
	if n <= rlpMaxShort {
		return []byte{short + byte(n)}
	}
	lenBytes := new(big.Int).SetUint64(uint64(n)).Bytes()
	return append([]byte{long + byte(len(lenBytes))}, lenBytes...)
}
