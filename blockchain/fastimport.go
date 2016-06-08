// fastimport.go
package blockchain

import (
	//	"fmt"
	"math/big"

	"github.com/btcsuite/btcd/database"
	//"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
)

// FastImportBlocks takes a slice of deserialized blocks arranged by height
// and rapidly inserts them into the database. It first inserts all UTXs, then
// combines all the updates for STXs into a unified update and writes them after.
//
// GOTTA GO FAST
func (b *BlockChain) FastImportBlocks(blocks []*btcutil.Block) error {
	b.chainLock.Lock()
	defer b.chainLock.Unlock()

	view := NewUtxoViewpoint()
	err := b.db.Update(func(dbTx database.Tx) error {
		curTotalTxns := b.stateSnapshot.TotalTxns
		formerBestHeight := b.stateSnapshot.Height
		finalBlockWork := new(big.Int).Set(b.bestNode.workSum)
		view.SetBestHash(b.bestNode.hash)

		stxoCount := 0
		for _, block := range blocks {
			stxoCount += countSpentOutputs(block)
		}
		stxos := make([]spentTxOut, 0, stxoCount)

		// Parse and create the UTXs. Jam them into the database.
		for i, block := range blocks {
			work := CalcWork(block.MsgBlock().Header.Bits)
			finalBlockWork.Add(finalBlockWork, work)

			curTotalTxns += uint64(len(block.MsgBlock().Transactions))

			err := view.fetchInputUtxos(b.db, block)
			if err != nil {
				return err
			}
			err = view.connectTransactions(block, &stxos)
			if err != nil {
				return err
			}

			// Add the block hash and height to the block index which tracks
			// the main chain.
			err = dbPutBlockIndex(dbTx, block.Sha(), formerBestHeight+int32(i)+1)
			if err != nil {
				return err
			}

			// Insert the block into the database if it's not already there.
			err = dbMaybeStoreBlock(dbTx, block)
			if err != nil {
				return err
			}

			// Allow the index manager to call each of the currently active
			// optional indexes with the block being connected so they can
			// update themselves accordingly.
			//
			// TODO fast import version of this too
			if b.indexManager != nil {
				err := b.indexManager.ConnectBlock(dbTx, block, view)
				if err != nil {
					return err
				}
			}
		}

		// Update the utxo set using the state of the utxo view.  This
		// entails removing all of the utxos spent and adding the new
		// ones created by the block.
		err := dbPutUtxoView(dbTx, view)
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

		// Insert the last block as the "best node" with its cumulative
		// work.
		bestNode := newBlockNode(&blocks[lenBlocks-1].MsgBlock().Header,
			blocks[lenBlocks-1].Sha(), formerBestHeight+int32(lenBlocks))

		numTxns := uint64(len(blocks[lenBlocks-1].MsgBlock().Transactions))
		blockSize := uint64(blocks[lenBlocks-1].MsgBlock().SerializeSize())
		state := newBestState(bestNode, blockSize, numTxns, curTotalTxns)
		err = dbPutBestState(dbTx, state, finalBlockWork)
		if err != nil {
			return err
		}

		b.stateLock.Lock()
		b.stateSnapshot = state
		b.stateLock.Unlock()

		return nil
	})
	if err != nil {
		return err
	}

	// put it in
	view.commit()

	return nil
}
