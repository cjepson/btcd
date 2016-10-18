// voting_test.go
package stake_test

import (
	"testing"

	"github.com/decred/dcrd/blockchain/stake"
)

func TestDecodingVoteBits(t *testing.T) {
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

	for i := range tests {
		test := tests[i]
		out := stake.DecodeVoteBitsPrefix(test.in)
		if out != test.out {
			t.Errorf("bad result on DecodeVoteBitsPrefix: got %v, want %v",
				out, test.out)
		}
	}
}
