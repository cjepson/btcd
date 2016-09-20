// Copyright (c) 2015-2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package stake_test

import (
	"bytes"
	"compress/bzip2"
	"encoding/gob"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/database"
	_ "github.com/decred/dcrd/database/ffldb"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrutil"
)

const (
	// testDbType is the database backend type to use for the tests.
	testDbType = "ffldb"

	// testDbRoot is the root directory used to create all test databases.
	testDbRoot = "testdbs"
)

// ticketsInBlock finds all the new tickets in the block.
func ticketsInBlock(bl *dcrutil.Block) []chainhash.Hash {
	tickets := make([]chainhash.Hash, 0)
	for _, stx := range bl.STransactions() {
		if stake.DetermineTxType(stx) == stake.TxTypeSStx {
			h := stx.Sha()
			tickets = append(tickets, *h)
		}
	}

	return tickets
}

// ticketsSpentInBlock finds all the tickets spent in the block.
func ticketsSpentInBlock(bl *dcrutil.Block) []chainhash.Hash {
	tickets := make([]chainhash.Hash, 0)
	for _, stx := range bl.STransactions() {
		if stake.DetermineTxType(stx) == stake.TxTypeSSGen {
			tickets = append(tickets, stx.MsgTx().TxIn[1].PreviousOutPoint.Hash)
		}
	}

	return tickets
}

// votesInBlock finds all the votes in the block.
func votesInBlock(bl *dcrutil.Block) []chainhash.Hash {
	votes := make([]chainhash.Hash, 0)
	for _, stx := range bl.STransactions() {
		if stake.DetermineTxType(stx) == stake.TxTypeSSGen {
			h := stx.Sha()
			votes = append(votes, *h)
		}
	}

	return votes
}

// revokedTicketsInBlock finds all the revoked tickets in the block.
func revokedTicketsInBlock(bl *dcrutil.Block) []chainhash.Hash {
	tickets := make([]chainhash.Hash, 0)
	for _, stx := range bl.STransactions() {
		if stake.DetermineTxType(stx) == stake.TxTypeSSRtx {
			tickets = append(tickets, stx.MsgTx().TxIn[0].PreviousOutPoint.Hash)
		}
	}

	return tickets
}

// nodesEqual does a cursory test to ensure that data returned from the API
// for any given node is equivalent.
func nodesEqual(a *stake.Node, b *stake.Node) error {
	if !reflect.DeepEqual(a.LiveTickets(), b.LiveTickets()) {
		return fmt.Errorf("live tickets were not equal between nodes; "+
			"a: %v, b: %v", len(a.LiveTickets()), len(b.LiveTickets()))
	}
	if !reflect.DeepEqual(a.MissedTickets(), b.MissedTickets()) {
		return fmt.Errorf("missed tickets were not equal between nodes; "+
			"a: %v, b: %v", len(a.MissedTickets()), len(b.MissedTickets()))
	}
	if !reflect.DeepEqual(a.RevokedTickets(), b.RevokedTickets()) {
		return fmt.Errorf("revoked tickets were not equal between nodes; "+
			"a: %v, b: %v", len(a.RevokedTickets()), len(b.RevokedTickets()))
	}
	if !reflect.DeepEqual(a.NewTickets(), b.NewTickets()) {
		return fmt.Errorf("new tickets were not equal between nodes; "+
			"a: %v, b: %v", len(a.NewTickets()), len(b.NewTickets()))
	}
	if !reflect.DeepEqual(a.UndoData(), b.UndoData()) {
		return fmt.Errorf("undo data were not equal between nodes; "+
			"a: %v, b: %v", len(a.UndoData()), len(b.UndoData()))
	}
	if !reflect.DeepEqual(a.Winners(), b.Winners()) {
		return fmt.Errorf("winners were not equal between nodes; "+
			"a: %v, b: %v", len(a.Winners()), len(b.Winners()))
	}
	if a.FinalState() != b.FinalState() {
		return fmt.Errorf("final state were not equal between nodes; "+
			"a: %x, b: %x", a.FinalState(), b.FinalState())
	}

	return nil
}

