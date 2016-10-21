// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package stake

import (
	"fmt"

	"github.com/decred/dcrd/blockchain/stake/internal/dbnamespace"
	"github.com/decred/dcrd/blockchain/stake/internal/votingdb"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/database"
)

// IssueVote is the state of a vote on a given issue as indicated in the vote's
// vote bits.  The mandatory vote bits, a little endian uint16, are organized as
// follows:
//
//   Bits     Description
//      0     Previous block is valid (boolean)
//      1     Undefined (boolean)
//    2-3     First issue (IssueVote)
//    4-5     Second issue (IssueVote)
//    6-7     Third issue (IssueVote)
//        ...
//  14-15     Seventh issue
//
type IssueVote uint8

const (
	// IssueVoteUndefined is what the votebits for this IssueVote should
	// be set to when the issue state is formally undefined.
	IssueVoteUndefined = 0 // 00

	// IssueVoteYes indicates a YES vote for this issue.
	IssueVoteYes = 1 // 01

	// IssueVoteNo indications a NO vote for this issue.
	IssueVoteNo = 2 // 10

	// IssueVoteAbstain indications abstaining from voting on this issue.
	IssueVoteAbstain = 3 // 11

	// issuesLen is the number of issues that can be represented by the
	// 14 remaining bits of vote bits.
	issuesLen = 7
)

// String satisfies the stringer interface for IssueVote.
func (i IssueVote) String() string {
	switch i {
	case IssueVoteUndefined:
		return "undefined"
	case IssueVoteYes:
		return "yes"
	case IssueVoteNo:
		return "no"
	case IssueVoteAbstain:
		return "abstain"
	}

	return "error (unknown issue vote type)"
}

// DecodedVoteBitsPrefix represents the structure of decoded vote bits for the
// passed vote bits.
type DecodedVoteBitsPrefix struct {
	BlockValid bool
	Unused     bool
	Issues     [issuesLen]IssueVote
}

// rotateLeft performs to a bitwise rotation left on a passed uint16.
func rotateLeft(value uint16, count uint16) uint16 {
	return (value << count) | (value >> (16 - count))
}

// DecodeVoteBitsPrefix decodes the passed 16 vote bits into their decoded
// structure for subsequent use in tallying or examination.
func EncodeVoteBitsPrefix(voteBits DecodedVoteBitsPrefix) uint16 {
	var val uint16

	// Issues.
	for i := 6; i >= 0; i-- {
		// Set the issue and bitshift it to the left.
		val += uint16(voteBits.Issues[i])
		val <<= 2
	}

	// First two bits.
	if voteBits.BlockValid {
		val |= 0x0001 // set 0000 ... 0001
	}
	if voteBits.Unused {
		val |= 0x0002 // set 0000 ... 0010
	}

	return val
}

// DecodeVoteBitsPrefix decodes the passed 16 vote bits into their decoded
// structure for subsequent use in tallying or examination.
func DecodeVoteBitsPrefix(voteBits uint16) DecodedVoteBitsPrefix {
	var dvbs DecodedVoteBitsPrefix

	// First two bits.
	dvbs.BlockValid = voteBits&0x0001 != 0 // b & 0000 ... 0001
	dvbs.Unused = voteBits&0x0002 != 0     // b & 0000 ... 0010

	// Issues.
	mask := uint16(0x0003) // 0000 ... 0011
	for i := uint16(0); i < issuesLen; i++ {
		// Move the mask to select the next issue.
		mask = rotateLeft(mask, 2)

		// Pop off the issue and shift downwards so that is corresponds
		// to the type of vote it should be.
		dvbs.Issues[i] = IssueVote((mask & voteBits) >> ((i * 2) + 2))
	}

	return dvbs
}

// VotingTally is a rolling tally of votes for some issue.  The index of the
// tally corresponds to the number of each vote types seen on the issue for
// this period.  For example, we might observe for some issue:
//    Undefined: 0
//    Yes:       100
//    No:        200
//    Abstain:   300
//
// This would be the VotingTally represented by {0,100,200,300}.
type VotingTally [4]uint16

// SerializeInto serializes the VotingTally into a passed byte slice, stored as
// little endian uint16s.  It takes a passed offset to begin writing into.
//
// The function does not check for the length of the byte slice and will panic
// if there is not enough space to write into.
func (v *VotingTally) SerializeInto(sl *[]byte, offset int) {
	slVal := *sl
	for i := 0; i < 8; i += 2 {
		dbnamespace.ByteOrder.PutUint16(slVal[offset+i:offset+i+2], v[i/2])
	}
}

