// stakenode_test.go
package blockchain

import (
	"bytes"
	"compress/bzip2"
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrutil"
)

// stakeNodesEqual does a cursory test to ensure that data returned from the API
// for any given node is equivalent.
func stakeNodesEqual(a *stake.Node, b *stake.Node) error {
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
			"a: %v, b: %v", a.Winners(), b.Winners())
	}
	if a.FinalState() != b.FinalState() {
		return fmt.Errorf("final state were not equal between nodes; "+
			"a: %x, b: %x", a.FinalState(), b.FinalState())
	}
	if a.PoolSize() != b.PoolSize() {
		return fmt.Errorf("pool size were not equal between nodes; "+
			"a: %x, b: %x", a.PoolSize(), b.PoolSize())
	}
	if !reflect.DeepEqual(a.SpentByBlock(), b.SpentByBlock()) {
		return fmt.Errorf("spentbyblock were not equal between nodes; "+
			"a: %x, b: %x", a.SpentByBlock(), b.SpentByBlock())
	}
	if !reflect.DeepEqual(a.MissedByBlock(), b.MissedByBlock()) {
		return fmt.Errorf("missedbyblock were not equal between nodes; "+
			"a: %x, b: %x", a.MissedByBlock(), b.MissedByBlock())
	}

	return nil
}

// pruneChildrenRecursivelyTest prunes the stake data present in a child node,
// then prunes the stake data from any children of that child recursively.
func (b *BlockChain) pruneChildrenRecursivelyTest(child *blockNode) {
	child.stakeNode = nil
	child.rollingTally = nil

	for _, anotherChild := range child.children {
		b.pruneChildrenRecursivelyTest(anotherChild)
	}
}

// pruneRecursivelyTest prunes the stake data from all nodes except the best
// node.
func (b *BlockChain) pruneRecursivelyTest() {
	node := b.bestNode.parent

	for {
		if node.parent != nil {
			node = node.parent
		} else {
			break
		}

		if len(node.children) > 0 {
			for _, child := range node.children {
				if !child.inMainChain {
					b.pruneChildrenRecursivelyTest(child)
				}
			}
		}

		node.stakeNode = nil
		node.rollingTally = nil
	}
}

// fetchNodeChildrenFromNodeTest is a recursive function that searches for
// a node in the children of the passed node.  If it finds the node, it writes
// it to the passed map.
func (b *BlockChain) fetchNodeChildrenFromNodeTest(child *blockNode, allNodes map[chainhash.Hash]*blockNode) {
	allNodes[child.hash] = child
	for _, anotherChild := range child.children {
		b.fetchNodeChildrenFromNodeTest(anotherChild, allNodes)
	}
}

// fetchNodeTest is an internal testing function that scours the blockchain
// looking for a node that corresponds to the passed hash.  It returns an
// error if it fails to find the node.  Because it stores a map each time,
// it is extremely expensive and should only be used during testing.
func (b *BlockChain) fetchNodeTest(hash chainhash.Hash) (*blockNode, error) {
	allNodes := make(map[chainhash.Hash]*blockNode)
	current := b.bestNode
	for {
		if current.hash == hash {
			return current, nil
		}

		allNodes[current.hash] = current

		if len(current.children) > 0 {
			for _, child := range current.children {
				b.fetchNodeChildrenFromNodeTest(child, allNodes)
			}
		}

		if current.parent != nil {
			current = current.parent
		} else {
			var err error
			current, err = b.getPrevNodeFromNode(current)
			if err != nil {
				return nil, err
			}
		}

		if current == nil {
			break
		}
	}

	node, ok := allNodes[hash]
	if ok {
		return node, nil
	}

	return nil, fmt.Errorf("can't find node %v", hash)
}