func TestTicketDB(t *testing.T) {
	// Declare some useful variables
	testBCHeight := int64(168)
	filename := filepath.Join("..", "/../blockchain/testdata", "blocks0to168.bz2")
	fi, err := os.Open(filename)
	bcStream := bzip2.NewReader(fi)
	defer fi.Close()

	// Create a buffer of the read file
	bcBuf := new(bytes.Buffer)
	bcBuf.ReadFrom(bcStream)

	// Create decoder from the buffer and a map to store the data
	bcDecoder := gob.NewDecoder(bcBuf)
	testBlockchainBytes := make(map[int64][]byte)

	// Decode the blockchain into the map
	if err := bcDecoder.Decode(&testBlockchainBytes); err != nil {
		t.Errorf("error decoding test blockchain")
	}
	testBlockchain := make(map[int64]*dcrutil.Block, len(testBlockchainBytes))
	for k, v := range testBlockchainBytes {
		bl, err := dcrutil.NewBlockFromBytes(v)
		if err != nil {
			t.Fatalf("couldn't decode block")
		}

		testBlockchain[k] = bl
	}

	// Create a new database to store the accepted stake node data into.
	dbName := "ffldb_staketest"
	dbPath := filepath.Join(testDbRoot, dbName)
	_ = os.RemoveAll(dbPath)
	testDb, err := database.Create(testDbType, dbPath, simNetParams.Net)
	if err != nil {
		t.Fatalf("error creating db: %v", err)
	}

	// Setup a teardown.
	defer os.RemoveAll(dbPath)
	defer os.RemoveAll(testDbRoot)
	defer testDb.Close()

	// Load the genesis block.
	var bestNode *stake.Node
	err = testDb.Update(func(dbTx database.Tx) error {
		var errLocal error
		bestNode, errLocal = stake.InitDatabaseState(dbTx, simNetParams)
		if errLocal != nil {
			return errLocal
		}

		return nil
	})
	if err != nil {
		t.Fatalf(err.Error())
	}

	// Cache all of our nodes so that we can check them when we start
	// disconnecting and going backwards through the blockchain.
	nodesForward := make([]*stake.Node, testBCHeight+1)
	loadedNodesForward := make([]*stake.Node, testBCHeight+1)
	nodesForward[0] = bestNode
	loadedNodesForward[0] = bestNode
	err = testDb.Update(func(dbTx database.Tx) error {
		for i := int64(1); i <= testBCHeight; i++ {
			block := testBlockchain[i]
			ticketsToAdd := make([]chainhash.Hash, 0)
			if i >= simNetParams.StakeEnabledHeight {
				matureHeight := (i - int64(simNetParams.TicketMaturity))
				ticketsToAdd = ticketsInBlock(testBlockchain[matureHeight])
			}
			header := block.MsgBlock().Header
			if int(header.PoolSize) != len(bestNode.LiveTickets()) {
				t.Errorf("bad number of live tickets: want %v, got %v",
					header.PoolSize, len(bestNode.LiveTickets()))
			}
			if header.FinalState != bestNode.FinalState() {
				t.Errorf("bad final state: want %x, got %x",
					header.FinalState, bestNode.FinalState())
			}

			// In memory addition test.
			bestNode, err = bestNode.ConnectNode(header,
				ticketsSpentInBlock(block), revokedTicketsInBlock(block),
				ticketsToAdd)
			if err != nil {
				return fmt.Errorf("couldn't connect node: %v", err.Error())
			}

			// Write the new node to db.
			nodesForward[i] = bestNode
			blockSha := block.Sha()
			err := stake.WriteConnectedBestNode(dbTx, bestNode, *blockSha)
			if err != nil {
				return fmt.Errorf("failure writing the best node: %v",
					err.Error())
			}

			// Reload the node from DB and make sure it's the same.
			blockHash := block.Sha()
			loadedNode, err := stake.LoadBestNode(dbTx, bestNode.Height(),
				*blockHash, header, simNetParams)
			if err != nil {
				return fmt.Errorf("failed to load the best node: %v",
					err.Error())
			}
			err = nodesEqual(loadedNode, bestNode)
			if err != nil {
				return fmt.Errorf("loaded best node was not same as "+
					"in memory best node: %v", err.Error())
			}
			loadedNodesForward[i] = loadedNode
		}

		return nil
	})
	if err != nil {
		t.Fatalf(err.Error())
	}

	nodesBackward := make([]*stake.Node, testBCHeight+1)
	nodesBackward[testBCHeight] = bestNode
	for i := testBCHeight; i >= int64(1); i-- {
		parentBlock := testBlockchain[i-1]
		ticketsToAdd := make([]chainhash.Hash, 0)
		if i >= simNetParams.StakeEnabledHeight {
			matureHeight := (i - 1 - int64(simNetParams.TicketMaturity))
			ticketsToAdd = ticketsInBlock(testBlockchain[matureHeight])
		}
		header := parentBlock.MsgBlock().Header
		blockUndoData := nodesForward[i-1].UndoData()
		formerBestNode := bestNode

		// In memory disconnection test.
		bestNode, err = bestNode.DisconnectNode(header, blockUndoData,
			ticketsToAdd, nil)
		if err != nil {
			t.Fatalf(err.Error())
		}

		err = nodesEqual(bestNode, nodesForward[i-1])
		if err != nil {
			t.Fatalf("non-equiv stake nodes: %v", err.Error())
		}

		// Try again using the database instead of the in memory
		// data to disconnect the node, too.
		var bestNodeUsingDB *stake.Node
		err = testDb.View(func(dbTx database.Tx) error {
			bestNodeUsingDB, err = formerBestNode.DisconnectNode(header, nil,
				nil, dbTx)
			if err != nil {
				return err
			}

			return nil
		})
		if err != nil {
			t.Errorf("couldn't disconnect using the database: %v",
				err.Error())
		}
		err = nodesEqual(bestNode, bestNodeUsingDB)
		if err != nil {
			t.Errorf("non-equiv stake nodes using db when disconnecting: %v",
				err.Error())
		}

		// Write the new best node to the database.
		nodesBackward[i-1] = bestNode
		err = testDb.Update(func(dbTx database.Tx) error {
			nodesForward[i] = bestNode
			parentBlockSha := parentBlock.Sha()
			err := stake.WriteDisconnectedBestNode(dbTx, bestNode,
				*parentBlockSha, formerBestNode.UndoData())
			if err != nil {
				return fmt.Errorf("failure writing the best node: %v",
					err.Error())
			}

			return nil
		})
		if err != nil {
			t.Errorf("%s", err.Error())
		}

		// Check the best node against the loaded best node from
		// the database after.
		err = testDb.View(func(dbTx database.Tx) error {
			parentBlockHash := parentBlock.Sha()
			loadedNode, err := stake.LoadBestNode(dbTx, bestNode.Height(),
				*parentBlockHash, header, simNetParams)
			if err != nil {
				return fmt.Errorf("failed to load the best node: %v",
					err.Error())
			}
			err = nodesEqual(loadedNode, bestNode)
			if err != nil {
				return fmt.Errorf("loaded best node was not same as "+
					"in memory best node: %v", err.Error())
			}
			err = nodesEqual(loadedNode, loadedNodesForward[i-1])
			if err != nil {
				return fmt.Errorf("loaded best node was not same as "+
					"in memory best node: %v", err.Error())
			}

			return nil
		})
		if err != nil {
			t.Errorf("%s", err.Error())
		}
	}

}