// Deserialize deserializes from a byte slice into the VotingTally it is called
// on.  It takes a passed offset to begin reading from.
//
// The function does not check for the length of the byte slice and will panic
// if there is not enough space to write into.
func (v *VotingTally) Deserialize(sl *[]byte, offset int) {
	slVal := *sl
	for i := 0; i < 8; i += 2 {
		v[i/2] = dbnamespace.ByteOrder.Uint16(slVal[offset+i : offset+i+2])
	}
}

// BlockKey is the block key for a given block, that is used to map fully
// tallied blocks (at each difficulty changing interval) to tally data in
// the database.
type BlockKey struct {
	Hash   chainhash.Hash
	Height uint32
}

// BlockKeySize is the size of a serialized block key.
const BlockKeySize = 36

// SerializeInto serializes the BlockKey into a passed byte slice, stored as
// a flat chainhash and a little endian uint32.  It takes a passed offset to
// begin writing into.
//
// The function does not check for the length of the byte slice and will panic
// if there is not enough space to write into.
func (b *BlockKey) SerializeInto(sl *[]byte, offset int) {
	slVal := *sl
	copy(slVal[offset:], b.Hash[:])
	offset += chainhash.HashSize
	dbnamespace.ByteOrder.PutUint32(slVal[offset:], b.Height)
}

// Deserialize deserializes from a byte slice into the BlockKey it is called
// on.  It takes a passed offset to begin reading from.
//
// The function does not check for the length of the byte slice and will panic
// if there is not enough space to write into.
func (b *BlockKey) Deserialize(sl *[]byte, offset int) {
	slVal := *sl
	copy(b.Hash[:], slVal[offset:])
	offset += chainhash.HashSize
	b.Height = dbnamespace.ByteOrder.Uint32(slVal[offset:])
}

// RollingVotingPrefixTally is a rolling tally of the decoded vote bits from
// a series  of votes.  The tallies of the issues are arranged in a two
// dimensional array.
type RollingVotingPrefixTally struct {
	CurrentIntervalBlock BlockKey
	LastIntervalBlock    BlockKey
	CurrentBlockHeight   uint32
	BlockValid           uint16
	Unused               uint16
	Issues               [issuesLen]VotingTally
}

// RollingVotingPrefixTallySize is the size of a serialized
// RollingVotingPrefixTally The size is calculated as
//   2x BlockKey (72 bytes) + 1x uint32 (4 bytes) +
//   2x uint16s (4 bytes) + 7x VotingTallies (56 bytes)
const RollingVotingPrefixTallySize = 72 + 4 + 4 + 56

// Serialize serializes a RollingVotingPrefixTally into a contiguous slice of
// bytes.  Integer values are serialized in little endian.
func (r *RollingVotingPrefixTally) Serialize() []byte {
	val := make([]byte, RollingVotingPrefixTallySize)
	offset := 0
	r.CurrentIntervalBlock.SerializeInto(&val, offset)
	offset += BlockKeySize
	r.LastIntervalBlock.SerializeInto(&val, offset)
	offset += BlockKeySize

	dbnamespace.ByteOrder.PutUint32(val[offset:offset+4], r.CurrentBlockHeight)
	offset += 4
	dbnamespace.ByteOrder.PutUint16(val[offset:offset+2], r.BlockValid)
	offset += 2
	dbnamespace.ByteOrder.PutUint16(val[offset:offset+2], r.Unused)
	offset += 2

	// Serialize the issues individually; the array size
	// is 8 bytes each.
	for i := 0; i < issuesLen; i++ {
		r.Issues[i].SerializeInto(&val, offset)
		offset += 8
	}

	return val
}

