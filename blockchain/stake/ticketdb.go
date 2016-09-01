// Copyright (c) 2015-2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package stake

import (
	"sync"

	"github.com/decred/dcrd/blockchain/dbnamespace"
	"github.com/decred/dcrd/blockchain/stake/internal/ticketdb"
	"github.com/decred/dcrd/blockchain/stake/internal/tickettreap"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/database"
	"github.com/decred/dcrd/wire"
)

// UndoTicketData is a pass through for ticketdb's UndoTicketData, which is
// stored in memory in the node.
type UndoTicketDataSlice []*ticketdb.UndoTicketData

// StakeNode is in-memory stake data for a node. It contains a list of database
// updates to be written in the case that the block is inserted in the main chain
// database. Because of its use of immutable treap data structures, it allows for
// a fast, efficient in-memory representation of the ticket database for each
// node. It handles connection of and disconnection of new blocks simply.
//
// Like the immutable treap structures, stake nodes themselves are considered
// to be immutable. The connection or disconnection of past or future nodes
// returns a pointer to a new stake node, which must be saved and used
// appropriately.
type StakeNode struct {
	// internalMutex ensures safe concurrent access of this data.
	internalMutex sync.RWMutex

	// liveTickets is the treap of the live tickets for this node.
	liveTickets *tickettreap.Immutable

	// missedTickets is the treap of missed tickets for this node.
	missedTickets *tickettreap.Immutable

	// revokedTickets is the treap of revoked tickets for this node.
	revokedTickets *tickettreap.Immutable

	// databaseUndoUpdate is the cache of the database used to undo
	// the current node's addition to the blockchain.
	databaseUndoUpdate UndoTicketDataSlice

	// databaseBlockTickets is the cache of the new tickets to insert
	// into the database.
	databaseBlockTickets ticketdb.TicketHashes

	// nextWinners is the list of the next winners for this block.
	nextWinners []*chainhash.Hash
}

// UndoData returns the stored UndoTicketDataSlice used to remove this node
// and restore it to the parent state.
func (sn *StakeNode) UndoData() UndoTicketDataSlice {
	return sn.databaseUndoUpdate
}

// UndoData returns the stored UndoTicketDataSlice used to remove this node
// and restore it to the parent state.
func (sn *StakeNode) NewTickets() []*chainhash.Hash {
	return sn.databaseBlockTickets
}

// InitializeTicketDatabase is used when the blockchain is initialized, to
// get the initial stake node from the database bucket. The blockchain must
// pass the height and the blockHash to confirm that the ticket database is
// on the same location in the blockchain as the blockchain itself. This
// function also checks to ensure that the database has not failed the
// upgrade process and reports the current version.
func InitializeTicketDatabase(dbTx database.Tx, height uint32, blockHash *chainhash.Hash) (*StakeNode, error) {
	info, err := ticketdb.DbFetchDatabaseInfo(dbTx)
	if err != nil {
		return nil, err
	}

	// Compare the tip and make sure it matches.
	state, err := ticketdb.DbFetchBestState(dbTx)
	if err != nil {
		return nil, err
	}
	if state.Hash != *blockHash || state.Height != height {
		return nil, stakeRuleError(ErrDatabaseCorrupt, "best state corruption")
	}

	// Restore the best node treaps form the database.
	node := new(StakeNode)
	node.liveTickets, err = ticketdb.DbLoadAllTickets(dbTx,
		dbnamespace.LiveTicketsBucketName)
	if err != nil {
		return nil, err
	}
	if node.liveTickets.Len() != int(state.Live) {
		return nil, stakeRuleError(ErrDatabaseCorrupt, "live tickets corruption")
	}
	node.missedTickets, err = ticketdb.DbLoadAllTickets(dbTx,
		dbnamespace.MissedTicketsBucketName)
	if err != nil {
		return nil, err
	}
	if node.missedTickets.Len() != int(state.Missed) {
		return nil, stakeRuleError(ErrDatabaseCorrupt, "missed tickets corruption")
	}
	node.revokedTickets, err = ticketdb.DbLoadAllTickets(dbTx,
		dbnamespace.RevokedTicketsBucketName)
	if err != nil {
		return nil, err
	}
	if node.revokedTickets.Len() != int(state.Revoked) {
		return nil, stakeRuleError(ErrDatabaseCorrupt, "revoked tickets "+
			"corruption")
	}

	// Restore the node undo and new tickets data.
	node.databaseUndoUpdate, err = ticketdb.DbFetchBlockUndoData(dbTx, height)
	if err != nil {
		return nil, err
	}
	node.databaseBlockTickets, err = ticketdb.DbFetchNewTickets(dbTx, height)
	if err != nil {
		return nil, err
	}

	// Restore the next winners for the node.
	node.nextWinners = make([]*chainhash.Hash, len(state.NextWinners))
	for i := range state.NextWinners {
		node.nextWinners[i] = &state.NextWinners[i]
	}

	log.Infof("Loaded stake database version %v", info.Version)

	return node, nil
}

