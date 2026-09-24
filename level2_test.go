package main

import (
	"errors"
	"sync"
	"testing"
)

func TestLevel2_Transfer_Basic(t *testing.T) {
	ledger := NewLedger()
	_ = ledger.CreateAccount("alice")
	_ = ledger.CreateAccount("bob")
	_, _ = ledger.Deposit("alice", 200)

	// 1. Valid transfer
	fromBal, toBal, err := ledger.Transfer("alice", "bob", 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fromBal != 150 || toBal != 50 {
		t.Fatalf("expected balances (150, 50), got (%d, %d)", fromBal, toBal)
	}

	// 2. Insufficient funds
	_, _, err = ledger.Transfer("alice", "bob", 300)
	if !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("expected ErrInsufficientFunds, got %v", err)
	}

	// 3. Self transfer
	_, _, err = ledger.Transfer("alice", "alice", 10)
	if !errors.Is(err, ErrSelfTransfer) {
		t.Fatalf("expected ErrSelfTransfer, got %v", err)
	}

	// 4. Invalid amount
	_, _, err = ledger.Transfer("alice", "bob", 0)
	if !errors.Is(err, ErrInvalidAmount) {
		t.Fatalf("expected ErrInvalidAmount, got %v", err)
	}

	// 5. Account not found
	_, _, err = ledger.Transfer("alice", "charlie", 10)
	if !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("expected ErrAccountNotFound, got %v", err)
	}
	_, _, err = ledger.Transfer("charlie", "bob", 10)
	if !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("expected ErrAccountNotFound, got %v", err)
	}
}

func TestLevel2_Transfer_DeadlockPrevention(t *testing.T) {
	ledger := NewLedger()
	_ = ledger.CreateAccount("userA")
	_ = ledger.CreateAccount("userB")
	_, _ = ledger.Deposit("userA", 100000)
	_, _ = ledger.Deposit("userB", 100000)

	const goroutines = 50
	const ops = 100

	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	// userA transfers to userB concurrently while userB transfers to userA
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				_, _, _ = ledger.Transfer("userA", "userB", 1)
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				_, _, _ = ledger.Transfer("userB", "userA", 1)
			}
		}()
	}

	wg.Wait()

	balA, _ := ledger.GetBalance("userA")
	balB, _ := ledger.GetBalance("userB")
	if balA+balB != 200000 {
		t.Fatalf("conservation of money violated! Total: %d, expected 200000", balA+balB)
	}
}

func TestLevel2_GetTopSpenders(t *testing.T) {
	ledger := NewLedger()
	_ = ledger.CreateAccount("alice")
	_ = ledger.CreateAccount("bob")
	_ = ledger.CreateAccount("charlie")
	_ = ledger.CreateAccount("david")

	_, _ = ledger.Deposit("alice", 1000)
	_, _ = ledger.Deposit("bob", 1000)
	_, _ = ledger.Deposit("charlie", 1000)
	_, _ = ledger.Deposit("david", 1000)

	// alice withdraws 200, transfers 100 to charlie -> total outflow: 300
	_, _ = ledger.Withdraw("alice", 200)
	_, _, _ = ledger.Transfer("alice", "charlie", 100)

	// bob transfers 300 to david -> total outflow: 300
	_, _, _ = ledger.Transfer("bob", "david", 300)

	// charlie withdraws 50 -> total outflow: 50
	_, _ = ledger.Withdraw("charlie", 50)

	// david has 0 outflow

	// Top 2 spenders:
	// alice: 300, bob: 300 -> tie breaker: "alice" < "bob" alphabetical
	top2 := ledger.GetTopSpenders(2)
	if len(top2) != 2 {
		t.Fatalf("expected 2 spenders, got %d", len(top2))
	}
	if top2[0].AccountID != "alice" || top2[0].TotalOutflow != 300 {
		t.Fatalf("expected top 1 to be alice (300), got %+v", top2[0])
	}
	if top2[1].AccountID != "bob" || top2[1].TotalOutflow != 300 {
		t.Fatalf("expected top 2 to be bob (300), got %+v", top2[1])
	}

	// Top 10 (requesting more than exists)
	all := ledger.GetTopSpenders(10)
	if len(all) != 4 {
		t.Fatalf("expected 4 spenders, got %d", len(all))
	}
	if all[2].AccountID != "charlie" || all[2].TotalOutflow != 50 {
		t.Fatalf("expected 3rd to be charlie (50), got %+v", all[2])
	}
	if all[3].AccountID != "david" || all[3].TotalOutflow != 0 {
		t.Fatalf("expected 4th to be david (0), got %+v", all[3])
	}

	// Top 0 or negative
	empty := ledger.GetTopSpenders(0)
	if len(empty) != 0 {
		t.Fatalf("expected empty slice for n=0, got %d", len(empty))
	}
}
