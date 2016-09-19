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

// refreshRateForV2Upgrade is the distance in blocks for the client to log
// updates about the upgrade to version 2 from version 1.
const refreshRateForV2Upgrade = 500

// upgradeToVersion2 upgrades a version 1 blockchain to version 2, allowing
// use of the new on-disk ticket database.
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

			// Iteratively connect the stake nodes in memory.
			header := block.MsgBlock().Header
			bestStakeNode, errLocal = bestStakeNode.ConnectNode(header,
				ticketsSpentInBlock(block), ticketsRevokedInBlock(block),
				newTickets)
			if errLocal != nil {
				return errLocal
			}

			// Write the top block stake node to the database.
			errLocal = stake.WriteConnectedBestNode(dbTx, bestStakeNode,
				*best.Hash)
			if errLocal != nil {
				return errLocal
			}

			// Write the best block node when we reach it.
			if i == best.Height {
				b.bestNode.stakeNode = bestStakeNode
				b.bestNode.stakeUndoData = bestStakeNode.UndoData()
				b.bestNode.newTickets = newTickets
				b.bestNode.ticketsSpent = ticketsSpentInBlock(block)
				b.bestNode.ticketsRevoked = ticketsRevokedInBlock(block)
			}

			if i%refreshRateForV2Upgrade == 0 {
				log.Infof("Upgrade to new stake database has proceeded to "+
					"block %v/%v", block.Height(), best.Height)
			}
		}

		// Write the new database version.
		b.dbInfo.version = 2
		errLocal = dbPutDatabaseInfo(dbTx, b.dbInfo)
		if errLocal != nil {
			return errLocal
		}

		return nil
	})
	if err != nil {
		return err
	}

	log.Infof("Upgrade to new stake database was successful!")

	return nil
}

// upgrade applies all possible upgrades to the blockchain database iteratively,
// updating old clients to the newest version.
func (b *BlockChain) upgrade() error {
	if b.dbInfo.version == 1 {
		err := b.upgradeToVersion2()
		if err != nil {
			return err
		}
	}

	return nil
}
