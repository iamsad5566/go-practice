package ledger_test

import (
	"math"
	"reflect"
	"testing"

	"bank-system/internal/ledger"
)

func history(t *testing.T, e *ledger.Engine, accountID string) []ledger.TransactionRecord {
	t.Helper()
	records, err := e.GetTransactionHistory(accountID)
	if err != nil {
		t.Fatalf("GetTransactionHistory(%q): unexpected error: %v", accountID, err)
	}
	return records
}

func assertHistory(t *testing.T, e *ledger.Engine, accountID string, want []ledger.TransactionRecord) {
	t.Helper()
	got := history(t, e, accountID)
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("history(%q) =\n  %+v\nwant\n  %+v", accountID, got, want)
	}
}

func TestHistory_NewAccountIsEmpty(t *testing.T) {
	e := newTestEngine()
	mustCreate(t, e, "alice", 100)

	assertHistory(t, e, "alice", nil)
}

func TestHistory_Rejects(t *testing.T) {
	tests := []struct {
		name      string
		accountID string
	}{
		{"unknown account", "ghost"},
		{"system account", ledger.FeeCollectorID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newTestEngine()

			_, err := e.GetTransactionHistory(tt.accountID)

			assertErrorIs(t, err, ledger.ErrAccountNotFound)
		})
	}
}

func TestHistory_TransferIsRecordedOnBothSides(t *testing.T) {
	e := newTestEngine()
	mustCreate(t, e, "alice", 100)
	mustCreate(t, e, "bob", 0)

	receipt, err := e.Transfer("alice", "bob", 30, 5)
	assertNoError(t, err)

	base := ledger.TransactionRecord{
		ID:            receipt.TransactionID,
		Status:        ledger.StatusSuccess,
		FromAccountID: "alice",
		ToAccountID:   "bob",
		Amount:        30,
		Fee:           5,
		Timestamp:     fixedNow,
	}
	out, in := base, base
	out.Type = ledger.RecordTransferOut
	in.Type = ledger.RecordTransferIn
	assertHistory(t, e, "alice", []ledger.TransactionRecord{out})
	assertHistory(t, e, "bob", []ledger.TransactionRecord{in})
}

func TestHistory_HoldLifecycleIsRecorded(t *testing.T) {
	e := newTestEngine()
	mustCreate(t, e, "alice", 100)
	mustCreate(t, e, "bob", 0)

	mustHold(t, e, "h1", "alice", 30)
	assertNoError(t, e.ReleaseHold("h1"))
	mustHold(t, e, "h2", "alice", 20)
	assertNoError(t, e.SettleHold("h2", "bob"))

	assertHistory(t, e, "alice", []ledger.TransactionRecord{
		{ID: 1, Type: ledger.RecordHold, Status: ledger.StatusSuccess, FromAccountID: "alice", HoldID: "h1", Amount: 30, Timestamp: fixedNow},
		{ID: 2, Type: ledger.RecordRelease, Status: ledger.StatusSuccess, FromAccountID: "alice", HoldID: "h1", Amount: 30, Timestamp: fixedNow},
		{ID: 3, Type: ledger.RecordHold, Status: ledger.StatusSuccess, FromAccountID: "alice", HoldID: "h2", Amount: 20, Timestamp: fixedNow},
		{ID: 4, Type: ledger.RecordSettleOut, Status: ledger.StatusSuccess, FromAccountID: "alice", ToAccountID: "bob", HoldID: "h2", Amount: 20, Timestamp: fixedNow},
	})
	assertHistory(t, e, "bob", []ledger.TransactionRecord{
		{ID: 4, Type: ledger.RecordSettleIn, Status: ledger.StatusSuccess, FromAccountID: "alice", ToAccountID: "bob", HoldID: "h2", Amount: 20, Timestamp: fixedNow},
	})
}