// testPrunedStakeData tests stake ticket and tallying data from the blockchain
// and then ensures that fetches of this data still work correctly and return
// the same data as was originally set in memory before the pruning.
func (b *BlockChain) testPrunedStakeData(hashes []chainhash.Hash) error {
	// The list of hashes should be in order.  Fetch the last node and
	// go backwards, storing all the intermediate stake data to check
	// for equivalence after.
	nodeStakeData := make([]struct {
		node         *blockNode
		stakeNode    *stake.Node
		rollingTally *stake.RollingVotingPrefixTally
	}, len(hashes))

	for i := len(hashes) - 1; i >= 0; i-- {
		n, err := b.fetchNodeTest(hashes[i])
		if err != nil {
			return err
		}

		nodeStakeData[i].node = n
		nodeStakeData[i].stakeNode = n.stakeNode
		nodeStakeData[i].rollingTally = n.rollingTally
	}

	for i := len(hashes) - 1; i >= 0; i-- {
		b.pruneRecursivelyTest()

		stakeNode, err := b.fetchStakeNode(nodeStakeData[i].node)
		if err != nil {
			return err
		}
		if err = stakeNodesEqual(stakeNode,
			nodeStakeData[i].stakeNode); err != nil {
			return fmt.Errorf("got not equal stake nodes at block %v: %v, %v (%v)",
				nodeStakeData[i].node.hash, stakeNode,
				nodeStakeData[i].stakeNode, err)
		}

		tally, err := b.fetchRollingTally(nodeStakeData[i].node)
		if err != nil {
			return err
		}
		if *tally != *nodeStakeData[i].rollingTally {
			return fmt.Errorf("got not equal tallying data at block %v: %v, %v",
				nodeStakeData[i].node.hash, *tally,
				*nodeStakeData[i].rollingTally)
		}
	}

	return nil
}

// TestReorgTestLongForStakeDataEquivalence performs a long reorganization and
// ensures the correct fetching of stake data for a mainchain and its sidechain
// by calling testPrunedStakeData at various times when manipulating the
// blockchain.
func TestReorgTestLongForStakeDataEquivalence(t *testing.T) {
	// Create a new database and chain instance to run tests against.
	chain, teardownFunc, err := chainSetup("stakedataequivtests",
		TestSimNetParams)
	if err != nil {
		t.Errorf("Failed to setup chain instance: %v", err)
		return
	}
	defer teardownFunc()

	// The genesis block should fail to connect since it's already
	// inserted.
	genesisBlock := TestSimNetParams.GenesisBlock
	err = chain.CheckConnectBlock(dcrutil.NewBlock(genesisBlock))
	if err == nil {
		t.Errorf("CheckConnectBlock: Did not receive expected error")
	}

	// Load up the rest of the blocks up to HEAD.
	filename := filepath.Join("testdata/", "reorgto179.bz2")
	fi, err := os.Open(filename)
	bcStream := bzip2.NewReader(fi)
	defer fi.Close()

	// Create a buffer of the read file
	bcBuf := new(bytes.Buffer)
	bcBuf.ReadFrom(bcStream)

	// Create decoder from the buffer and a map to store the data
	bcDecoder := gob.NewDecoder(bcBuf)
	blockChain := make(map[int64][]byte)

	// Decode the blockchain into the map
	if err := bcDecoder.Decode(&blockChain); err != nil {
		t.Errorf("error decoding test blockchain: %v", err.Error())
	}

	// Load up the short chain
	timeSource := NewMedianTime()
	finalIdx1 := 179
	mainchainBlockHashes := make([]chainhash.Hash, finalIdx1)
	for i := 1; i < finalIdx1+1; i++ {
		bl, err := dcrutil.NewBlockFromBytes(blockChain[int64(i)])
		if err != nil {
			t.Fatalf("NewBlockFromBytes error: %v", err.Error())
		}
		bl.SetHeight(int64(i))

		_, _, err = chain.ProcessBlock(bl, timeSource, BFNone)
		if err != nil {
			t.Fatalf("ProcessBlock error at height %v: %v", i, err.Error())
		}

		blSha := bl.Sha()
		mainchainBlockHashes[i-1] = *blSha
	}

	// Prune the stake data and test for each block.
	err = chain.testPrunedStakeData(mainchainBlockHashes)
	if err != nil {
		t.Fatalf("fatal error on stake node add test: %v", err)
	}

	// Load the long chain and begin loading blocks from that too,
	// forcing a reorganization
	// Load up the rest of the blocks up to HEAD.
	filename = filepath.Join("testdata/", "reorgto180.bz2")
	fi, err = os.Open(filename)
	bcStream = bzip2.NewReader(fi)
	defer fi.Close()

	// Create a buffer of the read file
	bcBuf = new(bytes.Buffer)
	bcBuf.ReadFrom(bcStream)

	// Create decoder from the buffer and a map to store the data
	bcDecoder = gob.NewDecoder(bcBuf)
	blockChain = make(map[int64][]byte)

	// Decode the blockchain into the map
	if err := bcDecoder.Decode(&blockChain); err != nil {
		t.Errorf("error decoding test blockchain: %v", err.Error())
	}

	forkPoint := 131
	finalIdx2 := 180
	sidechainBlockHashes := make([]chainhash.Hash, 0)
	for i := forkPoint; i < finalIdx2+1; i++ {
		// Test pruned data for all the sidechain nodes before
		// adding the final block and forcing the reorg.
		if i == finalIdx2 {
			err = chain.testPrunedStakeData(sidechainBlockHashes)
			if err != nil {
				t.Fatalf("error %v", err)
			}
		}

		bl, err := dcrutil.NewBlockFromBytes(blockChain[int64(i)])
		if err != nil {
			t.Fatalf("NewBlockFromBytes error: %v", err.Error())
		}
		bl.SetHeight(int64(i))

		_, _, err = chain.ProcessBlock(bl, timeSource, BFNone)
		if err != nil {
			t.Fatalf("ProcessBlock error: %v", err.Error())
		}

		blSha := bl.Sha()
		sidechainBlockHashes = append(sidechainBlockHashes, *blSha)
	}

	// Ensure our blockchain is at the correct best tip
	topBlock, _ := chain.GetTopBlock()
	tipHash := topBlock.Sha()
	expected, _ := chainhash.NewHashFromStr("5ab969d0afd8295b6cd1506f2a310d" +
		"259322015c8bd5633f283a163ce0e50594")
	if *tipHash != *expected {
		t.Errorf("Failed to correctly reorg; expected tip %v, got tip %v",
			expected, tipHash)
	}
	have, err := chain.HaveBlock(expected)
	if !have {
		t.Errorf("missing tip block after reorganization test")
	}
	if err != nil {
		t.Errorf("unexpected error testing for presence of new tip block "+
			"after reorg test: %v", err)
	}

	return
}

