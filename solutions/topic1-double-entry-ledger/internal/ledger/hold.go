package ledger

type holdStatus int

const (
	holdPending holdStatus = iota
	holdSettled
	holdReleased
)

// escrow is a hold placed on its owning account. It is guarded by the
// owning account's mutex.
type escrow struct {
	amount    int64
	status    holdStatus
	settledTo string
}

// holdSlot is the global index entry of a hold. ready is false while the
// Hold call that reserved the ID is still running.
type holdSlot struct {
	accountID string
	ready     bool
}

// Hold freezes amount on the account's available balance under holdID.
// Retrying a pending hold with the same parameters is a no-op.
func (e *Engine) Hold(holdID string, accountID string, amount int64) error {
	if holdID == "" || accountID == "" {
		return ErrInvalidID
	}
	if amount <= 0 {
		return ErrInvalidAmount
	}
	acc, err := e.lookup(accountID)
	if err != nil {
		return err
	}

	existing, reserved := e.reserveHoldID(holdID, accountID)
	if !reserved {
		return e.checkHoldRetry(holdID, existing, accountID, amount)
	}
	err = e.placeHold(acc, holdID, amount)
	e.completeHoldReservation(holdID, err == nil)
	return err
}

// SettleHold transfers the held funds to toAccountID.
// Retrying a settle to the same target is a no-op.
func (e *Engine) SettleHold(holdID string, toAccountID string) error {
	if holdID == "" || toAccountID == "" {
		return ErrInvalidID
	}
	owner, err := e.holdOwner(holdID)
	if err != nil {
		return err
	}
	if owner.id == toAccountID {
		return ErrSelfTransfer
	}
	to, err := e.lookup(toAccountID)
	if err != nil {
		return err
	}

	unlock := lockPair(owner, to)
	defer unlock()

	esc := owner.escrows[holdID]
	switch esc.status {
	case holdSettled:
		if esc.settledTo == toAccountID {
			return nil
		}
		return ErrHoldConflict
	case holdReleased:
		return ErrHoldFinalized
	}
	if !to.canReceive(esc.amount) {
		return ErrOverflow
	}

	owner.locked -= esc.amount
	to.available += esc.amount
	esc.status = holdSettled
	esc.settledTo = toAccountID

	out := e.stamp(succeeded(TransactionRecord{
		Type:          RecordSettleOut,
		FromAccountID: owner.id,
		ToAccountID:   toAccountID,
		HoldID:        holdID,
		Amount:        esc.amount,
	}))
	in := out
	in.Type = RecordSettleIn
	owner.record(out)
	to.record(in)
	return nil
}

// ReleaseHold returns the held funds to the owner's available balance.
// Retrying a release is a no-op.
func (e *Engine) ReleaseHold(holdID string) error {
	if holdID == "" {
		return ErrInvalidID
	}
	owner, err := e.holdOwner(holdID)
	if err != nil {
		return err
	}

	owner.mu.Lock()
	defer owner.mu.Unlock()

	esc := owner.escrows[holdID]
	switch esc.status {
	case holdReleased:
		return nil
	case holdSettled:
		return ErrHoldFinalized
	}

	owner.locked -= esc.amount
	owner.available += esc.amount
	esc.status = holdReleased

	owner.record(e.stamp(succeeded(TransactionRecord{
		Type:          RecordRelease,
		FromAccountID: owner.id,
		HoldID:        holdID,
		Amount:        esc.amount,
	})))
	return nil
}

// placeHold moves amount from the account's available to locked balance.
func (e *Engine) placeHold(acc *account, holdID string, amount int64) error {
	acc.mu.Lock()
	defer acc.mu.Unlock()

	rec := TransactionRecord{
		Type:          RecordHold,
		FromAccountID: acc.id,
		HoldID:        holdID,
		Amount:        amount,
	}
	if acc.available < amount {
		acc.record(e.stamp(failed(rec, ErrInsufficientBalance)))
		return ErrInsufficientBalance
	}
	acc.available -= amount
	acc.locked += amount
	acc.escrows[holdID] = &escrow{amount: amount, status: holdPending}
	acc.record(e.stamp(succeeded(rec)))
	return nil
}

// reserveHoldID claims holdID for accountID. If the ID is already taken it
// returns a copy of the existing slot and reserved=false.
func (e *Engine) reserveHoldID(holdID, accountID string) (existing holdSlot, reserved bool) {
	e.holdsMu.Lock()
	defer e.holdsMu.Unlock()

	if slot, ok := e.holdIndex[holdID]; ok {
		return *slot, false
	}
	e.holdIndex[holdID] = &holdSlot{accountID: accountID}
	return holdSlot{}, true
}

// completeHoldReservation publishes a successful hold, or frees the ID of a
// failed one so that it can be retried.
func (e *Engine) completeHoldReservation(holdID string, placed bool) {
	e.holdsMu.Lock()
	defer e.holdsMu.Unlock()

	if placed {
		e.holdIndex[holdID].ready = true
		return
	}
	delete(e.holdIndex, holdID)
}

// checkHoldRetry decides the outcome of a Hold call whose holdID is taken.
func (e *Engine) checkHoldRetry(holdID string, existing holdSlot, accountID string, amount int64) error {
	if !existing.ready {
		return ErrHoldInProgress
	}
	owner, err := e.lookup(existing.accountID)
	if err != nil {
		return err
	}

	owner.mu.Lock()
	defer owner.mu.Unlock()

	esc := owner.escrows[holdID]
	if esc.status != holdPending {
		return ErrHoldFinalized
	}
	if existing.accountID != accountID || esc.amount != amount {
		return ErrHoldConflict
	}
	return nil
}

// holdOwner resolves the account that owns a completed hold.
func (e *Engine) holdOwner(holdID string) (*account, error) {
	e.holdsMu.Lock()
	slot, ok := e.holdIndex[holdID]
	var existing holdSlot
	if ok {
		existing = *slot
	}
	e.holdsMu.Unlock()

	if !ok {
		return nil, ErrHoldNotFound
	}
	if !existing.ready {
		return nil, ErrHoldInProgress
	}
	return e.lookup(existing.accountID)
}
