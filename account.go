package main

import (
	"math"
	"sync"
)

// Account 自己持有一把 Mutex 保護餘額，讓不同帳戶的操作可以完全平行，
// 不需要搶 ledger 層級的鎖。欄位不匯出，確保所有存取都經過加鎖的方法。
type Account struct {
	mu           sync.Mutex
	id           string
	balance      int64
	totalOutflow int64 // 累計支出：成功的 Withdraw 與 Transfer 轉出
}

// AccountSpending 是 GetTopSpenders 回傳的唯讀快照，不持有任何鎖。
type AccountSpending struct {
	AccountID    string
	TotalOutflow int64
}

func newAccount(id string) *Account {
	return &Account{id: id}
}

func (a *Account) ID() string {
	return a.id
}

func (a *Account) Balance() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.balance
}

func (a *Account) spending() AccountSpending {
	a.mu.Lock()
	defer a.mu.Unlock()
	return AccountSpending{AccountID: a.id, TotalOutflow: a.totalOutflow}
}

// deposit 假設 amount 已由呼叫端驗證為 > 0。
func (a *Account) deposit(amount int64) (int64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.checkCredit(amount); err != nil {
		return a.balance, err
	}
	a.applyCredit(amount)
	return a.balance, nil
}

// withdraw 假設 amount 已由呼叫端驗證為 > 0。
func (a *Account) withdraw(amount int64) (int64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.checkDebit(amount); err != nil {
		return a.balance, err
	}
	a.applyDebit(amount)
	return a.balance, nil
}

// 以下 check*/apply* 皆假設呼叫端已持有 a.mu。
// 拆成「先檢查、後套用」，讓 Transfer 能在兩邊都檢查通過後才動任何餘額，確保原子性。

func (a *Account) checkCredit(amount int64) error {
	if a.balance > math.MaxInt64-amount {
		return ErrBalanceOverflow
	}
	return nil
}

func (a *Account) checkDebit(amount int64) error {
	if amount > a.balance {
		return ErrInsufficientFunds
	}
	return nil
}

func (a *Account) applyCredit(amount int64) {
	a.balance += amount
}

func (a *Account) applyDebit(amount int64) {
	a.balance -= amount
	a.totalOutflow += amount
}
