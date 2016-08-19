// Copyright (c) 2013-2016 The btcsuite developers
// Copyright (c) 2015-2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package blockchain

import (
	"math/big"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/database"
	"github.com/decred/dcrutil"
)

// topNodeHeight returns the height of the highest node present in the map of
// fetched nodes from our peer.
func (b *BlockChain) fastSyncTopNodeHeight() int64 {
	bestHeight := int64(0)
	for height, _ := range b.latestNodesFastSync {
		if height > bestHeight {
			bestHeight = height
		}
	}

	return bestHeight
}

// findFastSyncNodeParent
func (b *BlockChain) fastSyncFindNode(hash *chainhash.Hash) (bool, *blockNode) {
	/*
		b.latestNodesFastSyncLock.RLock()
		best, ok := b.latestNodesFastSync[b.fastSyncTopNodeHeight()]
		if !ok {
			return false, nil
		}
		b.latestNodesFastSyncLock.RUnlock()

		// Try to find the node.
		searchDepth := mainChainCacheSize * 2
		traversed := 0
		thisNode := best
		for {
			// The path is broken due to asynchronous
			// insertion of blocks.
			if thisNode.parent == nil {
				break
			}

			// Found the node we're looking for.
			if *thisNode.parent.hash == *best.hash {
				return true, thisNode.parent
			}

			// We've searched too many nodes, abort.
			if traversed > searchDepth {
				break
			}
		}
	*/
	b.latestNodesFastSyncLock.RLock()
	defer b.latestNodesFastSyncLock.RUnlock()
	for _, n := range b.latestNodesFastSync {
		if *n.hash == *hash {
			return true, n
		}
	}

	return false, nil
}

// fastSyncTraceNodeToCurrentHeight attempts to trace a whole, unbroken path
// between the passed node and the main chain. It will only look
// mainchainBlockCacheSize*2 many blocks into the past to do so. If the path to
// the main chain is incomplete or runs through mainchainBlockCacheSize*2 many
// blocks, it will return a bool indicating that the path is broken or absent
// (false). If the path exists, it will return the length of the path.
func (b *BlockChain) fastSyncTraceNodeToCurrentHeight(node *blockNode) (bool, []*blockNode) {
	// log.Infof("Try to find path for node %v", node.hash)
	best := b.bestNode

	searchDepth := mainChainCacheSize * 2
	traversed := 0
	thisNode := node
	foundBest := false
	path := make([]*blockNode, 0, mainChainCacheSize)
	for {
		// The path is broken due to asynchronous
		// insertion of blocks.
		if node.parent == nil {
			// log.Infof("FAIL to find path parent of %v nil!", node.hash)
			break
		}

		// We've searched too many nodes, abort.
		if traversed > searchDepth {
			break
		}

		// Path is complete, append the last node and
		// break.
		if *thisNode.parent.hash == *best.hash {
			// log.Infof("FOUND COMPLETE PATH!")
			path = append(path, thisNode)
			foundBest = true
			break
		}

		// Insert the node into the path and step
		// back in history.
		path = append(path, thisNode)
		thisNode = thisNode.parent
		traversed++
	}

	// Reverse the path so that it is in ascending order.
	for i := len(path)/2 - 1; i >= 0; i-- {
		opp := len(path) - 1 - i
		path[i], path[opp] = path[opp], path[i]
	}

	return foundBest, path
}

// fastImportNodes takes a slice of deserialized blocks arranged by height
// and rapidly inserts them into the database. It first inserts all UTXs, then
// combines all the updates for STXs into a unified update and writes them after.
//
// GOTTA GO FAST
func (b *BlockChain) fastImportNodes(blocks []*dcrutil.Block, nodes []*blockNode) error {
	view := NewUtxoViewpoint()
	err := b.db.Update(func(dbTx database.Tx) error {
		curTotalTxns := b.stateSnapshot.TotalTxns
		curTotalSubsidy := b.stateSnapshot.TotalSubsidy
		formerBestHeight := b.stateSnapshot.Height
		finalBlockWork := new(big.Int).Set(b.bestNode.workSum)
		view.SetBestHash(b.bestNode.hash)
		view.SetStakeViewpoint(ViewpointPrevValidInitial)

		// We have the first block, fetch it and use it to calculate
		// the spend outputs for the first block.
		initialParent, err := b.getBlockFromHash(b.bestNode.hash)
		if err != nil {
			return err
		}
		parent := initialParent

		/*
			stxoCount := 0
			for i := range blocks {
				// We need to fetch the first parent manually, but we
				// have all the rest of the parents in the passed slice.
				if i > 0 {
					parent = blocks[i-1]
				}
				stxoCount += countSpentOutputs(blocks[i], parent)
			}
			stxos := make([]spentTxOut, 0, stxoCount)
		*/

		// Parse and create the UTXs. Jam them into the database.
		parent = initialParent
		for i, block := range blocks {
			work := CalcWork(block.MsgBlock().Header.Bits)
			finalBlockWork.Add(finalBlockWork, work)

			curTotalTxns += countNumberOfTransactions(block, parent)
			curTotalSubsidy += CalculateAddedSubsidy(block, parent)
			stxoCount := countSpentOutputs(block, parent)
			stxos := make([]spentTxOut, 0, stxoCount)

			err := b.checkConnectBlock(nodes[i], block, view, &stxos)
			if err != nil {
				return err
			}

			// Add the block hash and height to the block index which tracks
			// the main chain.
			err = dbPutBlockIndex(dbTx, block.Sha(), formerBestHeight+int64(i)+1)
			if err != nil {
				return err
			}

			// Insert the block into the database if it's not already there.
			err = dbMaybeStoreBlock(dbTx, block)
			if err != nil {
				return err
			}

			// Update the utxo set using the state of the utxo view.  This
			// entails removing all of the utxos spent and adding the new
			// ones created by the block.
			err = dbPutUtxoView(dbTx, view)
			if err != nil {
				return err
			}

			// Update the transaction spend journal by adding a record for
			// the block that contains all txos spent by it.
			lenBlocks := len(blocks)
			err = dbPutSpendJournalEntry(dbTx, blocks[lenBlocks-1].Sha(), stxos)
			if err != nil {
				return err
			}

			// Allow the index manager to call each of the currently active
			// optional indexes with the block being connected so they can
			// update themselves accordingly.
			//
			// TODO fast import version of this too
			if b.indexManager != nil {
				err := b.indexManager.ConnectBlock(dbTx, block, parent, view)
				if err != nil {
					return err
				}
			}

			// Insert the last block as the "best node" with its cumulative
			// work.
			b.bestNode = nodes[len(nodes)-1]

			numTxns := countNumberOfTransactions(blocks[lenBlocks-1],
				blocks[lenBlocks-2])
			blockSize := uint64(blocks[lenBlocks-1].MsgBlock().Header.Size)
			state := newBestState(b.bestNode, blockSize, numTxns, curTotalTxns,
				curTotalSubsidy)
			err = dbPutBestState(dbTx, state, finalBlockWork)
			if err != nil {
				return err
			}

			b.stateLock.Lock()
			b.stateSnapshot = state
			b.stateLock.Unlock()

			// put it in
			view.commit()

			parent = blocks[i]
		}

		return nil
	})
	if err != nil {
		return err
	}

	// dump all the old blocks from the node cache, since they're now added
	for i := blocks[0].Height(); i < blocks[len(blocks)-1].Height(); i++ {
		delete(b.latestNodesFastSync, i)
	}

	return nil
}