func TestTallyingonSpoofedNodes(t *testing.T) {
	params := &chaincfg.MainNetParams

	// Create a new database and chain instance to run tests against.
	chain, teardownFunc, err := chainSetup("staketallyingtests", params)
	if err != nil {
		t.Errorf("Failed to setup chain instance: %v", err)
		return
	}
	defer teardownFunc()

	numNodes := int64(49968)
	forkHeight := int64(43056)

	// Issue 3 is Yes, while Issue 4 is No, but only by a single vote in one
	// window.
	mainchainVoteBits := func(i int64) []uint16 {
		if i >= params.StakeValidationHeight {
			switch i % 4 {
			case 0:
				return []uint16{0x0241, 0x0241, 0x0241, 0x0241, 0x0241}
			case 1:
				return []uint16{0x0241, 0x0241, 0x0241, 0x0241, 0x0241}
			case 2:
				return []uint16{0x0141, 0x0141, 0x0241, 0x0241, 0x0241}
			case 3:
				return []uint16{0x0141, 0x0141, 0x0141, 0x0241, 0x0241}
			}
		}

		return []uint16{}
	}

	// Issue 3 is Yes, while Issue 4 is Undecided, but only by a single vote
	// in one window.
	sidechainVoteBits := func(i int64) []uint16 {
		if i >= params.StakeValidationHeight {
			if i == forkHeight+3 {
				return []uint16{0x0141, 0x0141, 0x0141, 0x0241, 0x0141}
			}

			switch i % 4 {
			case 0:
				return []uint16{0x0241, 0x0241, 0x0241, 0x0241, 0x0241}
			case 1:
				return []uint16{0x0241, 0x0241, 0x0241, 0x0241, 0x0241}
			case 2:
				return []uint16{0x0141, 0x0141, 0x0241, 0x0241, 0x0241}
			case 3:
				return []uint16{0x0141, 0x0141, 0x0141, 0x0241, 0x0241}
			}
		}

		return []uint16{}
	}

	// Spoof a large number of nodes with varying voteBits settings
	// and run them forwards.
	var mainchainBest, sidechainBest *blockNode
	for i := int64(1); i < numNodes; i++ {
		// Make up a header.
		header := wire.BlockHeader{
			Version:   1,
			PrevBlock: chain.bestNode.hash,
			Height:    uint32(i),
			Nonce:     uint32(0),
		}

		// Make up a node hash.
		hB, err := header.Bytes()
		if err != nil {
			t.Fatalf("serialization err %v")
		}
		headerHash := chainhash.HashFuncH(hB)

		thisNode := new(blockNode)
		thisNode.header = header
		thisNode.hash = headerHash
		thisNode.height = i
		thisNode.parent = chain.bestNode
		thisNode.inMainChain = true
		thisNode.voteBitsSlice = mainchainVoteBits(i)
		chain.bestNode.children = append(chain.bestNode.children, thisNode)

		thisNode.rollingTally, err = chain.fetchRollingTally(thisNode)
		if err != nil {
			t.Fatalf("failure fetching mainchain tally %v", err)
		}

		if thisNode.height == forkHeight {
			sidechainBest = thisNode
		}

		chain.bestNode = thisNode
		mainchainBest = thisNode
	}

	mainchainVerdicts, err :=
		mainchainBest.rollingTally.GenerateVotingResults(chain.rollingTallyCache,
			nil, params.VotingIntervals, &chaincfg.MainNetParams)
	if err != nil {
		t.Fatalf("failed generating verdicts %v", err)
	}

	// Generate a side chain and attempt to get the correctly set
	// voteBits from there.  Store the results, then drop all the
	// block nodes.  After the block nodes are gone, recreate the
	// sidechain and ensure that the results are the same.
	for i := forkHeight + 1; i < numNodes; i++ {
		// Make up a header.
		header := wire.BlockHeader{
			Version:   1,
			PrevBlock: sidechainBest.hash,
			Height:    uint32(i),
			Nonce:     uint32(1),
		}

		// Make up a node hash.
		hB, err := header.Bytes()
		if err != nil {
			t.Fatalf("serialization err %v")
		}
		headerHash := chainhash.HashFuncH(hB)

		thisNode := new(blockNode)
		thisNode.header = header
		thisNode.hash = headerHash
		thisNode.height = i
		thisNode.parent = sidechainBest
		thisNode.inMainChain = false
		thisNode.voteBitsSlice = sidechainVoteBits(i)
		chain.bestNode.children = append(sidechainBest.children, thisNode)

		thisNode.rollingTally, err = chain.fetchRollingTally(thisNode)
		if err != nil {
			t.Fatalf("failure fetching mainchain tally %v", err)
		}

		if (i+1)%params.StakeDiffWindowSize == 0 {
			sidechainVerdicts, err :=
				thisNode.rollingTally.GenerateVotingResults(
					chain.rollingTallyCache, nil, 1, &chaincfg.MainNetParams)
			if err != nil {
				t.Fatalf("failed generating verdicts %v", err)
			}
			t.Errorf("height %v verdicts %v", i, sidechainVerdicts.Verdict)
		}

		sidechainBest = thisNode
	}

	sidechainVerdicts, err :=
		sidechainBest.rollingTally.GenerateVotingResults(chain.rollingTallyCache,
			nil, params.VotingIntervals, &chaincfg.MainNetParams)
	if err != nil {
		t.Fatalf("failed generating verdicts %v", err)
	}

	if !reflect.DeepEqual(mainchainVerdicts, sidechainVerdicts) {
		t.Errorf("got %v  want %v", mainchainVerdicts.Verdict,
			sidechainVerdicts.Verdict)
	}

	// Prune recursively and see if it can restore correctly.
	chain.pruneRecursivelyTest()
	mainchainVerdicts, err =
		mainchainBest.rollingTally.GenerateVotingResults(chain.rollingTallyCache,
			nil, params.VotingIntervals, &chaincfg.MainNetParams)
	if err != nil {
		t.Fatalf("failed generating verdicts %v", err)
	}
	sidechainVerdicts, err =
		sidechainBest.rollingTally.GenerateVotingResults(chain.rollingTallyCache,
			nil, params.VotingIntervals, &chaincfg.MainNetParams)
	if err != nil {
		t.Fatalf("failed generating verdicts %v", err)
	}

	if !reflect.DeepEqual(mainchainVerdicts, sidechainVerdicts) {
		t.Errorf("got %v  want %v", mainchainVerdicts.Verdict,
			sidechainVerdicts.Verdict)
	}
}