func TestHistory_InsufficientBalanceTransferIsRecordedAsFailed(t *testing.T) {
	e := newTestEngine()
	mustCreate(t, e, "alice", 10)
	mustCreate(t, e, "bob", 0)

	_, err := e.Transfer("alice", "bob", 10, 1)
	assertErrorIs(t, err, ledger.ErrInsufficientBalance)

	assertHistory(t, e, "alice", []ledger.TransactionRecord{{
		ID:            1,
		Type:          ledger.RecordTransferOut,
		Status:        ledger.StatusFailed,
		FromAccountID: "alice",
		ToAccountID:   "bob",
		Amount:        10,
		Fee:           1,
		FailureReason: ledger.ErrInsufficientBalance.Error(),
		Timestamp:     fixedNow,
	}})
	assertHistory(t, e, "bob", nil)
}

func TestHistory_InsufficientBalanceHoldIsRecordedAsFailed(t *testing.T) {
	e := newTestEngine()
	mustCreate(t, e, "alice", 10)

	err := e.Hold("h1", "alice", 11)
	assertErrorIs(t, err, ledger.ErrInsufficientBalance)

	assertHistory(t, e, "alice", []ledger.TransactionRecord{{
		ID:            1,
		Type:          ledger.RecordHold,
		Status:        ledger.StatusFailed,
		FromAccountID: "alice",
		HoldID:        "h1",
		Amount:        11,
		FailureReason: ledger.ErrInsufficientBalance.Error(),
		Timestamp:     fixedNow,
	}})
}

func TestHistory_OtherFailuresAndIdempotentRetriesAreNotRecorded(t *testing.T) {
	e := newTestEngine()
	mustCreate(t, e, "alice", 100)
	mustCreate(t, e, "bob", 0)
	mustCreate(t, e, "whale", math.MaxInt64)
	mustHold(t, e, "h1", "alice", 10)
	before := history(t, e, "alice")

	_, _ = e.Transfer("alice", "bob", -1, 0)  // invalid amount
	_, _ = e.Transfer("alice", "alice", 1, 0) // self transfer
	_, _ = e.Transfer("alice", "whale", 1, 0) // overflow
	_ = e.Hold("h1", "alice", 10)             // idempotent retry
	_ = e.Hold("h1", "alice", 99)             // conflict
	_ = e.SettleHold("h1", "alice")           // self settle
	assertNoError(t, e.ReleaseHold("h1"))     // recorded
	_ = e.ReleaseHold("h1")                   // idempotent retry
	_ = e.SettleHold("h1", "bob")             // finalized

	got := history(t, e, "alice")
	if len(got) != len(before)+1 || got[len(got)-1].Type != ledger.RecordRelease {
		t.Fatalf("history = %+v, want only the original hold and one release", got)
	}
	assertHistory(t, e, "bob", nil)
	assertHistory(t, e, "whale", nil)
}

func TestHistory_IsInChronologicalOrder(t *testing.T) {
	e := newTestEngine()
	mustCreate(t, e, "alice", 100)
	mustCreate(t, e, "bob", 100)

	_, _ = e.Transfer("alice", "bob", 1, 0)
	_, _ = e.Transfer("bob", "alice", 2, 0)
	mustHold(t, e, "h1", "alice", 3)
	_, _ = e.Transfer("alice", "bob", 1000, 0) // failed, still recorded

	got := history(t, e, "alice")
	wantTypes := []ledger.RecordType{ledger.RecordTransferOut, ledger.RecordTransferIn, ledger.RecordHold, ledger.RecordTransferOut}
	if len(got) != len(wantTypes) {
		t.Fatalf("len(history) = %d, want %d", len(got), len(wantTypes))
	}
	for i, r := range got {
		if r.Type != wantTypes[i] {
			t.Fatalf("history[%d].Type = %s, want %s", i, r.Type, wantTypes[i])
		}
		if i > 0 && r.ID <= got[i-1].ID {
			t.Fatalf("history IDs not increasing: %d then %d", got[i-1].ID, r.ID)
		}
	}
}

func TestHistory_ReturnsACopy(t *testing.T) {
	e := newTestEngine()
	mustCreate(t, e, "alice", 100)
	mustCreate(t, e, "bob", 0)
	_, _ = e.Transfer("alice", "bob", 1, 0)

	got := history(t, e, "alice")
	got[0].Amount = 999

	if history(t, e, "alice")[0].Amount != 1 {
		t.Fatal("mutating the returned history changed the ledger")
	}
}
