// Copyright (c) 2014 The btcsuite developers
// Copyright (c) 2015 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcrjson

// GetStakeDifficultyResult models the data returned from the
// getstakedifficulty command.
type GetStakeDifficultyResult struct {
	CurrentStakeDifficulty float64 `json:"current"`
	NextStakeDifficulty    float64 `json:"next"`
}

// EstimateStakeDiffResult models the data returned from the estimatestakediff
// command.
type EstimateStakeDiffResult struct {
	Min      float64  `json:"min"`
	Max      float64  `json:"max"`
	Expected float64  `json:"expected"`
	User     *float64 `json:"user,omitempty"`
}

// LiveTicketsResult models the data returned from the livetickets
// command.
type LiveTicketsResult struct {
	Tickets []string `json:"tickets"`
}

// MissedTicketsResult models the data returned from the missedtickets
// command.
type MissedTicketsResult struct {
	Tickets []string `json:"tickets"`
}

// Ticket is the structure representing a ticket.
type Ticket struct {
	Hash  string `json:"hash"`
	Owner string `json:"owner"`
}

// TicketFeeInfoMempool is ticket fee information about the mempool.
type TicketFeeInfoMempool struct {
	Number uint32  `json:"number"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
	Mean   float64 `json:"mean"`
	Median float64 `json:"median"`
	StdDev float64 `json:"stddev"`
}

// TicketFeeInfoBlock is ticket fee information about a block.
type TicketFeeInfoBlock struct {
	Height uint32  `json:"height"`
	Number uint32  `json:"number"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
	Mean   float64 `json:"mean"`
	Median float64 `json:"median"`
	StdDev float64 `json:"stddev"`
}

// TicketFeeInfoWindow is ticket fee information about an adjustment window.
type TicketFeeInfoWindow struct {
	StartHeight uint32  `json:"startheight"`
	EndHeight   uint32  `json:"endheight"`
	Number      uint32  `json:"number"`
	Min         float64 `json:"min"`
	Max         float64 `json:"max"`
	Mean        float64 `json:"mean"`
	Median      float64 `json:"median"`
	StdDev      float64 `json:"stddev"`
}

// TicketFeeInfoResult models the data returned from the ticketfeeinfo command.
// command.
type TicketFeeInfoResult struct {
	FeeInfoMempool TicketFeeInfoMempool  `json:"feeinfomempool"`
	FeeInfoBlocks  []TicketFeeInfoBlock  `json:"feeinfoblocks"`
	FeeInfoWindows []TicketFeeInfoWindow `json:"feeinfowindows"`
}

// TicketsForAddressResult models the data returned from the ticketforaddress
// command.
type TicketsForAddressResult struct {
	Tickets []string `json:"tickets"`
}
