// Copyright (c) 2015-2016 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package blockchain

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/big"
	"sort"

	"github.com/decred/bitset"
	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg/chainhash"
	database "github.com/decred/dcrd/database2"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrutil"
)

const (
	maxUint32 = 1<<32 - 1
)

var (
	// hashIndexBucketName is the name of the db bucket used to house to the
	// block hash -> block height index.
	hashIndexBucketName = []byte("hashidx")

	// heightIndexBucketName is the name of the db bucket used to house to
	// the block height -> block hash index.
	heightIndexBucketName = []byte("heightidx")

	// chainStateKeyName is the name of the db key used to store the best
	// chain state.
	chainStateKeyName = []byte("chainstate")

	// spendJournalBucketName is the name of the db bucket used to house
	// transactions outputs that are spent in each block.
	spendJournalBucketName = []byte("spendjournal")

	// utxoSetBucketName is the name of the db bucket used to house the
	// unspent transaction output set.
	utxoSetBucketName = []byte("utxoset")

	// byteOrder is the preferred byte order used for serializing numeric
	// fields for storage in the database.
	byteOrder = binary.LittleEndian
)

// errNotInMainChain signifies that a block hash or height that is not in the
// main chain was requested.
type errNotInMainChain string

// Error implements the error interface.
func (e errNotInMainChain) Error() string {
	return string(e)
}

// isNotInMainChainErr returns whether or not the passed error is an
// errNotInMainChain error.
func isNotInMainChainErr(err error) bool {
	_, ok := err.(errNotInMainChain)
	return ok
}

// errDeserialize signifies that a problem was encountered when deserializing
// data.
type errDeserialize string

// Error implements the error interface.
func (e errDeserialize) Error() string {
	return string(e)
}

// isDeserializeErr returns whether or not the passed error is an errDeserialize
// error.
func isDeserializeErr(err error) bool {
	_, ok := err.(errDeserialize)
	return ok
}

// -----------------------------------------------------------------------------
// The transaction spend journal consists of an entry for each block connected
// to the main chain which contains the transaction outputs the block spends
// serialized such that the order is the reverse of the order they were spent.
//
// This is required because reorganizing the chain necessarily entails
// disconnecting blocks to get back to the point of the fork which implies
// unspending all of the transaction outputs that each block previously spent.
// Since the utxo set, by definition, only contains unspent transaction outputs,
// the spent transaction outputs must be resurrected from somewhere.  There is
// more than one way this could be done, however this is the most straight
// forward method that does not require having a transaction index and unpruned
// blockchain.
//
// NOTE: This format is NOT self describing.  The additional details such as
// the number of entries (transaction inputs) are expected to come from the
// block itself and the utxo set.  The rationale in doing this is to save a
// significant amount of space.  This is also the reason the spent outputs are
// serialized in the reverse order they are spent because later transactions
// are allowed to spend outputs from earlier ones in the same block.
//
// The serialized format is:
//
//   [<scriptVersion><pkScript>],...
//   OPTIONAL: [<txVersion><height><index><flags>]
//
//   Field                Type           Size
//   scriptVersion        uint16         2 bytes
//   pkScript             VarInt+[]byte  variable
//
//   OPTIONAL
//     outputsLen         VarInt         variable (usually 1 byte)
//     txVersion          VarInt         variable
//     height             VarInt         variable
//     index              VarInt         variable
//     flags              byte           byte
//
// The serialized flags code format is:
//   bit  0   - containing transaction is a coinbase
//   bit  1   - containing transaction has an expiry
//   bits 2-3 - transaction type
//   bits 4-7 - unused
//
//   NOTE: The optional section of data is only inserted if the transaction
//         has been fully spent. Otherwise, these fields are left blank and
//         their presence is checked for by checking the len of the serialized
//         byte slice against the final cursor after popping off the first
//         three elements.
//
// Example:
//   TODO
// -----------------------------------------------------------------------------

// spentTxOut contains a spent transaction output and potentially additional
// contextual information such as whether or not it was contained in a coinbase
// transaction, the txVersion of the transaction it was contained in, and which
// block height the containing transaction was included in.  As described in
// the comments above, the additional contextual information will only be valid
// when this spent txout is spending the last unspent output of the containing
// transaction.
type spentTxOut struct {
	pkScript      []byte       // The public key script for the output.
	amount        int64        // The amount of the output.
	outputsLen    uint32       // The number of outputs in this tx.
	txVersion     int32        // The txVersion of creating tx.
	height        uint32       // Height of the the block containing the tx.
	index         uint32       // Index in the block of the transaction.
	txType        stake.TxType // The stake type of the transaction.
	scriptVersion uint16       // The version of the scripting language.

	txFullySpent bool // Whether or not the transaction is fully spent.
	isCoinBase   bool // Whether creating tx is a coinbase.
	hasExpiry    bool // The expiry of the creating tx.
}

// serializeVersionedScriptSize gets the size of a pkScript and the VarInt
// describing its size in bytes.
func serializeVersionedScriptSize(pkScript []byte) int {
	return 2 + serializeSizeVarInt(int64(len(pkScript))) + len(pkScript)
}

// putVersionedScript inserts a versioned pkScript into a byte slice and returns
// the offset of the cursor after writing. It takes a cursor as an argument to
// know where to start writing. This function will fail if the script is written
// out of bounds, producing a panic.
func putVersionedScript(target []byte, version uint16, pkScript []byte,
	offset int) int {
	binary.LittleEndian.PutUint16(target[offset:offset+2], version)
	offset += 2

	copy(target[offset:], pkScript[:])
	return offset + len(pkScript)
}

