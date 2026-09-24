// Package ledger implements an in-memory, concurrency-safe ledger engine
// supporting P2P transfers with fees and hold/settle/release escrow.
package ledger

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// FeeCollectorID is the reserved, internal account that receives transfer fees.
const FeeCollectorID = "system:fee_collector"

// systemIDPrefix marks IDs reserved for internal system accounts.
const systemIDPrefix = "system:"

type Engine struct {
	mu       sync.RWMutex
	accounts map[string]*account

	// holdsMu guards holdIndex, which enforces global holdID uniqueness and
	// maps each hold to its owning account. It is never held together with
	// an account lock.
	holdsMu   sync.Mutex
	holdIndex map[string]*holdSlot

	// feeBalance is the balance of FeeCollectorID. It is kept outside the
	// account map and updated atomically so that it never becomes a lock
	// hotspot shared by every transfer.
	feeBalance atomic.Int64
	seq        atomic.Int64
	now        func() time.Time
}

type Option func(*Engine)

// WithClock overrides the time source used for receipts and records.
func WithClock(now func() time.Time) Option {
	return func(e *Engine) { e.now = now }
}

func NewEngine(opts ...Option) *Engine {
	e := &Engine{
		accounts:  make(map[string]*account),
		holdIndex: make(map[string]*holdSlot),
		now:       time.Now,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

func (e *Engine) CreateAccount(accountID string, initialBalance int64) error {
	if accountID == "" {
		return ErrInvalidID
	}
	if initialBalance < 0 {
		return ErrInvalidAmount
	}
	if isSystemID(accountID) {
		return ErrAccountExists
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if _, exists := e.accounts[accountID]; exists {
		return ErrAccountExists
	}
	e.accounts[accountID] = newAccount(accountID, initialBalance)
	return nil
}

// GetBalance returns the available (spendable) balance of the account.
func (e *Engine) GetBalance(accountID string) (int64, error) {
	acc, err := e.lookup(accountID)
	if err != nil {
		return 0, err
	}
	return acc.availableBalance(), nil
}

// GetTransactionHistory returns every recorded operation on the account,
// oldest first.
func (e *Engine) GetTransactionHistory(accountID string) ([]TransactionRecord, error) {
	acc, err := e.lookup(accountID)
	if err != nil {
		return nil, err
	}
	return acc.historySnapshot(), nil
}

// FeeBalance returns the total fees collected by the system fee collector.
func (e *Engine) FeeBalance() int64 {
	return e.feeBalance.Load()
}

// lookup finds a user account. The engine lock is held only for the map read;
// accounts are never removed, so the pointer stays valid afterwards.
func (e *Engine) lookup(accountID string) (*account, error) {
	e.mu.RLock()
	acc, ok := e.accounts[accountID]
	e.mu.RUnlock()
	if !ok {
		return nil, ErrAccountNotFound
	}
	return acc, nil
}

func isSystemID(id string) bool {
	return strings.HasPrefix(id, systemIDPrefix)
}

// collectFee credits fee to the fee collector, failing without side effects
// if the collector balance would overflow.
func (e *Engine) collectFee(fee int64) error {
	for {
		current := e.feeBalance.Load()
		next, ok := checkedAdd(current, fee)
		if !ok {
			return ErrOverflow
		}
		if e.feeBalance.CompareAndSwap(current, next) {
			return nil
		}
	}
}

// stamp assigns the next transaction serial number and the current time.
// Callers must hold the locks of every account the record is appended to, so
// that each account's history stays ordered by ID.
func (e *Engine) stamp(r TransactionRecord) TransactionRecord {
	r.ID = e.seq.Add(1)
	r.Timestamp = e.now()
	return r
}
