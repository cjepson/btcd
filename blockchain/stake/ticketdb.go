// Copyright (c) 2015-2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package stake

import (
	"fmt"
	//"sync"

	"github.com/decred/dcrd/blockchain/dbnamespace"
	"github.com/decred/dcrd/blockchain/stake/internal/ticketdb"
	"github.com/decred/dcrd/blockchain/stake/internal/tickettreap"
	"github.com/decred/dcrd/chaincfg"
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
	height uint32

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

	// finalState is the calculated final state checksum from the live
	// tickets pool.
	finalState [6]byte

	// params is the blockchain parameters.
	params *chaincfg.Params
}

// UndoData returns the stored UndoTicketDataSlice used to remove this node
// and restore it to the parent state.
func (sn *StakeNode) UndoData() UndoTicketDataSlice {
	return sn.databaseUndoUpdate
}

// NewTickets returns the stored UndoTicketDataSlice used to remove this node
// and restore it to the parent state.
func (sn *StakeNode) NewTickets() []*chainhash.Hash {
	return sn.databaseBlockTickets
}

// ExistsLiveTicket returns whether or not a ticket exists in the live ticket
// treap for this stake node.
func (sn *StakeNode) ExistsLiveTicket(ticket *chainhash.Hash) bool {
	return sn.liveTickets.Has(tickettreap.Key(*ticket))
}

// LiveTickets returns the list of live tickets for this stake node.
func (sn *StakeNode) LiveTickets() []*chainhash.Hash {
	tickets := make([]*chainhash.Hash, sn.liveTickets.Len())
	i := 0
	sn.liveTickets.ForEach(func(k tickettreap.Key, v *tickettreap.Value) bool {
		h := chainhash.Hash(k)
		tickets[i] = &h
		i++
		return true
	})

	return tickets
}

// ExistsMissedTicket returns whether or not a ticket exists in the missed
// ticket treap for this stake node.
func (sn *StakeNode) ExistsMissedTicket(ticket *chainhash.Hash) bool {
	return sn.missedTickets.Has(tickettreap.Key(*ticket))
}

// MissedTickets returns the list of missed tickets for this stake node.
func (sn *StakeNode) MissedTickets() []*chainhash.Hash {
	tickets := make([]*chainhash.Hash, sn.missedTickets.Len())
	i := 0
	sn.missedTickets.ForEach(func(k tickettreap.Key, v *tickettreap.Value) bool {
		h := chainhash.Hash(k)
		tickets[i] = &h
		i++
		return true
	})

	return tickets
}

// Winners returns the current list of winners for this stake node, which
// can vote on this node.
func (sn *StakeNode) Winners() []*chainhash.Hash {
	return sn.Winners()
}

// LoadBestNode is used when the blockchain is initialized, to get the initial
// stake node from the database bucket. The blockchain must pass the height
// and the blockHash to confirm that the ticket database is on the same
// location in the blockchain as the blockchain itself. This function also
// checks to ensure that the database has not failed the upgrade process and
// reports the current version.
func LoadBestNode(dbTx database.Tx, height uint32, blockHash *chainhash.Hash, header *wire.BlockHeader, params *chaincfg.Params) (*StakeNode, error) {
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
	node.height = height
	node.params = params
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

	// Calculate the final state from the block header.
	stateBuffer := make([]byte, 0,
		(node.params.TicketsPerBlock+1)*chainhash.HashSize)
	for _, ticketHash := range node.nextWinners {
		stateBuffer = append(stateBuffer, ticketHash[:]...)
	}
	hB, err := header.Bytes()
	if err != nil {
		return nil, err
	}
	prng := NewHash256PRNG(hB)
	_, err = FindTicketIdxs(int64(node.liveTickets.Len()),
		int(node.params.TicketsPerBlock), prng)
	if err != nil {
		return nil, err
	}
	lastHash := prng.StateHash()
	stateBuffer = append(stateBuffer, lastHash[:]...)
	copy(node.finalState[:], chainhash.HashFuncB(stateBuffer)[0:6])

	log.Infof("Loaded stake database version %v", info.Version)

	return node, nil
}

// ticketData is a k,v pair for a ticket treap.
type ticketData struct {
	hash   chainhash.Hash
	height uint32
}

// hashInSlice determines if a hash exists in a slice of hashes.
func hashInSlice(h *chainhash.Hash, list []*chainhash.Hash) bool {
	for _, hash := range list {
		if h.IsEqual(hash) {
			return true
		}
	}

	return false
}

// hashInUndoData determines if a hash exists in a slice of ticket undo data.
/*
func hashInUndoData(h *chainhash.Hash, list UndoTicketDataSlice) bool {
	for _, entry := range list {
		if h.IsEqual(&entry.TicketHash) {
			return true
		}
	}

	return false
}
*/