// deserializeVersionedScript deserialized a versioned script and returns the
// version followed by the script. The final offset after reading is also
// returned.
func deserializeVersionedScript(serialized []byte, offset int) (uint16, []byte,
	int, error) {
	serializedLen := len(serialized)

	if serializedLen < 2 {
		return 0, nil, 0, errDeserialize("unexpected end of " +
			"data before reading the script version")
	}

	// Read the script version.
	version := binary.LittleEndian.Uint16(serialized[offset : offset+2])
	offset += 2

	// Read the size of the script.
	scriptSize, endVarIntBytesIdx := deserializeVarInt(serialized, offset)
	if offset >= serializedLen {
		return 0, nil, offset, errDeserialize("unexpected end of " +
			"data after reading the script size")
	}
	offset += endVarIntBytesIdx

	return version, serialized[offset : offset+int(scriptSize) : int(scriptSize)],
		offset + int(scriptSize), nil
}

// spentTxOutSerializeSize returns the number of bytes it would take to
// serialize the passed stxo according to the format described above.
func spentTxOutSerializeSize(stxo *spentTxOut) int {
	size := serializeVersionedScriptSize(stxo.pkScript)

	if stxo.txFullySpent {
		size += serializeSizeVarInt(int64(stxo.outputsLen))
		size += serializeSizeVarInt(int64(stxo.txVersion))
		size += serializeSizeVarInt(int64(stxo.height))
		size += serializeSizeVarInt(int64(stxo.index))
		size += 1 // Flags
	}

	return size
}

// putSpentTxOut serializes the passed stxo according to the format described
// above directly into the passed target byte slice.  The target byte slice must
// be at least large enough to handle the number of bytes returned by the
// spentTxOutSerializeSize function or it will panic.
func putSpentTxOut(target []byte, stxo *spentTxOut) int {
	offset := putVersionedScript(target, stxo.scriptVersion,
		stxo.pkScript, 0)

	if stxo.txFullySpent {
		offset = putVarInt(target, int64(stxo.outputsLen), offset)
		offset = putVarInt(target, int64(stxo.txVersion), offset)
		offset = putVarInt(target, int64(stxo.height), offset)
		offset = putVarInt(target, int64(stxo.index), offset)
	}

	return offset
}

// decodeSpentTxOut decodes the passed serialized stxo entry, possibly followed
// by other data, into the passed stxo struct.  It returns the number of bytes
// read.
//
// Since the serialized stxo entry does not contain the height, txVersion, or
// coinbase flag of the containing transaction when it still has utxos, the
// caller is responsible for passing in the containing transaction txVersion in
// that case.  The provided txVersion is ignore when it is serialized as a part of
// the stxo.
//
// An error will be returned if the txVersion is not serialized as a part of the
// stxo and is also not provided to the function.
func decodeSpentTxOut(serialized []byte, stxo *spentTxOut, txVersion int32,
	amount int64) (int, error) {
	serializedLen := len(serialized)

	// Ensure there are bytes to decode.
	if serializedLen == 0 {
		return 0, errDeserialize("no serialized bytes")
	}

	scriptVersion, pkScript, offset, err :=
		deserializeVersionedScript(serialized, 0)
	if err != nil {
		return 0, err
	}

	stxo.amount = amount
	stxo.scriptVersion = scriptVersion
	stxo.pkScript = pkScript

	// This script was not fully spent at this spending, so we're
	// done.
	if serializedLen <= offset {
		return offset, nil
	}

	// Decode the rest of the transaction details, since the transaction
	// was fully spent here.
	stxo.txFullySpent = true

	var outputsLen64, txVersion64, height64, index64 int64
	outputsLen64, offset = deserializeVarInt(serialized, offset)
	txVersion64, offset = deserializeVarInt(serialized, offset)
	height64, offset = deserializeVarInt(serialized, offset)
	index64, offset = deserializeVarInt(serialized, offset)

	stxo.txVersion = int32(txVersion64)
	stxo.height = uint32(height64)
	stxo.index = uint32(index64)
	stxo.outputsLen = uint32(outputsLen64)

	stxo.isCoinBase, stxo.hasExpiry, stxo.txType = decodeFlags(serialized[offset])

	return offset, nil
}

