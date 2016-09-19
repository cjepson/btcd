// Copyright (c) 2013-2016 The btcsuite developers
// Copyright (c) 2015-2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package blockchain

import (
	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/database"
)

// upgradeToVersion2 upgrades
func (b *BlockChain) upgradeToVersion2() error {
	best := b.BestSnapshot()

	// The upgrade is atomic, so there is no need to set the flag that
	// the database is undergoing an upgrade here.  Get the stake node
	// for the genesis block, and then begin connecting stake nodes
	// incrementally.
	err := b.db.Update(func(dbTx database.Tx) error {
		bestStakeNode, errLocal := stake.InitDatabaseState(dbTx, b.chainParams)
		if errLocal != nil {
			return errLocal
		}

		for i := int64(1); i <= best.Height; i++ {
			block, errLocal := dbFetchBlockByHeight(dbTx, i)
			if errLocal != nil {
				return errLocal
			}

			// If we need the tickets, fetch them too.
			newTickets := make([]chainhash.Hash, 0)
			if i >= b.chainParams.StakeEnabledHeight {
				matureHeight := i - int64(b.chainParams.TicketMaturity)
				matureBlock, errLocal := dbFetchBlockByHeight(dbTx, matureHeight)
				if errLocal != nil {
					return errLocal
				}
				for _, stx := range matureBlock.STransactions() {
					if is, _ := stake.IsSStx(stx); is {
						h := stx.Sha()
						newTickets = append(newTickets, *h)
					}
				}
			}

			header := block.MsgBlock().Header
			bestStakeNode, errLocal = bestStakeNode.ConnectNode(header,
				ticketsSpentInBlock(block), ticketsRevokedInBlock(block),
				newTickets)
			if errLocal != nil {
				return errLocal
			}

			if i == best.Height {
				b.bestNode.stakeNode = bestStakeNode
				b.bestNode.stakeUndoData = bestStakeNode.UndoData()
				b.bestNode.newTickets = newTickets
				b.bestNode.ticketsSpent = ticketsSpentInBlock(block)
				b.bestNode.ticketsRevoked = ticketsRevokedInBlock(block)
			}
		}

		errLocal = stake.WriteConnectedBestNode(dbTx, bestStakeNode, *best.Hash)
		if errLocal != nil {
			return errLocal
		}

		return nil
	})
	if err != nil {
		return err
	}

	return nil
}
