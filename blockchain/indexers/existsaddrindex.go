// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package indexers

import (
	"sync"

	"github.com/decred/dcrd/blockchain"
	"github.com/decred/dcrd/chaincfg"
	database "github.com/decred/dcrd/database2"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrutil"
)

var (
	// existsAddressIndexName is the human-readable name for the index.
	existsAddressIndexName = "exists address index"

	// existsAddrIndexKey is the key of the ever seen address index and
	// the db bucket used to house it.
	existsAddrIndexKey = []byte("existsaddridx")
)

// ExistsAddrIndex implements an "ever seen" address index.  Any address that
// is ever seen in a block or in the mempool is stored here as a key. Values
// are empty.  Once an address is seen, it is never removed from this store.
// This results in a local version of this database that is consistent only
// for this peer, but at minimum contains all the addresses seen on the
// blockchain itself.
//
// In addition, support is provided for a memory-only index of unconfirmed
// transactions such as those which are kept in the memory pool before inclusion
// in a block.
type ExistsAddrIndex struct {
	// The following fields are set when the instance is created and can't
	// be changed afterwards, so there is no need to protect them with a
	// separate mutex.
	db          database.DB
	chainParams *chaincfg.Params

	// The following fields are used to quickly link transactions and
	// addresses that have not been included into a block yet when an
	// address index is being maintained.  The are protected by the
	// unconfirmedLock field.
	//
	// The txnsByAddr field is used to keep an index of all transactions
	// which either create an output to a given address or spend from a
	// previous output to it keyed by the address.
	//
	// The addrsByTx field is essentially the reverse and is used to
	// keep an index of all addresses which a given transaction involves.
	// This allows fairly efficient updates when transactions are removed
	// once they are included into a block.
	unconfirmedLock sync.RWMutex
	existsAddr      map[[addrKeySize]byte]struct{}
}

// Ensure the ExistsAddrIndex type implements the Indexer interface.
var _ Indexer = (*ExistsAddrIndex)(nil)

// Ensure the ExistsAddrIndex type implements the NeedsInputser interface.
var _ NeedsInputser = (*ExistsAddrIndex)(nil)

// NeedsInputs signals that the index requires the referenced inputs in order
// to properly create the index.
//
// This implements the NeedsInputser interface.
func (idx *ExistsAddrIndex) NeedsInputs() bool {
	return false
}

// Init is only provided to satisfy the Indexer interface as there is nothing to
// initialize for this index.
//
// This is part of the Indexer interface.
func (idx *ExistsAddrIndex) Init() error {
	// Nothing to do.
	return nil
}

// Key returns the database key to use for the index as a byte slice.
//
// This is part of the Indexer interface.
func (idx *ExistsAddrIndex) Key() []byte {
	return existsAddrIndexKey
}

// Name returns the human-readable name of the index.
//
// This is part of the Indexer interface.
func (idx *ExistsAddrIndex) Name() string {
	return existsAddressIndexName
}

// Create is invoked when the indexer manager determines the index needs
// to be created for the first time.  It creates the bucket for the address
// index.
//
// This is part of the Indexer interface.
func (idx *ExistsAddrIndex) Create(dbTx database.Tx) error {
	_, err := dbTx.Metadata().CreateBucket(existsAddrIndexKey)
	return err
}

// dbPutExistsAddr uses an existing database transaction to update or add a
// used address index to the database.
func dbPutExistsAddr(dbTx database.Tx, bucket database.Bucket, addrKey [addrKeySize]byte) error {
	return bucket.Put(addrKey[:], nil)
}

// ExistsAddress
func (idx *ExistsAddrIndex) ExistsAddress(addr dcrutil.Address) (bool, error) {
	var region *database.BlockRegion
	err := idx.db.View(func(dbTx database.Tx) error {

	})
	return bool, err
}

// ConnectBlock is invoked by the index manager when a new block has been
// connected to the main chain.  This indexer adds a mapping for each address
// the transactions in the block involve.
//
// This is part of the Indexer interface.
func (idx *ExistsAddrIndex) ConnectBlock(dbTx database.Tx, block, parent *dcrutil.Block,
	view *blockchain.UtxoViewpoint) error {
	regularTxTreeValid := dcrutil.IsFlagSet16(block.MsgBlock().Header.VoteBits,
		dcrutil.BlockValid)
	var parentTxs []*dcrutil.Tx
	if regularTxTreeValid && block.Height() > 1 {
		parentTxs = parent.Transactions()
	}
	blockTxns := block.STransactions()
	allTxns := append(parentTxs, blockTxns...)

	usedAddrs := make(map[[addrKeySize]byte]struct{})

	for _, tx := range allTxns {
		for _, txIn := range tx.MsgTx().TxIn {
			if txscript.IsMultisigSigScript(txIn.SignatureScript) {
				rs, err :=
					txscript.MultisigRedeemScriptFromScriptSig(
						txIn.SignatureScript)
				if err != nil {
					continue
				}

				class, addrs, _, err := txscript.ExtractPkScriptAddrs(
					txscript.DefaultScriptVersion, rs, idx.chainParams)
				if err != nil {
					// Non-standard outputs are skipped.
					continue
				}
				if class != txscript.MultiSigTy {
					// This should never happen, but be paranoid.
					continue
				}

				for _, addr := range addrs {
					k, err := addrToKey(addr, idx.chainParams)
					if err != nil {
						continue
					}

					usedAddrs[k] = struct{}{}
				}
			}
		}

		for _, txOut := range tx.MsgTx().TxOut {
			_, addrs, _, err := txscript.ExtractPkScriptAddrs(
				txOut.Version, txOut.PkScript, idx.chainParams)
			if err != nil {
				// Non-standard outputs are skipped.
				continue
			}

			for _, addr := range addrs {
				k, err := addrToKey(addr, idx.chainParams)
				if err != nil {
					// Ignore unsupported address types.
					continue
				}

				usedAddrs[k] = struct{}{}
			}
		}
	}

	// Add the block hash to ID mapping to the index.
	meta := dbTx.Metadata()
	existsAddrIndex := meta.Bucket(existsAddrIndexKey)
	newUsedAddrs := make(map[[addrKeySize]byte]struct{})

	return nil
}

// DisconnectBlock is invoked by the index manager when a block has been
// disconnected from the main chain. Note that the exists address manager
// never removes addresses.
//
// This is part of the Indexer interface.
func (idx *ExistsAddrIndex) DisconnectBlock(dbTx database.Tx, block, parent *dcrutil.Block, view *blockchain.UtxoViewpoint) error {
	return nil
}