// deserializeSpendJournalEntry decodes the passed serialized byte slice into a
// slice of spent txouts according to the format described in detail above.
//
// Since the serialization format is not self describing, as noted in the
// format comments, this function also requires the transactions that spend the
// txouts and a utxo view that contains any remaining existing utxos in the
// transactions referenced by the inputs to the passed transasctions.
func deserializeSpendJournalEntry(serialized []byte, txns []*wire.MsgTx, view *UtxoViewpoint) ([]spentTxOut, error) {
	// Calculate the total number of stxos.
	var numStxos int
	for _, tx := range txns {
		numStxos += len(tx.TxIn)
	}

	// When a block has no spent txouts there is nothing to serialize.
	if len(serialized) == 0 {
		// Ensure the block actually has no stxos.  This should never
		// happen unless there is database corruption or an empty entry
		// erroneously made its way into the database.
		if numStxos != 0 {
			return nil, AssertError(fmt.Sprintf("mismatched spend "+
				"journal serialization - no serialization for "+
				"expected %d stxos", numStxos))
		}

		return nil, nil
	}

	// Loop backwards through all transactions so everything is read in
	// reverse order to match the serialization order.
	stxoIdx := numStxos - 1
	stxoInFlight := make(map[chainhash.Hash]int)
	offset := 0
	stxos := make([]spentTxOut, numStxos)
	for txIdx := len(txns) - 1; txIdx > -1; txIdx-- {
		tx := txns[txIdx]

		// Loop backwards through all of the transaction inputs and read
		// the associated stxo.
		for txInIdx := len(tx.TxIn) - 1; txInIdx > -1; txInIdx-- {
			txIn := tx.TxIn[txInIdx]
			stxo := &stxos[stxoIdx]
			stxoIdx--

			// Get the transaction txVersion for the stxo based on
			// whether or not it should be serialized as a part of
			// the stxo.  Recall that it is only serialized when the
			// stxo spends the final utxo of a transaction.  Since
			// they are deserialized in reverse order, this means
			// the first time an entry for a given containing tx is
			// encountered that is not already in the utxo view it
			// must have been the final spend and thus the extra
			// data will be serialized with the stxo.  Otherwise,
			// the txVersion must be pulled from the utxo entry.
			//
			// Since the view is not actually modified as the stxos
			// are read here and it's possible later entries
			// reference earlier ones, an inflight map is maintained
			// to detect this case and pull the tx txVersion from the
			// entry that contains the txVersion information as just
			// described.
			var txVersion int32
			originHash := &txIn.PreviousOutPoint.Hash
			entry := view.LookupEntry(originHash)
			if entry != nil {
				txVersion = entry.TxVersion()
			} else if idx, ok := stxoInFlight[*originHash]; ok {
				txVersion = stxos[idx].txVersion
			} else {
				stxoInFlight[*originHash] = stxoIdx + 1
			}

			var err error
			offset, err = decodeSpentTxOut(serialized[offset:], stxo,
				txVersion, txIn.ValueIn)
			if err != nil {
				return nil, errDeserialize(fmt.Sprintf("unable "+
					"to decode stxo for %v: %v",
					txIn.PreviousOutPoint, err))
			}
		}
	}

	return stxos, nil
}

// serializeSpendJournalEntry serializes all of the passed spent txouts into a
// single byte slice according to the format described in detail above.
func serializeSpendJournalEntry(stxos []spentTxOut) []byte {
	if len(stxos) == 0 {
		return nil
	}

	// Calculate the size needed to serialize the entire journal entry.
	var size int
	for i := range stxos {
		size += spentTxOutSerializeSize(&stxos[i])
	}
	serialized := make([]byte, size)

	// Serialize each individual stxo directly into the slice in reverse
	// order one after the other.
	var offset int
	for i := len(stxos) - 1; i > -1; i-- {
		offset += putSpentTxOut(serialized[offset:], &stxos[i])
	}

	return serialized
}

// dbFetchSpendJournalEntry fetches the spend journal entry for the passed
// block and deserializes it into a slice of spent txout entries.  The provided
// view MUST have the utxos referenced by all of the transactions available for
// the passed block since that information is required to reconstruct the spent
// txouts.
func dbFetchSpendJournalEntry(dbTx database.Tx, block *dcrutil.Block, view *UtxoViewpoint) ([]spentTxOut, error) {
	// Exclude the coinbase transaction since it can't spend anything.
	spendBucket := dbTx.Metadata().Bucket(spendJournalBucketName)
	serialized := spendBucket.Get(block.Sha()[:])
	blockTxns := block.MsgBlock().Transactions[1:]
	stxos, err := deserializeSpendJournalEntry(serialized, blockTxns, view)
	if err != nil {
		// Ensure any deserialization errors are returned as database
		// corruption errors.
		if isDeserializeErr(err) {
			return nil, database.Error{
				ErrorCode: database.ErrCorruption,
				Description: fmt.Sprintf("corrupt spend "+
					"information for %v: %v", block.Sha(),
					err),
			}
		}

		return nil, err
	}

	return stxos, nil
}

// dbPutSpendJournalEntry uses an existing database transaction to update the
// spend journal entry for the given block hash using the provided slice of
// spent txouts.   The spent txouts slice must contain an entry for every txout
// the transactions in the block spend in the order they are spent.
func dbPutSpendJournalEntry(dbTx database.Tx, blockHash *chainhash.Hash, stxos []spentTxOut) error {
	spendBucket := dbTx.Metadata().Bucket(spendJournalBucketName)
	serialized := serializeSpendJournalEntry(stxos)
	return spendBucket.Put(blockHash[:], serialized)
}

// dbRemoveSpendJournalEntry uses an existing database transaction to remove the
// spend journal entry for the passed block hash.
func dbRemoveSpendJournalEntry(dbTx database.Tx, blockHash *chainhash.Hash) error {
	spendBucket := dbTx.Metadata().Bucket(spendJournalBucketName)
	return spendBucket.Delete(blockHash[:])
}

// -----------------------------------------------------------------------------
// The unspent transaction output (utxo) set consists of an entry for each
// transaction which contains a utxo serialized using a format that is highly
// optimized to reduce space using domain specific compression algorithms.  This
// format is a slightly modified txVersion of the format used in Bitcoin Core.
//
// The serialized format is:
//
//   <txVersion><height><index><height><numOutputs><unspentness bitmap><flags>
//    [<compressed txouts>,...]
//
//   Field                Type     Size
//   txVersion            VarInt   variable
//   height               VarInt   variable
//   index                VarInt   variable
//   number of outputs    VarInt   variable
//   unspentness bitmap   []byte   variable (typically 1 byte)
//   flags                byte     1 byte
//   compressed txouts
//     compressed amount  VarInt   variable
//     compressed script  []byte   variable
//
// The serialized flags code format is:
//   bit  0   - containing transaction is a coinbase
//   bit  1   - containing transaction has an expiry
//   bits 2-3 - transaction type
//   bits 4-7 - unused
//
// -----------------------------------------------------------------------------

