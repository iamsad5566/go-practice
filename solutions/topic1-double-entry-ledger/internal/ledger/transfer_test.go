package ledger_test

import (
	"math"
	"testing"
	"time"

	"bank-system/internal/ledger"
)

var fixedNow = time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

func newTestEngine() *ledger.Engine {
	return ledger.NewEngine(ledger.WithClock(func() time.Time { return fixedNow }))
}

func assertFeeBalance(t *testing.T, e *ledger.Engine, want int64) {
	t.Helper()
	if got := e.FeeBalance(); got != want {
		t.Fatalf("FeeBalance() = %d, want %d", got, want)
	}
}

func TestTransfer_Rejects(t *testing.T) {
	tests := []struct {
		name     string
		from, to string
		amount   int64
		fee      int64
		wantErr  error
	}{
		{"zero amount", "alice", "bob", 0, 0, ledger.ErrInvalidAmount},
		{"negative amount", "alice", "bob", -10, 0, ledger.ErrInvalidAmount},
		{"negative fee", "alice", "bob", 10, -1, ledger.ErrInvalidAmount},
		{"empty from", "", "bob", 10, 0, ledger.ErrInvalidID},
		{"empty to", "alice", "", 10, 0, ledger.ErrInvalidID},
		{"self transfer", "alice", "alice", 10, 0, ledger.ErrSelfTransfer},
		{"unknown from", "ghost", "bob", 10, 0, ledger.ErrAccountNotFound},
		{"unknown to", "alice", "ghost", 10, 0, ledger.ErrAccountNotFound},
		{"from system account", ledger.FeeCollectorID, "bob", 10, 0, ledger.ErrAccountNotFound},
		{"to system account", "alice", ledger.FeeCollectorID, 10, 0, ledger.ErrAccountNotFound},
		{"insufficient for amount", "alice", "bob", 101, 0, ledger.ErrInsufficientBalance},
		{"insufficient for amount plus fee", "alice", "bob", 100, 1, ledger.ErrInsufficientBalance},
		{"amount plus fee overflows", "alice", "bob", 1, math.MaxInt64, ledger.ErrOverflow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newTestEngine()
			mustCreate(t, e, "alice", 100)
			mustCreate(t, e, "bob", 50)

			receipt, err := e.Transfer(tt.from, tt.to, tt.amount, tt.fee)

			assertErrorIs(t, err, tt.wantErr)
			if receipt != nil {
				t.Fatalf("receipt = %+v, want nil on failure", receipt)
			}
			assertBalance(t, e, "alice", 100)
			assertBalance(t, e, "bob", 50)
			assertFeeBalance(t, e, 0)
		})
	}
}

func TestTransfer_RejectsWhenReceiverWouldOverflow(t *testing.T) {
	e := newTestEngine()
	mustCreate(t, e, "alice", 100)
	mustCreate(t, e, "whale", math.MaxInt64)

	_, err := e.Transfer("alice", "whale", 1, 0)

	assertErrorIs(t, err, ledger.ErrOverflow)
	assertBalance(t, e, "alice", 100)
	assertBalance(t, e, "whale", math.MaxInt64)
}

func TestTransfer_MovesAmountAndCollectsFee(t *testing.T) {
	e := newTestEngine()
	mustCreate(t, e, "alice", 100)
	mustCreate(t, e, "bob", 50)

	_, err := e.Transfer("alice", "bob", 30, 5)

	if err != nil {
		t.Fatalf("Transfer: unexpected error: %v", err)
	}
	assertBalance(t, e, "alice", 65)
	assertBalance(t, e, "bob", 80)
	assertFeeBalance(t, e, 5)
}

func TestTransfer_CanSpendExactBalanceIncludingFee(t *testing.T) {
	e := newTestEngine()
	mustCreate(t, e, "alice", 100)
	mustCreate(t, e, "bob", 0)

	_, err := e.Transfer("alice", "bob", 90, 10)

	if err != nil {
		t.Fatalf("Transfer: unexpected error: %v", err)
	}
	assertBalance(t, e, "alice", 0)
	assertBalance(t, e, "bob", 90)
	assertFeeBalance(t, e, 10)
}

func TestTransfer_ReturnsReceipt(t *testing.T) {
	e := newTestEngine()
	mustCreate(t, e, "alice", 100)
	mustCreate(t, e, "bob", 50)

	receipt, err := e.Transfer("alice", "bob", 30, 5)

	if err != nil {
		t.Fatalf("Transfer: unexpected error: %v", err)
	}
	want := ledger.TransferReceipt{
		TransactionID: 1,
		Timestamp:     fixedNow,
		FromAccountID: "alice",
		ToAccountID:   "bob",
		Amount:        30,
		Fee:           5,
	}
	if *receipt != want {
		t.Fatalf("receipt = %+v, want %+v", *receipt, want)
	}
}

func TestTransfer_TransactionIDsIncrease(t *testing.T) {
	e := newTestEngine()
	mustCreate(t, e, "alice", 100)
	mustCreate(t, e, "bob", 50)

	first, _ := e.Transfer("alice", "bob", 1, 0)
	second, _ := e.Transfer("bob", "alice", 1, 0)

	if second.TransactionID <= first.TransactionID {
		t.Fatalf("transaction IDs not increasing: %d then %d", first.TransactionID, second.TransactionID)
	}
}
