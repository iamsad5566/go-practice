package main

import (
	"fmt"
	"sort"
)

type Ledger struct {
	Accounts     map[string]*Account
	Transactions map[string]*Transaction
}

type Account struct {
	AccountID string
	Balance   int64
	History   []string
}

type TransactionType int
type TransactionStatus int

const (
	DEPOSIT TransactionType = iota
	WITHDRAW
	TRANSFER
)

const (
	SUCCESS TransactionStatus = iota
	SCHEDULED
	FAILED
	ROLLED_BACK
)

type Transaction struct {
	TxID        string
	FromAccount string
	ToAccount   string
	Amount      int64
	Timestamp   int64
	TransactionType
	TransactionStatus
}

func NewLedger() *Ledger {
	return &Ledger{
		Accounts:     make(map[string]*Account),
		Transactions: make(map[string]*Transaction),
	}
}

func NewAccount(accountID string) *Account {
	return &Account{
		AccountID: accountID,
		Balance:   0,
		History:   make([]string, 0),
	}
}

func NewTransaction(txID, fromAccount, toAccount string, amount, timestamp int64,
	transactionType TransactionType, tranTransactionStatus TransactionStatus) *Transaction {
	return &Transaction{
		TxID:              txID,
		FromAccount:       fromAccount,
		ToAccount:         toAccount,
		Amount:            amount,
		Timestamp:         timestamp,
		TransactionType:   transactionType,
		TransactionStatus: tranTransactionStatus,
	}
}

func (l *Ledger) CreateAccount(accountID string) bool {
	if accountID == "" {
		return false
	}
	if _, ok := l.Accounts[accountID]; ok {
		return false
	}

	account := NewAccount(accountID)
	l.Accounts[accountID] = account
	return true
}

func (l *Ledger) Deposit(accountID string, amount int64) bool {
	if amount <= 0 || accountID == "" {
		return false
	}

	account, ok := l.Accounts[accountID]
	if !ok {
		return false
	}

	account.Balance += amount
	return true
}

func (l *Ledger) Withdraw(accountID string, amount int64) bool {
	if amount <= 0 || accountID == "" {
		return false
	}

	account, ok := l.Accounts[accountID]
	if !ok {
		return false
	}

	if account.Balance < amount {
		return false
	}

	account.Balance -= amount
	return true
}

func (l *Ledger) GetBalance(accountID string) (int64, bool) {
	if accountID == "" {
		return 0, false
	}

	account, ok := l.Accounts[accountID]
	if !ok {
		return 0, false
	} else {
		return account.Balance, true
	}
}

func (l *Ledger) Transfer(txID string, fromAccount string, toAccount string, amount int64, timestamp int64) bool {
	if txID == "" || amount <= 0 || fromAccount == toAccount || fromAccount == "" || toAccount == "" {
		return false
	}
	if _, ok := l.Transactions[txID]; ok {
		return false
	}

	from, okF := l.Accounts[fromAccount]
	to, okT := l.Accounts[toAccount]
	if !okF || !okT {
		return false
	}

	if from.Balance >= amount {
		transaction := NewTransaction(txID, fromAccount, toAccount, amount, timestamp, TRANSFER, SUCCESS)
		from.Balance -= amount
		to.Balance += amount
		l.Transactions[txID] = transaction
		from.History = append(from.History, txID)
		to.History = append(to.History, txID)
		return true
	}

	return false
}

func (l *Ledger) GetAccountHistory(accountID string) []Transaction {
	account, ok := l.Accounts[accountID]
	if !ok {
		return []Transaction{}
	}
	trans := make([]Transaction, len(account.History))
	for i, v := range account.History {
		t := l.Transactions[v]
		trans[i] = *t
	}
	sort.Slice(trans, func(i, j int) bool {
		if trans[i].Timestamp != trans[j].Timestamp {
			return trans[i].Timestamp < trans[j].Timestamp
		}
		return trans[i].TxID < trans[j].TxID
	})

	return trans
}

func (l *Ledger) GetTotalTransferVolume(accountID string) int64 {
	account, ok := l.Accounts[accountID]
	if !ok {
		return 0
	}

	total := int64(0)
	for _, txID := range account.History {
		trans := l.Transactions[txID]
		if trans.TransactionType == TRANSFER &&
			trans.FromAccount == accountID &&
			trans.TransactionStatus == SUCCESS {
			total += trans.Amount
		}
	}

	return total
}