// SerializeSize give the size of a serialized UTXO.
func (u *utxoOutput) SerializeSize() int {
	return serializeSizeVarInt(int64(compressTxOutAmount(uint64(u.amount)))) +
		serializeVersionedScriptSize(u.pkScript)
}

// SerializeTo serializes and writes a utxoOutput to a given target slice at
// the given offset. The function will panic if it attempts to write outside
// the slice bounds.
// utxoOutputs are serialized to the following:
//
//  <amount><scriptVersion><pkScriptLen><pkScript>
//
//   Field                Type     Size
//   amount               VarInt   variable
//   scriptVersion        uint16   variable
//   pkScriptLen          VarInt   variable
//   pkScript             []byte   variable
//
func (u *utxoOutput) PutSerialized(target []byte, offset int) int {
	// Write the amount.
	offset += putTxOutAmount(target, u.amount, offset)

	// Write the script.
	return putVersionedScript(target, u.scriptVersion, u.pkScript, offset)
}

// FromSerialized deserializes a UTXO from the passed byte slice, starting
// at index offset. It returns the final offset after reading.
func (u *utxoOutput) FromSerialized(serialized []byte, offset int) (int, error) {
	// Write the amount.
	var amount int64
	amount, offset = deserializeTxOutAmount(serialized, offset)

	// Write the script.
	var scriptVersion uint16
	var pkScript []byte
	var err error
	scriptVersion, pkScript, offset, err = deserializeVersionedScript(serialized,
		offset)
	if err != nil {
		return 0, err
	}

	u.amount = amount
	u.scriptVersion = scriptVersion
	u.pkScript = pkScript
	u.spent = false

	return offset, nil
}

// NewUTXOFromSerialized instantiates a new utxoOutput, then fills in the data
// using the serialized data and starting from offset. It returns the final
// offset.
func NewUTXOFromSerialized(serialized []byte, offset int) (*utxoOutput,
	int, error) {
	u := new(utxoOutput)

	var err error
	offset, err = u.FromSerialized(serialized, offset)

	return u, offset, err
}

// serializeUtxoEntry returns the entry serialized to a format that is suitable
// for long-term storage.  The format is described in detail above.
func serializeUtxoEntry(entry *UtxoEntry) ([]byte, error) {
	// Fully spent entries have no serialization.
	if entry.IsFullySpent() {
		return nil, nil
	}

	// Determine the output order by sorting the sparse output index keys.
	outputOrdered := make([]int, 0, len(entry.sparseOutputs))
	for outputIndex := range entry.sparseOutputs {
		outputOrdered = append(outputOrdered, int(outputIndex))
	}
	sort.Ints(outputOrdered)

	// Create the bitmap defining the spentness, then encode the
	// spentness information into it. We will copy it into the
	// final slice later.
	bitmap := bitset.NewBytes(int(entry.outputsLen))
	for _, i := range outputOrdered {
		if entry.sparseOutputs[uint32(i)].spent {
			bitmap.Set(i)
		}
	}

	// Get the size of the byte slice required to accomodate
	// the entirety of the serialized unspent transaction.
	size := serializeSizeVarInt(int64(entry.txVersion)) +
		serializeSizeVarInt(int64(entry.height)) +
		serializeSizeVarInt(int64(entry.index)) +
		serializeSizeVarInt(int64(entry.outputsLen)) +
		len(bitmap) +
		1 // flags

	for _, outputIndex := range outputOrdered {
		out := entry.sparseOutputs[uint32(outputIndex)]
		if out.spent {
			continue
		}
		size += out.SerializeSize()
	}

	// Allocate the byte slice and begin serializing.
	serialized := make([]byte, size)
	offset := putVarInt(serialized, int64(entry.txVersion), 0)
	offset = putVarInt(serialized[offset:], int64(entry.height), offset)
	offset = putVarInt(serialized[offset:], int64(entry.index), offset)
	offset = putVarInt(serialized[offset:], int64(entry.outputsLen), offset)

	copy(serialized[offset:], bitmap[:])
	offset += len(bitmap)

	// Insert the flags.
	serialized[offset] = encodeFlags(entry.isCoinBase, entry.hasExpiry,
		entry.txType)
	offset++

	// Serialize the compressed unspent transaction outputs.  Outputs that
	// are already compressed are serialized without modifications.
	for _, outputIndex := range outputOrdered {
		out := entry.sparseOutputs[uint32(outputIndex)]
		if out.spent {
			continue
		}

		offset += out.PutSerialized(serialized[offset:], offset)
	}

	return serialized, nil
}

