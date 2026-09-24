package ledger

import "errors"

var (
	ErrInvalidID           = errors.New("ledger: invalid id")
	ErrInvalidAmount       = errors.New("ledger: invalid amount")
	ErrAccountExists       = errors.New("ledger: account already exists")
	ErrAccountNotFound     = errors.New("ledger: account not found")
	ErrSelfTransfer        = errors.New("ledger: cannot transfer to the same account")
	ErrInsufficientBalance = errors.New("ledger: insufficient available balance")
	ErrOverflow            = errors.New("ledger: amount overflow")
	ErrHoldNotFound        = errors.New("ledger: hold not found")
	ErrHoldInProgress      = errors.New("ledger: hold is still being processed")
	ErrHoldConflict        = errors.New("ledger: hold id reused with different parameters")
	ErrHoldFinalized       = errors.New("ledger: hold already settled or released")
)
