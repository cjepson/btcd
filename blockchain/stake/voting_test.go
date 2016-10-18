// voting_test.go
package stake_test

import (
	"bytes"
	"encoding/hex"
	"reflect"
	"testing"

	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg/chainhash"
)

func bytesFromHex(s string) []byte {
	b, _ := hex.DecodeString(s)
	return b
}

func TestDecodingAndEncodingVoteBits(t *testing.T) {
	tests := []struct {
		name string
		in   uint16
		out  stake.DecodedVoteBitsPrefix
	}{
		{
			"no and all undefined",
			0x0000,
			stake.DecodedVoteBitsPrefix{
				BlockValid: false,
				Unused:     false,
				Issues: [7]stake.IssueVote{
					stake.IssueVoteUndefined, stake.IssueVoteUndefined,
					stake.IssueVoteUndefined, stake.IssueVoteUndefined,
					stake.IssueVoteUndefined, stake.IssueVoteUndefined,
					stake.IssueVoteUndefined,
				},
			},
		},
		{
			"yes and all undefined",
			0x0001,
			stake.DecodedVoteBitsPrefix{
				BlockValid: true,
				Unused:     false,
				Issues: [7]stake.IssueVote{
					stake.IssueVoteUndefined, stake.IssueVoteUndefined,
					stake.IssueVoteUndefined, stake.IssueVoteUndefined,
					stake.IssueVoteUndefined, stake.IssueVoteUndefined,
					stake.IssueVoteUndefined,
				},
			},
		},
		{
			"no (unused set) and all undefined",
			0x0002,
			stake.DecodedVoteBitsPrefix{
				BlockValid: false,
				Unused:     true,
				Issues: [7]stake.IssueVote{
					stake.IssueVoteUndefined, stake.IssueVoteUndefined,
					stake.IssueVoteUndefined, stake.IssueVoteUndefined,
					stake.IssueVoteUndefined, stake.IssueVoteUndefined,
					stake.IssueVoteUndefined,
				},
			},
		},
		{
			"yes and odd issues yes, even issues no",
			0x6665,
			stake.DecodedVoteBitsPrefix{
				BlockValid: true,
				Unused:     false,
				Issues: [7]stake.IssueVote{
					stake.IssueVoteYes, stake.IssueVoteNo,
					stake.IssueVoteYes, stake.IssueVoteNo,
					stake.IssueVoteYes, stake.IssueVoteNo,
					stake.IssueVoteYes,
				},
			},
		},
		{
			"yes and odd issues no, even issues abstain",
			0xBBB9,
			stake.DecodedVoteBitsPrefix{
				BlockValid: true,
				Unused:     false,
				Issues: [7]stake.IssueVote{
					stake.IssueVoteNo, stake.IssueVoteAbstain,
					stake.IssueVoteNo, stake.IssueVoteAbstain,
					stake.IssueVoteNo, stake.IssueVoteAbstain,
					stake.IssueVoteNo,
				},
			},
		},
		{
			"no and issue 1 yes, rest unused",
			0x0004,
			stake.DecodedVoteBitsPrefix{
				BlockValid: false,
				Unused:     false,
				Issues: [7]stake.IssueVote{
					stake.IssueVoteYes, stake.IssueVoteUndefined,
					stake.IssueVoteUndefined, stake.IssueVoteUndefined,
					stake.IssueVoteUndefined, stake.IssueVoteUndefined,
					stake.IssueVoteUndefined,
				},
			},
		},
	}

	// Encoding.
	for i := range tests {
		test := tests[i]
		in := stake.EncodeVoteBitsPrefix(test.out)
		if !reflect.DeepEqual(in, test.in) {
			t.Errorf("bad result on EncodeVoteBitsPrefix test %v: got %v, "+
				"want %v", test.name, in, test.in)
		}
	}

	// Decoding.
	for i := range tests {
		test := tests[i]
		out := stake.DecodeVoteBitsPrefix(test.in)
		if out != test.out {
			t.Errorf("bad result on DecodeVoteBitsPrefix test %v: got %v, "+
				"want %v", test.name, out, test.out)
		}
	}
}

// TestRollingVotingPrefixTallySerializing tests serializing and deserializing
// for RollingVotingPrefixTally.
func TestRollingVotingPrefixTallySerializing(t *testing.T) {
	tests := []struct {
		name string
		in   stake.RollingVotingPrefixTally
		out  []byte
	}{
		{
			"no and all undefined",
			stake.RollingVotingPrefixTally{
				StartBlockHash:     chainhash.Hash{byte(0x01)},
				StartBlockHeight:   99998,
				CurrentBlockHeight: 10200,
				BlockValid:         213,
				Unused:             492,
				Issues: [7]stake.VotingTally{
					stake.VotingTally{123, 321, 324, 2819},
					stake.VotingTally{523, 2355, 0, 0},
					stake.VotingTally{352, 2352, 2442, 44},
					stake.VotingTally{234, 0, 44, 344},
					stake.VotingTally{523, 223, 133, 3444},
					stake.VotingTally{0, 44, 3233, 432},
					stake.VotingTally{867, 1, 444, 33},
				},
			},
			bytesFromHex("01000000000000000000000000000000000000000000000000000000000000009e860100d8270000d500ec017b0041014401030b0b02330900000000600130098a092c00ea0000002c0058010b02df008500740d00002c00a10cb00163030100bc012100"),
		},
	}

	// Serialize.
	for i := range tests {
		test := tests[i]
		out := test.in.Serialize()
		if !bytes.Equal(out, test.out) {
			t.Errorf("bad result on test.in.Serialize(): got %x, want %x",
				out, test.out)
		}
	}

	// Deserialize.
	for i := range tests {
		test := tests[i]
		var tally stake.RollingVotingPrefixTally
		err := tally.Deserialize(test.out)
		if err != nil {
			t.Errorf("unexpected error %v")
		}
		if !reflect.DeepEqual(tally, test.in) {
			t.Errorf("bad result on tally.Deserialize() test %v: got %v, "+
				"want %v", test.name, tally, test.in)
		}
	}

	// Test short read error.
	for i := range tests {
		test := tests[i]
		var tally stake.RollingVotingPrefixTally
		err := tally.Deserialize(test.out[:len(test.out)-1])
		if err == nil ||
			err.(stake.RuleError).ErrorCode != stake.ErrMemoryCorruption {
			t.Errorf("expected ErrMemoryCorruption on test %v, got %v",
				test.name, err)
		}
	}
}