// Deserialize deserializes a contiguous slice of bytes into the
// RollingVotingPrefixTally the function is called on.
func (r *RollingVotingPrefixTally) Deserialize(val []byte) error {
	if len(val) < RollingVotingPrefixTallySize {
		str := fmt.Sprintf("short read of serialized RollingVotingPrefixTally "+
			"when deserializing (got %v, want %v bytes)", len(val),
			RollingVotingPrefixTallySize)
		return stakeRuleError(ErrMemoryCorruption, str)
	}

	offset := 0
	r.CurrentIntervalBlock.Deserialize(&val, offset)
	offset += BlockKeySize
	r.LastIntervalBlock.Deserialize(&val, offset)
	offset += BlockKeySize

	r.CurrentBlockHeight = dbnamespace.ByteOrder.Uint32(val[offset : offset+4])
	offset += 4
	r.BlockValid = dbnamespace.ByteOrder.Uint16(val[offset : offset+2])
	offset += 2
	r.Unused = dbnamespace.ByteOrder.Uint16(val[offset : offset+2])
	offset += 2

	// Serialize the issues individually.  The array size
	// is 8 bytes each.
	for i := 0; i < issuesLen; i++ {
		r.Issues[i].Deserialize(&val, offset)
		offset += 8
	}

	return nil
}

// RollingVotingPrefixTallyCache is a cache of voting tallies for interval
// blocks containing intermediate tallies.  On connection of the last block
// in the interval period, the final tally is written with the key being
// the first block in the interval.
// TODO This cache stores the block key twice, to cut down on memory usage
// in the future you could reconstruct from the k->v like database does.
type RollingVotingPrefixTallyCache map[BlockKey]RollingVotingPrefixTally

// initCacheSize is how many interval windows into the past the cache should
// restore on startup from the database.  It is equivalent to 4 months on
// mainnet.
const initCacheSize = 240

// InitRollingTallyCache initializes a rolling tally cache from the stored
// tallies in the database.  It loads the best state, then restores the past
// tallies by iterating backwards over the records.
func InitRollingTallyCache(dbTx database.Tx, params *chaincfg.Params) (RollingVotingPrefixTallyCache, error) {
	best, err := votingdb.DbFetchBestState(dbTx)
	if err != nil {
		return nil, err
	}

	var bestTally RollingVotingPrefixTally
	err = bestTally.Deserialize(best.CurrentTally)
	if err != nil {
		return nil, err
	}

	// Nothing to load if this is the first time.
	if best.Hash == *params.GenesisHash {
		return make(RollingVotingPrefixTallyCache), nil
	}

	// Iterate backwards through the entries in the database, loading
	// them into the cache.
	cache := make(RollingVotingPrefixTallyCache)
	currentKey := bestTally.LastIntervalBlock
	var currentKeyB [36]byte
	buf := currentKeyB[:]
	for i := 0; i < initCacheSize; i++ {
		// Break if we reach the genesis block.
		if currentKey.Hash == *params.GenesisHash {
			break
		}

		currentKey.SerializeInto(&buf, 0)
		currentTallyB, err := votingdb.DbFetchBlockTally(dbTx, buf)
		if err != nil {
			return nil, err
		}
		var currentTally RollingVotingPrefixTally
		err = currentTally.Deserialize(currentTallyB)
		if err != nil {
			return nil, err
		}

		cache[currentKey] = currentTally
		currentKey = currentTally.LastIntervalBlock
	}

	return cache, nil
}

// rollover resets the RollingVotingPrefixTally when reaching a new tallying
// interval.  It checks to see if the new interval block height corresponds
// to the expected next block height.  If the height is also at a key block
// interval, it rolls over these values as well.  Finally, it increments the
// block height.
func (r *RollingVotingPrefixTally) rollover(blockHash chainhash.Hash, blockHeight uint32, params *chaincfg.Params) error {
	if blockHeight != r.CurrentBlockHeight+1 {
		str := fmt.Sprintf("reset called, but next block height does not "+
			"correspond to the expected next block height (got %v, expect %v)",
			blockHeight, r.CurrentBlockHeight+1)
		return stakeRuleError(ErrBadVotingConnectBlock, str)
	}

	// New vote tallying interval.  The fields are reset here rather
	// than above, because this interval should always coincide with
	// a key block interval as well.  Not that this value stays the
	// same for params.VoteKeyBlockInterval /
	// params.StakeDiffWindowSize many interval periods.
	if blockHeight%uint32(params.StakeDiffWindowSize) == 0 {
		r.LastIntervalBlock.Hash = r.CurrentIntervalBlock.Hash
		r.LastIntervalBlock.Height = r.CurrentIntervalBlock.Height
		r.CurrentIntervalBlock.Hash = blockHash
		r.CurrentIntervalBlock.Height = blockHeight

		// Reset the remaining fields of the struct.
		r.BlockValid = 0
		r.Unused = 0
		for i := 0; i < issuesLen; i++ {
			for j := 0; j < 4; j++ {
				r.Issues[i][j] = 0
			}
		}
	}

	r.CurrentBlockHeight++

	return nil
}

