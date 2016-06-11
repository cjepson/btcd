// Copyright (c) 2015-2016 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package blockchain

import (
	"encoding/binary"
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

// decodeCompressedScriptSize treats the passed serialized bytes as a compressed
// script, possibly followed by other data, and returns the number of bytes it
// occupies taking into account the special encoding of the script size by the
// domain specific compression algorithm described above.
func decodeCompressedScriptSize(serialized []byte) int {
	scriptSize, bytesRead := deserializeVLQ(serialized)
	if bytesRead == 0 {
		return 0
	}

	scriptSize += uint64(bytesRead)
	return int(scriptSize)
}

// putScript inserts the script into the target byte slice by first encoding
// the size of the empty byte slice, and then the script itself.
func putScript(scriptVersion uint16, target, pkScript []byte) int {
	encodedVersion := uint64(len(pkScript))
	versionSizeLen := putVLQ(target, encodedSize)
	encodedSize := uint64(len(pkScript))
	vlqSizeLen := putVLQ(target, versionSizeLen+encodedSize)
	copy(target[vlqSizeLen:], pkScript)
	return vlqSizeLen + len(pkScript)
}

// -----------------------------------------------------------------------------
// The serialized format for an output is:
//
//   <scriptVersion><script>
//
//   Field     Type         Size
//   version   uint16       2 bytes
//   script    VLQ+[]byte   variable
//
// -----------------------------------------------------------------------------

// serializeScriptSize 
func serializeScriptSize(pkScript []byte) int {
	return 2+ serializeSizeVLQ(uint64(len(pkScript))) + len(pkScript)
}

// putVersionedScript
func putVersionedScript(target []byte, version uint16, pkScript) int {
	if preCompressed {
		offset := putVLQ(target, amount)
		copy(target[offset:], pkScript)
		return offset + len(pkScript)
	}

	offset := putVLQ(target, compressTxOutAmount(amount))
	offset += putCompressedScript(target[offset:], pkScript)
	return offset
}

// decodeVersionedScript
func decodeVersionedScript(serialized []byte, offset int) (uint16, []byte, error) {
	// Read the script version.
	version := binary.LittleEndian.Uint16(serialized[offset, offset+2])
	
	// Read the size of the script.
	scriptSize, endVLQBytesIdx := deserializeVLQ(serialized, offset)
	if bytesRead >= len(serialized) {
		return 0, nil, bytesRead, errDeserialize("unexpected end of " +
			"data after reading the script size")
	}
	offset += endVLQBytesIdx
	
	return version, serialized[offset:offset+scriptSize:scriptSize], nil
}
