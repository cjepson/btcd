// Copyright (c) 2015-2016 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package blockchain

import (
	"encoding/binary"

	"github.com/decred/dcrd/blockchain/stake"
)

// serializeSizeVarInt returns the number of bytes it would take to serialize the
// passed number as a variable-length quantity.
func serializeSizeVarInt(n int64) int {
	switch {
	case n > 0:
		switch {
		case n < 64:
			return 1
		case n < 8192:
			return 2
		case n < 1048576:
			return 3
		case n < 134217728:
			return 4
		case n < 17179869184:
			return 5
		case n < 2199023255552:
			return 6
		case n < 281474976710656:
			return 7
		case n < 36028797018963968:
			return 8
		case n < 4611686018427387904:
			return 9
		default:
			return 10
		}
	default:
		switch {
		case n > -65:
			return 1
		case n > -8193:
			return 2
		case n > -1048577:
			return 3
		case n > -134217729:
			return 4
		case n > -17179869185:
			return 5
		case n > -2199023255553:
			return 6
		case n > -281474976710657:
			return 7
		case n > -36028797018963969:
			return 8
		case n > -4611686018427387905:
			return 9
		default:
			return 10
		}
	}
}

// putVarInt serializes the provided number to a variable-length quantity. The
// result is placed directly into the passed byte slice which must be at least
// large enough to handle the number of bytes returned by the serializeSizeVarInt
// function or it will panic. The final offset is then returned.
func putVarInt(target []byte, n int64, offset int) int {
	return binary.PutVarint(target[offset:], n) + offset
}

// deserializeVarInt deserializes the provided variable-length quantity according
// to the format described above.  It also returns the final offset after reading.
func deserializeVarInt(serialized []byte, offset int) (int64, int) {
	val, bytesRead := binary.Varint(serialized[offset:])
	return val, bytesRead + offset
}

// -----------------------------------------------------------------------------
// In order to reduce the size of stored amounts, a domain specific compression
// algorithm is used which relies on there typically being a lot of zeroes at
// end of the amounts.  The compression algorithm used here was obtained from
// Bitcoin Core, so all credits for the algorithm go to it.
//
// While this is simply exchanging one uint64 for another, the resulting value
// for typical amounts has a much smaller magnitude which results in fewer bytes
// when encoded as variable length quantity.  For example, consider the amount
// of 0.1 DCR which is 10000000 atoms.  Encoding 10000000 as a VarInt would take
// 4 bytes while encoding the compressed value of 8 as a VarInt only takes 1 byte.
//
// Essentially the compression is achieved by splitting the value into an
// exponent in the range [0-9] and a digit in the range [1-9], when possible,
// and encoding them in a way that can be decoded.  More specifically, the
// encoding is as follows:
// - 0 is 0
// - Find the exponent, e, as the largest power of 10 that evenly divides the
//   value up to a maximum of 9
// - When e < 9, the final digit can't be 0 so store it as d and remove it by
//   dividing the value by 10 (call the result n).  The encoded value is thus:
//   1 + 10*(9*n + d-1) + e
// - When e==9, the only thing known is the amount is not 0.  The encoded value
//   is thus:
//   1 + 10*(n-1) + e   ==   10 + 10*(n-1)
//
// Example encodings:
// (The numbers in parenthesis are the number of bytes when serialized as a VarInt)
//            0 (1) -> 0        (1)           *  0.00000000 BTC
//         1000 (2) -> 4        (1)           *  0.00001000 BTC
//        10000 (2) -> 5        (1)           *  0.00010000 BTC
//     12345678 (4) -> 111111101(4)           *  0.12345678 BTC
//     50000000 (4) -> 47       (1)           *  0.50000000 BTC
//    100000000 (4) -> 9        (1)           *  1.00000000 BTC
//    500000000 (5) -> 49       (1)           *  5.00000000 BTC
//   1000000000 (5) -> 10       (1)           * 10.00000000 BTC
// -----------------------------------------------------------------------------

