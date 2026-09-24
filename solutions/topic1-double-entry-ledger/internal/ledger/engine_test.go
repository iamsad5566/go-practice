package ledger_test

import (
	"errors"
	"testing"

	"bank-system/internal/ledger"
)

func mustCreate(t *testing.T, e *ledger.Engine, id string, balance int64) {
	t.Helper()
	if err := e.CreateAccount(id, balance); err != nil {
		t.Fatalf("CreateAccount(%q, %d): unexpected error: %v", id, balance, err)
	}
}

func assertBalance(t *testing.T, e *ledger.Engine, id string, want int64) {
	t.Helper()
	got, err := e.GetBalance(id)
	if err != nil {
		t.Fatalf("GetBalance(%q): unexpected error: %v", id, err)
	}
	if got != want {
		t.Fatalf("GetBalance(%q) = %d, want %d", id, got, want)
	}
}

func assertErrorIs(t *testing.T, got, want error) {
	t.Helper()
	if !errors.Is(got, want) {
		t.Fatalf("error = %v, want %v", got, want)
	}
}

func TestCreateAccount_ThenGetBalance(t *testing.T) {
	e := ledger.NewEngine()

	mustCreate(t, e, "alice", 100)

	assertBalance(t, e, "alice", 100)
}

func TestCreateAccount_ZeroBalanceIsAllowed(t *testing.T) {
	e := ledger.NewEngine()

	mustCreate(t, e, "alice", 0)

	assertBalance(t, e, "alice", 0)
}

func TestCreateAccount_Rejects(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		balance int64
		wantErr error
	}{
		{"empty id", "", 100, ledger.ErrInvalidID},
		{"negative balance", "bob", -1, ledger.ErrInvalidAmount},
		{"duplicate id", "alice", 100, ledger.ErrAccountExists},
		{"system fee collector id", ledger.FeeCollectorID, 100, ledger.ErrAccountExists},
		{"any system-prefixed id", "system:treasury", 100, ledger.ErrAccountExists},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := ledger.NewEngine()
			mustCreate(t, e, "alice", 100)

			err := e.CreateAccount(tt.id, tt.balance)

			assertErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestCreateAccount_DuplicateDoesNotOverwriteBalance(t *testing.T) {
	e := ledger.NewEngine()
	mustCreate(t, e, "alice", 100)

	_ = e.CreateAccount("alice", 999)

	assertBalance(t, e, "alice", 100)
}

func TestGetBalance_UnknownAccount(t *testing.T) {
	e := ledger.NewEngine()

	_, err := e.GetBalance("ghost")

	assertErrorIs(t, err, ledger.ErrAccountNotFound)
}

func TestGetBalance_SystemAccountIsNotExposed(t *testing.T) {
	e := ledger.NewEngine()

	_, err := e.GetBalance(ledger.FeeCollectorID)

	assertErrorIs(t, err, ledger.ErrAccountNotFound)
}
