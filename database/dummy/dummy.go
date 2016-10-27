// dummy.go
package dummy

import (
	"fmt"

	"github.com/btcsuite/btclog"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/database"
	"github.com/decred/dcrutil"
)

var log = btclog.Disabled

const (
	dbType = "dummy"
)

func init() {
	create := func(...interface{}) (database.DB, error) { return &db{}, nil }
	open := func(...interface{}) (database.DB, error) { return &db{}, nil }

	// Register the driver.
	driver := database.Driver{
		DbType:    dbType,
		Create:    create,
		Open:      open,
		UseLogger: nil,
	}
	if err := database.RegisterDriver(driver); err != nil {
		panic(fmt.Sprintf("Failed to regiser database driver '%s': %v",
			dbType, err))
	}
}

// cursor is an internal type used to represent a cursor over key/value pairs
// and nested buckets of a bucket and implements the database.Cursor interface.
type cursor struct {
}

// Enforce cursor implements the database.Cursor interface.
var _ database.Cursor = (*cursor)(nil)

// Bucket returns the bucket the cursor was created for.
//
// This function is part of the database.Cursor interface implementation.
func (c *cursor) Bucket() database.Bucket {
	return c.Bucket()
}

// Delete removes the current key/value pair the cursor is at without
// invalidating the cursor.
//
// Returns the following errors as required by the interface contract:
//   - ErrIncompatibleValue if attempted when the cursor points to a nested
//     bucket
//   - ErrTxNotWritable if attempted against a read-only transaction
//   - ErrTxClosed if the transaction has already been closed
//
// This function is part of the database.Cursor interface implementation.
func (c *cursor) Delete() error {
	return nil
}

// First positions the cursor at the first key/value pair and returns whether or
// not the pair exists.
//
// This function is part of the database.Cursor interface implementation.
func (c *cursor) First() bool {
	return false
}

// Last positions the cursor at the last key/value pair and returns whether or
// not the pair exists.
//
// This function is part of the database.Cursor interface implementation.
func (c *cursor) Last() bool {
	return false
}

// Next moves the cursor one key/value pair forward and returns whether or not
// the pair exists.
//
// This function is part of the database.Cursor interface implementation.
func (c *cursor) Next() bool {
	return false
}

// Prev moves the cursor one key/value pair backward and returns whether or not
// the pair exists.
//
// This function is part of the database.Cursor interface implementation.
func (c *cursor) Prev() bool {
	return false
}

// Seek positions the cursor at the first key/value pair that is greater than or
// equal to the passed seek key.  Returns false if no suitable key was found.
//
// This function is part of the database.Cursor interface implementation.
func (c *cursor) Seek(seek []byte) bool {
	return false
}

// Key returns the current key the cursor is pointing to.
//
// This function is part of the database.Cursor interface implementation.
func (c *cursor) Key() []byte {
	return []byte{}
}

// Value returns the current value the cursor is pointing to.  This will be nil
// for nested buckets.
//
// This function is part of the database.Cursor interface implementation.
func (c *cursor) Value() []byte {
	return []byte{}
}

// cursorType defines the type of cursor to create.
type cursorType int

// The following constants define the allowed cursor types.
const (
	// ctKeys iterates through all of the keys in a given bucket.
	ctKeys cursorType = iota

	// ctBuckets iterates through all directly nested buckets in a given
	// bucket.
	ctBuckets

	// ctFull iterates through both the keys and the directly nested buckets
	// in a given bucket.
	ctFull
)

// bucket is an internal type used to represent a collection of key/value pairs
// and implements the database.Bucket interface.
type bucket struct {
	tx *transaction
	id [4]byte
}

// Enforce bucket implements the database.Bucket interface.
var _ database.Bucket = (*bucket)(nil)

// Bucket retrieves a nested bucket with the given key.  Returns nil if
// the bucket does not exist.
//
// This function is part of the database.Bucket interface implementation.
func (b *bucket) Bucket(key []byte) database.Bucket {
	return &bucket{}
}

// CreateBucket creates and returns a new nested bucket with the given key.
//
// Returns the following errors as required by the interface contract:
//   - ErrBucketExists if the bucket already exists
//   - ErrBucketNameRequired if the key is empty
//   - ErrIncompatibleValue if the key is otherwise invalid for the particular
//     implementation
//   - ErrTxNotWritable if attempted against a read-only transaction
//   - ErrTxClosed if the transaction has already been closed
//
// This function is part of the database.Bucket interface implementation.
func (b *bucket) CreateBucket(key []byte) (database.Bucket, error) {
	return &bucket{}, nil
}

