package outbox

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"sync"
	"time"
)

// 投遞請求攜帶的 header 名稱。
const (
	// headerDeliveryID 每次投遞嘗試都不同，供 merchant 記錄投遞審計。
	headerDeliveryID = "X-Delivery-ID"
	// headerEventID 跨所有重試恆定，是 merchant 端去重的唯一鍵。
	headerEventID = "X-Event-ID"
	// headerEventType 讓 merchant 不必解析負載就能路由。
	headerEventType = "X-Event-Type"
	// headerTimestamp 是簽章時的 Unix 秒，供 merchant 設定重播窗口。
	headerTimestamp = "X-Timestamp"
	// headerSignature 是 HMAC-SHA256(secret, timestamp + "." + payload) 的十六進位表示。
	headerSignature = "X-Signature-SHA256"
	// headerSequence 是引擎指派的單調序號。
	// 因為本引擎不保證 per-merchant FIFO，這個欄位是 merchant 自行處理亂序的依據。
	headerSequence = "X-Event-Sequence"
)

// SecretStore 提供 merchant 的簽章密鑰。
//
// 抽成介面是為了配合實務部署：密鑰的真實來源是資料庫或 KMS，但它變動頻率極低，
// 因此正確做法是服務啟動時載入記憶體、由這個介面提供讀取，
// 並在輪替時同時更新後端與快取（見 MemorySecretStore.Put）。
// 這樣每次投遞都不必回資料庫，卻仍保有即時輪替的能力。
type SecretStore interface {
	SecretFor(merchantID string) ([]byte, error)
}

// MemorySecretStore 是以記憶體快取為底的 SecretStore 實作，可安全並發使用。
type MemorySecretStore struct {
	mu      sync.RWMutex
	secrets map[string][]byte
}

// NewMemorySecretStore 建立一個記憶體密鑰庫。
func NewMemorySecretStore() *MemorySecretStore {
	return &MemorySecretStore{secrets: make(map[string][]byte)}
}

// Put 寫入或輪替某 merchant 的密鑰，立即生效。
// 實務上這裡會與資料庫寫入包在同一個流程內，維持後端與快取一致。
func (s *MemorySecretStore) Put(merchantID string, secret []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// 存副本：呼叫端之後改動自己的 slice 不該影響已註冊的密鑰。
	s.secrets[merchantID] = append([]byte(nil), secret...)
}

// SecretFor 取得密鑰副本。
//
// 刻意回傳副本而非內部 slice：密鑰會被交到 HMAC 計算與呼叫端手上，
// 若外流的是內部 slice，任何一處的改動都會靜默破壞整個服務的簽章。
func (s *MemorySecretStore) SecretFor(merchantID string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	secret, ok := s.secrets[merchantID]
	if !ok {
		return nil, fmt.Errorf("%w: merchant %s", ErrSecretNotFound, merchantID)
	}
	return append([]byte(nil), secret...), nil
}

// signer 組裝投遞請求的 header 並計算簽章。
type signer struct {
	secrets SecretStore
}

// buildHeaders 產生一次投遞嘗試所需的完整 header，並回傳該次嘗試的 Delivery-ID。
//
// 密鑰在此取得而非在引擎啟動時快取進 entry：密鑰輪替後，
// 下一次重試就會自動用新密鑰簽章，不需要任何額外的失效機制。
func (s *signer) buildHeaders(rec EventRecord, now time.Time) (map[string]string, string, error) {
	secret, err := s.secrets.SecretFor(rec.Event.MerchantID)
	if err != nil {
		return nil, "", err
	}

	deliveryID, err := newDeliveryID()
	if err != nil {
		return nil, "", err
	}

	timestamp := strconv.FormatInt(now.Unix(), 10)
	headers := map[string]string{
		headerDeliveryID: deliveryID,
		headerEventID:    string(rec.Event.EventID),
		headerEventType:  rec.Event.EventType,
		headerTimestamp:  timestamp,
		headerSequence:   strconv.FormatUint(rec.Event.Sequence, 10),
		headerSignature:  sign(secret, timestamp, rec.Event.Payload),
	}
	return headers, deliveryID, nil
}

// sign 計算 HMAC-SHA256(secret, timestamp + "." + payload)。
//
// 時間戳必須納入簽章範圍，否則攻擊者可以攔下一個合法請求，
// 在任意時間點原封不動重播——簽章仍然有效，因為它只涵蓋了負載。
// 把時間戳簽進去，merchant 才能安全地拒絕窗口外的請求。
func sign(secret []byte, timestamp string, payload []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// newDeliveryID 產生每次嘗試唯一的投遞編號。
func newDeliveryID() (string, error) {
	var buf [12]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("outbox: generate delivery id: %w", err)
	}
	return "dlv_" + hex.EncodeToString(buf[:]), nil
}