// deserializeUtxoEntry decodes a utxo entry from the passed serialized byte
// slice into a new UtxoEntry using a format that is suitable for long-term
// storage.  The format is described in detail above.
func deserializeUtxoEntry(serialized []byte) (*UtxoEntry, error) {
	// Deserialize the txVersion.
	txVersion64, offset := deserializeVarInt(serialized, 0)
	if offset >= len(serialized) {
		return nil, errDeserialize("unexpected end of data after txVersion")
	}

	// Deserialize the height.
	var height64 int64
	height64, offset = deserializeVarInt(serialized, offset)
	if offset >= len(serialized) {
		return nil, errDeserialize("unexpected end of data after height")
	}

	// Deserialize the index.
	var index64 int64
	index64, offset = deserializeVarInt(serialized, offset)
	if offset >= len(serialized) {
		return nil, errDeserialize("unexpected end of data after index")
	}

	// Deserialize the number of outputs.
	var outputsLen64 int64
	outputsLen64, offset = deserializeVarInt(serialized, offset)
	if offset >= len(serialized) {
		return nil, errDeserialize("unexpected end of data after num outputs")
	}

	// Create the empty bitmap.
	bitmap := bitset.NewBytes(int(outputsLen64))

	// Ensure there are enough bytes left to deserialize the unspentness
	// bitmap.
	if len(serialized[offset:]) < len(bitmap) {
		return nil, errDeserialize("unexpected end of data for " +
			"unspentness bitmap")
	}
	copy(bitmap[:], serialized[offset:])
	offset += len(bitmap)

	// Decode the flags.
	isCoinBase, hasExpiry, txType := decodeFlags(serialized[offset])
	offset += 1

	// Create a new utxo entry with the details deserialized above to house
	// all of the utxos.
	entry := newUtxoEntry(
		int32(txVersion64),
		uint32(height64),
		uint32(index64),
		uint32(outputsLen64),
		isCoinBase,
		hasExpiry,
		txType,
	)

	// Decode the unspentness bitmap adding a sparse output for each unspent
	// output.
	for i := int(0); i < int(outputsLen64); i++ {
		if !bitmap.Get(i) { // if unspent
			var utxo *utxoOutput
			var err error
			utxo, offset, err = NewUTXOFromSerialized(serialized, offset)
			if err != nil {
				return nil, err
			}
		}
	}

	return entry, nil
}

// dbFetchUtxoEntry uses an existing database transaction to fetch all unspent
// outputs for the provided Bitcoin transaction hash from the utxo set.
//
// When there is no entry for the provided hash, nil will be returned for the
// both the entry and the error.
func dbFetchUtxoEntry(dbTx database.Tx, hash *chainhash.Hash) (*UtxoEntry, error) {
	// Fetch the unspent transaction output information for the passed
	// transaction hash.  Return now when there is no entry.
	utxoBucket := dbTx.Metadata().Bucket(utxoSetBucketName)
	serializedUtxo := utxoBucket.Get(hash[:])
	if serializedUtxo == nil {
		return nil, nil
	}

	// A non-nil zero-length entry means there is an entry in the database
	// for a fully spent transaction which should never be the case.
	if len(serializedUtxo) == 0 {
		return nil, AssertError(fmt.Sprintf("database contains entry "+
			"for fully spent tx %v", hash))
	}

	// Deserialize the utxo entry and return it.
	entry, err := deserializeUtxoEntry(serializedUtxo)
	if err != nil {
		// Ensure any deserialization errors are returned as database
		// corruption errors.
		if isDeserializeErr(err) {
			return nil, database.Error{
				ErrorCode: database.ErrCorruption,
				Description: fmt.Sprintf("corrupt utxo entry "+
					"for %v: %v", hash, err),
			}
		}

		return nil, err
	}

	return entry, nil
}

// dbPutUtxoView uses an existing database transaction to update the utxo set
// in the database based on the provided utxo view contents and state.  In
// particular, only the entries that have been marked as modified are written
// to the database.
func dbPutUtxoView(dbTx database.Tx, view *UtxoViewpoint) error {
	utxoBucket := dbTx.Metadata().Bucket(utxoSetBucketName)
	for txHashIter, entry := range view.entries {
		// No need to update the database if the entry was not modified.
		if entry == nil || !entry.modified {
			continue
		}

		// Serialize the utxo entry without any entries that have been
		// spent.
		serialized, err := serializeUtxoEntry(entry)
		if err != nil {
			return err
		}

		// Make a copy of the hash because the iterator changes on each
		// loop iteration and thus slicing it directly would cause the
		// data to change out from under the put/delete funcs below.
		txHash := txHashIter

		// Remove the utxo entry if it is now fully spent.
		if serialized == nil {
			if err := utxoBucket.Delete(txHash[:]); err != nil {
				return err
			}

			continue
		}

		// At this point the utxo entry is not fully spent, so store its
		// serialization in the database.
		err = utxoBucket.Put(txHash[:], serialized)
		if err != nil {
			return err
		}
	}

	return nil
}

// -----------------------------------------------------------------------------
// The block index consists of two buckets with an entry for every block in the
// main chain.  One bucket is for the hash to height mapping and the other is
// for the height to hash mapping.
//
// The serialized format for values in the hash to height bucket is:
//   <height>
//
//   Field      Type     Size
//   height     uint32   4 bytes
//
// The serialized format for values in the height to hash bucket is:
//   <hash>
//
//   Field      Type             Size
//   hash       chainhash.Hash   chainhash.Hash
// -----------------------------------------------------------------------------

// dbPutBlockIndex uses an existing database transaction to update or add the
// block index entries for the hash to height and height to hash mappings for
// the provided values.
func dbPutBlockIndex(dbTx database.Tx, hash *chainhash.Hash, height int64) error {
	// Serialize the height for use in the index entries.
	var serializedHeight [4]byte
	byteOrder.PutUint32(serializedHeight[:], uint32(height))

	// Add the block hash to height mapping to the index.
	meta := dbTx.Metadata()
	hashIndex := meta.Bucket(hashIndexBucketName)
	if err := hashIndex.Put(hash[:], serializedHeight[:]); err != nil {
		return err
	}

	// Add the block height to hash mapping to the index.
	heightIndex := meta.Bucket(heightIndexBucketName)
	return heightIndex.Put(serializedHeight[:], hash[:])
}