// CreateBucketIfNotExists creates and returns a new nested bucket with the
// given key if it does not already exist.
//
// Returns the following errors as required by the interface contract:
//   - ErrBucketNameRequired if the key is empty
//   - ErrIncompatibleValue if the key is otherwise invalid for the particular
//     implementation
//   - ErrTxNotWritable if attempted against a read-only transaction
//   - ErrTxClosed if the transaction has already been closed
//
// This function is part of the database.Bucket interface implementation.
func (b *bucket) CreateBucketIfNotExists(key []byte) (database.Bucket, error) {
	return b.CreateBucket(key)
}

// DeleteBucket removes a nested bucket with the given key.
//
// Returns the following errors as required by the interface contract:
//   - ErrBucketNotFound if the specified bucket does not exist
//   - ErrTxNotWritable if attempted against a read-only transaction
//   - ErrTxClosed if the transaction has already been closed
//
// This function is part of the database.Bucket interface implementation.
func (b *bucket) DeleteBucket(key []byte) error {
	return nil
}

// Cursor returns a new cursor, allowing for iteration over the bucket's
// key/value pairs and nested buckets in forward or backward order.
//
// You must seek to a position using the First, Last, or Seek functions before
// calling the Next, Prev, Key, or Value functions.  Failure to do so will
// result in the same return values as an exhausted cursor, which is false for
// the Prev and Next functions and nil for Key and Value functions.
//
// This function is part of the database.Bucket interface implementation.
func (b *bucket) Cursor() database.Cursor {
	return &cursor{}
}

// ForEach invokes the passed function with every key/value pair in the bucket.
// This does not include nested buckets or the key/value pairs within those
// nested buckets.
//
// WARNING: It is not safe to mutate data while iterating with this method.
// Doing so may cause the underlying cursor to be invalidated and return
// unexpected keys and/or values.
//
// Returns the following errors as required by the interface contract:
//   - ErrTxClosed if the transaction has already been closed
//
// NOTE: The values returned by this function are only valid during a
// transaction.  Attempting to access them after a transaction has ended will
// likely result in an access violation.
//
// This function is part of the database.Bucket interface implementation.
func (b *bucket) ForEach(fn func(k, v []byte) error) error {
	return nil
}

// ForEachBucket invokes the passed function with the key of every nested bucket
// in the current bucket.  This does not include any nested buckets within those
// nested buckets.
//
// WARNING: It is not safe to mutate data while iterating with this method.
// Doing so may cause the underlying cursor to be invalidated and return
// unexpected keys.
//
// Returns the following errors as required by the interface contract:
//   - ErrTxClosed if the transaction has already been closed
//
// NOTE: The values returned by this function are only valid during a
// transaction.  Attempting to access them after a transaction has ended will
// likely result in an access violation.
//
// This function is part of the database.Bucket interface implementation.
func (b *bucket) ForEachBucket(fn func(k []byte) error) error {
	return nil
}

// Writable returns whether or not the bucket is writable.
//
// This function is part of the database.Bucket interface implementation.
func (b *bucket) Writable() bool {
	return false
}

// Put saves the specified key/value pair to the bucket.  Keys that do not
// already exist are added and keys that already exist are overwritten.
//
// Returns the following errors as required by the interface contract:
//   - ErrKeyRequired if the key is empty
//   - ErrIncompatibleValue if the key is the same as an existing bucket
//   - ErrTxNotWritable if attempted against a read-only transaction
//   - ErrTxClosed if the transaction has already been closed
//
// This function is part of the database.Bucket interface implementation.
func (b *bucket) Put(key, value []byte) error {
	return nil
}

// Get returns the value for the given key.  Returns nil if the key does not
// exist in this bucket.  An empty slice is returned for keys that exist but
// have no value assigned.
//
// NOTE: The value returned by this function is only valid during a transaction.
// Attempting to access it after a transaction has ended results in undefined
// behavior.  Additionally, the value must NOT be modified by the caller.
//
// This function is part of the database.Bucket interface implementation.
func (b *bucket) Get(key []byte) []byte {
	return nil
}

// Delete removes the specified key from the bucket.  Deleting a key that does
// not exist does not return an error.
//
// Returns the following errors as required by the interface contract:
//   - ErrKeyRequired if the key is empty
//   - ErrIncompatibleValue if the key is the same as an existing bucket
//   - ErrTxNotWritable if attempted against a read-only transaction
//   - ErrTxClosed if the transaction has already been closed
//
// This function is part of the database.Bucket interface implementation.
func (b *bucket) Delete(key []byte) error {
	return nil
}

// pendingBlock houses a block that will be written to disk when the database
// transaction is committed.
type pendingBlock struct {
}

// transaction represents a database transaction.  It can either be read-only or
// read-write and implements the database.Bucket interface.  The transaction
// provides a root bucket against which all read and writes occur.
type transaction struct {
}

// Enforce transaction implements the database.Tx interface.
var _ database.Tx = (*transaction)(nil)

