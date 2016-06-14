// Copyright (c) 2015-2016 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package blockchain

import (
	"encoding/binary"

	"github.com/decred/dcrd/blockchain/stake"
)

// -----------------------------------------------------------------------------
// A variable length quantity (VLQ) is an encoding that uses an arbitrary number
// of binary octets to represent an arbitrarily large integer.  The scheme
// employs a most significant byte (MSB) base-128 encoding where the high bit in
// each byte indicates whether or not the byte is the final one.  In addition,
// to ensure there are no redundant encodings, an offset is subtracted every
// time a group of 7 bits is shifted out.  Therefore each integer can be
// represented in exactly one way, and each representation stands for exactly
// one integer.
//
// Another nice property of this encoding is that it provides a compact
// representation of values that are typically used to indicate sizes.  For
// example, the values 0 - 127 are represented with a single byte, 128 - 16511
// with two bytes, and 16512 - 2113663 with three bytes.
//
// While the encoding allows arbitrarily large integers, it is artificially
// limited in this code to an unsigned 64-bit integer for efficiency purposes.
//
// Example encodings:
//           0 -> [0x00]
//         127 -> [0x7f]                 * Max 1-byte value
//         128 -> [0x80 0x00]
//         129 -> [0x80 0x01]
//         255 -> [0x80 0x7f]
//         256 -> [0x81 0x00]
//       16511 -> [0xff 0x7f]            * Max 2-byte value
//       16512 -> [0x80 0x80 0x00]
//       32895 -> [0x80 0xff 0x7f]
//     2113663 -> [0xff 0xff 0x7f]       * Max 3-byte value
//   270549119 -> [0xff 0xff 0xff 0x7f]  * Max 4-byte value
//      2^64-1 -> [0x80 0xfe 0xfe 0xfe 0xfe 0xfe 0xfe 0xfe 0xfe 0x7f]
//
// References:
//   https://en.wikipedia.org/wiki/Variable-length_quantity
//   http://www.codecodex.com/wiki/Variable-Length_Integers
// -----------------------------------------------------------------------------

// serializeSizeVLQ returns the number of bytes it would take to serialize the
// passed number as a variable-length quantity according to the format described
// above.
func serializeSizeVLQ(n uint64) int {
	size := 1
	for ; n > 0x7f; n = (n >> 7) - 1 {
		size++
	}

	return size
}

// putVLQ serializes the provided number to a variable-length quantity according
// to the format described above and returns the number of bytes of the encoded
// value.  The result is placed directly into the passed byte slice which must
// be at least large enough to handle the number of bytes returned by the
// serializeSizeVLQ function or it will panic.
func putVLQ(target []byte, n uint64, offset int) int {
	for ; ; offset++ {
		// The high bit is set when another byte follows.
		highBitMask := byte(0x80)
		if offset == 0 {
			highBitMask = 0x00
		}

		target[offset] = byte(n&0x7f) | highBitMask
		if n <= 0x7f {
			break
		}
		n = (n >> 7) - 1
	}

	// Reverse the bytes so it is MSB-encoded.
	for i, j := 0, offset; i < j; i, j = i+1, j-1 {
		target[i], target[j] = target[j], target[i]
	}

	return offset + 1
}

// deserializeVLQ deserializes the provided variable-length quantity according
// to the format described above.  It also returns the number of bytes
// deserialized.
func deserializeVLQ(serialized []byte) (uint64, int) {
	var n uint64
	var size int
	for _, val := range serialized {
		size++
		n = (n << 7) | uint64(val&0x7f)
		if val&0x80 != 0x80 {
			break
		}
		n++
	}

	return n, size
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
// of 0.1 DCR which is 10000000 atoms.  Encoding 10000000 as a VLQ would take
// 4 bytes while encoding the compressed value of 8 as a VLQ only takes 1 byte.
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
// (The numbers in parenthesis are the number of bytes when serialized as a VLQ)
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
	b << txTypeShift

	if isCoinBase {
		b |= 0x01
	}
	if hasExpiry {
		b |= 0x02
	}
}

// decodeFlags decodes transaction flags from a single byte into their
// respective data types.
func decodeFlags(b byte) (bool, bool, stake.TxType) {
	isCoinBase := b&0x01 != 0
	hasExpiry := b&(1<<1) != 0
	txType := stake.TxType((b & txTypeBitmask) >> txTypeShift)

	return isCoinBase, hasExpiry, txType
}

// -----------------------------------------------------------------------------
// The serialized format for most spent output journal entries is:
//
//   <script><scriptVersion>
//
//   Field     Type         Size
//   version   uint16       2 bytes
//   script    VLQ+[]byte   variable
//
// Decred stores the amount of an output in the TxIn, so for any roll back of
// a UTXO we typically need only store the script itself.
//
// The serialized format for an unspent transaction output is:
//
//   Field     Type         Size
//   version   uint16       2 bytes
//   script    VLQ+[]byte   variable
// ....
//
// -----------------------------------------------------------------------------

// decodeScriptSize treats the passed serialized bytes as a compressed script,
// possibly followed by other data, and returns the number of bytes it
// occupies taking into account the  VLQ encoded script size.
func decodeScriptSize(serialized []byte) int {
	scriptSize, bytesRead := deserializeVLQ(serialized)
	scriptSize += uint64(bytesRead)
	return int(scriptSize)
}

// serializeVersionedScriptSize gets the size of a pkScript and the VLQ describing
// its size in bytes.
func serializeVersionedScriptSize(pkScript []byte) int {
	return serializeSizeVLQ(uint64(len(pkScript))) + len(pkScript) + 2
}

// putVersionedScript inserts a versioned pkScript into a byte slice and returns
// the offset of the cursor after writing. It takes a cursor as an argument to
// know where to start writing. This function will fail if the script is written
// out of bounds, producing a panic.
func putVersionedScript(target []byte, version uint16, pkScript []byte, offset int) int {
	binary.LittleEndian.PutUint16(target[offset:offset+2], version)
	offset += 2

	copy(putScript(target[offset:], pkScript[:]))
	return offset + len(pkScript)
}

// decodeVersionedScript
func decodeVersionedScript(serialized []byte, offset int) (uint16, []byte, error) {
	// Read the script version.
	version := binary.LittleEndian.Uint16(serialized[offset : offset+2])
	offset += 2

	// Read the size of the script.
	scriptSize, endVLQBytesIdx := deserializeVLQ(serialized, offset)
	if bytesRead >= len(serialized) {
		return 0, nil, bytesRead, errDeserialize("unexpected end of " +
			"data after reading the script size")
	}
	offset += endVLQBytesIdx

	return version, serialized[offset : offset+scriptSize : scriptSize], nil
}
