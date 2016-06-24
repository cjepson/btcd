// Copyright (c) 2015-2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package blockchain

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// hexToBytes converts the passed hex string into bytes and will panic if there
// is an error.  This is only provided for the hard-coded constants so errors in
// the source code can be detected. It will only (and must only) be called with
// hard-coded values.
func hexToBytes(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic("invalid hex in source file: " + s)
	}
	return b
}

// TestVLQ ensures the variable length quantity serialization, deserialization,
// and size calculation works as expected.
func TestVLQ(t *testing.T) {
	t.Parallel()

	tests := []struct {
		val        uint64
		serialized []byte
	}{
		{0, hexToBytes("00")},
		{1, hexToBytes("01")},
		{127, hexToBytes("7f")},
		{128, hexToBytes("8000")},
		{129, hexToBytes("8001")},
		{255, hexToBytes("807f")},
		{256, hexToBytes("8100")},
		{16383, hexToBytes("fe7f")},
		{16384, hexToBytes("ff00")},
		{16511, hexToBytes("ff7f")}, // Max 2-byte value
		{16512, hexToBytes("808000")},
		{16513, hexToBytes("808001")},
		{16639, hexToBytes("80807f")},
		{32895, hexToBytes("80ff7f")},
		{2113663, hexToBytes("ffff7f")}, // Max 3-byte value
		{2113664, hexToBytes("80808000")},
		{270549119, hexToBytes("ffffff7f")}, // Max 4-byte value
		{270549120, hexToBytes("8080808000")},
		{2147483647, hexToBytes("86fefefe7f")},
		{2147483648, hexToBytes("86fefeff00")},
		{4294967295, hexToBytes("8efefefe7f")}, // Max uint32, 5 bytes
	}

	for _, test := range tests {
		// Ensure the function to calculate the serialized size without
		// actually serializing the value is calculated properly.
		gotSize := serializeSizeVLQ(test.val)
		if gotSize != len(test.serialized) {
			t.Errorf("serializeSizeVLQ: did not get expected size "+
				"for %d - got %d, want %d", test.val, gotSize,
				len(test.serialized))
			continue
		}

		// Ensure the value serializes to the expected bytes.
		gotBytes := make([]byte, gotSize)
		gotBytesWritten := putVLQ(gotBytes, test.val)
		if !bytes.Equal(gotBytes, test.serialized) {
			t.Errorf("putVLQUnchecked: did not get expected bytes "+
				"for %d - got %x, want %x", test.val, gotBytes,
				test.serialized)
			continue
		}
		if gotBytesWritten != len(test.serialized) {
			t.Errorf("putVLQUnchecked: did not get expected number "+
				"of bytes written for %d - got %d, want %d",
				test.val, gotBytesWritten, len(test.serialized))
			continue
		}

		// Ensure the serialized bytes deserialize to the expected
		// value.
		gotVal, gotBytesRead := deserializeVLQ(test.serialized)
		if gotVal != test.val {
			t.Errorf("deserializeVLQ: did not get expected value "+
				"for %x - got %d, want %d", test.serialized,
				gotVal, test.val)
			continue
		}
		if gotBytesRead != len(test.serialized) {
			t.Errorf("deserializeVLQ: did not get expected number "+
				"of bytes read for %d - got %d, want %d",
				test.serialized, gotBytesRead,
				len(test.serialized))
			continue
		}
	}
}

// TestAmountCompression ensures the domain-specific transaction output amount
// compression and decompression works as expected.
func TestAmountCompression(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		uncompressed uint64
		compressed   uint64
	}{
		{
			name:         "0 DCR (sometimes used in nulldata)",
			uncompressed: 0,
			compressed:   0,
		},
		{
			name:         "546 atoms (current network dust value)",
			uncompressed: 546,
			compressed:   4911,
		},
		{
			name:         "0.00001 DCR (typical transaction fee)",
			uncompressed: 1000,
			compressed:   4,
		},
		{
			name:         "0.0001 DCR (typical transaction fee)",
			uncompressed: 10000,
			compressed:   5,
		},
		{
			name:         "0.12345678 DCR",
			uncompressed: 12345678,
			compressed:   111111101,
		},
		{
			name:         "0.5 DCR",
			uncompressed: 50000000,
			compressed:   48,
		},
		{
			name:         "1 DCR",
			uncompressed: 100000000,
			compressed:   9,
		},
		{
			name:         "5 DCR",
			uncompressed: 500000000,
			compressed:   49,
		},
		{
			name:         "21000000 DCR (max minted coins)",
			uncompressed: 2100000000000000,
			compressed:   21000000,
		},
	}

	for _, test := range tests {
		// Ensure the amount compresses to the expected value.
		gotCompressed := compressTxOutAmount(test.uncompressed)
		if gotCompressed != test.compressed {
			t.Errorf("compressTxOutAmount (%s): did not get "+
				"expected value - got %d, want %d", test.name,
				gotCompressed, test.compressed)
			continue
		}

		// Ensure the value decompresses to the expected value.
		gotDecompressed := decompressTxOutAmount(test.compressed)
		if gotDecompressed != test.uncompressed {
			t.Errorf("decompressTxOutAmount (%s): did not get "+
				"expected value - got %d, want %d", test.name,
				gotDecompressed, test.uncompressed)
			continue
		}
	}
}