// compressTxOutAmount compresses the passed amount according to the domain
// specific compression algorithm described above.
func compressTxOutAmount(amount uint64) uint64 {
	// No need to do any work if it's zero.
	if amount == 0 {
		return 0
	}

	// Find the largest power of 10 (max of 9) that evenly divides the
	// value.
	exponent := uint64(0)
	for amount%10 == 0 && exponent < 9 {
		amount /= 10
		exponent++
	}

	// The compressed result for exponents less than 9 is:
	// 1 + 10*(9*n + d-1) + e
	if exponent < 9 {
		lastDigit := amount % 10
		amount /= 10
		return 1 + 10*(9*amount+lastDigit-1) + exponent
	}

	// The compressed result for an exponent of 9 is:
	// 1 + 10*(n-1) + e   ==   10 + 10*(n-1)
	return 10 + 10*(amount-1)
}

// decompressTxOutAmount returns the original amount the passed compressed
// amount represents according to the domain specific compression algorithm
// described above.
func decompressTxOutAmount(amount uint64) uint64 {
	// No need to do any work if it's zero.
	if amount == 0 {
		return 0
	}

	// The decompressed amount is either of the following two equations:
	// x = 1 + 10*(9*n + d - 1) + e
	// x = 1 + 10*(n - 1)       + 9
	amount--

	// The decompressed amount is now one of the following two equations:
	// x = 10*(9*n + d - 1) + e
	// x = 10*(n - 1)       + 9
	exponent := amount % 10
	amount /= 10

	// The decompressed amount is now one of the following two equations:
	// x = 9*n + d - 1  | where e < 9
	// x = n - 1        | where e = 9
	n := uint64(0)
	if exponent < 9 {
		lastDigit := amount%9 + 1
		amount /= 9
		n = amount*10 + lastDigit
	} else {
		n = amount + 1
	}

	// Apply the exponent.
	for ; exponent > 0; exponent-- {
		n *= 10
	}

	return n
}

// putTxOutAmount inserts an amount into a byte slice in the encoded,
// compressed format. It begins writing at the passed offset, then
// returns the final offset.
func putTxOutAmount(target []byte, amount int64, offset int) int {
	return putVarInt(target, int64(compressTxOutAmount(uint64(amount))), offset)
}

// deserializeTxOutAmount deserializes a transaction output amount from a passed
// byte slice, beginning at the passed offset. It returns the amount and the
// final offset.
func deserializeTxOutAmount(serialized []byte, offset int) (int64, int) {
	return deserializeVarInt(serialized, offset)
}

// -----------------------------------------------------------------------------
// Decred specific transaction encoding flags
//
// Details about a transaction needed to determine how it may be spent
// according to consensus rules are given by these flags.
//
// The following details are encoded into a single byte, where the index
// of the bit is given in zeroeth order:
//     0: Is coinbase
//     1: Has an expiry
//   2-3: Transaction type
//   4-7: Unused
//
// 0 and 1 are bit flags, while the transaction type is encoded with a bitmask
// and used to describe the underlying int.
//
// -----------------------------------------------------------------------------

const (
	// txTypeBitmask describes the bitmask that yields the 3rd and 4th bits
	// from the flags byte.
	txTypeBitmask = 0x0c

	// txTypeShift is the number of bits to shift falgs to the right to yield the
	// correct integer value after applying the bitmask with AND.
	txTypeShift = 2
)

// encodeFlags encodes transaction flags into a single byte.
func encodeFlags(isCoinBase bool, hasExpiry bool, txType stake.TxType) byte {
	b := uint8(txType)
	b <<= txTypeShift

	if isCoinBase {
		b |= 0x01
	}
	if hasExpiry {
		b |= 0x02
	}

	return b
}

// decodeFlags decodes transaction flags from a single byte into their
// respective data types.
func decodeFlags(b byte) (bool, bool, stake.TxType) {
	isCoinBase := b&0x01 != 0
	hasExpiry := b&(1<<1) != 0
	txType := stake.TxType((b & txTypeBitmask) >> txTypeShift)

	return isCoinBase, hasExpiry, txType
}