// dbRemoveBlockIndex uses an existing database transaction remove block index
// entries from the hash to height and height to hash mappings for the provided
// values.
func dbRemoveBlockIndex(dbTx database.Tx, hash *chainhash.Hash, height int64) error {
	// Remove the block hash to height mapping.
	meta := dbTx.Metadata()
	hashIndex := meta.Bucket(hashIndexBucketName)
	if err := hashIndex.Delete(hash[:]); err != nil {
		return err
	}

	// Remove the block height to hash mapping.
	var serializedHeight [4]byte
	byteOrder.PutUint32(serializedHeight[:], uint32(height))
	heightIndex := meta.Bucket(heightIndexBucketName)
	return heightIndex.Delete(serializedHeight[:])
}

// dbFetchHeightByHash uses an existing database transaction to retrieve the
// height for the provided hash from the index.
func dbFetchHeightByHash(dbTx database.Tx, hash *chainhash.Hash) (int64, error) {
	meta := dbTx.Metadata()
	hashIndex := meta.Bucket(hashIndexBucketName)
	serializedHeight := hashIndex.Get(hash[:])
	if serializedHeight == nil {
		str := fmt.Sprintf("block %s is not in the main chain", hash)
		return 0, errNotInMainChain(str)
	}

	return int64(byteOrder.Uint32(serializedHeight)), nil
}

// dbFetchHashByHeight uses an existing database transaction to retrieve the
// hash for the provided height from the index.
func dbFetchHashByHeight(dbTx database.Tx, height int64) (*chainhash.Hash, error) {
	var serializedHeight [4]byte
	byteOrder.PutUint32(serializedHeight[:], uint32(height))

	meta := dbTx.Metadata()
	heightIndex := meta.Bucket(heightIndexBucketName)
	hashBytes := heightIndex.Get(serializedHeight[:])
	if hashBytes == nil {
		str := fmt.Sprintf("no block at height %d exists", height)
		return nil, errNotInMainChain(str)
	}

	var hash chainhash.Hash
	copy(hash[:], hashBytes)
	return &hash, nil
}

// -----------------------------------------------------------------------------
// The best chain state consists of the best block hash and height, the total
// number of transactions up to and including those in the best block, and the
// accumulated work sum up to and including the best block.
//
// The serialized format is:
//
//   <block hash><block height><total txns><work sum length><work sum>
//
//   Field             Type             Size
//   block hash        chainhash.Hash   chainhash.HashSize
//   block height      uint32           4 bytes
//   total txns        uint64           8 bytes
//   total stxns       uint64           8 bytes
//   work sum length   uint32           4 bytes
//   work sum          big.Int          work sum length
// -----------------------------------------------------------------------------

// bestChainState represents the data to be stored the database for the current
// best chain state.
type bestChainState struct {
	hash      chainhash.Hash
	height    uint32
	totalTxns uint64
	workSum   *big.Int
}

// serializeBestChainState returns the serialization of the passed block best
// chain state.  This is data to be stored in the chain state bucket.
func serializeBestChainState(state bestChainState) []byte {
	// Calculate the full size needed to serialize the chain state.
	workSumBytes := state.workSum.Bytes()
	workSumBytesLen := uint32(len(workSumBytes))
	serializedLen := chainhash.HashSize + 4 + 8 + 4 + workSumBytesLen

	// Serialize the chain state.
	serializedData := make([]byte, serializedLen)
	copy(serializedData[0:chainhash.HashSize], state.hash[:])
	offset := uint32(chainhash.HashSize)
	byteOrder.PutUint32(serializedData[offset:], state.height)
	offset += 4
	byteOrder.PutUint64(serializedData[offset:], state.totalTxns)
	offset += 8
	byteOrder.PutUint32(serializedData[offset:], workSumBytesLen)
	offset += 4
	copy(serializedData[offset:], workSumBytes)
	return serializedData[:]
}

// deserializeBestChainState deserializes the passed serialized best chain
// state.  This is data stored in the chain state bucket and is updated after
// every block is connected or disconnected form the main chain.
// block.
func deserializeBestChainState(serializedData []byte) (bestChainState, error) {
	// Ensure the serialized data has enough bytes to properly deserialize
	// the hash, height, total transactions, and work sum length.
	if len(serializedData) < chainhash.HashSize+16 {
		return bestChainState{}, database.Error{
			ErrorCode:   database.ErrCorruption,
			Description: "corrupt best chain state",
		}
	}

	state := bestChainState{}
	copy(state.hash[:], serializedData[0:chainhash.HashSize])
	offset := uint32(chainhash.HashSize)
	state.height = byteOrder.Uint32(serializedData[offset : offset+4])
	offset += 4
	state.totalTxns = byteOrder.Uint64(serializedData[offset : offset+8])
	offset += 8
	workSumBytesLen := byteOrder.Uint32(serializedData[offset : offset+4])
	offset += 4

	// Ensure the serialized data has enough bytes to deserialize the work
	// sum.
	if uint32(len(serializedData[offset:])) < workSumBytesLen {
		return bestChainState{}, database.Error{
			ErrorCode:   database.ErrCorruption,
			Description: "corrupt best chain state",
		}
	}
	workSumBytes := serializedData[offset : offset+workSumBytesLen]
	state.workSum = new(big.Int).SetBytes(workSumBytes)

	return state, nil
}

// dbPutBestState uses an existing database transaction to update the best chain
// state with the given parameters.
func dbPutBestState(dbTx database.Tx, snapshot *BestState, workSum *big.Int) error {
	// Serialize the current best chain state.
	serializedData := serializeBestChainState(bestChainState{
		hash:      *snapshot.Hash,
		height:    uint32(snapshot.Height),
		totalTxns: snapshot.TotalTxns,
		workSum:   workSum,
	})

	// Store the current best chain state into the database.
	return dbTx.Metadata().Put(chainStateKeyName, serializedData)
}

