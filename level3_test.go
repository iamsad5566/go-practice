package main

import (
	"errors"
	"testing"
)

func TestLevel3_ScheduleTransfer_Basic(t *testing.T) {
	ledger := NewLedger()
	_ = ledger.CreateAccount("alice")
	_ = ledger.CreateAccount("bob")
	_, _ = ledger.Deposit("alice", 200)

	// 1. Schedule a transfer
	payID1, err := ledger.ScheduleTransfer("alice", "bob", 100, 1000)
	if err != nil {
		t.Fatalf("unexpected error scheduling: %v", err)
	}
	if payID1 == "" {
		t.Fatalf("expected non-empty payment ID")
	}

	// Balance should NOT change yet
	balAlice, _ := ledger.GetBalance("alice")
	balBob, _ := ledger.GetBalance("bob")
	if balAlice != 200 || balBob != 0 {
		t.Fatalf("balances should not change upon scheduling, got alice=%d, bob=%d", balAlice, balBob)
	}

	// Status should be SCHEDULED
	status, err := ledger.GetPaymentStatus(payID1)
	if err != nil {
		t.Fatalf("unexpected error getting status: %v", err)
	}
	if status != StatusScheduled {
		t.Fatalf("expected status %s, got %s", StatusScheduled, status)
	}

	// 2. Process at time 500 (before execution time 1000) -> nothing executed
	results := ledger.ProcessScheduledPayments(500)
	if len(results) != 0 {
		t.Fatalf("expected 0 processed payments at t=500, got %d", len(results))
	}

	// 3. Process at time 1000 -> executed successfully
	results = ledger.ProcessScheduledPayments(1000)
	if len(results) != 1 {
		t.Fatalf("expected 1 processed payment at t=1000, got %d", len(results))
	}
	if results[0].PaymentID != payID1 || results[0].Status != StatusSuccess {
		t.Fatalf("expected payment success, got %+v", results[0])
	}

	// Verify balance changes
	balAlice, _ = ledger.GetBalance("alice")
	balBob, _ = ledger.GetBalance("bob")
	if balAlice != 100 || balBob != 100 {
		t.Fatalf("expected alice=100, bob=100, got alice=%d, bob=%d", balAlice, balBob)
	}

	// Verify TopSpenders includes this scheduled payment outflow
	top := ledger.GetTopSpenders(1)
	if len(top) != 1 || top[0].AccountID != "alice" || top[0].TotalOutflow != 100 {
		t.Fatalf("top spender should reflect scheduled payment outflow of 100: %+v", top)
	}

	// Verify payment status is now SUCCESS
	status, _ = ledger.GetPaymentStatus(payID1)
	if status != StatusSuccess {
		t.Fatalf("expected status %s, got %s", StatusSuccess, status)
	}
}

func TestLevel3_InsufficientFunds_AtExecution(t *testing.T) {
	ledger := NewLedger()
	_ = ledger.CreateAccount("alice")
	_ = ledger.CreateAccount("bob")
	_, _ = ledger.Deposit("alice", 50)

	// Alice schedules 100 at t=1000, but only has 50
	payID, err := ledger.ScheduleTransfer("alice", "bob", 100, 1000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	results := ledger.ProcessScheduledPayments(1000)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != StatusFailed {
		t.Fatalf("expected StatusFailed due to insufficient funds, got %+v", results[0])
	}

	// Balances should remain untouched
	balAlice, _ := ledger.GetBalance("alice")
	balBob, _ := ledger.GetBalance("bob")
	if balAlice != 50 || balBob != 0 {
		t.Fatalf("balances should be untouched: alice=%d, bob=%d", balAlice, balBob)
	}

	// Status should be FAILED
	status, _ := ledger.GetPaymentStatus(payID)
	if status != StatusFailed {
		t.Fatalf("expected status %s, got %s", StatusFailed, status)
	}
}

func TestLevel3_CancelPayment(t *testing.T) {
	ledger := NewLedger()
	_ = ledger.CreateAccount("alice")
	_ = ledger.CreateAccount("bob")
	_, _ = ledger.Deposit("alice", 100)

	payID, _ := ledger.ScheduleTransfer("alice", "bob", 50, 1000)

	// 1. Cancel payment
	err := ledger.CancelPayment(payID)
	if err != nil {
		t.Fatalf("unexpected error canceling payment: %v", err)
	}

	// 2. Status should be CANCELLED
	status, _ := ledger.GetPaymentStatus(payID)
	if status != StatusCancelled {
		t.Fatalf("expected status %s, got %s", StatusCancelled, status)
	}

	// 3. Canceling again should return ErrPaymentAlreadyProcessed
	err = ledger.CancelPayment(payID)
	if !errors.Is(err, ErrPaymentAlreadyProcessed) {
		t.Fatalf("expected ErrPaymentAlreadyProcessed, got %v", err)
	}

	// 4. Process at t=1500 should ignore cancelled payment
	results := ledger.ProcessScheduledPayments(1500)
	if len(results) != 0 {
		t.Fatalf("cancelled payment should not be processed, got %d results", len(results))
	}

	// Balances should be unchanged
	balAlice, _ := ledger.GetBalance("alice")
	if balAlice != 100 {
		t.Fatalf("expected alice balance 100, got %d", balAlice)
	}
}

func TestLevel3_ExecutionOrder(t *testing.T) {
	ledger := NewLedger()
	_ = ledger.CreateAccount("alice")
	_ = ledger.CreateAccount("bob")
	_, _ = ledger.Deposit("alice", 100)

	// Pay1: at t=2000, amount=60
	pay1, _ := ledger.ScheduleTransfer("alice", "bob", 60, 2000)
	// Pay2: at t=1000, amount=60 (should execute first!)
	pay2, _ := ledger.ScheduleTransfer("alice", "bob", 60, 1000)

	// At t=2500, process both:
	// pay2 executes first (alice 100 -> 40, SUCCESS)
	// pay1 executes second (alice 40 < 60, FAILS due to insufficient funds)
	results := ledger.ProcessScheduledPayments(2500)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results[0].PaymentID != pay2 || results[0].Status != StatusSuccess {
		t.Fatalf("expected pay2 first and success, got %+v", results[0])
	}
	if results[1].PaymentID != pay1 || results[1].Status != StatusFailed {
		t.Fatalf("expected pay1 second and failed, got %+v", results[1])
	}

	balAlice, _ := ledger.GetBalance("alice")
	if balAlice != 40 {
		t.Fatalf("expected alice balance 40, got %d", balAlice)
	}
}
