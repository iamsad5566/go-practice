package main

import (
	"sort"
	"strconv"
	"sync/atomic"
)

type Manipulation string
type TransferStatus int

const (
	MDeposit  Manipulation = "deposit"
	MRetrieve Manipulation = "retrieve"
	MTransfer Manipulation = "transfer"
	MRollback Manipulation = "rollback"
)

const (
	PENDING TransferStatus = iota
	SUCCESS
	FAILED
	CANCELLED
)

func main() {
	bankSystem := NewBankSystem()
}

type BankSystem struct {
	ID                 int64
	Account            map[string]*Account
	SchedualedTransfer []*ScheduledOrder
}

type Account struct {
	ID        string
	Balance   int64
	CreatedAt int64
	UpdatedAt int64
	Records   []*Record
}

type Record struct {
	ID        int64
	AccountID string
	Manipulation
	Amount        int64
	CreatedAt     int64
	TargetAccount string
	IsRolledBack  bool
}

type ScheduledOrder struct {
	Record
	Status             TransferStatus
	ScheduledTimestamp int64
}

func NewBankSystem() *BankSystem {
	return &BankSystem{
		Account: make(map[string]*Account),
	}
}

func (b *BankSystem) CreateAccount(timestamp int64, accountId string) bool {
	if _, ok := b.Account[accountId]; !ok {
		account := &Account{
			ID:        accountId,
			Balance:   0,
			CreatedAt: timestamp,
			UpdatedAt: timestamp,
		}
		b.Account[accountId] = account
		return true
	} else {
		return false
	}
}

func (b *BankSystem) Deposit(timestamp int64, accountId string, amount int64) (int64, bool) {
	if amount <= 0 {
		return 0, false
	}

	if account, ok := b.Account[accountId]; !ok {
		return 0, false
	} else {
		account.Balance += amount

		account.Records = append(account.Records, &Record{
			ID:           atomic.AddInt64(&b.ID, 1),
			AccountID:    accountId,
			Manipulation: MDeposit,
			Amount:       amount,
			CreatedAt:    timestamp,
		})
		return account.Balance, true
	}
}

func (b *BankSystem) Transfer(timestamp int64, fromId string, toId string, amount int64) bool {
	if amount <= 0 {
		return false
	}

	accountFrom := b.Account[fromId]
	accountTo := b.Account[toId]
	if accountFrom == nil || accountTo == nil || accountFrom == accountTo {
		return false
	}

	if accountFrom.Balance < amount {
		return false
	}

	accountFrom.Balance -= amount
	accountTo.Balance += amount

	accountFrom.Records = append(accountFrom.Records, &Record{
		ID:            atomic.AddInt64(&b.ID, 1),
		AccountID:     accountFrom.ID,
		Manipulation:  MTransfer,
		Amount:        -amount,
		CreatedAt:     timestamp,
		TargetAccount: accountTo.ID,
	})

	accountTo.Records = append(accountTo.Records, &Record{
		ID:            atomic.AddInt64(&b.ID, 1),
		AccountID:     accountTo.ID,
		Manipulation:  MTransfer,
		Amount:        amount,
		CreatedAt:     timestamp,
		TargetAccount: accountFrom.ID,
	})
	return true
}

func (b *BankSystem) GetTotalTransactedAmount(timestamp int64, accountId string) (int64, bool) {
	account := b.Account[accountId]
	totalTransfer := int64(0)
	if account == nil {
		return totalTransfer, false
	}

	for _, record := range account.Records {
		if record.CreatedAt <= timestamp && record.Manipulation == MTransfer && record.Amount < 0 {
			totalTransfer += record.Amount
		}
	}
	return -totalTransfer, true
}

func (b *BankSystem) GetTopKAccountsByTransfers(timestamp int64, k int) []string {
	type acountTmp struct {
		ID     string
		Amount int64
	}
	accounts := make([]acountTmp, 0, len(b.Account))

	for _, account := range b.Account {
		transacted, _ := b.GetTotalTransactedAmount(timestamp, account.ID)
		accounts = append(accounts, acountTmp{ID: account.ID, Amount: transacted})
	}

	sort.Slice(accounts, func(i, j int) bool {
		if accounts[i].Amount != accounts[j].Amount {
			return accounts[i].Amount > accounts[j].Amount
		}
		return accounts[i].ID < accounts[j].ID
	})

	res := make([]string, min(k, len(accounts)))
	for i := 0; i < min(k, len(accounts)); i++ {
		res[i] = accounts[i].ID
	}

	return res
}

func (b *BankSystem) ScheduleTransfer(timestamp int64, scheduleTime int64, fromId string, toId string, amount int64) (string, bool) {
	if fromId == toId {
		return "", false
	}

	fromAccount := b.Account[fromId]
	toAccount := b.Account[toId]
	if fromAccount == nil || toAccount == nil {
		return "", false
	}

	scheduledRecord := &Record{
		ID:            atomic.AddInt64(&b.ID, 1),
		AccountID:     fromId,
		TargetAccount: toId,
		Amount:        amount,
		CreatedAt:     timestamp,
	}
	b.SchedualedTransfer = append(b.SchedualedTransfer, &ScheduledOrder{
		Record:             *scheduledRecord,
		Status:             PENDING,
		ScheduledTimestamp: scheduleTime,
	})
	schId := strconv.FormatInt(b.ID, 10)
	return schId, true
}

