// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package stake

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/database"
	_ "github.com/decred/dcrd/database/ffldb"
)

func bytesFromHex(s string) []byte {
	b, _ := hex.DecodeString(s)
	return b
}

func TestDecodingAndEncodingVoteBits(t *testing.T) {
	tests := []struct {
		name string
		in   uint16
		out  DecodedVoteBitsPrefix
	}{
		{
			"no and all undefined",
			0x0000,
			DecodedVoteBitsPrefix{
				BlockValid: false,
				Unused:     false,
				Issues: [7]IssueVote{
					IssueVoteUndefined, IssueVoteUndefined,
					IssueVoteUndefined, IssueVoteUndefined,
					IssueVoteUndefined, IssueVoteUndefined,
					IssueVoteUndefined,
				},
			},
		},
		{
			"yes and all undefined",
			0x0001,
			DecodedVoteBitsPrefix{
				BlockValid: true,
				Unused:     false,
				Issues: [7]IssueVote{
					IssueVoteUndefined, IssueVoteUndefined,
					IssueVoteUndefined, IssueVoteUndefined,
					IssueVoteUndefined, IssueVoteUndefined,
					IssueVoteUndefined,
				},
			},
		},
		{
			"no (unused set) and all undefined",
			0x0002,
			DecodedVoteBitsPrefix{
				BlockValid: false,
				Unused:     true,
				Issues: [7]IssueVote{
					IssueVoteUndefined, IssueVoteUndefined,
					IssueVoteUndefined, IssueVoteUndefined,
					IssueVoteUndefined, IssueVoteUndefined,
					IssueVoteUndefined,
				},
			},
		},
		{
			"yes and odd issues yes, even issues no",
			0x6665,
			DecodedVoteBitsPrefix{
				BlockValid: true,
				Unused:     false,
				Issues: [7]IssueVote{
					IssueVoteYes, IssueVoteNo,
					IssueVoteYes, IssueVoteNo,
					IssueVoteYes, IssueVoteNo,
					IssueVoteYes,
				},
			},
		},
		{
			"yes and odd issues no, even issues abstain",
			0xBBB9,
			DecodedVoteBitsPrefix{
				BlockValid: true,
				Unused:     false,
				Issues: [7]IssueVote{
					IssueVoteNo, IssueVoteAbstain,
					IssueVoteNo, IssueVoteAbstain,
					IssueVoteNo, IssueVoteAbstain,
					IssueVoteNo,
				},
			},
		},
		{
			"no and issue 1 yes, rest unused",
			0x0004,
			DecodedVoteBitsPrefix{
				BlockValid: false,
				Unused:     false,
				Issues: [7]IssueVote{
					IssueVoteYes, IssueVoteUndefined,
					IssueVoteUndefined, IssueVoteUndefined,
					IssueVoteUndefined, IssueVoteUndefined,
					IssueVoteUndefined,
				},
			},
		},
	}

	// Encoding.
	for i := range tests {
		test := tests[i]
		in := EncodeVoteBitsPrefix(test.out)
		if !reflect.DeepEqual(in, test.in) {
			t.Errorf("bad result on EncodeVoteBitsPrefix test %v: got %v, "+
				"want %v", test.name, in, test.in)
		}
	}

	// Decoding.
	for i := range tests {
		test := tests[i]
		out := DecodeVoteBitsPrefix(test.in)
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
		in   RollingVotingPrefixTally
		out  []byte
	}{
		{
			"no and all undefined",
			RollingVotingPrefixTally{
				CurrentIntervalBlock: BlockKey{Hash: chainhash.Hash{byte(0x01)}, Height: 38829},
				LastIntervalBlock:    BlockKey{Hash: chainhash.Hash{byte(0x02)}, Height: 38859},
				CurrentBlockHeight:   10200,
				BlockValid:           213,
				Unused:               492,
				Issues: [7]VotingTally{
					VotingTally{123, 321, 324, 2819},
					VotingTally{523, 2355, 0, 0},
					VotingTally{352, 2352, 2442, 44},
					VotingTally{234, 0, 44, 344},
					VotingTally{523, 223, 133, 3444},
					VotingTally{0, 44, 3233, 432},
					VotingTally{867, 1, 444, 33},
				},
			},
			bytesFromHex("0100000000000000000000000000000000000000000000000000000000000000ad9700000200000000000000000000000000000000000000000000000000000000000000cb970000d8270000d500ec017b0041014401030b0b02330900000000600130098a092c00ea0000002c0058010b02df008500740d00002c00a10cb00163030100bc012100"),
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
		var tally RollingVotingPrefixTally
		err := tally.Deserialize(test.out)
		if err != nil {
			t.Errorf("unexpected error %v", err)
		}
		if !reflect.DeepEqual(tally, test.in) {
			t.Errorf("bad result on tally.Deserialize() test %v: got %v, "+
				"want %v", test.name, tally, test.in)
		}
	}

	// Test short read error.
	for i := range tests {
		test := tests[i]
		var tally RollingVotingPrefixTally
		err := tally.Deserialize(test.out[:len(test.out)-1])
		if err == nil ||
			err.(RuleError).ErrorCode != ErrMemoryCorruption {
			t.Errorf("expected ErrMemoryCorruption on test %v, got %v",
				test.name, err)
		}
	}
}

// TestBitsSliceAddingAndSubstracting tests adding and then substracting some vote
// bits to/from a tally.
func TestBitsSliceAddingAndSubstracting(t *testing.T) {
	tests := []struct {
		name     string
		tally    RollingVotingPrefixTally
		votebits []uint16
		out      RollingVotingPrefixTally
	}{
		{
			"simple addition of 5 votebits",
			RollingVotingPrefixTally{
				CurrentIntervalBlock: BlockKey{Hash: chainhash.Hash{byte(0x01)}, Height: 38829},
				LastIntervalBlock:    BlockKey{Hash: chainhash.Hash{byte(0x02)}, Height: 38859},
				CurrentBlockHeight:   10200,
				BlockValid:           6,
				Unused:               7,
				Issues: [7]VotingTally{
					VotingTally{5, 4, 3, 2},
					VotingTally{5, 4, 3, 2},
					VotingTally{5, 4, 3, 2},
					VotingTally{5, 4, 3, 2},
					VotingTally{5, 4, 3, 2},
					VotingTally{5, 4, 3, 2},
					VotingTally{5, 4, 3, 2},
				},
			},
			// Add 3x "yes and odd issues yes, even issues no", 1x "yes and odd issues no, even issues abstain", 1x "yes and unused yes, rest undeclared"
			// Total:
			//   +5 BlockValid
			//   +1 Unused
			//   +3 Yes on all odd issues
			//   +3 No on all even issues
			//   +1 No on all odd issues
			//   +1 Abstain on all even issues
			[]uint16{0x6665, 0xBBB9, 0x0003, 0x6665, 0x6665},
			RollingVotingPrefixTally{
				CurrentIntervalBlock: BlockKey{Hash: chainhash.Hash{byte(0x01)}, Height: 38829},
				LastIntervalBlock:    BlockKey{Hash: chainhash.Hash{byte(0x02)}, Height: 38859},
				CurrentBlockHeight:   10200,
				BlockValid:           6 + 5,
				Unused:               7 + 1,
				Issues: [7]VotingTally{
					VotingTally{5 + 1, 4 + 3, 3 + 1, 2}, // #1
					VotingTally{5 + 1, 4, 3 + 3, 2 + 1}, // #2
					VotingTally{5 + 1, 4 + 3, 3 + 1, 2}, // #3
					VotingTally{5 + 1, 4, 3 + 3, 2 + 1}, // #4
					VotingTally{5 + 1, 4 + 3, 3 + 1, 2}, // #5
					VotingTally{5 + 1, 4, 3 + 3, 2 + 1}, // #6
					VotingTally{5 + 1, 4 + 3, 3 + 1, 2}, // #7
				},
			},
		},
	}

	for i := range tests {
		testTally := tests[i].tally
		testTally.AddVoteBitsSlice(tests[i].votebits)
		if !reflect.DeepEqual(testTally, tests[i].out) {
			t.Errorf("bad result on AddVoteBitsSlice test %v: got %v, "+
				"want %v", tests[i].name, testTally, tests[i].out)
		}

		testTally.SubtractVoteBitsSlice(tests[i].votebits)
		if !reflect.DeepEqual(testTally, tests[i].tally) {
			t.Errorf("bad result on SubtractVoteBitsSlice test %v: got %v, "+
				"want %v", tests[i].name, testTally, tests[i].tally)
		}
	}
}

// TestAddingTallies tests adding and then substracting some vote
// bits to/from a tally.
func TestAddingTallies(t *testing.T) {
	tests := []struct {
		name   string
		tally1 RollingVotingPrefixTally
		tally2 RollingVotingPrefixTally
		out    RollingVotingPrefixTally
	}{
		{
			"simple addition of 1 or 2 to every field",
			RollingVotingPrefixTally{
				CurrentIntervalBlock: BlockKey{Hash: chainhash.Hash{byte(0x01)}, Height: 38829},
				LastIntervalBlock:    BlockKey{Hash: chainhash.Hash{byte(0x02)}, Height: 38859},
				CurrentBlockHeight:   10200,
				BlockValid:           6,
				Unused:               7,
				Issues: [7]VotingTally{
					VotingTally{5, 4, 3, 2},
					VotingTally{5, 4, 3, 2},
					VotingTally{5, 4, 3, 2},
					VotingTally{5, 4, 3, 2},
					VotingTally{5, 4, 3, 2},
					VotingTally{5, 4, 3, 2},
					VotingTally{5, 4, 3, 2},
				},
			},
			RollingVotingPrefixTally{
				CurrentIntervalBlock: BlockKey{Hash: chainhash.Hash{byte(0x01)}, Height: 38829},
				LastIntervalBlock:    BlockKey{Hash: chainhash.Hash{byte(0x02)}, Height: 38859},
				CurrentBlockHeight:   10200,
				BlockValid:           1,
				Unused:               2,
				Issues: [7]VotingTally{
					VotingTally{1, 2, 1, 2},
					VotingTally{2, 1, 2, 1},
					VotingTally{1, 2, 1, 2},
					VotingTally{2, 1, 2, 1},
					VotingTally{1, 2, 1, 2},
					VotingTally{2, 1, 2, 1},
					VotingTally{1, 2, 1, 2},
				},
			},
			RollingVotingPrefixTally{
				CurrentIntervalBlock: BlockKey{Hash: chainhash.Hash{byte(0x01)}, Height: 38829},
				LastIntervalBlock:    BlockKey{Hash: chainhash.Hash{byte(0x02)}, Height: 38859},
				CurrentBlockHeight:   10200,
				BlockValid:           6 + 1,
				Unused:               7 + 2,
				Issues: [7]VotingTally{
					VotingTally{5 + 1, 4 + 2, 3 + 1, 2 + 2},
					VotingTally{5 + 2, 4 + 1, 3 + 2, 2 + 1},
					VotingTally{5 + 1, 4 + 2, 3 + 1, 2 + 2},
					VotingTally{5 + 2, 4 + 1, 3 + 2, 2 + 1},
					VotingTally{5 + 1, 4 + 2, 3 + 1, 2 + 2},
					VotingTally{5 + 2, 4 + 1, 3 + 2, 2 + 1},
					VotingTally{5 + 1, 4 + 2, 3 + 1, 2 + 2},
				},
			},
		},
	}

	for i := range tests {
		testTally := tests[i].tally1
		testTally.AddTally(tests[i].tally2)
		if !reflect.DeepEqual(testTally, tests[i].out) {
			t.Errorf("bad result on addTally test %v: got %v, "+
				"want %v", tests[i].name, testTally, tests[i].out)
		}
	}
}

// numBlocks is the number of "blocks" to add/remove below, including the
// genesis block which is automatically skipped.
const numBlocks = 1 + 99999

// numTallies is the number of tallies to store and assess for correctness.
const numTallies = numBlocks - 1

// TestVotingDbAndSpoofedChain tests block connection, disconnect, and
// a spoofed blockchain.
func TestVotingDbAndSpoofedChain(t *testing.T) {
	// Setup the database.
	dbName := "ffldb_votingtest"
	dbPath := filepath.Join(testDbRoot, dbName)
	_ = os.RemoveAll(dbPath)
	testDb, err := database.Create(testDbType, dbPath, chaincfg.MainNetParams.Net)
	if err != nil {
		t.Fatalf("error creating db: %v", err)
	}

	// Setup a teardown.
	defer os.RemoveAll(dbPath)
	defer os.RemoveAll(testDbRoot)
	defer testDb.Close()

	// Create the buckets and best state for the genesis
	// block.
	var tally *RollingVotingPrefixTally
	err = testDb.Update(func(dbTx database.Tx) error {
		tally, err = InitVotingDatabaseState(dbTx, &chaincfg.MainNetParams)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		t.Fatalf("error initializing voting db: %v", err)
	}

	// Load the cache.
	var cache RollingVotingPrefixTallyCache
	err = testDb.View(func(dbTx database.Tx) error {
		cache, err = InitRollingTallyCache(dbTx, &chaincfg.MainNetParams)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		t.Fatalf("error initializing cache: %v", err)
	}

	// Start adding some "blocks".
	bestTally := *tally
	var talliesForward [numTallies]RollingVotingPrefixTally
	vbSlice := []uint16{}
	err = testDb.Update(func(dbTx database.Tx) error {
		for i := 1; i < numBlocks; i++ {
			if int64(i) >= chaincfg.MainNetParams.StakeValidationHeight {
				vbSlice = []uint16{0x6665, 0x6665, 0x6665, 0x6665, 0x6665}
			}

			bestTally, err = bestTally.ConnectBlockToTally(cache, dbTx,
				chainhash.Hash{byte(i)}, uint32(i), vbSlice,
				&chaincfg.MainNetParams)
			if err != nil {
				return err
			}

			talliesForward[i-1] = bestTally

			err = WriteConnectedBlockTally(dbTx, chainhash.Hash{byte(i)},
				uint32(i), &bestTally, &chaincfg.MainNetParams)
			if err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		t.Errorf("unexpected error adding blocks: %v", err)
	}

	// Go backwards, seeing if the state can be reverted.
	var talliesBackward [numTallies]RollingVotingPrefixTally
	err = testDb.Update(func(dbTx database.Tx) error {
		for i := numBlocks - 1; i >= 1; i-- {
			if int64(i) < chaincfg.MainNetParams.StakeValidationHeight {
				vbSlice = []uint16{}
			}
			bestTallyCopy := bestTally
			talliesBackward[i-1] = bestTally

			bestTally, err = bestTally.DisconnectBlockFromTally(cache, dbTx,
				chainhash.Hash{byte(i)}, uint32(i), vbSlice, nil,
				&chaincfg.MainNetParams)
			if err != nil {
				return err
			}

			err = WriteDisconnectedBlockTally(dbTx, chainhash.Hash{byte(i)},
				uint32(i), &bestTallyCopy, vbSlice, &chaincfg.MainNetParams)
			if err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		t.Errorf("unexpected error removing blocks: %v", err)
	}

	for i := range talliesForward {
		if talliesForward[i] != talliesBackward[i] {
			t.Errorf("non-equivalent disconnection tallies at height %v:"+
				" backward %v, forward %v", i, talliesBackward[i],
				talliesForward[i])
		}
	}
}