// ticketData is a k,v pair for a ticket treap.
type ticketData struct {
	hash   chainhash.Hash
	height uint32
}

// ConnectStakeNode connects a child to a parent stake node, returning the
// modified stake node for the child.
func ConnectStakeNode(node *StakeNode, header *wire.BlockHeader, tickets []*chainhash.Hash) (*StakeNode, error) {

}

// disconnectStakeNode disconnects a stake node from itself and returns the
// state of the parent node. The database transaction should be included if the
// UndoTicketDataSlice or tickets are nil in order to look up the undo data or
// tickets from the database.
func disconnectStakeNode(node *StakeNode, height uint32, parentUtds UndoTicketDataSlice, parentTickets []*chainhash.Hash, dbTx database.Tx) (*StakeNode, error) {
	// The undo ticket slice is normally stored in memory for the most
	// recent blocks and the sidechain, but it may be the case that it
	// is missing because it's in the mainchain and very old (thus
	// outside the node cache). In this case, restore this data from
	// disk.
	if parentUtds == nil || parentTickets == nil {
		if dbTx == nil {
			return nil, stakeRuleError(ErrMissingDatabaseTx, "needed to "+
				"look up undo data in the database, but no dbtx passed")
		}

		var err error
		parentUtds, err = ticketdb.DbFetchBlockUndoData(dbTx, height)
		if err != nil {
			return nil, err
		}

		parentTickets, err = ticketdb.DbFetchNewTickets(dbTx, height)
		if err != nil {
			return nil, err
		}
	}

	restoredNode := &StakeNode{
		liveTickets:          node.liveTickets,
		missedTickets:        node.missedTickets,
		revokedTickets:       node.revokedTickets,
		databaseUndoUpdate:   parentUtds,
		databaseBlockTickets: parentTickets,
	}
	nextWinners := make([]*chainhash.Hash, 0)
	for _, undo := range node.databaseUndoUpdate {
		k := tickettreap.Key(undo.TicketHash)
		v := &tickettreap.Value{Height: undo.TicketHeight}

		switch {
		// All flags are unset; this is a newly added ticket.
		// Remove it from the list of live tickets.
		case !undo.Missed && !undo.Revoked && !undo.Expired && !undo.Spent:
			restoredNode.liveTickets = restoredNode.liveTickets.Delete(k)

		// The ticket was missed and revoked. It needs to
		// be moved from the revoked ticket treap to the
		// missed ticket treap.
		case undo.Missed && undo.Revoked:
			restoredNode.revokedTickets = restoredNode.revokedTickets.Delete(k)
			restoredNode.missedTickets = restoredNode.missedTickets.Put(k, v)

		// The ticket was missed and was previously live.
		// Remove it from the missed tickets bucket and
		// move it to the live tickets bucket.
		case undo.Missed && !undo.Revoked:
			nextWinners = append(nextWinners, &undo.TicketHash)
			restoredNode.missedTickets = restoredNode.missedTickets.Delete(k)
			restoredNode.liveTickets = restoredNode.liveTickets.Put(k, v)

		// The ticket was spent. Reinsert it into the live
		// tickets treap.
		case undo.Spent:
			nextWinners = append(nextWinners, &undo.TicketHash)
			restoredNode.liveTickets = restoredNode.liveTickets.Put(k, v)
		}
	}
	restoredNode.nextWinners = nextWinners

	return restoredNode, nil
}

// DisconnectNode disconnects a stake node from the node and returns a pointer
// to the stake node of the parent.
func (sn *StakeNode) DisconnectNode(height uint32, parentUtds UndoTicketDataSlice, parentTickets []*chainhash.Hash, dbTx database.Tx) (*StakeNode, error) {
	return disconnectStakeNode(sn, height, parentUtds, parentTickets, dbTx)
}