func (b *BankSystem) ExecuteScheduledTransfers(timestamp int64) int {
	exed := 0
	sort.Slice(b.SchedualedTransfer, func(i, j int) bool {
		if b.SchedualedTransfer[i].ScheduledTimestamp != b.SchedualedTransfer[j].ScheduledTimestamp {
			return b.SchedualedTransfer[i].ScheduledTimestamp < b.SchedualedTransfer[j].ScheduledTimestamp
		}
		return b.SchedualedTransfer[i].CreatedAt < b.SchedualedTransfer[j].CreatedAt
	})

	for _, order := range b.SchedualedTransfer {
		if order.Status != PENDING {
			continue
		}

		if order.ScheduledTimestamp > timestamp {
			break
		}
		balance := b.Account[order.AccountID].Balance
		if balance < order.Amount {
			order.Status = FAILED
		} else {
			b.Account[order.AccountID].Balance -= order.Amount
			b.Account[order.TargetAccount].Balance += order.Amount
			b.Account[order.AccountID].Records = append(b.Account[order.AccountID].Records, &Record{
				ID:            atomic.AddInt64(&b.ID, 1),
				AccountID:     order.AccountID,
				Manipulation:  MTransfer,
				Amount:        -order.Amount,
				CreatedAt:     order.ScheduledTimestamp,
				TargetAccount: order.TargetAccount,
			})
			b.Account[order.TargetAccount].Records = append(b.Account[order.TargetAccount].Records, &Record{
				ID:            atomic.AddInt64(&b.ID, 1),
				AccountID:     order.TargetAccount,
				Manipulation:  MTransfer,
				Amount:        order.Amount,
				CreatedAt:     order.ScheduledTimestamp,
				TargetAccount: order.AccountID,
			})
			order.Status = SUCCESS
			exed++
		}
	}
	return exed
}

func (b *BankSystem) CancelScheduledTransfer(timestamp int64, transferId string) bool {
	var targetOrder *ScheduledOrder
	for _, order := range b.SchedualedTransfer {
		if strconv.FormatInt(order.ID, 10) == transferId {
			targetOrder = order
		}
	}

	if targetOrder == nil || targetOrder.Status != PENDING || targetOrder.ScheduledTimestamp < timestamp {
		return false
	}

	targetOrder.Status = CANCELLED
	return true
}

func (b *BankSystem) RollbackTransfer(timestamp int64, transferRecordId int64) bool {
	var targetOrder *ScheduledOrder
	for _, order := range b.SchedualedTransfer {
		if order.ID == transferRecordId {
			targetOrder = order
		}
	}

	if targetOrder != nil && targetOrder.Status == CANCELLED {
		return false
	}

	if targetOrder != nil && targetOrder.Status == PENDING {
		targetOrder.Status = CANCELLED
		return true
	}

	var transferRecordFrom *Record
	var transferRecordTo *Record

	for _, account := range b.Account {
		for _, record := range account.Records {
			if record.ID == transferRecordId {
				if record.Amount < 0 {
					transferRecordFrom = record
				} else {
					transferRecordTo = record
				}
			}
		}
	}

	if transferRecordFrom == nil {
		for _, record := range b.Account[transferRecordTo.TargetAccount].Records {
			if record.CreatedAt == transferRecordTo.CreatedAt {
				transferRecordFrom = record
			}
		}
	} else if transferRecordTo == nil {
		for _, record := range b.Account[transferRecordFrom.TargetAccount].Records {
			if record.CreatedAt == transferRecordFrom.CreatedAt {
				transferRecordTo = record
			}
		}
	}

	if transferRecordFrom == nil || transferRecordTo == nil || transferRecordFrom.Manipulation == MRollback || transferRecordFrom.IsRolledBack || transferRecordTo.IsRolledBack {
		return false
	}

	if b.Account[transferRecordTo.AccountID].Balance < abs(transferRecordTo.Amount) {
		return false
	}

	b.Account[transferRecordFrom.AccountID].Balance += abs(transferRecordFrom.Amount)
	b.Account[transferRecordTo.AccountID].Balance -= abs(transferRecordTo.Amount)
	rollBackRecordF := &Record{
		ID:            atomic.AddInt64(&b.ID, 10),
		AccountID:     transferRecordFrom.AccountID,
		Manipulation:  MRollback,
		CreatedAt:     timestamp,
		TargetAccount: transferRecordTo.AccountID,
		Amount:        abs(transferRecordFrom.Amount),
	}

	rollBackRecordT := &Record{
		ID:            atomic.AddInt64(&b.ID, 10),
		AccountID:     transferRecordTo.AccountID,
		Manipulation:  MRollback,
		CreatedAt:     timestamp,
		TargetAccount: transferRecordFrom.AccountID,
		Amount:        -abs(transferRecordTo.Amount),
	}

	transferRecordFrom.IsRolledBack = true
	transferRecordTo.IsRolledBack = true
	b.Account[transferRecordFrom.AccountID].Records = append(b.Account[transferRecordFrom.AccountID].Records, rollBackRecordF)
	b.Account[transferRecordTo.AccountID].Records = append(b.Account[transferRecordTo.AccountID].Records, rollBackRecordT)
	return true
}

func min(i, j int) int {
	if i < j {
		return i
	}
	return j
}

func abs(i int64) int64 {
	if i < 0 {
		return -i
	}
	return i
}