// --------------------------------------------------------------------------------
// TESTING VARIABLES BEGIN HERE

// simNetPowLimit is the highest proof of work value a Decred block
// can have for the simulation test network.  It is the value 2^255 - 1.
var simNetPowLimit = new(big.Int).Sub(new(big.Int).Lsh(bigOne, 255), bigOne)

// SimNetParams defines the network parameters for the simulation test Decred
// network.  This network is similar to the normal test network except it is
// intended for private use within a group of individuals doing simulation
// testing.  The functionality is intended to differ in that the only nodes
// which are specifically specified are used to create the network rather than
// following normal discovery rules.  This is important as otherwise it would
// just turn into another public testnet.
var simNetParams = &chaincfg.Params{
	Name:        "simnet",
	Net:         wire.SimNet,
	DefaultPort: "18555",

	// Chain parameters
	GenesisBlock:             &simNetGenesisBlock,
	GenesisHash:              &simNetGenesisHash,
	CurrentBlockVersion:      0,
	PowLimit:                 simNetPowLimit,
	PowLimitBits:             0x207fffff,
	ResetMinDifficulty:       false,
	GenerateSupported:        true,
	MaximumBlockSize:         1000000,
	TimePerBlock:             time.Second * 1,
	WorkDiffAlpha:            1,
	WorkDiffWindowSize:       8,
	WorkDiffWindows:          4,
	TargetTimespan:           time.Second * 1 * 8, // TimePerBlock * WindowSize
	RetargetAdjustmentFactor: 4,

	// Subsidy parameters.
	BaseSubsidy:           50000000000,
	MulSubsidy:            100,
	DivSubsidy:            101,
	ReductionInterval:     128,
	WorkRewardProportion:  6,
	StakeRewardProportion: 3,
	BlockTaxProportion:    1,

	// Checkpoints ordered from oldest to newest.
	Checkpoints: nil,

	// Mempool parameters
	RelayNonStdTxs: true,

	// Address encoding magics
	PubKeyAddrID:     [2]byte{0x27, 0x6f}, // starts with Sk
	PubKeyHashAddrID: [2]byte{0x0e, 0x91}, // starts with Ss
	PKHEdwardsAddrID: [2]byte{0x0e, 0x71}, // starts with Se
	PKHSchnorrAddrID: [2]byte{0x0e, 0x53}, // starts with SS
	ScriptHashAddrID: [2]byte{0x0e, 0x6c}, // starts with Sc
	PrivateKeyID:     [2]byte{0x23, 0x07}, // starts with Ps

	// BIP32 hierarchical deterministic extended key magics
	HDPrivateKeyID: [4]byte{0x04, 0x20, 0xb9, 0x03}, // starts with sprv
	HDPublicKeyID:  [4]byte{0x04, 0x20, 0xbd, 0x3d}, // starts with spub

	// BIP44 coin type used in the hierarchical deterministic path for
	// address generation.
	HDCoinType: 115, // ASCII for s

	// Decred PoS parameters
	MinimumStakeDiff:      20000,
	TicketPoolSize:        64,
	TicketsPerBlock:       5,
	TicketMaturity:        16,
	TicketExpiry:          256, // 4*TicketPoolSize
	CoinbaseMaturity:      16,
	SStxChangeMaturity:    1,
	TicketPoolSizeWeight:  4,
	StakeDiffAlpha:        1,
	StakeDiffWindowSize:   8,
	StakeDiffWindows:      8,
	MaxFreshStakePerBlock: 40,            // 8*TicketsPerBlock
	StakeEnabledHeight:    16 + 16,       // CoinbaseMaturity + TicketMaturity
	StakeValidationHeight: 16 + (64 * 2), // CoinbaseMaturity + TicketPoolSize*2
	StakeBaseSigScript:    []byte{0xDE, 0xAD, 0xBE, 0xEF},

	// Decred organization related parameters
	//
	// "Dev org" address is a 3-of-3 P2SH going to wallet:
	// aardvark adroitness aardvark adroitness
	// aardvark adroitness aardvark adroitness
	// aardvark adroitness aardvark adroitness
	// aardvark adroitness aardvark adroitness
	// aardvark adroitness aardvark adroitness
	// aardvark adroitness aardvark adroitness
	// aardvark adroitness aardvark adroitness
	// aardvark adroitness aardvark adroitness
	// briefcase
	// (seed 0x00000000000000000000000000000000000000000000000000000000000000)
	//
	// This same wallet owns the three ledger outputs for simnet.
	//
	// P2SH details for simnet dev org is below.
	//
	// address: Scc4ZC844nzuZCXsCFXUBXTLks2mD6psWom
	// redeemScript: 532103e8c60c7336744c8dcc7b85c27789950fc52aa4e48f895ebbfb
	// ac383ab893fc4c2103ff9afc246e0921e37d12e17d8296ca06a8f92a07fbe7857ed1d4
	// f0f5d94e988f21033ed09c7fa8b83ed53e6f2c57c5fa99ed2230c0d38edf53c0340d0f
	// c2e79c725a53ae
	//   (3-of-3 multisig)
	// Pubkeys used:
	//   SkQmxbeuEFDByPoTj41TtXat8tWySVuYUQpd4fuNNyUx51tF1csSs
	//   SkQn8ervNvAUEX5Ua3Lwjc6BAuTXRznDoDzsyxgjYqX58znY7w9e4
	//   SkQkfkHZeBbMW8129tZ3KspEh1XBFC1btbkgzs6cjSyPbrgxzsKqk
	//
	OrganizationAddress: "ScuQxvveKGfpG1ypt6u27F99Anf7EW3cqhq",
	BlockOneLedger:      BlockOneLedgerSimNet,
}

