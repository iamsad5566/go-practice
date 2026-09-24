package ledger

import "sync"

// account holds the mutable state of a single user account.
// Every field below mu is guarded by mu.
//
// Invariant: available + locked never exceeds math.MaxInt64, so releasing
// locked funds back to available can never overflow.
type account struct {
	id string

	mu        sync.Mutex
	available int64
	locked    int64
	escrows   map[string]*escrow  // keyed by holdID
	history   []TransactionRecord // append-only, in chronological order
}

func newAccount(id string, initialBalance int64) *account {
	return &account{
		id:        id,
		available: initialBalance,
		escrows:   make(map[string]*escrow),
	}
}

func (a *account) availableBalance() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.available
}

// historySnapshot returns a copy so callers cannot mutate the ledger.
func (a *account) historySnapshot() []TransactionRecord {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]TransactionRecord(nil), a.history...)
}

// record appends to the audit history. Caller must hold a.mu.
func (a *account) record(r TransactionRecord) {
	a.history = append(a.history, r)
}

// total is the account's full holdings. Caller must hold a.mu.
func (a *account) total() int64 {
	return a.available + a.locked
}

// canReceive reports whether amount can be credited without overflow.
// Caller must hold a.mu.
func (a *account) canReceive(amount int64) bool {
	_, ok := checkedAdd(a.total(), amount)
	return ok
}

// lockPair locks two distinct accounts in ascending ID order to prevent
// ABBA deadlocks, and returns the matching unlock function.
func lockPair(a, b *account) (unlock func()) {
	first, second := a, b
	if second.id < first.id {
		first, second = second, first
	}
	first.mu.Lock()
	second.mu.Lock()
	return func() {
		second.mu.Unlock()
		first.mu.Unlock()
	}
}