/*
// TestCompressedTxOut ensures the transaction output serialization and
// deserialization works as expected.
func TestCompressedTxOut(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		amount       uint64
		compAmount   uint64
		pkScript     []byte
		compPkScript []byte
		version      int32
		compressed   []byte
	}{
		{
			name:         "nulldata with 0 DCR",
			amount:       0,
			compAmount:   0,
			pkScript:     hexToBytes("6a200102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"),
			compPkScript: hexToBytes("286a200102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"),
			version:      1,
			compressed:   hexToBytes("00286a200102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"),
		},
		{
			name:         "pay-to-pubkey-hash dust",
			amount:       546,
			compAmount:   4911,
			pkScript:     hexToBytes("76a9141018853670f9f3b0582c5b9ee8ce93764ac32b9388ac"),
			compPkScript: hexToBytes("001018853670f9f3b0582c5b9ee8ce93764ac32b93"),
			version:      1,
			compressed:   hexToBytes("a52f001018853670f9f3b0582c5b9ee8ce93764ac32b93"),
		},
		{
			name:         "pay-to-pubkey uncompressed 1 DCR",
			amount:       100000000,
			compAmount:   9,
			pkScript:     hexToBytes("4104192d74d0cb94344c9569c2e77901573d8d7903c3ebec3a957724895dca52c6b40d45264838c0bd96852662ce6a847b197376830160c6d2eb5e6a4c44d33f453eac"),
			compPkScript: hexToBytes("04192d74d0cb94344c9569c2e77901573d8d7903c3ebec3a957724895dca52c6b4"),
			version:      1,
			compressed:   hexToBytes("0904192d74d0cb94344c9569c2e77901573d8d7903c3ebec3a957724895dca52c6b4"),
		},
	}

	for _, test := range tests {
		// Ensure the function to calculate the serialized size without
		// actually serializing the txout is calculated properly.
		gotSize := compressedTxOutSize(test.amount, test.pkScript,
			test.version, false)
		if gotSize != len(test.compressed) {
			t.Errorf("compressedTxOutSize (%s): did not get "+
				"expected size - got %d, want %d", test.name,
				gotSize, len(test.compressed))
			continue
		}

		// Ensure the txout compresses to the expected value.
		gotCompressed := make([]byte, gotSize)
		gotBytesWritten := putCompressedTxOut(gotCompressed,
			test.amount, test.pkScript, test.version, false)
		if !bytes.Equal(gotCompressed, test.compressed) {
			t.Errorf("compressTxOut (%s): did not get expected "+
				"bytes - got %x, want %x", test.name,
				gotCompressed, test.compressed)
			continue
		}
		if gotBytesWritten != len(test.compressed) {
			t.Errorf("compressTxOut (%s): did not get expected "+
				"number of bytes written - got %d, want %d",
				test.name, gotBytesWritten,
				len(test.compressed))
			continue
		}

		// Ensure the serialized bytes are decoded back to the expected
		// compressed values.
		gotAmount, gotScript, gotBytesRead, err := decodeCompressedTxOut(
			test.compressed, test.version)
		if err != nil {
			t.Errorf("decodeCompressedTxOut (%s): unexpected "+
				"error: %v", test.name, err)
			continue
		}
		if gotAmount != test.compAmount {
			t.Errorf("decodeCompressedTxOut (%s): did not get "+
				"expected amount - got %d, want %d",
				test.name, gotAmount, test.compAmount)
			continue
		}
		if !bytes.Equal(gotScript, test.compPkScript) {
			t.Errorf("decodeCompressedTxOut (%s): did not get "+
				"expected script - got %x, want %x",
				test.name, gotScript, test.compPkScript)
			continue
		}
		if gotBytesRead != len(test.compressed) {
			t.Errorf("decodeCompressedTxOut (%s): did not get "+
				"expected number of bytes read - got %d, want %d",
				test.name, gotBytesRead, len(test.compressed))
			continue
		}

		// Ensure the compressed values decompress to the expected
		// txout.
		gotAmount = decompressTxOutAmount(gotAmount)
		if gotAmount != test.amount {
			t.Errorf("decompressTxOut (%s): did not get expected "+
				"value - got %d, want %d", test.name, gotAmount,
				test.amount)
			continue
		}
		gotScript = decompressScript(gotScript, test.version)
		if !bytes.Equal(gotScript, test.pkScript) {
			t.Errorf("decompressTxOut (%s): did not get expected "+
				"script - got %x, want %x", test.name,
				gotScript, test.pkScript)
			continue
		}
	}
}

// TestTxOutCompressionErrors ensures calling various functions related to
// txout compression with incorrect data returns the expected results.
func TestTxOutCompressionErrors(t *testing.T) {
	t.Parallel()

	// A compressed txout with missing compressed script must error.
	compressedTxOut := hexToBytes("00")
	_, _, _, err := decodeCompressedTxOut(compressedTxOut, 1)
	if !isDeserializeErr(err) {
		t.Fatalf("decodeCompressedTxOut with missing compressed script "+
			"did not return expected error type - got %T, want "+
			"errDeserialize", err)
	}

	// A compressed txout with short compressed script must error.
	compressedTxOut = hexToBytes("0010")
	_, _, _, err = decodeCompressedTxOut(compressedTxOut, 1)
	if !isDeserializeErr(err) {
		t.Fatalf("decodeCompressedTxOut with short compressed script "+
			"did not return expected error type - got %T, want "+
			"errDeserialize", err)
	}
}
*/
