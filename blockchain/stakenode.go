// Copyright (c) 2013-2016 The btcsuite developers
// Copyright (c) 2015-2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package blockchain

import (
	"fmt"

	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/database"
)

// nodeAtHeightFromTopNode goes backwards through a node until it a reaches
// the node with a desired block height; it returns this block.  The benefit is
// this works for both the main chain and the side chain.
func (b *BlockChain) nodeAtHeightFromTopNode(node *blockNode,
	toTraverse int64) (*blockNode, error) {
	oldNode := node
	var err error

	for i := 0; i < int(toTraverse); i++ {
		// Get the previous block node.
		oldNode, err = b.getPrevNodeFromNode(oldNode)
		if err != nil {
			return nil, err
		}

		if oldNode == nil {
			return nil, fmt.Errorf("unable to obtain previous node; " +
				"ancestor is genesis block")
		}
	}

	return oldNode, nil
}

// fetchNewTicketsForNode fetches the list of newly maturing tickets for a
// given node by traversing backwards through its parents until it finds the
// block that contains the original tickets to mature.
//
// This function is NOT safe for concurrent access and must be called with
// the chainLock held for writes.
func (b *BlockChain) fetchNewTicketsForNode(node *blockNode) ([]chainhash.Hash, error) {
	// If we're before the stake enabled height, there can be no
	// tickets in the live ticket pool.
	if node.height < b.chainParams.StakeEnabledHeight {
		return []chainhash.Hash{}, nil
	}

	// If we already cached the tickets, simply return the cached list.
	// It's important to make the distinction here that nil means the
	// value was never looked up, while an empty slice of pointers means
	// that there were no new tickets at this height.
	if node.newTickets != nil {
		return node.newTickets, nil
	}

	// Calculate block number for where new tickets matured from and retrieve
	// this block from DB or in memory if it's a sidechain.
	matureNode, err := b.nodeAtHeightFromTopNode(node,
		int64(b.chainParams.TicketMaturity))
	if err != nil {
		return nil, err
	}

	matureBlock, errBlock := b.fetchBlockFromHash(&matureNode.hash)
	if errBlock != nil {
		return nil, errBlock
	}

	tickets := []chainhash.Hash{}
	for _, stx := range matureBlock.MsgBlock().STransactions {
		if is, _ := stake.IsSStx(stx); is {
			h := stx.TxSha()
			tickets = append(tickets, h)
		}
	}

	// Set the new tickets in memory so that they exist for future
	// reference in the node.
	node.newTickets = tickets

	return tickets, nil
}

