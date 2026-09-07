package main

import (
	"sync/atomic"
)

type Manipulation string

const (
	MDeposit  Manipulation = "deposit"
	MRetrieve Manipulation = "retrieve"
	MTransfer Manipulation = "transfer"
)

func main() {
	bankSystem := NewBankSystem()
}

type BankSystem struct {
	ID      int64
	Account map[string]*Account
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
