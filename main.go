package main

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"sync"
)

var _ Ledger = (*BankLedger)(nil)

// BankLedger 採用兩層鎖：
//   - mu (RWMutex) 只保護 accounts map 本身的結構（新增帳戶用寫鎖、查找用讀鎖）。
//   - 每個 Account 的 Mutex 保護各自的餘額。
//
// 查找完帳戶就釋放 map 讀鎖，餘額異動只鎖單一帳戶，
// 因此熱門帳戶的存提款不會阻塞其他帳戶，也不會阻塞 CreateAccount。
type BankLedger struct {
	mu       sync.RWMutex
	accounts map[string]*Account
}

func NewLedger() *BankLedger {
	return &BankLedger{
		accounts: make(map[string]*Account),
	}
}

func (b *BankLedger) CreateAccount(accountID string) error {
	if err := validateAccountID(accountID); err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if _, exists := b.accounts[accountID]; exists {
		return fmt.Errorf("create account %q: %w", accountID, ErrAccountAlreadyExists)
	}
	b.accounts[accountID] = newAccount(accountID)
	return nil
}

func (b *BankLedger) Deposit(accountID string, amount int64) (int64, error) {
	if err := validateAccountID(accountID); err != nil {
		return 0, err
	}
	if err := validateAmount(amount); err != nil {
		return 0, err
	}

	acc, err := b.getAccount(accountID)
	if err != nil {
		return 0, err
	}
	return acc.deposit(amount)
}

func (b *BankLedger) Withdraw(accountID string, amount int64) (int64, error) {
	if err := validateAccountID(accountID); err != nil {
		return 0, err
	}
	if err := validateAmount(amount); err != nil {
		return 0, err
	}

	acc, err := b.getAccount(accountID)
	if err != nil {
		return 0, err
	}
	return acc.withdraw(amount)
}

func (b *BankLedger) GetBalance(accountID string) (int64, error) {
	if err := validateAccountID(accountID); err != nil {
		return 0, err
	}

	acc, err := b.getAccount(accountID)
	if err != nil {
		return 0, err
	}
	return acc.Balance(), nil
}

func (b *BankLedger) Transfer(fromID, toID string, amount int64) (int64, int64, error) {
	if err := validateAccountID(fromID); err != nil {
		return 0, 0, err
	}
	if err := validateAccountID(toID); err != nil {
		return 0, 0, err
	}
	if fromID == toID {
		return 0, 0, fmt.Errorf("transfer %q: %w", fromID, ErrSelfTransfer)
	}
	if err := validateAmount(amount); err != nil {
		return 0, 0, err
	}

	from, to, err := b.getAccountPair(fromID, toID)
	if err != nil {
		return 0, 0, err
	}

	unlock := lockPair(from, to)
	defer unlock()

	// 兩邊都先檢查，全部通過才套用，任何一步失敗都不會留下半套狀態。
	if err := from.checkDebit(amount); err != nil {
		return 0, 0, fmt.Errorf("transfer from %q: %w", fromID, err)
	}
	if err := to.checkCredit(amount); err != nil {
		return 0, 0, fmt.Errorf("transfer to %q: %w", toID, err)
	}
	from.applyDebit(amount)
	to.applyCredit(amount)

	return from.balance, to.balance, nil
}

// GetTopSpenders 對每個帳戶各自加鎖取快照，不會一次鎖住所有帳戶，
// 因此排行期間其他交易仍可進行；代價是結果不是跨帳戶的全域一致快照。
func (b *BankLedger) GetTopSpenders(n int) []AccountSpending {
	if n <= 0 {
		return []AccountSpending{}
	}

	accounts := b.snapshotAccounts()
	spenders := make([]AccountSpending, 0, len(accounts))
	for _, acc := range accounts {
		spenders = append(spenders, acc.spending())
	}

	slices.SortFunc(spenders, func(x, y AccountSpending) int {
		if c := cmp.Compare(y.TotalOutflow, x.TotalOutflow); c != 0 {
			return c
		}
		return cmp.Compare(x.AccountID, y.AccountID)
	})

	return spenders[:min(n, len(spenders))]
}

// lockPair 一律依 AccountID 字典序加鎖，所有路徑的加鎖順序一致，
// 所以 A→B 與 B→A 同時轉帳也不會形成 ABBA deadlock。
// 呼叫端需保證 x、y 為不同帳戶。
func lockPair(x, y *Account) (unlock func()) {
	first, second := x, y
	if second.id < first.id {
		first, second = second, first
	}
	first.mu.Lock()
	second.mu.Lock()
	return func() {
		second.mu.Unlock()
		first.mu.Unlock()
	}
}

func (b *BankLedger) getAccountPair(fromID, toID string) (*Account, *Account, error) {
	b.mu.RLock()
	from, fromOK := b.accounts[fromID]
	to, toOK := b.accounts[toID]
	b.mu.RUnlock()

	if !fromOK {
		return nil, nil, fmt.Errorf("account %q: %w", fromID, ErrAccountNotFound)
	}
	if !toOK {
		return nil, nil, fmt.Errorf("account %q: %w", toID, ErrAccountNotFound)
	}
	return from, to, nil
}

func (b *BankLedger) snapshotAccounts() []*Account {
	b.mu.RLock()
	defer b.mu.RUnlock()

	accounts := make([]*Account, 0, len(b.accounts))
	for _, acc := range b.accounts {
		accounts = append(accounts, acc)
	}
	return accounts
}

// getAccount 只在查 map 時持有讀鎖。帳戶一旦建立就不會被移除，
// 所以釋放讀鎖後繼續使用 *Account 是安全的；若日後支援刪除帳戶需重新檢視這點。
func (b *BankLedger) getAccount(accountID string) (*Account, error) {
	b.mu.RLock()
	acc, ok := b.accounts[accountID]
	b.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("account %q: %w", accountID, ErrAccountNotFound)
	}
	return acc, nil
}

func validateAccountID(accountID string) error {
	if strings.TrimSpace(accountID) == "" {
		return ErrInvalidAccountID
	}
	return nil
}

func validateAmount(amount int64) error {
	if amount <= 0 {
		return fmt.Errorf("amount %d: %w", amount, ErrInvalidAmount)
	}
	return nil
}
