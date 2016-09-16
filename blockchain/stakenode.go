// stakenode.go
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

// fetchNewTicketsForNode
func (b *BlockChain) fetchNewTicketsForNode(node *blockNode) ([]*chainhash.Hash, error) {
	//
	if node.height < b.chainParams.StakeEnabledHeight {
		return []*chainhash.Hash{}, nil
	}

	// If we already cached the tickets, simply return the cached list.
	// It's important to make the distinction here that nil means the
	// value was never looked up, while an empty slice of pointers means
	// that there were no new tickets at this height.
	if node.newTickets != nil {
		return node.newTickets, nil
	}

	// Calculate block number for where new tickets matured from and retrieve
	// this block from db.
	matureNode, err := b.nodeAtHeightFromTopNode(node,
		int64(b.chainParams.TicketMaturity))
	if err != nil {
		return nil, err
	}

	matureBlock, errBlock := b.fetchBlockFromHash(matureNode.hash)
	if errBlock != nil {
		return nil, errBlock
	}

	// Store pointers to empty ticket data in the ticket store and mark them as
	// non-existing.
	tickets := []*chainhash.Hash{}
	for _, stx := range matureBlock.STransactions() {
		if is, _ := stake.IsSStx(stx); is {
			tickets = append(tickets, stx.Sha())
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
// This function MUST be called with the chain state lock held (for reads).
func (b *BlockChain) fetchStakeNode(node *blockNode) (*stake.StakeNode, error) {
	// If we already have the stake node fetched, returned the cached result.
	// Stake nodes are immutable.
	if node.stakeNode != nil {
		return node.stakeNode, nil
	}

	// If the parent stake node is cached, connect the stake node from
	// there.
	if node.parent != nil {
		if node.stakeNode == nil && node.parent.stakeNode != nil {
			var err error
			if node.newTickets == nil {
				node.newTickets, err = b.fetchNewTicketsForNode(node)
				if err != nil {
					return nil, err
				}
			}

			node.stakeNode, err = node.parent.stakeNode.ConnectNode(&node.header,
				node.ticketsSpent, node.ticketsRevoked, node.newTickets)
			if err != nil {
				return nil, err
			}

			return node.stakeNode, nil
		}
	}

	// We need to generate a path to the stake node and restore it
	// it through the entire path.  The bestNode stake node must
	// always be filled in, so assume it is safe to being working
	// backwards from there.
	//fmt.Printf("GET STAKE NODE FOR %v %v (parent %v %v)(current best %v, %v)\n", node.height, node.hash, node.parent.height, node.parent.hash, b.bestNode.height, b.bestNode.hash)
	detachNodes, attachNodes, err := b.getReorganizeNodes(node)
	if err != nil {
		return nil, err
	}
	//fmt.Printf("DETACHNODES %v (len %v)\n", detachNodes, detachNodes.Len())
	currentChild := b.bestNode
	err = b.db.View(func(dbTx database.Tx) error {
		for e := detachNodes.Front(); e != nil; e = e.Next() {
			n := e.Value.(*blockNode)
			//fmt.Printf("disconnecting node %v %v\n", n.height, n.hash)
			var errLocal error
			if n.stakeNode == nil {
				n.stakeNode, errLocal =
					currentChild.stakeNode.DisconnectNode(&n.header,
						n.stakeUndoData, n.newTickets, dbTx)
			}
			if errLocal != nil {
				return errLocal
			}
			currentChild = n
			//fmt.Printf("CURRENT CHILD %v %v %v, CHILD PARENY %v %v\n", currentChild.height, currentChild.stakeNode.Height(), currentChild.hash, currentChild.parent.height, currentChild.parent.hash)
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
		if currentChild.parent.stakeNode == nil {
			currentChild.parent.stakeNode, errLocal =
				currentChild.stakeNode.DisconnectNode(&currentChild.header,
					currentChild.stakeUndoData, currentChild.newTickets, dbTx)
		}
		if errLocal != nil {
			return errLocal
		}

		currentChild = currentChild.parent
		return nil
	})
	if err != nil {
		return nil, err
	}

	if attachNodes.Len() == 0 {
		if *currentChild.hash != *node.hash ||
			currentChild.height != node.height {
			return nil, AssertError("failed to restore stake node to " +
				"fork point when fetching")
		}

		return currentChild.stakeNode, nil
	}

	// The requested node is on a side chain, so we need to apply the
	// transactions and spend information from each of the nodes to attach.
	// Not that side chain ticket data and undo data is always stored
	// in memory, so there is not need to use the database here.
	mostRecentlyConnected := currentChild
	//fmt.Printf("mostRecentlyConnected currentChild %v %v %v\n", mostRecentlyConnected.height, mostRecentlyConnected.stakeNode.Height(), mostRecentlyConnected.hash)
	for e := attachNodes.Front(); e != nil; e = e.Next() {
		n := e.Value.(*blockNode)
		//fmt.Printf("connecting node %v %v, cur child %v %v\n", n.height, n.hash, mostRecentlyConnected.height, mostRecentlyConnected.hash)

		if n.stakeNode == nil {
			//fmt.Printf("FILL IN STAKE NODE CONNECT FOR %v %v\n", n.height, n.hash)
			if n.newTickets == nil {
				n.newTickets, err = b.fetchNewTicketsForNode(n)
				if err != nil {
					return nil, err
				}
			}

			n.stakeNode, err = mostRecentlyConnected.stakeNode.ConnectNode(&n.header,
				n.ticketsSpent, n.ticketsRevoked, n.newTickets)
			if err != nil {
				return nil, err
			}
		}

		mostRecentlyConnected = n
	}

	/*
		// Connect the final block we wish to add.
		if node.newTickets == nil {
			node.newTickets, err = b.fetchNewTicketsForNode(node)
			if err != nil {
				return nil, err
			}
		}
		fmt.Printf("MOST RECENT %v %v %v\n", mostRecentlyConnected.height, mostRecentlyConnected.hash, mostRecentlyConnected.stakeNode)
		node.stakeNode, err =
			mostRecentlyConnected.stakeNode.ConnectNode(&node.header,
				node.ticketsSpent, node.ticketsRevoked, node.newTickets)
		if err != nil {
			return nil, err
		}


	*/
	//fmt.Printf("FINAL RETURNED STAKE NODE %v %v %v\n", mostRecentlyConnected.stakeNode.Height(), mostRecentlyConnected.height, mostRecentlyConnected.hash)

	return mostRecentlyConnected.stakeNode, nil
}
