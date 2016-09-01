// error.go
package ticketdb

import (
	"fmt"
)

// ErrorCode identifies a kind of error.
type ErrorCode int

// These constants are used to identify a specific RuleError.
const (
	// ErrUndoDataShortRead indicates that the given undo serialized data
	// was took small.
	ErrUndoDataShortRead = iota

	// ErrUndoDataNoEntries indicates that the data for undoing ticket data
	// in a serialized entry was corrupt.
	ErrUndoDataCorrupt

	// ErrTicketHashesShortRead indicates that the given ticket hashes
	// serialized data was took small.
	ErrTicketHashesShortRead

	// ErrTicketHashesCorrupt indicates that the data for ticket hashes
	// in a serialized entry was corrupt.
	ErrTicketHashesCorrupt

	// ErrUninitializedBucket indicates that a database bucket was not
	// initialized and therefore could not be written to or read from.
	ErrUninitializedBucket

	// ErrMissingKey indicates that a key was not found in a bucket.
	ErrMissingKey

	// ErrChainStateShortRead indicates that the given chain state data
	// was too small.
	ErrChainStateShortRead

	// ErrDatabaseInfoShortRead indicates that the given database information
	// was too small.
	ErrDatabaseInfoShortRead

	// ErrLoadAllTickets indicates that there was an error loading the tickets
	// from the database, presumably at startup.
	ErrLoadAllTickets
)

// Map of ErrorCode values back to their constant names for pretty printing.
var errorCodeStrings = map[ErrorCode]string{
	ErrUndoDataShortRead:     "ErrUndoDataShortRead",
	ErrUndoDataCorrupt:       "ErrUndoDataCorrupt",
	ErrTicketHashesShortRead: "ErrTicketHashesShortRead",
	ErrTicketHashesCorrupt:   "ErrTicketHashesCorrupt",
	ErrUninitializedBucket:   "ErrUninitializedBucket",
	ErrMissingKey:            "ErrMissingKey",
	ErrChainStateShortRead:   "ErrChainStateShortRead",
	ErrDatabaseInfoShortRead: "ErrDatabaseInfoShortRead",
	ErrLoadAllTickets:        "ErrLoadAllTickets",
}

// String returns the ErrorCode as a human-readable name.
func (e ErrorCode) String() string {
	if s := errorCodeStrings[e]; s != "" {
		return s
	}
	return fmt.Sprintf("Unknown ErrorCode (%d)", int(e))
}

// TicketDBError identifies a an error in the stake database for tickets.
// The caller can use type assertions to determine if a failure was
// specifically due to a rule violation and access the ErrorCode field to
// ascertain the specific reason for the rule violation.
type TicketDBError struct {
	ErrorCode   ErrorCode // Describes the kind of error
	Description string    // Human readable description of the issue
}

// Error satisfies the error interface and prints human-readable errors.
func (e TicketDBError) Error() string {
	return e.Description
}

// GetCode satisfies the error interface and prints human-readable errors.
func (e TicketDBError) GetCode() ErrorCode {
	return e.ErrorCode
}

// ticketDBError creates an TicketDBError given a set of arguments.
func ticketDBError(c ErrorCode, desc string) TicketDBError {
	return TicketDBError{ErrorCode: c, Description: desc}
}