// createChainState initializes both the database and the chain state to the
// genesis block.  This includes creating the necessary buckets and inserting
// the genesis block, so it must only be called on an uninitialized database.
func (b *BlockChain) createChainState() error {
	// Create a new node from the genesis block and set it as both the root
	// node and the best node.
	genesisBlock := dcrutil.NewBlock(b.chainParams.GenesisBlock)
	header := &genesisBlock.MsgBlock().Header
	node := newBlockNode(header, genesisBlock.Sha(), 0, []uint16{})
	node.inMainChain = true
	b.bestNode = node
	b.root = node

	// Add the new node to the index which is used for faster lookups.
	b.index[*node.hash] = node

	// Initialize the state related to the best block.
	numTxns := uint64(len(genesisBlock.MsgBlock().Transactions))
	blockSize := uint64(genesisBlock.MsgBlock().SerializeSize())
	b.stateSnapshot = newBestState(b.bestNode, blockSize, numTxns, numTxns)

	// Create the initial the database chain state including creating the
	// necessary index buckets and inserting the genesis block.
	err := b.db.Update(func(dbTx database.Tx) error {
		// Create the bucket that houses the chain block hash to height
		// index.
		meta := dbTx.Metadata()
		_, err := meta.CreateBucket(hashIndexBucketName)
		if err != nil {
			return err
		}

		// Create the bucket that houses the chain block height to hash
		// index.
		_, err = meta.CreateBucket(heightIndexBucketName)
		if err != nil {
			return err
		}

		// Create the bucket that houses the spend journal data.
		_, err = meta.CreateBucket(spendJournalBucketName)
		if err != nil {
			return err
		}

		// Create the bucket that houses the utxo set.  Note that the
		// genesis block coinbase transaction is intentionally not
		// inserted here since it is not spendable by consensus rules.
		_, err = meta.CreateBucket(utxoSetBucketName)
		if err != nil {
			return err
		}

		// Add the genesis block hash to height and height to hash
		// mappings to the index.
		err = dbPutBlockIndex(dbTx, b.bestNode.hash, b.bestNode.height)
		if err != nil {
			return err
		}

		// Store the current best chain state into the database.
		err = dbPutBestState(dbTx, b.stateSnapshot, b.bestNode.workSum)
		if err != nil {
			return err
		}

		// Store the genesis block into the database.
		return dbTx.StoreBlock(genesisBlock)
	})
	return err
}

// initChainState attempts to load and initialize the chain state from the
// database.  When the db does not yet contain any chain state, both it and the
// chain state are initialized to the genesis block.
func (b *BlockChain) initChainState() error {
	// Attempt to load the chain state from the database.
	var isStateInitialized bool
	err := b.db.View(func(dbTx database.Tx) error {
		// Fetch the stored chain state from the database metadata.
		// When it doesn't exist, it means the database hasn't been
		// initialized for use with chain yet, so break out now to allow
		// that to happen under a writable database transaction.
		serializedData := dbTx.Metadata().Get(chainStateKeyName)
		if serializedData == nil {
			return nil
		}
		log.Tracef("Serialized chain state: %x", serializedData)
		state, err := deserializeBestChainState(serializedData)
		if err != nil {
			return err
		}

		// Load the raw block bytes for the best block.
		blockBytes, err := dbTx.FetchBlock(&state.hash)
		if err != nil {
			return err
		}
		var block wire.MsgBlock
		err = block.Deserialize(bytes.NewReader(blockBytes))
		if err != nil {
			return err
		}

		// Create a new node and set it as both the root node and the
		// best node.  The preceding nodes will be loaded on demand as
		// needed.
		// TODO CJ Get vote bits from db
		header := &block.Header
		node := newBlockNode(header, &state.hash, int64(state.height),
			[]uint16{})
		node.inMainChain = true
		node.workSum = state.workSum
		b.bestNode = node
		b.root = node

		// Add the new node to the indices for faster lookups.
		prevHash := node.parentHash
		b.index[*node.hash] = node
		b.depNodes[*prevHash] = append(b.depNodes[*prevHash], node)

		// Initialize the state related to the best block.
		blockSize := uint64(len(blockBytes))
		numTxns := uint64(len(block.Transactions))
		b.stateSnapshot = newBestState(b.bestNode, blockSize, numTxns,
			state.totalTxns)

		isStateInitialized = true
		return nil
	})
	if err != nil {
		return err
	}

	// There is nothing more to do if the chain state was initialized.
	if isStateInitialized {
		return nil
	}

	// At this point the database has not already been initialized, so
	// initialize both it and the chain state to the genesis block.
	return b.createChainState()
}

// dbFetchHeaderByHash uses an existing database transaction to retrieve the
// block header for the provided hash.
func dbFetchHeaderByHash(dbTx database.Tx, hash *chainhash.Hash) (*wire.BlockHeader, error) {
	headerBytes, err := dbTx.FetchBlockHeader(hash)
	if err != nil {
		return nil, err
	}

	var header wire.BlockHeader
	err = header.Deserialize(bytes.NewReader(headerBytes))
	if err != nil {
		return nil, err
	}

	return &header, nil
}

// dbFetchHeaderByHeight uses an existing database transaction to retrieve the
// block header for the provided height.
func dbFetchHeaderByHeight(dbTx database.Tx, height int64) (*wire.BlockHeader, error) {
	hash, err := dbFetchHashByHeight(dbTx, height)
	if err != nil {
		return nil, err
	}

	return dbFetchHeaderByHash(dbTx, hash)
}