// BlockOneLedgerSimNet is the block one output ledger for the simulation
// network. See below under "Decred organization related parameters" for
// information on how to spend these outputs.
var BlockOneLedgerSimNet = []*chaincfg.TokenPayout{
	{Address: "Sshw6S86G2bV6W32cbc7EhtFy8f93rU6pae", Amount: 100000 * 1e8},
	{Address: "SsjXRK6Xz6CFuBt6PugBvrkdAa4xGbcZ18w", Amount: 100000 * 1e8},
	{Address: "SsfXiYkYkCoo31CuVQw428N6wWKus2ZEw5X", Amount: 100000 * 1e8},
}

var bigOne = new(big.Int).SetInt64(1)

// simNetGenesisHash is the hash of the first block in the block chain for the
// simulation test network.
var simNetGenesisHash = simNetGenesisBlock.BlockSha()

// simNetGenesisMerkleRoot is the hash of the first transaction in the genesis
// block for the simulation test network.  It is the same as the merkle root for
// the main network.
var simNetGenesisMerkleRoot = genesisMerkleRoot

// genesisCoinbaseTx legacy is the coinbase transaction for the genesis blocks for
// the regression test network and test network.
var genesisCoinbaseTxLegacy = wire.MsgTx{
	Version: 1,
	TxIn: []*wire.TxIn{
		{
			PreviousOutPoint: wire.OutPoint{
				Hash:  chainhash.Hash{},
				Index: 0xffffffff,
			},
			SignatureScript: []byte{
				0x04, 0xff, 0xff, 0x00, 0x1d, 0x01, 0x04, 0x45, /* |.......E| */
				0x54, 0x68, 0x65, 0x20, 0x54, 0x69, 0x6d, 0x65, /* |The Time| */
				0x73, 0x20, 0x30, 0x33, 0x2f, 0x4a, 0x61, 0x6e, /* |s 03/Jan| */
				0x2f, 0x32, 0x30, 0x30, 0x39, 0x20, 0x43, 0x68, /* |/2009 Ch| */
				0x61, 0x6e, 0x63, 0x65, 0x6c, 0x6c, 0x6f, 0x72, /* |ancellor| */
				0x20, 0x6f, 0x6e, 0x20, 0x62, 0x72, 0x69, 0x6e, /* | on brin| */
				0x6b, 0x20, 0x6f, 0x66, 0x20, 0x73, 0x65, 0x63, /* |k of sec|*/
				0x6f, 0x6e, 0x64, 0x20, 0x62, 0x61, 0x69, 0x6c, /* |ond bail| */
				0x6f, 0x75, 0x74, 0x20, 0x66, 0x6f, 0x72, 0x20, /* |out for |*/
				0x62, 0x61, 0x6e, 0x6b, 0x73, /* |banks| */
			},
			Sequence: 0xffffffff,
		},
	},
	TxOut: []*wire.TxOut{
		{
			Value: 0x00000000,
			PkScript: []byte{
				0x41, 0x04, 0x67, 0x8a, 0xfd, 0xb0, 0xfe, 0x55, /* |A.g....U| */
				0x48, 0x27, 0x19, 0x67, 0xf1, 0xa6, 0x71, 0x30, /* |H'.g..q0| */
				0xb7, 0x10, 0x5c, 0xd6, 0xa8, 0x28, 0xe0, 0x39, /* |..\..(.9| */
				0x09, 0xa6, 0x79, 0x62, 0xe0, 0xea, 0x1f, 0x61, /* |..yb...a| */
				0xde, 0xb6, 0x49, 0xf6, 0xbc, 0x3f, 0x4c, 0xef, /* |..I..?L.| */
				0x38, 0xc4, 0xf3, 0x55, 0x04, 0xe5, 0x1e, 0xc1, /* |8..U....| */
				0x12, 0xde, 0x5c, 0x38, 0x4d, 0xf7, 0xba, 0x0b, /* |..\8M...| */
				0x8d, 0x57, 0x8a, 0x4c, 0x70, 0x2b, 0x6b, 0xf1, /* |.W.Lp+k.| */
				0x1d, 0x5f, 0xac, /* |._.| */
			},
		},
	},
	LockTime: 0,
	Expiry:   0,
}

