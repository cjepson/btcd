// Copyright (c) 2013-2014 The btcsuite developers
// Copyright (c) 2015-2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package blockchain

import (
	"fmt"

	"github.com/decred/dcrd/chaincfg/chainhash"
	// "github.com/decred/dcrd/database"
)

// NextWinningTickets returns the next tickets eligible for spending as SSGen
// on the top block. It also returns the ticket pool size.
//
// This function is safe for concurrent access.
func (b *BlockChain) NextWinningTickets() ([]*chainhash.Hash, int, [6]byte,
	error) {
	b.chainLock.Lock()
	defer b.chainLock.Unlock()

	return b.bestNode.stakeNode.Winners(), b.bestNode.stakeNode.PoolSize(),
		b.bestNode.stakeNode.FinalState(), nil
}

// winningTicketsForNode is a helper function that returns winning tickets
// along with the ticket pool size and transaction store for the given node.
//
// This function is NOT safe for concurrent access.
func (b *BlockChain) winningTicketsForNode(node *blockNode) ([]*chainhash.Hash,
	int, [6]byte, error) {
	if node.height < b.chainParams.StakeEnabledHeight {
		return []*chainhash.Hash{}, 0, [6]byte{}, nil
	}
	stakeNode, err := b.fetchStakeNode(node)
	if err != nil {
		return []*chainhash.Hash{}, 0, [6]byte{}, err
	}

	return stakeNode.Winners(), b.bestNode.stakeNode.PoolSize(),
		b.bestNode.stakeNode.FinalState(), nil
}

// GetWinningTickets takes a node block hash and returns the next tickets
// eligible for spending as SSGen.
//
// This function is safe for concurrent access.
func (b *BlockChain) GetWinningTickets(nodeHash chainhash.Hash) ([]*chainhash.Hash,
	int, [6]byte, error) {
	b.chainLock.Lock()
	defer b.chainLock.Unlock()

	var node *blockNode
	if n, exists := b.index[nodeHash]; exists {
		node = n
	} else {
		node, _ = b.findNode(&nodeHash)
	}

	if node == nil {
		return nil, 0, [6]byte{}, fmt.Errorf("node doesn't exist")
	}

	winningTickets, poolSize, finalState, err :=
		b.winningTicketsForNode(node)
	if err != nil {
		return nil, 0, [6]byte{}, err
	}

	return winningTickets, poolSize, finalState, nil
}

// GetMissedTickets returns a list of currently missed tickets.
//
// This function is safe for concurrent access.
func (b *BlockChain) GetMissedTickets() []*chainhash.Hash {
	b.chainLock.Lock()
	defer b.chainLock.Unlock()

	return b.bestNode.stakeNode.MissedTickets()
}
