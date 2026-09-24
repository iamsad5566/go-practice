package ledger

// Transfer moves amount from one account to another. The sender additionally
// pays fee, which is credited to the system fee collector. The operation is
// all-or-nothing: every check runs before any balance is mutated.
func (e *Engine) Transfer(fromAccountID, toAccountID string, amount int64, fee int64) (*TransferReceipt, error) {
	if err := validateTransfer(fromAccountID, toAccountID, amount, fee); err != nil {
		return nil, err
	}
	from, err := e.lookup(fromAccountID)
	if err != nil {
		return nil, err
	}
	to, err := e.lookup(toAccountID)
	if err != nil {
		return nil, err
	}

	unlock := lockPair(from, to)
	defer unlock()

	debit, ok := checkedAdd(amount, fee)
	if !ok {
		return nil, ErrOverflow
	}
	out := TransactionRecord{
		Type:          RecordTransferOut,
		FromAccountID: fromAccountID,
		ToAccountID:   toAccountID,
		Amount:        amount,
		Fee:           fee,
	}
	if from.available < debit {
		from.record(e.stamp(failed(out, ErrInsufficientBalance)))
		return nil, ErrInsufficientBalance
	}
	if !to.canReceive(amount) {
		return nil, ErrOverflow
	}
	// Collecting the fee is the last step that can fail, so nothing has to be
	// rolled back if it does.
	if err := e.collectFee(fee); err != nil {
		return nil, err
	}

	from.available -= debit
	to.available += amount

	out = e.stamp(succeeded(out))
	in := out
	in.Type = RecordTransferIn
	from.record(out)
	to.record(in)

	return &TransferReceipt{
		TransactionID: out.ID,
		Timestamp:     out.Timestamp,
		FromAccountID: fromAccountID,
		ToAccountID:   toAccountID,
		Amount:        amount,
		Fee:           fee,
	}, nil
}

func validateTransfer(fromAccountID, toAccountID string, amount, fee int64) error {
	if fromAccountID == "" || toAccountID == "" {
		return ErrInvalidID
	}
	if amount <= 0 || fee < 0 {
		return ErrInvalidAmount
	}
	if fromAccountID == toAccountID {
		return ErrSelfTransfer
	}
	return nil
}
