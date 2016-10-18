// Copyright (c) 2015-2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package stake

import (
	"encoding/binary"
	"fmt"

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
		binary.LittleEndian.PutUint16(slVal[offset+i:offset+i+2], v[i/2])
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
		v[i/2] = binary.LittleEndian.Uint16(slVal[offset+i : offset+i+2])
	}
}

// RollingPrefixTally is a rolling tally of the decoded vote bits from a series
// of votes.  The tallies of the issues are arranged in a two dimensionaly
// array.
type RollingVotingPrefixTally struct {
	StartBlockHash     chainhash.Hash
	StartBlockHeight   uint32
	CurrentBlockHeight uint32
	BlockValid         uint16
	Unused             uint16
	Issues             [7]VotingTally
}

// RollingVotingPrefixTallySize is the size of a serialized
// RollingVotingPrefixTally The size is calculated as
//   chainhash.HashSize (32 bytes) + 2x uint32s (8 bytes) +
//   2x uint16s (4 bytes) + 7x VotingTallies (56 bytes)
const RollingVotingPrefixTallySize = chainhash.HashSize + 4 + 4 + 2 + 2 + 56

// Serialize serializes a RollingVotingPrefixTally into a contiguous slice of
// bytes.  Integer values are serialized in little endian.
func (r *RollingVotingPrefixTally) Serialize() []byte {
	val := make([]byte, RollingVotingPrefixTallySize)
	offset := 0
	copy(val[offset:], r.StartBlockHash[:])
	offset += chainhash.HashSize

	binary.LittleEndian.PutUint32(val[offset:offset+4], r.StartBlockHeight)
	offset += 4
	binary.LittleEndian.PutUint32(val[offset:offset+4], r.CurrentBlockHeight)
	offset += 4
	binary.LittleEndian.PutUint16(val[offset:offset+2], r.BlockValid)
	offset += 2
	binary.LittleEndian.PutUint16(val[offset:offset+2], r.Unused)
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
	copy(r.StartBlockHash[:], val[offset:])
	offset += chainhash.HashSize

	r.StartBlockHeight = binary.LittleEndian.Uint32(val[offset : offset+4])
	offset += 4
	r.CurrentBlockHeight = binary.LittleEndian.Uint32(val[offset : offset+4])
	offset += 4
	r.BlockValid = binary.LittleEndian.Uint16(val[offset : offset+2])
	offset += 2
	r.Unused = binary.LittleEndian.Uint16(val[offset : offset+2])
	offset += 2

	// Serialize the issues individually; the array size
	// is 8 bytes each.
	for i := 0; i < 7; i++ {
		r.Issues[i].Deserialize(&val, offset)
		offset += 8
	}

	return nil
}
