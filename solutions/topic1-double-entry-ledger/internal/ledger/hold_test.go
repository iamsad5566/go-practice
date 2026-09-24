package ledger_test

import (
	"math"
	"testing"

	"bank-system/internal/ledger"
)

func mustHold(t *testing.T, e *ledger.Engine, holdID, accountID string, amount int64) {
	t.Helper()
	if err := e.Hold(holdID, accountID, amount); err != nil {
		t.Fatalf("Hold(%q, %q, %d): unexpected error: %v", holdID, accountID, amount, err)
	}
}

func assertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- Hold ---

func TestHold_ReducesAvailableBalance(t *testing.T) {
	e := newTestEngine()
	mustCreate(t, e, "alice", 100)

	mustHold(t, e, "h1", "alice", 30)

	assertBalance(t, e, "alice", 70)
}

func TestHold_HeldFundsCannotBeTransferred(t *testing.T) {
	e := newTestEngine()
	mustCreate(t, e, "alice", 100)
	mustCreate(t, e, "bob", 0)
	mustHold(t, e, "h1", "alice", 80)

	_, err := e.Transfer("alice", "bob", 30, 0)

	assertErrorIs(t, err, ledger.ErrInsufficientBalance)
}

func TestHold_Rejects(t *testing.T) {
	tests := []struct {
		name      string
		holdID    string
		accountID string
		amount    int64
		wantErr   error
	}{
		{"empty hold id", "", "alice", 10, ledger.ErrInvalidID},
		{"empty account id", "h1", "", 10, ledger.ErrInvalidID},
		{"zero amount", "h1", "alice", 0, ledger.ErrInvalidAmount},
		{"negative amount", "h1", "alice", -5, ledger.ErrInvalidAmount},
		{"unknown account", "h1", "ghost", 10, ledger.ErrAccountNotFound},
		{"system account", "h1", ledger.FeeCollectorID, 10, ledger.ErrAccountNotFound},
		{"insufficient balance", "h1", "alice", 101, ledger.ErrInsufficientBalance},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newTestEngine()
			mustCreate(t, e, "alice", 100)

			err := e.Hold(tt.holdID, tt.accountID, tt.amount)

			assertErrorIs(t, err, tt.wantErr)
			assertBalance(t, e, "alice", 100)
		})
	}
}

func TestHold_RetryWithSameParamsIsIdempotent(t *testing.T) {
	e := newTestEngine()
	mustCreate(t, e, "alice", 100)
	mustHold(t, e, "h1", "alice", 30)

	err := e.Hold("h1", "alice", 30)

	assertNoError(t, err)
	assertBalance(t, e, "alice", 70)
}

func TestHold_ReusingIDWithDifferentParamsConflicts(t *testing.T) {
	tests := []struct {
		name      string
		accountID string
		amount    int64
	}{
		{"different amount", "alice", 31},
		{"different account", "bob", 30},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newTestEngine()
			mustCreate(t, e, "alice", 100)
			mustCreate(t, e, "bob", 100)
			mustHold(t, e, "h1", "alice", 30)

			err := e.Hold("h1", tt.accountID, tt.amount)

			assertErrorIs(t, err, ledger.ErrHoldConflict)
			assertBalance(t, e, "alice", 70)
			assertBalance(t, e, "bob", 100)
		})
	}
}

func TestHold_ReusingFinalizedIDFails(t *testing.T) {
	tests := []struct {
		name     string
		finalize func(e *ledger.Engine) error
	}{
		{"after settle", func(e *ledger.Engine) error { return e.SettleHold("h1", "bob") }},
		{"after release", func(e *ledger.Engine) error { return e.ReleaseHold("h1") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newTestEngine()
			mustCreate(t, e, "alice", 100)
			mustCreate(t, e, "bob", 0)
			mustHold(t, e, "h1", "alice", 30)
			assertNoError(t, tt.finalize(e))

			err := e.Hold("h1", "alice", 30)

			assertErrorIs(t, err, ledger.ErrHoldFinalized)
		})
	}
}

func TestHold_FailedHoldDoesNotConsumeID(t *testing.T) {
	e := newTestEngine()
	mustCreate(t, e, "alice", 10)
	mustCreate(t, e, "bob", 100)
	_ = e.Hold("h1", "alice", 50)
	_, err := e.Transfer("bob", "alice", 40, 0)
	assertNoError(t, err)

	err = e.Hold("h1", "alice", 50)

	assertNoError(t, err)
	assertBalance(t, e, "alice", 0)
}

// --- SettleHold ---

func TestSettleHold_MovesHeldFundsToTarget(t *testing.T) {
	e := newTestEngine()
	mustCreate(t, e, "alice", 100)
	mustCreate(t, e, "bob", 10)
	mustHold(t, e, "h1", "alice", 30)

	err := e.SettleHold("h1", "bob")

	assertNoError(t, err)
	assertBalance(t, e, "alice", 70)
	assertBalance(t, e, "bob", 40)
	assertFeeBalance(t, e, 0)
}

