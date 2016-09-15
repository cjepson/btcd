// stakenode.go
package blockchain

import (
	"fmt"

	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg/chainhash"
)

// nodeAtHeightFromTopNode goes backwards through a node until it a reaches
// the node with a desired block height; it returns this block. The benefit is
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

func fetchNewTicketsForNode(node *blockNode) ([]*chainhash.Hash, error) {
	// Calculate block number for where new tickets matured from and retrieve
	// this block from db.
	matureNode, err := b.nodeAtHeightFromTopNode(node, tM)
	if err != nil {
		return err
	}

	matureBlock, errBlock := b.fetchBlockFromHash(matureNode.hash)
	if errBlock != nil {
		return errBlock
	}

	// Store pointers to empty ticket data in the ticket store and mark them as
	// non-existing.
	for _, stx := range matureBlock.STransactions() {
		if is, _ := stake.IsSStx(stx); is {
			// Leave this pointing to nothing, as the ticket technically does not
			// exist. It may exist when we add blocks later, but we can fill it
			// out then.
			td := &stake.TicketData{}

			tpd := NewTicketPatchData(td,
				TiNonexisting,
				nil)

			tixStore[*stx.Sha()] = tpd
		}
	}
}

// fetchStakeNode will scour the blockchain from the best block, for which we
// know that there is valid stake node. The first step is finding a path to the
// ancestor, or, if on a side chain, the path to the common ancestor, followed
// by the path to the sidechain node. After this path is established, the
// algorithm walks along the path, regenerating and storing intermediate nodes
// as it does so, until the final stake node of interest is populated with the
// correct data.
//
// This function MUST be called with the chain state lock held (for reads).
func (b *BlockChain) fetchStakeNode(node *blockNode) (*stake.StakeNode, error) {
	// The best node should always have a valid stake node loaded.
	if node == b.bestNode {
		if b.bestNode.stakeNode != nil {
			return b.bestNode.stakeNode
		} else {
			return fmt.Errorf("best node stake node missing")
		}
	}

	// First, make sure we don't already have the stake node cached.
	attach, detach := b.getReorganizeNodes(node)

	for e := detachNodes.Front(); e != nil; e = e.Next() {
		n := e.Value.(*blockNode)
	}
}
