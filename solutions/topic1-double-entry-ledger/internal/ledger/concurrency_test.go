package ledger_test

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"testing"
	"time"

	"bank-system/internal/ledger"
)

// runWithin fails the test if fn does not finish in time, which is how a
// deadlock shows up.
func runWithin(t *testing.T, timeout time.Duration, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		fn()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatalf("did not finish within %s: possible deadlock", timeout)
	}
}

func TestConcurrent_MixedOperationsConserveMoney(t *testing.T) {
	const (
		numAccounts    = 10
		initialBalance = 1_000
		numWorkers     = 200
		opsPerWorker   = 200
	)
	e := ledger.NewEngine()
	ids := make([]string, numAccounts)
	for i := range ids {
		ids[i] = fmt.Sprintf("acc-%d", i)
		mustCreate(t, e, ids[i], initialBalance)
	}

	runWithin(t, 30*time.Second, func() {
		var wg sync.WaitGroup
		for w := range numWorkers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				rng := rand.New(rand.NewPCG(uint64(w), 0))
				for i := range opsPerWorker {
					from := ids[rng.IntN(numAccounts)]
					to := ids[rng.IntN(numAccounts)]
					amount := rng.Int64N(50) + 1
					switch rng.IntN(5) {
					case 0, 1:
						_, _ = e.Transfer(from, to, amount, rng.Int64N(3))
					case 2:
						holdID := fmt.Sprintf("w%d-op%d", w, i)
						if e.Hold(holdID, from, amount) == nil {
							_ = e.SettleHold(holdID, to)
							_ = e.ReleaseHold(holdID) // releases only if settle was rejected
						}
					case 3:
						_, _ = e.GetBalance(from)
					case 4:
						_, _ = e.GetTransactionHistory(from)
					}
				}
			}()
		}
		wg.Wait()
	})

	var total int64
	for _, id := range ids {
		balance, err := e.GetBalance(id)
		assertNoError(t, err)
		if balance < 0 {
			t.Fatalf("GetBalance(%q) = %d, want non-negative", id, balance)
		}
		total += balance
	}
	if want := int64(numAccounts * initialBalance); total+e.FeeBalance() != want {
		t.Fatalf("money not conserved: accounts %d + fees %d = %d, want %d",
			total, e.FeeBalance(), total+e.FeeBalance(), want)
	}
}

func TestConcurrent_OpposingTransfersDoNotDeadlock(t *testing.T) {
	const rounds = 2_000
	e := ledger.NewEngine()
	mustCreate(t, e, "alice", rounds)
	mustCreate(t, e, "bob", rounds)

	runWithin(t, 10*time.Second, func() {
		var wg sync.WaitGroup
		for range rounds {
			wg.Add(2)
			go func() { defer wg.Done(); _, _ = e.Transfer("alice", "bob", 1, 0) }()
			go func() { defer wg.Done(); _, _ = e.Transfer("bob", "alice", 1, 0) }()
		}
		wg.Wait()
	})

	alice, _ := e.GetBalance("alice")
	bob, _ := e.GetBalance("bob")
	if alice+bob != 2*rounds {
		t.Fatalf("alice %d + bob %d = %d, want %d", alice, bob, alice+bob, 2*rounds)
	}
}

func TestConcurrent_SameHoldIDIsAppliedOnce(t *testing.T) {
	const callers = 100
	e := ledger.NewEngine()
	mustCreate(t, e, "alice", 100)

	var wg sync.WaitGroup
	errs := make(chan error, callers)
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- e.Hold("h1", "alice", 10)
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil && !errors.Is(err, ledger.ErrHoldInProgress) {
			t.Fatalf("Hold: unexpected error: %v", err)
		}
	}
	assertBalance(t, e, "alice", 90)
	if n := len(history(t, e, "alice")); n != 1 {
		t.Fatalf("len(history) = %d, want exactly one hold record", n)
	}
}

func TestConcurrent_SettleAndReleaseOfSameHoldHaveOneWinner(t *testing.T) {
	const holds = 500
	e := ledger.NewEngine()
	mustCreate(t, e, "alice", holds)
	mustCreate(t, e, "bob", 0)
	for i := range holds {
		mustHold(t, e, fmt.Sprintf("h%d", i), "alice", 1)
	}

	var wg sync.WaitGroup
	settleErrs := make([]error, holds)
	releaseErrs := make([]error, holds)
	for i := range holds {
		holdID := fmt.Sprintf("h%d", i)
		wg.Add(2)
		go func() { defer wg.Done(); settleErrs[i] = e.SettleHold(holdID, "bob") }()
		go func() { defer wg.Done(); releaseErrs[i] = e.ReleaseHold(holdID) }()
	}
	wg.Wait()

	var settled int64
	for i := range holds {
		settleWon, releaseWon := settleErrs[i] == nil, releaseErrs[i] == nil
		if settleWon == releaseWon {
			t.Fatalf("hold h%d: settle err %v, release err %v; want exactly one winner", i, settleErrs[i], releaseErrs[i])
		}
		if settleWon {
			settled++
		}
	}
	assertBalance(t, e, "bob", settled)
	assertBalance(t, e, "alice", holds-settled)
}
