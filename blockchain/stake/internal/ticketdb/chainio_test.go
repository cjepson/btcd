// chainio_test.go
package ticketdb

import (
	"bytes"
	"encoding/hex"
	"reflect"
	"testing"

	"github.com/decred/dcrd/chaincfg/chainhash"
)

// hexToBytes converts a hex string to bytes, without returning any errors.
func hexToBytes(s string) []byte {
	b, _ := hex.DecodeString(s)

	return b
}

// TestBlockUndoDataSerializing ensures serializing and deserializing the
// block undo data works as expected.
func TestBlockUndoDataSerializing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		utds       []*UndoTicketData
		serialized []byte
	}{
		{
			name: "two ticket datas",
			utds: []*UndoTicketData{
				&UndoTicketData{
					TicketHash:   chainhash.HashFuncH([]byte{0x00}),
					TicketHeight: 123456,
					Missed:       true,
					Revoked:      false,
					Expired:      true,
				},
				&UndoTicketData{
					TicketHash:   chainhash.HashFuncH([]byte{0x01}),
					TicketHeight: 122222,
					Missed:       false,
					Revoked:      true,
					Expired:      false,
				},
			},
			serialized: hexToBytes("0ce8d4ef4dd7cd8d62dfded9d4edb0a774ae6a41929a74da23109e8f11139c8740e20100054a6c419a1e25c85327115c4ace586decddfe2990ed8f3d4d801871158338501d6edd010002"),
		},
	}

	for i, test := range tests {
		// Ensure the state serializes to the expected value.
		gotBytes := serializeBlockUndoData(test.utds)
		if !bytes.Equal(gotBytes, test.serialized) {
			t.Errorf("serializeBlockUndoData #%d (%s): mismatched "+
				"bytes - got %x, want %x", i, test.name,
				gotBytes, test.serialized)
			continue
		}

		// Ensure the serialized bytes are decoded back to the expected
		// state.
		utds, err := deserializeBlockUndoData(test.serialized)
		if err != nil {
			t.Errorf("deserializeBlockUndoData #%d (%s) "+
				"unexpected error: %v", i, test.name, err)
			continue
		}
		if !reflect.DeepEqual(utds, test.utds) {
			t.Errorf("deserializeBlockUndoData #%d (%s) "+
				"mismatched state - got %v, want %v", i,
				test.name, utds, test.utds)
			continue

		}
	}
}

// TestBlockUndoDataDeserializing performs negative tests against decoding block
// undo data to ensure error paths work as expected.
func TestBlockUndoDataDeserializingErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		serialized []byte
		errCode    ErrorCode
	}{
		{
			name:       "short read",
			serialized: hexToBytes("00"),
			errCode:    ErrUndoDataShortRead,
		},
		{
			name:       "bad size",
			serialized: hexToBytes("00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"),
			errCode:    ErrUndoDataCorrupt,
		},
	}

	for _, test := range tests {
		// Ensure the expected error type is returned.
		_, err := deserializeBlockUndoData(test.serialized)
		ticketDBErr, ok := err.(TicketDBError)
		if !ok {
			t.Errorf("couldn't convert deserializeBlockUndoData error "+
				"to ticket db error (err: %v)", err)
			continue
		}
		if ticketDBErr.GetCode() != test.errCode {
			t.Errorf("deserializeBlockUndoData (%s): expected error type "+
				"does not match - got %v, want %v", test.name,
				ticketDBErr.ErrorCode, test.errCode)
			continue
		}
	}
}

// TestTicketHashesSerializing ensures serializing and deserializing the
// ticket hashes works as expected.
func TestTicketHashesSerializing(t *testing.T) {
	t.Parallel()
	hash1 := chainhash.HashFuncH([]byte{0x00})
	hash2 := chainhash.HashFuncH([]byte{0x01})

	tests := []struct {
		name       string
		ths        TicketHashes
		serialized []byte
	}{
		{
			name: "two ticket hashes",
			ths: TicketHashes{
				&hash1,
				&hash2,
			},
			serialized: hexToBytes("0ce8d4ef4dd7cd8d62dfded9d4edb0a774ae6a41929a74da23109e8f11139c874a6c419a1e25c85327115c4ace586decddfe2990ed8f3d4d801871158338501d"),
		},
	}

	for i, test := range tests {
		// Ensure the state serializes to the expected value.
		gotBytes := serializeTicketHashes(test.ths)
		if !bytes.Equal(gotBytes, test.serialized) {
			t.Errorf("serializeBlockUndoData #%d (%s): mismatched "+
				"bytes - got %x, want %x", i, test.name,
				gotBytes, test.serialized)
			continue
		}

		// Ensure the serialized bytes are decoded back to the expected
		// state.
		ths, err := deserializeTicketHashes(test.serialized)
		if err != nil {
			t.Errorf("deserializeBlockUndoData #%d (%s) "+
				"unexpected error: %v", i, test.name, err)
			continue
		}
		if !reflect.DeepEqual(ths, test.ths) {
			t.Errorf("deserializeBlockUndoData #%d (%s) "+
				"mismatched state - got %v, want %v", i,
				test.name, ths, test.ths)
			continue

		}
	}
}

// TestTicketHashesDeserializingErrors performs negative tests against decoding block
// undo data to ensure error paths work as expected.
func TestTicketHashesDeserializingErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		serialized []byte
		errCode    ErrorCode
	}{
		{
			name:       "short read",
			serialized: hexToBytes("00"),
			errCode:    ErrTicketHashesShortRead,
		},
		{
			name:       "bad size",
			serialized: hexToBytes("00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"),
			errCode:    ErrTicketHashesCorrupt,
		},
	}

	for _, test := range tests {
		// Ensure the expected error type is returned.
		_, err := deserializeTicketHashes(test.serialized)
		ticketDBErr, ok := err.(TicketDBError)
		if !ok {
			t.Errorf("couldn't convert deserializeTicketHashes error "+
				"to ticket db error (err: %v)", err)
			continue
		}
		if ticketDBErr.GetCode() != test.errCode {
			t.Errorf("deserializeTicketHashes (%s): expected error type "+
				"does not match - got %v, want %v", test.name,
				ticketDBErr.ErrorCode, test.errCode)
			continue
		}
	}
}