// Metadata returns the top-most bucket for all metadata storage.
//
// This function is part of the database.Tx interface implementation.
func (tx *transaction) Metadata() database.Bucket {
	return &bucket{}
}

// StoreBlock
//
// This function is part of the database.Tx interface implementation.
func (tx *transaction) StoreBlock(block *dcrutil.Block) error {
	return nil
}

// HasBlock returns whether or not a block with the given hash exists in the
// database.
//
// Returns the following errors as required by the interface contract:
//   - ErrTxClosed if the transaction has already been closed
//
// This function is part of the database.Tx interface implementation.
func (tx *transaction) HasBlock(hash *chainhash.Hash) (bool, error) {
	return false, nil
}

// HasBlocks returns whether or not the blocks with the provided hashes
// exist in the database.
//
// Returns the following errors as required by the interface contract:
//   - ErrTxClosed if the transaction has already been closed
//
// This function is part of the database.Tx interface implementation.
func (tx *transaction) HasBlocks(hashes []chainhash.Hash) ([]bool, error) {
	return []bool{}, nil
}

// FetchBlockHeader returns the raw serialized bytes for the block header
// identified by the given hash.  The raw bytes are in the format returned by
// Serialize on a wire.BlockHeader.
//
// Returns the following errors as required by the interface contract:
//   - ErrBlockNotFound if the requested block hash does not exist
//   - ErrTxClosed if the transaction has already been closed
//   - ErrCorruption if the database has somehow become corrupted
//
// NOTE: The data returned by this function is only valid during a
// database transaction.  Attempting to access it after a transaction
// has ended results in undefined behavior.  This constraint prevents
// additional data copies and allows support for memory-mapped database
// implementations.
//
// This function is part of the database.Tx interface implementation.
func (tx *transaction) FetchBlockHeader(hash *chainhash.Hash) ([]byte, error) {
	return []byte{}, nil
}

// FetchBlockHeaders returns the raw serialized bytes for the block headers
// identified by the given hashes.  The raw bytes are in the format returned by
// Serialize on a wire.BlockHeader.
//
// Returns the following errors as required by the interface contract:
//   - ErrBlockNotFound if the any of the requested block hashes do not exist
//   - ErrTxClosed if the transaction has already been closed
//   - ErrCorruption if the database has somehow become corrupted
//
// NOTE: The data returned by this function is only valid during a database
// transaction.  Attempting to access it after a transaction has ended results
// in undefined behavior.  This constraint prevents additional data copies and
// allows support for memory-mapped database implementations.
//
// This function is part of the database.Tx interface implementation.
func (tx *transaction) FetchBlockHeaders(hashes []chainhash.Hash) ([][]byte, error) {
	return [][]byte{}, nil
}

// FetchBlock returns the raw serialized bytes for the block identified by the
// given hash.  The raw bytes are in the format returned by Serialize on a
// wire.MsgBlock.
//
// Returns the following errors as required by the interface contract:
//   - ErrBlockNotFound if the requested block hash does not exist
//   - ErrTxClosed if the transaction has already been closed
//   - ErrCorruption if the database has somehow become corrupted
//
// In addition, returns ErrDriverSpecific if any failures occur when reading the
// block files.
//
// NOTE: The data returned by this function is only valid during a database
// transaction.  Attempting to access it after a transaction has ended results
// in undefined behavior.  This constraint prevents additional data copies and
// allows support for memory-mapped database implementations.
//
// This function is part of the database.Tx interface implementation.
func (tx *transaction) FetchBlock(hash *chainhash.Hash) ([]byte, error) {
	return nil, nil
}

// FetchBlocks returns the raw serialized bytes for the blocks identified by the
// given hashes.  The raw bytes are in the format returned by Serialize on a
// wire.MsgBlock.
//
// Returns the following errors as required by the interface contract:
//   - ErrBlockNotFound if any of the requested block hashed do not exist
//   - ErrTxClosed if the transaction has already been closed
//   - ErrCorruption if the database has somehow become corrupted
//
// In addition, returns ErrDriverSpecific if any failures occur when reading the
// block files.
//
// NOTE: The data returned by this function is only valid during a database
// transaction.  Attempting to access it after a transaction has ended results
// in undefined behavior.  This constraint prevents additional data copies and
// allows support for memory-mapped database implementations.
//
// This function is part of the database.Tx interface implementation.
func (tx *transaction) FetchBlocks(hashes []chainhash.Hash) ([][]byte, error) {
	return nil, nil
}

// fetchPendingRegion attempts to fetch the provided region from any block which
// are pending to be written on commit.  It will return nil for the byte slice
// when the region references a block which is not pending.  When the region
// does reference a pending block, it is bounds checked and returns
// ErrBlockRegionInvalid if invalid.
func (tx *transaction) fetchPendingRegion(region *database.BlockRegion) ([]byte, error) {
	return nil, nil
}