// genesisMerkleRoot is the hash of the first transaction in the genesis block
// for the main network.
var genesisMerkleRoot = genesisCoinbaseTxLegacy.TxSha()

var regTestGenesisCoinbaseTx = wire.MsgTx{
	Version: 1,
	TxIn: []*wire.TxIn{
		{
			PreviousOutPoint: wire.OutPoint{
				Hash:  chainhash.Hash{},
				Index: 0xffffffff,
			},
			SignatureScript: []byte{
				0x04, 0xff, 0xff, 0x00, 0x1d, 0x01, 0x04, 0x45, /* |.......E| */
				0x54, 0x68, 0x65, 0x20, 0x54, 0x69, 0x6d, 0x65, /* |The Time| */
				0x73, 0x20, 0x30, 0x33, 0x2f, 0x4a, 0x61, 0x6e, /* |s 03/Jan| */
				0x2f, 0x32, 0x30, 0x30, 0x39, 0x20, 0x43, 0x68, /* |/2009 Ch| */
				0x61, 0x6e, 0x63, 0x65, 0x6c, 0x6c, 0x6f, 0x72, /* |ancellor| */
				0x20, 0x6f, 0x6e, 0x20, 0x62, 0x72, 0x69, 0x6e, /* | on brin| */
				0x6b, 0x20, 0x6f, 0x66, 0x20, 0x73, 0x65, 0x63, /* |k of sec|*/
				0x6f, 0x6e, 0x64, 0x20, 0x62, 0x61, 0x69, 0x6c, /* |ond bail| */
				0x6f, 0x75, 0x74, 0x20, 0x66, 0x6f, 0x72, 0x20, /* |out for |*/
				0x62, 0x61, 0x6e, 0x6b, 0x73, /* |banks| */
			},
			Sequence: 0xffffffff,
		},
	},
	TxOut: []*wire.TxOut{
		{
			Value:   0x00000000,
			Version: 0x0000,
			PkScript: []byte{
				0x41, 0x04, 0x67, 0x8a, 0xfd, 0xb0, 0xfe, 0x55, /* |A.g....U| */
				0x48, 0x27, 0x19, 0x67, 0xf1, 0xa6, 0x71, 0x30, /* |H'.g..q0| */
				0xb7, 0x10, 0x5c, 0xd6, 0xa8, 0x28, 0xe0, 0x39, /* |..\..(.9| */
				0x09, 0xa6, 0x79, 0x62, 0xe0, 0xea, 0x1f, 0x61, /* |..yb...a| */
				0xde, 0xb6, 0x49, 0xf6, 0xbc, 0x3f, 0x4c, 0xef, /* |..I..?L.| */
				0x38, 0xc4, 0xf3, 0x55, 0x04, 0xe5, 0x1e, 0xc1, /* |8..U....| */
				0x12, 0xde, 0x5c, 0x38, 0x4d, 0xf7, 0xba, 0x0b, /* |..\8M...| */
				0x8d, 0x57, 0x8a, 0x4c, 0x70, 0x2b, 0x6b, 0xf1, /* |.W.Lp+k.| */
				0x1d, 0x5f, 0xac, /* |._.| */
			},
		},
	},
	LockTime: 0,
	Expiry:   0,
}

// simNetGenesisBlock defines the genesis block of the block chain which serves
// as the public transaction ledger for the simulation test network.
var simNetGenesisBlock = wire.MsgBlock{
	Header: wire.BlockHeader{
		Version: 1,
		PrevBlock: chainhash.Hash([chainhash.HashSize]byte{ // Make go vet happy.
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		}),
		MerkleRoot: simNetGenesisMerkleRoot,
		StakeRoot: chainhash.Hash([chainhash.HashSize]byte{ // Make go vet happy.
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		}),
		VoteBits:    uint16(0x0000),
		FinalState:  [6]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		Voters:      uint16(0x0000),
		FreshStake:  uint8(0x00),
		Revocations: uint8(0x00),
		Timestamp:   time.Unix(1401292357, 0), // 2009-01-08 20:54:25 -0600 CST
		PoolSize:    uint32(0),
		Bits:        0x207fffff, // 545259519
		SBits:       int64(0x0000000000000000),
		Nonce:       0x00000000,
		Height:      uint32(0),
	},
	Transactions:  []*wire.MsgTx{&regTestGenesisCoinbaseTx},
	STransactions: []*wire.MsgTx{},
}
