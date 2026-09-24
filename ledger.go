package main

import "errors"

var (
	ErrInvalidAccountID     = errors.New("invalid account id")
	ErrAccountAlreadyExists = errors.New("account already exists")
	ErrAccountNotFound      = errors.New("account not found")
	ErrInvalidAmount        = errors.New("invalid amount")
	ErrInsufficientFunds    = errors.New("insufficient funds")
	ErrBalanceOverflow      = errors.New("balance overflow")
	ErrSelfTransfer         = errors.New("cannot transfer to self")
)

type Ledger interface {
	// 建立新帳戶，初始餘額為 0
	CreateAccount(accountID string) error

	// 存款：增加餘額，回傳更新後的餘額
	Deposit(accountID string, amount int64) (newBalance int64, err error)

	// 提款：扣除餘額，回傳更新後的餘額
	Withdraw(accountID string, amount int64) (newBalance int64, err error)

	// 查詢餘額
	GetBalance(accountID string) (balance int64, err error)

	// 轉帳：從 fromID 轉帳 amount 給 toID，回傳兩者更新後的餘額 (fromBal, toBal, err)
	Transfer(fromID string, toID string, amount int64) (fromBal int64, toBal int64, err error)

	// 取得累計支出最高的前 n 名帳戶
	GetTopSpenders(n int) []AccountSpending
}