// AddVoteBitsSlice adds a slice of vote bits to a tally, extracting the relevant
// bits from each vote bits and then incrementing the relevant portion of the
// talley.
func (r *RollingVotingPrefixTally) AddVoteBitsSlice(voteBitsSlice []uint16) {
	for i := range voteBitsSlice {
		decoded := DecodeVoteBitsPrefix(voteBitsSlice[i])

		if decoded.BlockValid {
			r.BlockValid++
		}
		if decoded.Unused {
			r.Unused++
		}

		// Extract the setting of the issue and add it to the relevant
		// portion of the array that stores how an issue was voted.
		// This portion of code might not be clear.  decoded.Issues[j]
		// refers to 0...3, which is the length of the array for the
		// issues in the rolling tally.  By using it as an index, you
		// only increment the voting selection that the user indicated
		// in their vote bits.
		for j := 0; j < issuesLen; j++ {
			r.Issues[j][decoded.Issues[j]]++
		}
	}
}

// AddTally adds two tallies together, storing the result in the original
// tally.
func (r *RollingVotingPrefixTally) AddTally(tally RollingVotingPrefixTally) {
	r.BlockValid += tally.BlockValid
	r.Unused += tally.Unused
	for i := 0; i < issuesLen; i++ {
		for j := 0; j < 4; j++ {
			r.Issues[i][j] += tally.Issues[i][j]
		}
	}
}

var zeroKey = BlockKey{chainhash.Hash{0x00}, 0}

// FetchIntervalTally fetches a finalized interval tally from the cache or
// database.  It returns an error if it can not find the relevant tally, which
// should exist even if the block is on a sidechain.
func FetchIntervalTally(key *BlockKey, cache RollingVotingPrefixTallyCache, dbTx database.Tx, params *chaincfg.Params) (*RollingVotingPrefixTally, error) {
	// Exception for the genesis block window.
	if *key == zeroKey {
		var tally RollingVotingPrefixTally
		tally.CurrentIntervalBlock = BlockKey{*params.GenesisHash, 0}
		return &tally, nil
	}

	if cache != nil {
		tally, exists := cache[*key]
		if exists {
			return &tally, nil
		}
	}

	if dbTx != nil {
		var keyB [36]byte
		buf := keyB[:]
		key.SerializeInto(&buf, 0)
		tallyB, err := votingdb.DbFetchBlockTally(dbTx, buf)
		if err != nil && err.(votingdb.DBError).ErrorCode !=
			votingdb.ErrMissingKey {
			return nil, err
		}

		if len(tallyB) > 0 {
			var tally RollingVotingPrefixTally
			err = tally.Deserialize(tallyB)
			if err != nil {
				return nil, err
			}

			return &tally, nil
		}
	}

	return nil, stakeRuleError(ErrMissingTally, fmt.Sprintf("failed to "+
		"find tally for key %v in the interval cache or interval bucket of the "+
		"database", key))
}

// ConnectBlockToTally connects a "block" to a tally.  The only components of
// the block required are the hash, the height, the extracted 16-bit mandatory
// vote bits, and the network parameters.
//
// The work flow is as below:
//  1. rollover, which pushes the talley to the next height and resets/sets
//         relevant fields about the blockchain states.
//  2. addTally, which adds the tally of the voteBits slice.
//
// The resulting RollingVotingPrefixTally is an "immutable" object similar to
//     the stake node of tickets.go.
func (r *RollingVotingPrefixTally) ConnectBlockToTally(intervalCache RollingVotingPrefixTallyCache, dbTx database.Tx, blockHash chainhash.Hash, blockHeight uint32, voteBitsSlice []uint16, params *chaincfg.Params) (RollingVotingPrefixTally, error) {
	tally := *r

	err := tally.rollover(blockHash, blockHeight, params)
	if err != nil {
		return RollingVotingPrefixTally{}, err
	}

	// Quick sanity check.
	if int64(blockHeight) < params.StakeValidationHeight {
		if len(voteBitsSlice) > 0 {
			return RollingVotingPrefixTally{},
				stakeRuleError(ErrBadVotingConnectBlock, "got block with "+
					"votebits before stake validation height when tallying")
		}
	}
	if int64(blockHeight) >= params.StakeValidationHeight {
		if len(voteBitsSlice) <= int(params.TicketsPerBlock)/2 {
			str := fmt.Sprintf("bad number of voters attempted to be connected "+
				"to rolling tally (got %v, min %v)", len(voteBitsSlice),
				((params.TicketsPerBlock)/2)+1)
			return RollingVotingPrefixTally{},
				stakeRuleError(ErrBadVotingConnectBlock, str)
		}
	}

	tally.AddVoteBitsSlice(voteBitsSlice)

	// This is the final block in the interval window, so write it to the
	// cache now.
	if (tally.CurrentBlockHeight+1)%uint32(params.StakeDiffWindowSize) == 0 {
		intervalCache[tally.CurrentIntervalBlock] = tally
	}

	return tally, nil
}