func (l *Ledger) ScheduleTransfer(txID string, fromAccount string, toAccount string, amount int64, scheduleTime int64) bool {
	if txID == "" || amount <= 0 || fromAccount == toAccount || fromAccount == "" || toAccount == "" {
		return false
	}
	if _, ok := l.Transactions[txID]; ok {
		return false
	}

	from, okF := l.Accounts[fromAccount]
	to, okT := l.Accounts[toAccount]
	if !okF || !okT {
		return false
	}

	tx := NewTransaction(txID, fromAccount, toAccount, amount, scheduleTime, TRANSFER, SCHEDULED)
	l.Transactions[txID] = tx
	from.History = append(from.History, txID)
	to.History = append(to.History, txID)
	return true
}

func (l *Ledger) ProcessScheduledTransfers(currentTime int64) int {
	waitingList := make([]*Transaction, 0)
	for _, t := range l.Transactions {
		if t.TransactionType == TRANSFER && t.TransactionStatus == SCHEDULED &&
			t.Timestamp <= currentTime {
			waitingList = append(waitingList, t)
		}
	}

	sort.Slice(waitingList, func(i, j int) bool {
		if waitingList[i].Timestamp != waitingList[j].Timestamp {
			return waitingList[i].Timestamp < waitingList[j].Timestamp
		}
		return waitingList[i].TxID < waitingList[j].TxID
	})

	total := 0

	for _, t := range waitingList {
		from := l.Accounts[t.FromAccount]
		to := l.Accounts[t.ToAccount]
		if from.Balance >= t.Amount {
			from.Balance -= t.Amount
			to.Balance += t.Amount
			t.TransactionStatus = SUCCESS
			total++
		} else {
			t.TransactionStatus = FAILED
		}
	}

	return total
}

func (l *Ledger) RollbackTransaction(txID string) bool {
	tx, ok := l.Transactions[txID]
	if !ok {
		return false
	}

	if tx.TransactionStatus != SUCCESS {
		return false
	}

	from := l.Accounts[tx.FromAccount]
	to := l.Accounts[tx.ToAccount]
	if to.Balance >= tx.Amount {
		from.Balance += tx.Amount
		to.Balance -= tx.Amount
		tx.TransactionStatus = ROLLED_BACK
		return true
	}

	return false
}

func runLevel1Tests() {
	l := NewLedger()

	// Test 1: Account creation
	assert(l.CreateAccount("acc_1") == true, "Test 1.1 Failed: Create acc_1")
	assert(l.CreateAccount("acc_1") == false, "Test 1.2 Failed: Duplicate acc_1")
	assert(l.CreateAccount("") == false, "Test 1.3 Failed: Empty accountID")

	// Test 2: Initial Balance
	bal, ok := l.GetBalance("acc_1")
	assert(ok == true && bal == 0, "Test 2.1 Failed: Initial balance should be 0")
	_, ok = l.GetBalance("acc_unknown")
	assert(ok == false, "Test 2.2 Failed: Unknown account should return ok=false")

	// Test 3: Deposit
	assert(l.Deposit("acc_1", 100) == true, "Test 3.1 Failed: Deposit 100")
	bal, _ = l.GetBalance("acc_1")
	assert(bal == 100, "Test 3.2 Failed: Balance should be 100")
	assert(l.Deposit("acc_1", 0) == false, "Test 3.3 Failed: Deposit 0 should fail")
	assert(l.Deposit("acc_1", -50) == false, "Test 3.4 Failed: Deposit negative should fail")
	assert(l.Deposit("acc_unknown", 100) == false, "Test 3.5 Failed: Deposit to unknown should fail")

	// Test 4: Withdraw
	assert(l.Withdraw("acc_1", 40) == true, "Test 4.1 Failed: Withdraw 40")
	bal, _ = l.GetBalance("acc_1")
	assert(bal == 60, "Test 4.2 Failed: Balance should be 60")
	assert(l.Withdraw("acc_1", 100) == false, "Test 4.3 Failed: Withdraw insufficient balance")
	bal, _ = l.GetBalance("acc_1")
	assert(bal == 60, "Test 4.4 Failed: Balance should remain 60 after failed withdraw")
	assert(l.Withdraw("acc_1", 0) == false, "Test 4.5 Failed: Withdraw 0 should fail")

	fmt.Println(">>> Level 1 Tests Passed! <<<")
}

