// Copyright (c) 2015-2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package ticketdb

import (
	"github.com/decred/dcrd/blockchain/dbnamespace"
	"github.com/decred/dcrd/chaincfg/chainhash"
)

// Database structure -------------------------------------------------------------
//
//   Buckets
//
// There are 5 buckets from the database reserved for tickets. These are:
// 1. Live
//     Live ticket bucket, for tickets currently in the lottery
//
//     k: ticket hash
//     v: height
//
// 2. Missed
//     Missed tickets bucket, for all tickets that are missed.
//
//     k: ticket hash
//     v: empty
//
// 3. Expired
//     Expired tickets bucket, for all tickets that are expired.
//
//     k: ticket hash
//     v: empty
//
// 4. BlockUndo
//     Block removal data, for reverting the the first 3 database buckets to
//     a previous state.
//
//     k: height
//     v: serialized undo ticket data
//
// 5. TicketsToAdd
//     Tickets to add bucket, which tells which tickets will be maturing and
//     entering the (1) in the event that a block at that height is added.
//
//     k: height
//     v: serialized list of ticket hashes
//
// For pruned nodes, both 4 and 5 can be curtailed to include only the most
// recent blocks.
//
// Procedures ---------------------------------------------------------------------
//
//   Adding a block
//
// The steps for the addition of a block are as follows:
// 1. Remove the n (constant, n=5 for all Decred networks) many tickets that were
//     selected this block.  The results of this feed into two database updates:
//         ------> A database entry containing all the data for the block
//            |     required to undo the adding of the block (as serialized
//            |     SpentTicketData and MissedTicketData)
//            \--> All missed tickets must be moved to the missed ticket bucket.
//
// 2. Expire any tickets from this block.
//     The results of this feed into two database updates:
//         ------> A database entry containing all the data for the block
//            |     required to undo the adding of the block (as serialized
//            |     MissedTicketData)
//            \--> All expired tickets must be moved to the missed ticket bucket.
//
// 3. All revocations in the block are processed, and the revoked ticket moved
//     from the missed ticket bucket to the revocations bucket:
//         ------> A database entry containing all the data for the block
//            |     required to undo the adding of the block (as serialized
//            |     MissedTicketData, revoked flag added)
//            \--> All revoked tickets must be moved to the revoked ticket bucket.
//
// 4. All newly maturing tickets must be added to the live ticket bucket. These
//     are previously stored in the "tickets to add" bucket so they can more
//     easily be pulled down when adding a block without having to load the
//     entire block itself and suffer the deserialization overhead. The only
//     things that must be written for this step are newly added tickets to the
//     ticket database, along with their respective heights.
//
//   Removing a block
//
// Steps 1 through 4 above are iterated through in reverse. The newly maturing
//  ticket hashes are fetched from the "tickets to add" bucket for the given
//  height that was used at this block height, and the tickets are dropped from
//  the live ticket bucket. The UndoTicketData is then fetched for the block and
//  iterated through in reverse order (it was stored in forward order) to restore
//  any changes to the relevant buckets made when inserting the block. Finally,
//  the data for the block removed is purged from both the BlockUndo and
//  TicketsToAdd buckets.

// LiveTicketData is the data for live tickets to be written to the disk with
// the addition of every block.
type LiveTicketData struct {
	TicketHash   chainhash.Hash
	TicketHeight uint32
}

// UndoTicketData is the data for any ticket that has been spent, missed, or
// expired at some new height.  It is used to roll back the database in the
// event of reorganizations or determining if a side chain block is valid.
// The last 3 are encoded as a single byte of flags.
// The flags describe a particular state for the ticket:
//  1. Missed is set, but Expired is not (0000 0001 or 0000 00101). The ticket
//      was selected in the lottery at this block height but missed, or the
//      ticket became too old and was missed. The ticket is being moved to the
//      missed ticket bucket from the live ticket bucket.
//  2. Missed and revoked are set (0000 0011 or 0000 0111). The ticket was
//      missed previously at a block before this one and was revoked, and
//      as such is being moved to the revoked ticket bucket from the missed
//      ticket bucket.
//  3. All flags are unset. The ticket has been spent and is removed from the
//      live ticket bucket.
type UndoTicketData struct {
	TicketHash   chainhash.Hash
	TicketHeight uint32
	Missed       bool
	Revoked      bool
	Expired      bool
}

// undoTicketDataSize is the serialized size of an UndoTicketData struct in bytes.
const undoTicketDataSize = 37

// convertUndoBitFlags converts the bools of the UndoTicketData struct into a
// series of bitflags in a single byte.
func convertUndoBitFlags(missed, revoked, expired bool) byte {
	var b byte
	if missed {
		b |= 1 << 0
	}
	if revoked {
		b |= 1 << 1
	}
	if expired {
		b |= 1 << 2
	}

	return b
}

// SerializeBlockUndoData serializes an entire list of relevant tickets for
// undoing tickets at any given height.
func SerializeBlockUndoData(utds []*UndoTicketData) []byte {
	// 4 bytes for the number of tickets serialized in little endian.
	b := make([]byte, 4+len(utds)*undoTicketDataSize)
	offset := 0
	dbnamespace.ByteOrder.PutUint32(b[offset:offset+4], uint32(len(utds)))
	offset += 4
	for _, utd := range utds {
		copy(b[offset:offset+chainhash.HashSize], utd.TicketHash[:])
		offset += chainhash.HashSize
		dbnamespace.ByteOrder.PutUint32(b[offset:offset+4], utd.TicketHeight)
		offset += 4
		b[offset] = convertUndoBitFlags(utd.Missed, utd.Revoked, utd.Expired)
		offset += 1
	}

	return b
}

// DeserializeBlockUndoData deserializes a list of UndoTicketData for an entire
// block.
func DeserializeBlockUndoData() ([]byte, error) {

}