/*
	// TODO handling of longer tallies?

	// This is the final block in the key block window, so write it to that
	// cache now after summing up the tally for the entire week.  To do so,
	// we must sum up the interval tallies from params.VoteKeyBlockInterval /
	// params.StakeDiffWindowSize many periods (14 on mainnet, corresponding
	// to one week).  The key block tally is then stored inside its respective
	// cache.
	if (tally.CurrentBlockHeight+1)%uint32(params.StakeDiffWindowSize) == 0 {
		tallySum := tally
		currentKey := &tally.LastIntervalBlock
		iterations := int(params.VoteKeyBlockInterval/
			params.StakeDiffWindowSize) - 1
		for i := 0; i < iterations-1; i++ {
			tallyLocal, err := FetchIntervalTally(currentKey, intervalCache, dbTx)
			if err != nil {
				return RollingVotingPrefixTally{}, err
			}

			tallySum.AddTally(*tallyLocal)
			currentKey = &tallyLocal.LastIntervalBlock
		}
		keyBlockCache[tally.LastKeyBlock] = tallySum
	}

*/

// SubtractVoteBitsSlice subtracts a slice of vote bits to a tally,
// extracting the relevant bits from each vote bits and then
// incrementing the relevant portion of the talley.
func (r *RollingVotingPrefixTally) SubtractVoteBitsSlice(voteBitsSlice []uint16) {
	for i := range voteBitsSlice {
		decoded := DecodeVoteBitsPrefix(voteBitsSlice[i])

		if decoded.BlockValid {
			r.BlockValid--
		}
		if decoded.Unused {
			r.Unused--
		}

		// Extract the setting of the issue and subtract it from the
		// relevant portion of the array that stores how an issue was
		// voted.
		for j := 0; j < issuesLen; j++ {
			r.Issues[j][decoded.Issues[j]]--
		}
	}
}

// revert reverts a tally to its previous state after the disconnection of a
// block.  Importantly, if the rollback is all the way to the last interval
// update to the tally where it was previously reset, it loads the data from
// the passed lastIntervalTally instead of further subtracting votes (which
// would result in underflow).
func (r *RollingVotingPrefixTally) revert(blockHeight uint32, voteBitsSlice []uint16, lastIntervalTally *RollingVotingPrefixTally, params *chaincfg.Params) error {
	if blockHeight != r.CurrentBlockHeight {
		str := fmt.Sprintf("revert called, but block height does not "+
			"correspond to the expected prev block height (got %v, expect %v)",
			blockHeight, r.CurrentBlockHeight)
		return stakeRuleError(ErrBadVotingRemoveBlock, str)
	}

	// Old short voting interval.  This interval should always coincide with
	// a key block interval as well, so we roll back to the old state of the
	// voting tally.
	if blockHeight%uint32(params.StakeDiffWindowSize) == 0 {
		*r = *lastIntervalTally
	} else {
		r.SubtractVoteBitsSlice(voteBitsSlice)
		r.CurrentBlockHeight--
	}

	return nil
}