func runLevel2Tests() {
	l := NewLedger()
	l.CreateAccount("alice")
	l.CreateAccount("bob")
	l.CreateAccount("charlie")

	l.Deposit("alice", 500)
	l.Deposit("bob", 200)

	// Test 1: Basic Transfer
	assert(l.Transfer("tx_1", "alice", "bob", 150, 1000) == true, "Test 1.1 Failed: Transfer alice -> bob")
	balA, _ := l.GetBalance("alice")
	balB, _ := l.GetBalance("bob")
	assert(balA == 350, "Test 1.2 Failed: Alice balance should be 350")
	assert(balB == 350, "Test 1.3 Failed: Bob balance should be 350")

	// Test 2: Idempotency / Duplicate TxID
	assert(l.Transfer("tx_1", "alice", "bob", 50, 1001) == false, "Test 2.1 Failed: Duplicate tx_1 should fail")
	balA, _ = l.GetBalance("alice")
	assert(balA == 350, "Test 2.2 Failed: Balance should not change on duplicate tx")

	// Test 3: Insufficient Funds (Atomic rollback)
	assert(l.Transfer("tx_2", "alice", "bob", 400, 1002) == false, "Test 3.1 Failed: Alice overdraft")
	balA, _ = l.GetBalance("alice")
	balB, _ = l.GetBalance("bob")
	assert(balA == 350 && balB == 350, "Test 3.2 Failed: Balances must be untouched on failure")

	// Test 4: Self Transfer & Invalid Accounts
	assert(l.Transfer("tx_3", "alice", "alice", 50, 1003) == false, "Test 4.1 Failed: Self transfer invalid")
	assert(l.Transfer("tx_4", "alice", "unknown", 50, 1004) == false, "Test 4.2 Failed: Target unknown")
	assert(l.Transfer("tx_5", "unknown", "bob", 50, 1005) == false, "Test 4.3 Failed: Source unknown")
	assert(l.Transfer("", "alice", "bob", 50, 1006) == false, "Test 4.4 Failed: Empty TxID")
	assert(l.Transfer("tx_6", "alice", "bob", 0, 1007) == false, "Test 4.5 Failed: Zero amount")

	// Test 5: Multiple transfers and History Ordering
	// timestamp 出現平手與非線性插入
	assert(l.Transfer("tx_b", "bob", "charlie", 50, 2000) == true, "Test 5.1")
	assert(l.Transfer("tx_a", "alice", "charlie", 50, 2000) == true, "Test 5.2") // same timestamp as tx_b

	// Charlie's history should have tx_b and tx_a, ordered by Timestamp (2000), then TxID ("tx_a" < "tx_b")
	charlieHist := l.GetAccountHistory("charlie")
	assert(len(charlieHist) == 2, "Test 5.3 Failed: Charlie should have 2 transactions")
	assert(charlieHist[0].TxID == "tx_a", "Test 5.4 Failed: History tie-breaker sorting error")
	assert(charlieHist[1].TxID == "tx_b", "Test 5.5 Failed: History tie-breaker sorting error")

	// Test 6: GetTotalTransferVolume (only counts outgoing transfers)
	// Alice sent: tx_1(150) + tx_a(50) = 200
	assert(l.GetTotalTransferVolume("alice") == 200, "Test 6.1 Failed: Alice total transfer volume")
	// Bob sent: tx_b(50) = 50
	assert(l.GetTotalTransferVolume("bob") == 50, "Test 6.2 Failed: Bob total transfer volume")
	// Charlie sent: 0
	assert(l.GetTotalTransferVolume("charlie") == 0, "Test 6.3 Failed: Charlie sent 0")
	// Unknown account: 0
	assert(l.GetTotalTransferVolume("ghost") == 0, "Test 6.4 Failed: Unknown account volume")

	fmt.Println(">>> Level 2 Tests Passed! <<<")
}