func TestSettleHold_SettledFundsCannotBeReleased(t *testing.T) {
	e := newTestEngine()
	mustCreate(t, e, "alice", 100)
	mustCreate(t, e, "bob", 0)
	mustHold(t, e, "h1", "alice", 30)
	assertNoError(t, e.SettleHold("h1", "bob"))

	err := e.ReleaseHold("h1")

	assertErrorIs(t, err, ledger.ErrHoldFinalized)
	assertBalance(t, e, "alice", 70)
}

func TestSettleHold_RetryToSameTargetIsIdempotent(t *testing.T) {
	e := newTestEngine()
	mustCreate(t, e, "alice", 100)
	mustCreate(t, e, "bob", 0)
	mustHold(t, e, "h1", "alice", 30)
	assertNoError(t, e.SettleHold("h1", "bob"))

	err := e.SettleHold("h1", "bob")

	assertNoError(t, err)
	assertBalance(t, e, "bob", 30)
}

func TestSettleHold_Rejects(t *testing.T) {
	tests := []struct {
		name    string
		holdID  string
		to      string
		wantErr error
	}{
		{"empty hold id", "", "bob", ledger.ErrInvalidID},
		{"empty target", "h1", "", ledger.ErrInvalidID},
		{"unknown hold", "nope", "bob", ledger.ErrHoldNotFound},
		{"settle to hold owner", "h1", "alice", ledger.ErrSelfTransfer},
		{"unknown target", "h1", "ghost", ledger.ErrAccountNotFound},
		{"system target", "h1", ledger.FeeCollectorID, ledger.ErrAccountNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newTestEngine()
			mustCreate(t, e, "alice", 100)
			mustCreate(t, e, "bob", 0)
			mustHold(t, e, "h1", "alice", 30)

			err := e.SettleHold(tt.holdID, tt.to)

			assertErrorIs(t, err, tt.wantErr)
			assertBalance(t, e, "alice", 70)
			assertBalance(t, e, "bob", 0)
			assertNoError(t, e.ReleaseHold("h1")) // hold is still pending
		})
	}
}

func TestSettleHold_AlreadySettledToAnotherTargetConflicts(t *testing.T) {
	e := newTestEngine()
	mustCreate(t, e, "alice", 100)
	mustCreate(t, e, "bob", 0)
	mustCreate(t, e, "carol", 0)
	mustHold(t, e, "h1", "alice", 30)
	assertNoError(t, e.SettleHold("h1", "bob"))

	err := e.SettleHold("h1", "carol")

	assertErrorIs(t, err, ledger.ErrHoldConflict)
	assertBalance(t, e, "bob", 30)
	assertBalance(t, e, "carol", 0)
}

func TestSettleHold_AfterReleaseFails(t *testing.T) {
	e := newTestEngine()
	mustCreate(t, e, "alice", 100)
	mustCreate(t, e, "bob", 0)
	mustHold(t, e, "h1", "alice", 30)
	assertNoError(t, e.ReleaseHold("h1"))

	err := e.SettleHold("h1", "bob")

	assertErrorIs(t, err, ledger.ErrHoldFinalized)
	assertBalance(t, e, "alice", 100)
	assertBalance(t, e, "bob", 0)
}

func TestSettleHold_RejectsWhenTargetWouldOverflow(t *testing.T) {
	e := newTestEngine()
	mustCreate(t, e, "alice", 100)
	mustCreate(t, e, "whale", math.MaxInt64)
	mustHold(t, e, "h1", "alice", 30)

	err := e.SettleHold("h1", "whale")

	assertErrorIs(t, err, ledger.ErrOverflow)
	assertBalance(t, e, "whale", math.MaxInt64)
	assertNoError(t, e.ReleaseHold("h1")) // hold is still pending
	assertBalance(t, e, "alice", 100)
}

// --- ReleaseHold ---

func TestReleaseHold_RestoresAvailableBalance(t *testing.T) {
	e := newTestEngine()
	mustCreate(t, e, "alice", 100)
	mustHold(t, e, "h1", "alice", 30)

	err := e.ReleaseHold("h1")

	assertNoError(t, err)
	assertBalance(t, e, "alice", 100)
}

func TestReleaseHold_RetryIsIdempotent(t *testing.T) {
	e := newTestEngine()
	mustCreate(t, e, "alice", 100)
	mustHold(t, e, "h1", "alice", 30)
	assertNoError(t, e.ReleaseHold("h1"))

	err := e.ReleaseHold("h1")

	assertNoError(t, err)
	assertBalance(t, e, "alice", 100)
}

func TestReleaseHold_Rejects(t *testing.T) {
	tests := []struct {
		name    string
		holdID  string
		wantErr error
	}{
		{"empty hold id", "", ledger.ErrInvalidID},
		{"unknown hold", "nope", ledger.ErrHoldNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newTestEngine()

			err := e.ReleaseHold(tt.holdID)

			assertErrorIs(t, err, tt.wantErr)
		})
	}
}
