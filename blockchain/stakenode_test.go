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
	"github.com/decred/dcrd/chaincfg/chainhash"
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

// pruneChildrenRecursivelyTest
func (b *BlockChain) pruneChildrenRecursivelyTest(child *blockNode) {
	// fmt.Printf("prune sidechain child node %v h %v of stake data\n", child.hash, child.height)
	child.stakeNode = nil
	child.rollingTally = nil

	for _, anotherChild := range child.children {
		b.pruneChildrenRecursivelyTest(anotherChild)
	}
}

// pruneFromTopBlock
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

// fetchNodeChildrenFromNodeTest
func (b *BlockChain) fetchNodeChildrenFromNodeTest(child *blockNode, allNodes map[chainhash.Hash]*blockNode) {
	//fmt.Printf("fetch children for hash %v\n", child.hash)
	allNodes[child.hash] = child
	for _, anotherChild := range child.children {
		b.fetchNodeChildrenFromNodeTest(anotherChild, allNodes)
	}
}

// fetchNode
func (b *BlockChain) fetchNodeTest(hash chainhash.Hash) (*blockNode, error) {
	//fmt.Printf("fetch node %v\n", hash)
	allNodes := make(map[chainhash.Hash]*blockNode)

	current := b.bestNode
	for {
		//fmt.Printf("current iterate thru %v (parent %v)\n", current.hash, current.parent)
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
			//fmt.Printf("getPrevNodeFromNode %v %v (genesis %v)\n", current, err, b.chainParams.GenesisHash)
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

// testPrunedStakeData
func (b *BlockChain) testPrunedStakeData(hashes []chainhash.Hash) error {
	// The list of hashes should be in order.  Fetch the last node and
	// go backwards, storing all the intermediate stake data to check
	// for equivalence after.
	nodeStakeData := make([]struct {
		node      *blockNode
		stakeNode *stake.Node
		//stakeUndoData stake.UndoTicketDataSlice
		rollingTally *stake.RollingVotingPrefixTally
	}, len(hashes))

	for i := len(hashes) - 1; i >= 0; i-- {
		// fmt.Printf("doing this for %v (%v)\n", hashes[i], i)
		n, err := b.fetchNodeTest(hashes[i])
		if err != nil {
			return err
		}

		// fmt.Printf("nodeStakeData[i].stakeNode %v\n", nodeStakeData[i].stakeNode)

		nodeStakeData[i].node = n
		nodeStakeData[i].stakeNode = n.stakeNode
		//nodeStakeData[i].stakeUndoData = n.stakeUndoData
		nodeStakeData[i].rollingTally = n.rollingTally
	}

	for i := len(hashes) - 1; i >= 0; i-- {
		//fmt.Printf("fetch node %v height %v\n", node.hash, i)
		b.pruneRecursivelyTest()

		stakeNode, err := b.fetchStakeNode(nodeStakeData[i].node)
		if err != nil {
			return err
		}
		// fmt.Printf("i %v\n", i)
		if err = stakeNodesEqual(stakeNode,
			nodeStakeData[i].stakeNode); err != nil {
			return fmt.Errorf("got not equal stake nodes at block %v: %v, %v (%v)",
				nodeStakeData[i].node.hash, stakeNode,
				nodeStakeData[i].stakeNode, err)
		}

		// The stake undo data should now be set correctly, too.
		/*
			if !reflect.DeepEqual(nodeStakeData[i].node.stakeUndoData,
				nodeStakeData[i].stakeUndoData) {
				return fmt.Errorf("got not equal stake undo data at block %v: %v, %v",
					nodeStakeData[i].node.hash, nodeStakeData[i].node.stakeUndoData,
					nodeStakeData[i].stakeUndoData)
			}
		*/

		tally, err := b.fetchRollingTally(nodeStakeData[i].node)
		if err != nil {
			return err
		}
		if *tally != *nodeStakeData[i].rollingTally {
			return fmt.Errorf("got not equal tallying data at block %v: %v, %v",
				nodeStakeData[i].node.hash, *tally,
				*nodeStakeData[i].rollingTally)
		}
		//fmt.Printf("tally got %v\n", tally)

	}

	return nil
}

// TestReorgTestLongForStakeDataEquivalence does a single, large reorganization.
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
	//fmt.Printf("genesis sha %v\n", genesisBlock.Transactions[0].TxSha())
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

		//fmt.Printf("put node %v (best %v) mainchain %v orphan %v\n", blSha, chain.stateSnapshot.Hash, mainchain, orphan)
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

		// var mainchain, orphan bool
		_, _, err = chain.ProcessBlock(bl, timeSource, BFNone)
		if err != nil {
			t.Fatalf("ProcessBlock error: %v", err.Error())
		}

		blSha := bl.Sha()
		sidechainBlockHashes = append(sidechainBlockHashes, *blSha)
		// fmt.Printf("put node %v (best %v) mainchain %v orphan %v\n", blSha, chain.stateSnapshot.Hash, mainchain, orphan)
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