// dbFetchBlockByHash uses an existing database transaction to retrieve the raw
// block for the provided hash, deserialize it, retrieve the appropriate height
// from the index, and return a dcrutil.Block with the height set.
func dbFetchBlockByHash(dbTx database.Tx, hash *chainhash.Hash) (*dcrutil.Block, error) {
	// First find the height associated with the provided hash in the index.
	blockHeight, err := dbFetchHeightByHash(dbTx, hash)
	if err != nil {
		return nil, err
	}

	// Load the raw block bytes from the database.
	blockBytes, err := dbTx.FetchBlock(hash)
	if err != nil {
		return nil, err
	}

	// Create the encapsulated block and set the height appropriately.
	block, err := dcrutil.NewBlockFromBytes(blockBytes)
	if err != nil {
		return nil, err
	}
	block.SetHeight(blockHeight)

	return block, nil
}

// dbFetchBlockByHeight uses an existing database transaction to retrieve the
// raw block for the provided height, deserialize it, and return a dcrutil.Block
// with the height set.
func dbFetchBlockByHeight(dbTx database.Tx, height int64) (*dcrutil.Block, error) {
	// First find the hash associated with the provided height in the index.
	hash, err := dbFetchHashByHeight(dbTx, height)
	if err != nil {
		return nil, err
	}

	// Load the raw block bytes from the database.
	blockBytes, err := dbTx.FetchBlock(hash)
	if err != nil {
		return nil, err
	}

	// Create the encapsulated block and set the height appropriately.
	block, err := dcrutil.NewBlockFromBytes(blockBytes)
	if err != nil {
		return nil, err
	}
	block.SetHeight(height)

	return block, nil
}

// dbMainChainHasBlock uses an existing database transaction to return whether
// or not the main chain contains the block identified by the provided hash.
func dbMainChainHasBlock(dbTx database.Tx, hash *chainhash.Hash) bool {
	hashIndex := dbTx.Metadata().Bucket(hashIndexBucketName)
	return hashIndex.Get(hash[:]) != nil
}

// BlockHeightByHash returns the height of the block with the given hash in the
// main chain.
//
// This function is safe for concurrent access.
func (b *BlockChain) BlockHeightByHash(hash *chainhash.Hash) (int64, error) {
	var height int64
	err := b.db.View(func(dbTx database.Tx) error {
		var err error
		height, err = dbFetchHeightByHash(dbTx, hash)
		return err
	})
	return height, err
}

// BlockHashByHeight returns the hash of the block at the given height in the
// main chain.
//
// This function is safe for concurrent access.
func (b *BlockChain) BlockHashByHeight(blockHeight int64) (*chainhash.Hash, error) {
	var hash *chainhash.Hash
	err := b.db.View(func(dbTx database.Tx) error {
		var err error
		hash, err = dbFetchHashByHeight(dbTx, blockHeight)
		return err
	})
	return hash, err
}

// BlockByHeight returns the block at the given height in the main chain.
//
// This function is safe for concurrent access.
func (b *BlockChain) BlockByHeight(blockHeight int64) (*dcrutil.Block, error) {
	var block *dcrutil.Block
	err := b.db.View(func(dbTx database.Tx) error {
		var err error
		block, err = dbFetchBlockByHeight(dbTx, blockHeight)
		return err
	})
	return block, err
}

// BlockByHash returns the block from the main chain with the given hash with
// the appropriate chain height set.
//
// This function is safe for concurrent access.
func (b *BlockChain) BlockByHash(hash *chainhash.Hash) (*dcrutil.Block, error) {
	var block *dcrutil.Block
	err := b.db.View(func(dbTx database.Tx) error {
		var err error
		block, err = dbFetchBlockByHash(dbTx, hash)
		return err
	})
	return block, err
}

// HeightRange returns a range of block hashes for the given start and end
// heights.  It is inclusive of the start height and exclusive of the end
// height.  The end height will be limited to the current main chain height.
//
// This function is safe for concurrent access.
func (b *BlockChain) HeightRange(startHeight, endHeight int64) ([]chainhash.Hash, error) {
	// Ensure requested heights are sane.
	if startHeight < 0 {
		return nil, fmt.Errorf("start height of fetch range must not "+
			"be less than zero - got %d", startHeight)
	}
	if endHeight < startHeight {
		return nil, fmt.Errorf("end height of fetch range must not "+
			"be less than the start height - got start %d, end %d",
			startHeight, endHeight)
	}

	// There is nothing to do when the start and end heights are the same,
	// so return now to avoid the chain lock and a database transaction.
	if startHeight == endHeight {
		return nil, nil
	}

	// Grab a lock on the chain to prevent it from changing due to a reorg
	// while building the hashes.
	b.chainLock.RLock()
	defer b.chainLock.RUnlock()

	// When the requested start height is after the most recent best chain
	// height, there is nothing to do.
	latestHeight := b.bestNode.height
	if startHeight > latestHeight {
		return nil, nil
	}

	// Limit the ending height to the latest height of the chain.
	if endHeight > latestHeight+1 {
		endHeight = latestHeight + 1
	}

	// Fetch as many as are available within the specified range.
	var hashList []chainhash.Hash
	err := b.db.View(func(dbTx database.Tx) error {
		hashes := make([]chainhash.Hash, 0, endHeight-startHeight)
		for i := startHeight; i < endHeight; i++ {
			hash, err := dbFetchHashByHeight(dbTx, i)
			if err != nil {
				return err
			}
			hashes = append(hashes, *hash)
		}

		// Set the list to be returned to the constructed list.
		hashList = hashes
		return nil
	})
	return hashList, err
}