// connectStakeNode connects a child to a parent stake node, returning the
// modified stake node for the child.  It is important to keep in mind that
// the argument node is the parent node, and that the child stake node is
// returned after subsequent modification of the parent node's immutable
// data.
func connectStakeNode(node *StakeNode, header *wire.BlockHeader, ticketsSpentInBlock []*chainhash.Hash, revokedTickets []*chainhash.Hash, newTickets []*chainhash.Hash) (*StakeNode, error) {
	connectedNode := &StakeNode{
		height:               node.height + 1,
		liveTickets:          node.liveTickets,
		missedTickets:        node.missedTickets,
		revokedTickets:       node.revokedTickets,
		databaseUndoUpdate:   make(UndoTicketDataSlice, 0),
		databaseBlockTickets: make(ticketdb.TicketHashes, 0),
		nextWinners:          make([]*chainhash.Hash, 0),
		params:               node.params,
	}

	// Iterate through all possible winners and construct the undo data,
	// updating the live and missed ticket treaps as necessary.
	for _, ticket := range node.nextWinners {
		k := tickettreap.Key(*ticket)
		v := connectedNode.liveTickets.Get(tickettreap.Key(*ticket))
		if v == nil {
			return nil, stakeRuleError(ErrMissingTicket, fmt.Sprintf(
				"ticket %v was supposed to be in the live ticket "+
					"treap, but could not be found"))
		}

		// If it's spent in this block, mark it as being spent. Otherwise,
		// it was missed. Spent tickets are dropped from the live ticket
		// bucket, while missed tickets are pushed to the missed ticket
		// bucket.
		wasSpent := false
		if hashInSlice(ticket, ticketsSpentInBlock) {
			wasSpent = true
			connectedNode.liveTickets = connectedNode.liveTickets.Delete(k)
		} else {
			connectedNode.liveTickets = connectedNode.liveTickets.Delete(k)
			connectedNode.missedTickets = connectedNode.missedTickets.Put(k, v)
		}

		// This data MUST be ordered correctly so that when the block is
		// undone, the final state checksum can be recalculated correctly.
		connectedNode.databaseUndoUpdate =
			append(connectedNode.databaseUndoUpdate, &ticketdb.UndoTicketData{
				TicketHash:   *ticket,
				TicketHeight: v.Height,
				Missed:       !wasSpent,
				Revoked:      false,
				Spent:        wasSpent,
			})
	}

	// Find the expiring tickets and drop them as well. We already know what
	// the winners are from the cached information in the previous block, so
	// no drop the results of that here.
	toExpireHeight := connectedNode.height -
		uint32(connectedNode.params.TicketExpiry)
	expired := connectedNode.liveTickets.FetchExpired(toExpireHeight)
	for _, treapKey := range expired {
		v := connectedNode.liveTickets.Get(*treapKey)
		connectedNode.liveTickets.Delete(*treapKey)
		ticketHash := chainhash.Hash(*treapKey)

		connectedNode.databaseUndoUpdate =
			append(connectedNode.databaseUndoUpdate, &ticketdb.UndoTicketData{
				TicketHash:   ticketHash,
				TicketHeight: v.Height,
				Missed:       true,
				Revoked:      false,
				Spent:        false,
			})
	}

	// Process all the revocations, moving them from the missed to the
	// revoked treap and recording them in the undo data.
	for _, revokedTicket := range revokedTickets {
		v := connectedNode.liveTickets.Get(tickettreap.Key(*revokedTicket))
		connectedNode.liveTickets.Delete(tickettreap.Key(*revokedTicket))

		connectedNode.databaseUndoUpdate =
			append(connectedNode.databaseUndoUpdate, &ticketdb.UndoTicketData{
				TicketHash:   *revokedTicket,
				TicketHeight: v.Height,
				Missed:       true,
				Revoked:      true,
				Spent:        false,
			})
	}

	// Add all the new tickets.
	for _, newTicket := range newTickets {
		node.databaseBlockTickets = append(node.databaseBlockTickets, newTicket)
		k := tickettreap.Key(*newTicket)
		v := &tickettreap.Value{Height: connectedNode.height}
		connectedNode.liveTickets.Put(k, v)

		connectedNode.databaseUndoUpdate =
			append(connectedNode.databaseUndoUpdate, &ticketdb.UndoTicketData{
				TicketHash:   *newTicket,
				TicketHeight: v.Height,
				Missed:       false,
				Revoked:      false,
				Spent:        false,
			})
	}

	// Find the next set of winners.
	hB, err := header.Bytes()
	if err != nil {
		return nil, err
	}
	prng := NewHash256PRNG(hB)
	idxs, err := FindTicketIdxs(int64(connectedNode.liveTickets.Len()),
		int(connectedNode.params.TicketsPerBlock), prng)
	if err != nil {
		return nil, err
	}

	stateBuffer := make([]byte, 0,
		(connectedNode.params.TicketsPerBlock+1)*chainhash.HashSize)
	nextWinnersKeys := connectedNode.liveTickets.FetchWinners(idxs)
	for _, treapKey := range nextWinnersKeys {
		ticketHash := chainhash.Hash(*treapKey)
		connectedNode.nextWinners = append(connectedNode.nextWinners, &ticketHash)
		stateBuffer = append(stateBuffer, ticketHash[:]...)
	}
	lastHash := prng.StateHash()
	stateBuffer = append(stateBuffer, lastHash[:]...)
	copy(connectedNode.finalState[:], chainhash.HashFuncB(stateBuffer)[0:6])

	return connectedNode, nil
}