// FetchBlockRegion returns the raw serialized bytes for the given block region.
//
// For example, it is possible to directly extract transactions and/or scripts
// from a block with this function.  Depending on the backend implementation,
// this can provide significant savings by avoiding the need to load entire
// blocks.
//
// The raw bytes are in the format returned by Serialize on a wire.MsgBlock and
// the Offset field in the provided BlockRegion is zero-based and relative to
// the start of the block (byte 0).
//
// Returns the following errors as required by the interface contract:
//   - ErrBlockNotFound if the requested block hash does not exist
//   - ErrBlockRegionInvalid if the region exceeds the bounds of the associated
//     block
//   - ErrTxClosed if the transaction has already been closed
//   - ErrCorruption if the database has somehow become corrupted
//
// In addition, returns ErrDriverSpecific if any failures occur when reading the
// block files.
//
// NOTE: The data returned by this function is only valid during a database
// transaction.  Attempting to access it after a transaction has ended results
// in undefined behavior.  This constraint prevents additional data copies and
// allows support for memory-mapped database implementations.
//
// This function is part of the database.Tx interface implementation.
func (tx *transaction) FetchBlockRegion(region *database.BlockRegion) ([]byte, error) {
	return nil, nil
}

// FetchBlockRegions returns the raw serialized bytes for the given block
// regions.
//
// For example, it is possible to directly extract transactions and/or scripts
// from various blocks with this function.  Depending on the backend
// implementation, this can provide significant savings by avoiding the need to
// load entire blocks.
//
// The raw bytes are in the format returned by Serialize on a wire.MsgBlock and
// the Offset fields in the provided BlockRegions are zero-based and relative to
// the start of the block (byte 0).
//
// Returns the following errors as required by the interface contract:
//   - ErrBlockNotFound if any of the request block hashes do not exist
//   - ErrBlockRegionInvalid if one or more region exceed the bounds of the
//     associated block
//   - ErrTxClosed if the transaction has already been closed
//   - ErrCorruption if the database has somehow become corrupted
//
// In addition, returns ErrDriverSpecific if any failures occur when reading the
// block files.
//
// NOTE: The data returned by this function is only valid during a database
// transaction.  Attempting to access it after a transaction has ended results
// in undefined behavior.  This constraint prevents additional data copies and
// allows support for memory-mapped database implementations.
//
// This function is part of the database.Tx interface implementation.
func (tx *transaction) FetchBlockRegions(regions []database.BlockRegion) ([][]byte, error) {
	return nil, nil
}

// close marks the transaction closed then releases any pending data, the
// underlying snapshot, the transaction read lock, and the write lock when the
// transaction is writable.
func (tx *transaction) close() {
}

// writePendingAndCommit writes pending block data to the flat block files,
// updates the metadata with their locations as well as the new current write
// location, and commits the metadata to the memory database cache.  It also
// properly handles rollback in the case of failures.
//
// This function MUST only be called when there is pending data to be written.
func (tx *transaction) writePendingAndCommit() error {
	return nil
}

// Commit commits all changes that have been made to the root metadata bucket
// and all of its sub-buckets to the database cache which is periodically synced
// to persistent storage.  In addition, it commits all new blocks directly to
// persistent storage bypassing the db cache.  Blocks can be rather large, so
// this help increase the amount of cache available for the metadata updates and
// is safe since blocks are immutable.
//
// This function is part of the database.Tx interface implementation.
func (tx *transaction) Commit() error {
	return nil
}

// Rollback undoes all changes that have been made to the root bucket and all of
// its sub-buckets.
//
// This function is part of the database.Tx interface implementation.
func (tx *transaction) Rollback() error {
	return nil
}

// db represents a collection of namespaces which are persisted and implements
// the database.DB interface.  All database access is performed through
// transactions which are obtained through the specific Namespace.
type db struct {
}

// Enforce db implements the database.DB interface.
var _ database.DB = (*db)(nil)

// Type returns the database driver type the current database instance was
// created with.
//
// This function is part of the database.DB interface implementation.
func (db *db) Type() string {
	return dbType
}

// begin
//
// This function is only separate because it returns the internal transaction
// which is used by the managed transaction code while the database method
// returns the interface.
func (db *db) begin(writable bool) (*transaction, error) {
	return &transaction{}, nil
}

// Begin
//
// This function is part of the database.DB interface implementation.
func (db *db) Begin(writable bool) (database.Tx, error) {
	return db.begin(writable)
}

// View
//
// This function is part of the database.DB interface implementation.
func (db *db) View(fn func(database.Tx) error) error {
	return nil
}

// Update
//
// This function is part of the database.DB interface implementation.
func (db *db) Update(fn func(database.Tx) error) error {
	return nil
}

// Close
//
// This function is part of the database.DB interface implementation.
func (db *db) Close() error {
	return nil
}
