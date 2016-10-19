// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package stake

import (
	"fmt"

	"github.com/decred/dcrd/blockchain/stake/internal/dbnamespace"
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
	LastKeyBlock       BlockKey
	LastIntervalBlock  BlockKey
	CurrentBlockHeight uint32
	BlockValid         uint16
	Unused             uint16
	Issues             [issuesLen]VotingTally
}

// RollingVotingPrefixTallySize is the size of a serialized
// RollingVotingPrefixTally The size is calculated as
//   2x BlockKey (72 bytes) + 1x uint32s (4 bytes) +
//   2x uint16s (4 bytes) + 7x VotingTallies (56 bytes)
const RollingVotingPrefixTallySize = 72 + 4 + 4 + 56

// Serialize serializes a RollingVotingPrefixTally into a contiguous slice of
// bytes.  Integer values are serialized in little endian.
func (r *RollingVotingPrefixTally) Serialize() []byte {
	val := make([]byte, RollingVotingPrefixTallySize)
	offset := 0
	r.LastKeyBlock.SerializeInto(&val, offset)
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
	r.LastKeyBlock.Deserialize(&val, offset)
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
type RollingVotingPrefixTallyCache map[BlockKey]RollingVotingPrefixTally

// InitRollingTallyCache
func InitRollingTallyCache(dbTx database.Tx, bestBlockHeight int64, lastKeyBlockHash chainhash.Hash, lastKeyBlockHeight uint32) {

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

	// New key block interval.
	if blockHeight%uint32(params.VoteKeyBlockInterval) == 0 {
		r.LastKeyBlock.Hash = blockHash
		r.LastKeyBlock.Height = blockHeight
	}

	// New short voting interval.  The fields are reset here rather
	// than above, because this interval should always coincide with
	// a key block interval as well.
	if blockHeight%uint32(params.StakeDiffWindowSize) == 0 {
		r.LastIntervalBlock.Hash = blockHash
		r.LastIntervalBlock.Height = blockHeight

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

// AddTally adds a slice of vote bits to a tally, extracting the relevant bits
// from each vote bits and then incrementing the relevant portion of the talley.
func (r *RollingVotingPrefixTally) AddTally(voteBitsSlice []uint16) {
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
func (r *RollingVotingPrefixTally) ConnectBlockToTally(cache RollingVotingPrefixTallyCache, blockHash chainhash.Hash, blockHeight uint32, voteBitsSlice []uint16, params *chaincfg.Params) (RollingVotingPrefixTally, error) {
	tally := *r

	err := tally.rollover(blockHash, blockHeight, params)
	if err != nil {
		return RollingVotingPrefixTally{}, err
	}

	// Quick sanity check.
	if len(voteBitsSlice) <= int(params.TicketsPerBlock)/2 {
		str := fmt.Sprintf("bad number of voters attempted to be connected "+
			"to rolling tally (got %v, min %v)", len(voteBitsSlice),
			((params.TicketsPerBlock)/2)+1)
		return RollingVotingPrefixTally{},
			stakeRuleError(ErrBadVotingConnectBlock, str)
	}

	tally.AddTally(voteBitsSlice)

	// This is the final block in the interval window, so write it to the
	// cache now.
	if (tally.CurrentBlockHeight+1)%uint32(params.StakeDiffWindowSize) == 0 {
		cache[BlockKey{Hash: blockHash, Height: uint32(blockHeight)}] =
			tally
	}

	return tally, nil
}

// SubtractTally subtracts a slice of vote bits to a tally, extracting the
// relevant bits from each vote bits and then incrementing the relevant
// portion of the talley.
func (r *RollingVotingPrefixTally) SubtractTally(voteBitsSlice []uint16) {
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
	if blockHeight != r.CurrentBlockHeight-1 {
		str := fmt.Sprintf("revert called, but next block height does not "+
			"correspond to the expected prev block height (got %v, expect %v)",
			blockHeight, r.CurrentBlockHeight-1)
		return stakeRuleError(ErrBadVotingRemoveBlock, str)
	}

	// Old short voting interval.  This interval should always coincide with
	// a key block interval as well, so we roll back to the old state of the
	// voting tally.
	if blockHeight%uint32(params.StakeDiffWindowSize) == 0 {
		*r = *lastIntervalTally
	} else {
		r.SubtractTally(voteBitsSlice)
	}

	r.CurrentBlockHeight--

	return nil
}

// DisconnectBlockFromTally
func (r *RollingVotingPrefixTally) DisconnectBlockFromTally(cache RollingVotingPrefixTallyCache, dbTx database.Tx, blockHash chainhash.Hash, blockHeight uint32, voteBitsSlice []uint16, lastIntervalTally *RollingVotingPrefixTally, params *chaincfg.Params) (RollingVotingPrefixTally, error) {
	tally := *r

	// Search the cache.
	if lastIntervalTally == nil {
		fromCache, exists := cache[tally.LastIntervalBlock]
		if exists {
			lastIntervalTally = &fromCache
		}
	}

	// Search the database.
	if lastIntervalTally == nil {
		// TODO
	}

	// If we still can't find it, return an error.
	if lastIntervalTally == nil {
		// TODO
	}

	err := tally.revert(blockHeight, voteBitsSlice, lastIntervalTally, params)
	if err != nil {
		return RollingVotingPrefixTally{}, err
	}

	return tally, nil
}

// WriteConnectedBlockTally

// WriteDisconnectedBlockTally