// disconnectStakeNode disconnects a stake node from itself and returns the
// state of the parent node. The database transaction should be included if the
// UndoTicketDataSlice or tickets are nil in order to look up the undo data or
// tickets from the database.
func disconnectStakeNode(node *StakeNode, parentHeader *wire.BlockHeader, parentUtds UndoTicketDataSlice, parentTickets []*chainhash.Hash, dbTx database.Tx) (*StakeNode, error) {
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
		parentUtds, err = ticketdb.DbFetchBlockUndoData(dbTx, node.height)
		if err != nil {
			return nil, err
		}

		parentTickets, err = ticketdb.DbFetchNewTickets(dbTx, node.height)
		if err != nil {
			return nil, err
		}
	}

	restoredNode := &StakeNode{
		height:               node.height - 1,
		liveTickets:          node.liveTickets,
		missedTickets:        node.missedTickets,
		revokedTickets:       node.revokedTickets,
		databaseUndoUpdate:   parentUtds,
		databaseBlockTickets: parentTickets,
		params:               node.params,
	}
	nextWinners := make([]*chainhash.Hash, 0)
	stateBuffer := make([]byte, 0,
		(node.params.TicketsPerBlock+1)*chainhash.HashSize)
	for _, undo := range node.databaseUndoUpdate {
		k := tickettreap.Key(undo.TicketHash)
		v := &tickettreap.Value{Height: undo.TicketHeight}

		switch {
		// All flags are unset; this is a newly added ticket.
		// Remove it from the list of live tickets.
		case !undo.Missed && !undo.Revoked && !undo.Spent:
			restoredNode.liveTickets = restoredNode.liveTickets.Delete(k)

		// The ticket was missed and revoked. It needs to
		// be moved from the revoked ticket treap to the
		// missed ticket treap.
		case undo.Missed && undo.Revoked:
			restoredNode.revokedTickets = restoredNode.revokedTickets.Delete(k)
			restoredNode.missedTickets = restoredNode.missedTickets.Put(k, v)

		// The ticket was missed and was previously live.
		// Remove it from the missed tickets bucket and
		// move it to the live tickets bucket. Note that
		// these should be sorted such that the winning
		// tickets are skimmed first in order to restore
		// the final state checksum, meaning that expired
		// tickets that were missed will come last.
		case undo.Missed && !undo.Revoked:
			nextWinners = append(nextWinners, &undo.TicketHash)
			stateBuffer = append(stateBuffer, undo.TicketHash[:]...)
			restoredNode.missedTickets = restoredNode.missedTickets.Delete(k)
			restoredNode.liveTickets = restoredNode.liveTickets.Put(k, v)

		// The ticket was spent. Reinsert it into the live
		// tickets treap.
		case undo.Spent:
			nextWinners = append(nextWinners, &undo.TicketHash)
			stateBuffer = append(stateBuffer, undo.TicketHash[:]...)
			restoredNode.liveTickets = restoredNode.liveTickets.Put(k, v)

		default:
			return nil, stakeRuleError(ErrMemoryCorruption, "unknown ticket "+
				"state in undo data")
		}
	}
	restoredNode.nextWinners = nextWinners

	phB, err := parentHeader.Bytes()
	if err != nil {
		return nil, err
	}
	prng := NewHash256PRNG(phB)
	_, err = FindTicketIdxs(int64(restoredNode.liveTickets.Len()),
		int(node.params.TicketsPerBlock), prng)
	if err != nil {
		return nil, err
	}
	lastHash := prng.StateHash()
	stateBuffer = append(stateBuffer, lastHash[:]...)
	copy(restoredNode.finalState[:], chainhash.HashFuncB(stateBuffer)[0:6])

	return restoredNode, nil
}

// DisconnectNode disconnects a stake node from the node and returns a pointer
// to the stake node of the parent.
func (sn *StakeNode) DisconnectNode(parentHeader *wire.BlockHeader, parentUtds UndoTicketDataSlice, parentTickets []*chainhash.Hash, dbTx database.Tx) (*StakeNode, error) {
	return disconnectStakeNode(sn, parentHeader, parentUtds, parentTickets, dbTx)
}

// WriteBestNode writes the best node to the database under an atomic database
// transaction. It also has the ability to drop reversion data for all nodes
// after this node on the main chain, for example if you are removing the best
// node and moving to its parent.
func WriteBestNode(dbTx database.Tx, node *StakeNode, dropReversionData bool) error {
	return nil
}
