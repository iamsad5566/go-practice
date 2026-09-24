package main

import (
	"errors"
	"sync"
	"testing"
)

func TestLevel1_CreateAccount(t *testing.T) {
	ledger := NewLedger()

	// 1. Successfully create account
	err := ledger.CreateAccount("acc1")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	// 2. Check initial balance
	bal, err := ledger.GetBalance("acc1")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if bal != 0 {
		t.Fatalf("expected balance 0, got %d", bal)
	}

	// 3. Duplicate account should return ErrAccountAlreadyExists
	err = ledger.CreateAccount("acc1")
	if !errors.Is(err, ErrAccountAlreadyExists) {
		t.Fatalf("expected ErrAccountAlreadyExists, got %v", err)
	}

	// 4. Empty account ID should return error
	err = ledger.CreateAccount("")
	if err == nil {
		t.Fatalf("expected error for empty accountID, got nil")
	}
}

func TestLevel1_DepositAndWithdraw(t *testing.T) {
	ledger := NewLedger()
	_ = ledger.CreateAccount("alice")

	// 1. Deposit
	newBal, err := ledger.Deposit("alice", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if newBal != 100 {
		t.Fatalf("expected balance 100, got %d", newBal)
	}

	// 2. Deposit invalid amount (<= 0)
	_, err = ledger.Deposit("alice", 0)
	if !errors.Is(err, ErrInvalidAmount) {
		t.Fatalf("expected ErrInvalidAmount for 0 deposit, got %v", err)
	}
	_, err = ledger.Deposit("alice", -50)
	if !errors.Is(err, ErrInvalidAmount) {
		t.Fatalf("expected ErrInvalidAmount for negative deposit, got %v", err)
	}

	// 3. Deposit to non-existent account
	_, err = ledger.Deposit("unknown", 50)
	if !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("expected ErrAccountNotFound, got %v", err)
	}

	// 4. Withdraw valid amount
	newBal, err = ledger.Withdraw("alice", 40)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if newBal != 60 {
		t.Fatalf("expected balance 60, got %d", newBal)
	}

	// 5. Withdraw insufficient funds
	_, err = ledger.Withdraw("alice", 100)
	if !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("expected ErrInsufficientFunds, got %v", err)
	}

	// 6. Withdraw invalid amount
	_, err = ledger.Withdraw("alice", -10)
	if !errors.Is(err, ErrInvalidAmount) {
		t.Fatalf("expected ErrInvalidAmount, got %v", err)
	}

	// 7. Withdraw non-existent account
	_, err = ledger.Withdraw("unknown", 10)
	if !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("expected ErrAccountNotFound, got %v", err)
	}

	// Final check on balance
	bal, err := ledger.GetBalance("alice")
	if err != nil || bal != 60 {
		t.Fatalf("expected final balance 60, got %d (err: %v)", bal, err)
	}
}

func TestLevel1_Concurrency(t *testing.T) {
	ledger := NewLedger()
	_ = ledger.CreateAccount("concurrent_acc")

	const goroutines = 50
	const opsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	// Concurrent deposits
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				_, _ = ledger.Deposit("concurrent_acc", 10)
			}
		}()
	}

	// Concurrent withdraws
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				_, _ = ledger.Withdraw("concurrent_acc", 5)
			}
		}()
	}

	wg.Wait()

	bal, err := ledger.GetBalance("concurrent_acc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Total deposited: 50 * 100 * 10 = 50000
	// Total withdrawn: 50 * 100 * 5 = 25000
	// Expected balance: 25000
	expected := int64(goroutines * opsPerGoroutine * 5)
	if bal != expected {
		t.Fatalf("expected balance %d, got %d", expected, bal)
	}
}