// DisconnectBlockFromTally disconnects a "block" from a rolling tally.  In the
// simplest case, it just subtracts the individual votes on issues and then
// rolls back the height.  In cases of rolling back an interval block (where
// values for the votes were reset), it needs the previous interval tally to
// be restored.  This is passed as lastIntervalTally.  If this does not exist,
// the passed cache and the database will be searched to see if this interval
// tally can be found.
func (r *RollingVotingPrefixTally) DisconnectBlockFromTally(intervalCache RollingVotingPrefixTallyCache, dbTx database.Tx, blockHash chainhash.Hash, blockHeight uint32, voteBitsSlice []uint16, lastIntervalTally *RollingVotingPrefixTally, params *chaincfg.Params) (RollingVotingPrefixTally, error) {
	tally := *r

	// Search the cache and the database.
	if lastIntervalTally == nil {
		var err error
		fmt.Printf("r.LastIntervalBlock %v\n", r.LastIntervalBlock)
		lastIntervalTally, err = FetchIntervalTally(&r.LastIntervalBlock,
			intervalCache, dbTx, params)
		if err != nil {
			return RollingVotingPrefixTally{}, err
		}
	}

	fmt.Printf("last interval tally %v\n", lastIntervalTally)
	err := tally.revert(blockHeight, voteBitsSlice, lastIntervalTally, params)
	if err != nil {
		return RollingVotingPrefixTally{}, err
	}

	return tally, nil
}

// InitVotingDatabaseState initializes the chain with the best state being the
// genesis block.
func InitVotingDatabaseState(dbTx database.Tx, params *chaincfg.Params) (*RollingVotingPrefixTally, error) {
	// Create the database.
	err := votingdb.DbCreate(dbTx)
	if err != nil {
		return nil, err
	}

	// Write the new block undo and new tickets data to the
	// database for the genesis block.
	var best votingdb.BestChainState
	var bestTally RollingVotingPrefixTally
	best.Hash = *params.GenesisHash
	best.Height = 0
	bestTally.CurrentIntervalBlock = BlockKey{best.Hash, best.Height}
	best.CurrentTally = bestTally.Serialize()

	err = votingdb.DbPutBestState(dbTx, best)
	if err != nil {
		return nil, err
	}

	return &bestTally, nil
}

// LoadVotingDatabaseState loads the best chain state of the voting datatabase.
func LoadVotingDatabaseState(dbTx database.Tx) (*RollingVotingPrefixTally, error) {
	best, err := votingdb.DbFetchBestState(dbTx)
	if err != nil {
		return nil, err
	}

	var bestTally RollingVotingPrefixTally
	err = bestTally.Deserialize(best.CurrentTally)
	if err != nil {
		return nil, err
	}

	return &bestTally, nil
}

// WriteConnectedBlockTally writes a block tally to the database if the block is
// the last block in the block interval.  It also updates the current best block
// tally in the best chain component of the database.
func WriteConnectedBlockTally(dbTx database.Tx, blockHash chainhash.Hash, blockHeight uint32, tally *RollingVotingPrefixTally, params *chaincfg.Params) error {
	var best votingdb.BestChainState
	best.Hash = blockHash
	best.Height = blockHeight
	best.CurrentTally = tally.Serialize()

	if (tally.CurrentBlockHeight+1)%uint32(params.StakeDiffWindowSize) == 0 {
		err := votingdb.DbPutBlockTally(dbTx, best.CurrentTally)
		if err != nil {
			return err
		}
	}

	return votingdb.DbPutBestState(dbTx, best)
}

// WriteDisconnectedBlockTally disconnects a block tally from the database,
// subtracting the slice of vote bits or restoring it to the previous tally's
// state if it falls into an interval block.  We don't need to call the cache
// here, because we know that disconnects are only on the mainchain and that the
// mainchain MUST have the previous interval block in it.
func WriteDisconnectedBlockTally(dbTx database.Tx, blockHash chainhash.Hash, blockHeight uint32, tally *RollingVotingPrefixTally, voteBitsSlice []uint16, params *chaincfg.Params) error {
	disconnectedTally, err := tally.DisconnectBlockFromTally(nil, dbTx,
		blockHash, blockHeight, voteBitsSlice, nil, params)
	if err != nil {
		return err
	}

	var best votingdb.BestChainState
	best.Hash = blockHash
	best.Height = blockHeight
	best.CurrentTally = disconnectedTally.Serialize()

	// Overwrite the previous best if we're disconnecting into a
	// previous window.
	if (disconnectedTally.CurrentBlockHeight+1)%
		uint32(params.StakeDiffWindowSize) == 0 {
		err = votingdb.DbPutBlockTally(dbTx, best.CurrentTally)
		if err != nil {
			return err
		}
	}

	return votingdb.DbPutBestState(dbTx, best)
}