func runLevel3Tests() {
	l := NewLedger()
	l.CreateAccount("alice")
	l.CreateAccount("bob")
	l.CreateAccount("charlie")

	l.Deposit("alice", 300)
	l.Deposit("bob", 100)

	// Test 1: Immediate Transfer (Level 2 behavior, status should be SUCCESS)
	assert(l.Transfer("tx_imm", "alice", "bob", 100, 1000) == true, "Test 1.1")
	balA, _ := l.GetBalance("alice")
	balB, _ := l.GetBalance("bob")
	assert(balA == 200 && balB == 200, "Test 1.2")

	// Test 2: ScheduleTransfer should not alter balances immediately
	// Alice wants to transfer 150 to charlie at t=3000
	assert(l.ScheduleTransfer("tx_sched_1", "alice", "charlie", 150, 3000) == true, "Test 2.1")
	// Bob wants to transfer 250 (more than current 200) to charlie at t=2000
	assert(l.ScheduleTransfer("tx_sched_2", "bob", "charlie", 250, 2000) == true, "Test 2.2")
	// Duplicate txID should fail
	assert(l.ScheduleTransfer("tx_imm", "alice", "charlie", 50, 4000) == false, "Test 2.3")

	balA, _ = l.GetBalance("alice")
	balB, _ = l.GetBalance("bob")
	balC, _ := l.GetBalance("charlie")
	assert(balA == 200 && balB == 200 && balC == 0, "Test 2.4: Balances untouched before process")

	// Test 3: Process at t=2500 (Only tx_sched_2 is due, but Bob has only 200 -> should FAIL)
	successCount := l.ProcessScheduledTransfers(2500)
	assert(successCount == 0, "Test 3.1: tx_sched_2 should fail due to insufficient funds")
	balB, _ = l.GetBalance("bob")
	balC, _ = l.GetBalance("charlie")
	assert(balB == 200 && balC == 0, "Test 3.2: Balances unchanged on failed schedule")

	// Test 4: Process at t=3500 (tx_sched_1 is due, Alice has 200 >= 150 -> should SUCCEED)
	successCount = l.ProcessScheduledTransfers(3500)
	assert(successCount == 1, "Test 4.1: tx_sched_1 should succeed")
	balA, _ = l.GetBalance("alice")
	balC, _ = l.GetBalance("charlie")
	assert(balA == 50, "Test 4.2: Alice balance 200 - 150 = 50")
	assert(balC == 150, "Test 4.3: Charlie balance 0 + 150 = 150")

	// Test 5: Re-running Process at same or later time shouldn't re-process finished transfers
	assert(l.ProcessScheduledTransfers(4000) == 0, "Test 5.1: No more pending transfers")

	// Test 6: Rollback transaction
	// Rollback tx_imm (alice -> bob 100)
	// Currently Alice=50, Bob=200. After rollback: Alice=150, Bob=100.
	assert(l.RollbackTransaction("tx_imm") == true, "Test 6.1: Rollback tx_imm should succeed")
	balA, _ = l.GetBalance("alice")
	balB, _ = l.GetBalance("bob")
	assert(balA == 150 && balB == 100, "Test 6.2: Balances reverted properly")

	// Cannot rollback already rolled back tx
	assert(l.RollbackTransaction("tx_imm") == false, "Test 6.3: Cannot rollback twice")

	// Rollback failure: Charlie tries to rollback tx_sched_1 (150 sent to charlie).
	// But suppose Charlie spends money first:
	l.Withdraw("charlie", 50) // Charlie now has 100, but rollback needs 150
	assert(l.RollbackTransaction("tx_sched_1") == false, "Test 6.4: Charlie doesn't have enough to refund")
	balA, _ = l.GetBalance("alice")
	balC, _ = l.GetBalance("charlie")
	assert(balA == 150 && balC == 100, "Test 6.5: Balances preserved on failed rollback")

	// Test 7: Total Transfer Volume with status filter
	// Alice sent tx_imm (ROLLED_BACK) and tx_sched_1 (SUCCESS 150). Only tx_sched_1 counts!
	assert(l.GetTotalTransferVolume("alice") == 150, "Test 7.1: Only SUCCESS tx counted")

	fmt.Println(">>> Level 3 Tests Passed! <<<")
}

func assert(cond bool, msg string) {
	if !cond {
		panic(msg)
	}
}

func main() {
	runLevel1Tests()
	runLevel2Tests()
	runLevel3Tests()
}
