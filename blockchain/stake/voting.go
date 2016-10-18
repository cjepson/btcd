// Copyright (c) 2015-2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package stake

import (
	"fmt"

	"github.com/decred/dcrd/blockchain/stake/internal/dbnamespace"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
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
	Issues     [7]IssueVote
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
	for i := uint16(0); i < 7; i++ {
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
	Issues             [7]VotingTally
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
	for i := 0; i < 7; i++ {
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
	for i := 0; i < 7; i++ {
		r.Issues[i].Deserialize(&val, offset)
		offset += 8
	}

	return nil
}

// rollover resets the RollingVotingPrefixTally when reaching a new tallying
// interval.  It checks to see if the new interval block height corresponds
// to the expected next block height.  If the height is also at a key block
// interval, it rolls over these values as well.  Finally, it increments the
// block height.
func (r *RollingVotingPrefixTally) rollover(blockHash chainhash.Hash, blockHeight uint32, params *chaincfg.Params) error {
	if blockHeight != r.CurrentBlockHeight+1 {
		return fmt.Errorf("reset called, but next block height does not "+
			"correspond to the expected next block height (got %v, expect %v)",
			blockHeight, r.CurrentBlockHeight+1)
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
		for i := 0; i < 7; i++ {
			for j := 0; j < 4; j++ {
				r.Issues[i][j] = 0
			}
		}
	}

	r.CurrentBlockHeight++

	return nil
}

// ConnectBlockToTally
func (r *RollingVotingPrefixTally) ConnectBlockToTally(blockHash chainhash.Hash, blockHeight uint32, voteBitsSlice []uint16, params *chaincfg.Params) {

}

// DisconnectBlockFromTally
func (r *RollingVotingPrefixTally) DisconnectBlockFromTally(blockHash chainhash.Hash, blockHeight uint32, voteBitsSlice []uint16, parentTally RollingVotingPrefixTally, params *chaincfg.Params) {

}

// RollingVotingPrefixTallyCache
type RollingVotingPrefixTallyCache map[BlockKey]RollingVotingPrefixTally

// InitRollingTallyCache
func InitRollingTallyCache(bestBlockHeight int64, lastKeyBlockHash chainhash.Hash, lastKeyBlockHeight uint32) {

}

// WriteConnectedBlockTally

// WriteDisconnectedBlockTally
