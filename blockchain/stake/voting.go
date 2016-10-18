// Copyright (c) 2015-2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package stake

// import "fmt"
import "github.com/decred/dcrd/chaincfg/chainhash"

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

// RollingPrefixTally is a rolling tally of the decoded vote bits from a series
// of votes.  The tallies of the issues are arranged in a two dimensionaly
// array.
type RollingVotingPrefixTally struct {
	StartBlockHash   chainhash.Hash
	StartBlockHeight int64
	CurrentBlock     int64
	BlockValid       uint16
	Unused           uint16
	Issues           [7]VotingTally
}
