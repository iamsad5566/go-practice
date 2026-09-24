package ledger

import "time"

type TransferReceipt struct {
	TransactionID int64
	Timestamp     time.Time
	FromAccountID string
	ToAccountID   string
	Amount        int64
	Fee           int64
}

type RecordType string

const (
	RecordTransferOut RecordType = "TRANSFER_OUT"
	RecordTransferIn  RecordType = "TRANSFER_IN"
	RecordHold        RecordType = "HOLD"
	RecordRelease     RecordType = "RELEASE"
	RecordSettleOut   RecordType = "SETTLE_OUT"
	RecordSettleIn    RecordType = "SETTLE_IN"
)

type RecordStatus string

const (
	StatusSuccess RecordStatus = "SUCCESS"
	StatusFailed  RecordStatus = "FAILED"
)

// TransactionRecord is one entry of an account's audit history. Fields that
// do not apply to a record's Type are left empty: FromAccountID is the account
// whose funds are acted on, ToAccountID is the receiving account (transfers
// and settlements only), and HoldID and Fee are set only where relevant.
type TransactionRecord struct {
	ID            int64
	Type          RecordType
	Status        RecordStatus
	FromAccountID string
	ToAccountID   string
	HoldID        string
	Amount        int64
	Fee           int64
	FailureReason string
	Timestamp     time.Time
}

func succeeded(r TransactionRecord) TransactionRecord {
	r.Status = StatusSuccess
	return r
}

func failed(r TransactionRecord, reason error) TransactionRecord {
	r.Status = StatusFailed
	r.FailureReason = reason.Error()
	return r
}