// fetchStakeNode will scour the blockchain from the best block, for which we
// know that there is valid stake node.  The first step is finding a path to the
// ancestor, or, if on a side chain, the path to the common ancestor, followed
// by the path to the sidechain node.  After this path is established, the
// algorithm walks along the path, regenerating and storing intermediate nodes
// as it does so, until the final stake node of interest is populated with the
// correct data.
//
// This function MUST be called with the chain state lock held (for writes).
func (b *BlockChain) fetchStakeNode(node *blockNode) (*stake.Node, error) {
	// If we already have the stake node fetched, returned the cached result.
	// Stake nodes are immutable.
	if node.stakeNode != nil {
		return node.stakeNode, nil
	}

	// If the parent stake node is cached, connect the stake node
	// from there.
	if node.parent != nil {
		if node.stakeNode == nil && node.parent.stakeNode != nil {
			var err error
			if node.newTickets == nil {
				node.newTickets, err = b.fetchNewTicketsForNode(node)
				if err != nil {
					return nil, err
				}
			}

			node.stakeNode, err = node.parent.stakeNode.ConnectNode(node.header,
				node.ticketsSpent,
				node.ticketsRevoked,
				node.newTickets)
			if err != nil {
				return nil, err
			}

			return node.stakeNode, nil
		}
	}

	// We need to generate a path to the stake node and restore
	// it through the entire path.  The bestNode stake node must
	// always be filled in, so assume it is safe to begin working
	// backwards from there.
	detachNodes, attachNodes, err := b.getReorganizeNodes(node)
	if err != nil {
		return nil, err
	}
	current := b.bestNode

	// Move backwards through the main chain, undoing the ticket
	// treaps for each block.  The database is passed because the
	// undo data and new tickets data for each block may not yet
	// be filled in and may require the database to look up.
	err = b.db.View(func(dbTx database.Tx) error {
		for e := detachNodes.Front(); e != nil; e = e.Next() {
			n := e.Value.(*blockNode)
			var errLocal error
			if n.stakeNode == nil {
				n.stakeNode, errLocal =
					current.stakeNode.DisconnectNode(n.header,
						n.stakeUndoData, n.newTickets, dbTx)
			}
			if errLocal != nil {
				return errLocal
			}
			current = n
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	// Detach the final block and get the filled in node for the fork
	// point.
	err = b.db.View(func(dbTx database.Tx) error {
		var errLocal error
		if current.parent.stakeNode == nil {
			current.parent.stakeNode, errLocal =
				current.stakeNode.DisconnectNode(current.parent.header,
					current.parent.stakeUndoData, current.parent.newTickets, dbTx)
		}
		if errLocal != nil {
			return errLocal
		}

		current = current.parent
		return nil
	})
	if err != nil {
		return nil, err
	}

	// The node is at a fork point in the block chain, so just return
	// this stake node.
	if attachNodes.Len() == 0 {
		if current.hash != node.hash ||
			current.height != node.height {
			return nil, AssertError("failed to restore stake node to " +
				"fork point when fetching")
		}

		return current.stakeNode, nil
	}

	// The requested node is on a side chain, so we need to apply the
	// transactions and spend information from each of the nodes to attach.
	// Not that side chain ticket data and undo data is always stored
	// in memory, so there is not need to use the database here.
	for e := attachNodes.Front(); e != nil; e = e.Next() {
		n := e.Value.(*blockNode)

		if n.stakeNode == nil {
			if n.newTickets == nil {
				n.newTickets, err = b.fetchNewTicketsForNode(n)
				if err != nil {
					return nil, err
				}
			}

			n.stakeNode, err = current.stakeNode.ConnectNode(n.header,
				n.ticketsSpent, n.ticketsRevoked, n.newTickets)
			if err != nil {
				return nil, err
			}
		}

		current = n
	}

	return current.stakeNode, nil
}

// fetchRollingTally will scour the blockchain from the best block, for which we
// know that there is valid rolling tally.  The first step is finding a path to
// the ancestor, or, if on a side chain, the path to the common ancestor, followed
// by the path to the sidechain node.  After this path is established, the
// algorithm walks along the path, regenerating and storing intermediate tallies
// as it does so, until the final tally of interest is populated with the correct
// data.
//
// This function MUST be called with the chain state lock held (for writes).
func (b *BlockChain) fetchRollingTally(node *blockNode) (*stake.RollingVotingPrefixTally, error) {
	b.rollingTallyCacheLock.Lock()
	defer b.rollingTallyCacheLock.Unlock()

	// If we already have the rolling tally fetched, returned the cached result.
	// Rolling tallies are immutable.
	if node.rollingTally != nil {
		return node.rollingTally, nil
	}

	// If the parent rolling tally is cached, connect the rolling tally
	// from there.
	if node.parent != nil {
		if node.rollingTally == nil && node.parent.rollingTally != nil {
			var tally stake.RollingVotingPrefixTally
			err := b.db.View(func(dbTx database.Tx) error {
				var err error
				tally, err =
					node.parent.rollingTally.ConnectBlockToTally(
						b.rollingTallyCache, dbTx, node.hash,
						uint32(node.height), node.voteBitsSlice,
						b.chainParams)
				if err != nil {
					return err
				}

				return nil
			})
			if err != nil {
				return nil, err
			}
			node.rollingTally = &tally

			return node.rollingTally, nil
		}
	}

	// We need to generate a path to the rolling tally and restore
	// it through the entire path.  The bestNode tally must
	// always be filled in, so assume it is safe to begin working
	// backwards from there.
	detachNodes, attachNodes, err := b.getReorganizeNodes(node)
	if err != nil {
		return nil, err
	}
	current := b.bestNode

	// Move backwards through the main chain, undoing the block
	// tallies for each block.  The database and cache are passed
	// because the data required to restore to an earlier state
	// might be located in either the cache or the database.
	err = b.db.View(func(dbTx database.Tx) error {
		for e := detachNodes.Front(); e != nil; e = e.Next() {
			n := e.Value.(*blockNode)
			var tally stake.RollingVotingPrefixTally
			var errLocal error
			if n.rollingTally == nil {
				fmt.Printf("current rolling tally %v\n", current.rollingTally)
				tally, errLocal =
					current.rollingTally.DisconnectBlockFromTally(
						b.rollingTallyCache, dbTx, current.hash,
						uint32(current.height), current.voteBitsSlice,
						nil, b.chainParams)
				if errLocal != nil {
					return errLocal
				}

				n.rollingTally = &tally
			}

			current = n
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	// Detach the final block and get the filled in tally for the fork
	// point.
	err = b.db.View(func(dbTx database.Tx) error {
		var tally stake.RollingVotingPrefixTally
		var errLocal error
		if current.parent.rollingTally == nil {
			fmt.Printf("current rolling tally 2 %v\n", current.rollingTally)
			tally, errLocal = current.rollingTally.DisconnectBlockFromTally(
				b.rollingTallyCache, dbTx, current.hash,
				uint32(current.height), current.voteBitsSlice,
				nil, b.chainParams)
			if errLocal != nil {
				return errLocal
			}

			current.parent.rollingTally = &tally
		}
		current = current.parent

		return nil
	})
	if err != nil {
		return nil, err
	}

	// The tally is at a fork point in the block chain, so just return
	// this tally.
	if attachNodes.Len() == 0 {
		if current.hash != node.hash ||
			current.height != node.height {
			return nil, AssertError("failed to restore rolling tally to " +
				"fork point when fetching")
		}

		return current.rollingTally, nil
	}

	// The requested tally is on a side chain, so we need to apply the
	// blocks from this sidechain to the current tally and write any
	// interval states to the cache.
	err = b.db.View(func(dbTx database.Tx) error {
		for e := attachNodes.Front(); e != nil; e = e.Next() {
			n := e.Value.(*blockNode)

			var tally stake.RollingVotingPrefixTally
			var errLocal error
			if n.rollingTally == nil {
				tally, errLocal = current.rollingTally.ConnectBlockToTally(
					b.rollingTallyCache, dbTx, n.hash, uint32(n.height),
					n.voteBitsSlice, b.chainParams)
				if errLocal != nil {
					return errLocal
				}

				n.rollingTally = &tally
			}

			current = n
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return current.rollingTally, nil
}

func (b *BlockChain) TestPrunedStakeData() error {
	node := b.bestNode

	tallies := make([]stake.RollingVotingPrefixTally, int(node.height))
	tallies[int(node.height)-1] = *node.rollingTally
	for i := node.height - 1; i >= 1; i-- {
		fmt.Printf("height %v\n", i)

		node = node.parent
		tallies[i-1] = *node.rollingTally
		node.rollingTally = nil
	}

	node = b.bestNode
	for i := node.height; i >= 1; i-- {
		fmt.Printf("fetch node %v height %v\n", node.hash, i)
		tally, err := b.fetchRollingTally(node)
		if err != nil {
			return err
		}
		if *tally != tallies[i-1] {
			return fmt.Errorf("got not equal: %v, %v", node.hash, *tally, tallies[i])
		}
		fmt.Printf("tally got %v\n", tally)

		if i != node.height {
			node.rollingTally = nil
		}

		node = node.parent
	}

	return nil
}
