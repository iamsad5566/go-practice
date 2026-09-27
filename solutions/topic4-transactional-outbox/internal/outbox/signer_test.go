package outbox

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"testing"
	"time"
)

func TestMemorySecretStore(t *testing.T) {
	store := NewMemorySecretStore()
	store.Put("merchant-1", []byte("s3cret"))

	got, err := store.SecretFor("merchant-1")
	if err != nil {
		t.Fatalf("SecretFor() 失敗: %v", err)
	}
	if string(got) != "s3cret" {
		t.Errorf("SecretFor() = %q, 預期 s3cret", got)
	}

	if _, err := store.SecretFor("unknown"); !errors.Is(err, ErrSecretNotFound) {
		t.Errorf("未知 merchant 應回 ErrSecretNotFound, 得到 %v", err)
	}
}

// 密鑰輪替：Put 覆寫後立即生效，不必重啟服務。
func TestMemorySecretStoreRotate(t *testing.T) {
	store := NewMemorySecretStore()
	store.Put("merchant-1", []byte("old"))
	store.Put("merchant-1", []byte("new"))

	got, _ := store.SecretFor("merchant-1")
	if string(got) != "new" {
		t.Fatalf("輪替後 SecretFor() = %q, 預期 new", got)
	}
}

// 回傳的密鑰必須是副本，否則呼叫端改動 slice 會污染整個服務的簽章。
func TestMemorySecretStoreReturnsCopy(t *testing.T) {
	store := NewMemorySecretStore()
	secret := []byte("s3cret")
	store.Put("merchant-1", secret)

	secret[0] = 'X'
	got, _ := store.SecretFor("merchant-1")
	if string(got) != "s3cret" {
		t.Fatalf("store 內的密鑰被外部改動了: %q", got)
	}

	got[0] = 'Y'
	again, _ := store.SecretFor("merchant-1")
	if string(again) != "s3cret" {
		t.Fatalf("SecretFor() 回傳的不是副本: %q", again)
	}
}

func TestBuildHeaders(t *testing.T) {
	store := NewMemorySecretStore()
	store.Put("merchant-1", []byte("s3cret"))
	s := &signer{secrets: store}

	now := time.Unix(1700000000, 0)
	entry := newEntry(validEvent(), "evt-1", 42, now)
	rec := entry.snapshot()

	headers, deliveryID, err := s.buildHeaders(rec, now)
	if err != nil {
		t.Fatalf("buildHeaders() 失敗: %v", err)
	}
	if deliveryID == "" {
		t.Error("deliveryID 不應為空")
	}

	want := map[string]string{
		headerDeliveryID: deliveryID,
		headerEventID:    "evt-1",
		headerEventType:  "payment.settled",
		headerTimestamp:  "1700000000",
		headerSequence:   "42",
	}
	for k, v := range want {
		if headers[k] != v {
			t.Errorf("headers[%s] = %q, 預期 %q", k, headers[k], v)
		}
	}

	// 簽章必須完全符合 PRD 定義：HMAC-SHA256(secret, timestamp + "." + payload)。
	mac := hmac.New(sha256.New, []byte("s3cret"))
	mac.Write([]byte("1700000000." + string(rec.Event.Payload)))
	if wantSig := hex.EncodeToString(mac.Sum(nil)); headers[headerSignature] != wantSig {
		t.Errorf("簽章 = %q, 預期 %q", headers[headerSignature], wantSig)
	}
}

// 每次投遞嘗試的 Delivery-ID 必須不同（供 merchant 記錄投遞審計），
// 但 Event-ID 必須恆定（供 merchant 去重）。這是 at-least-once 下冪等的關鍵。
func TestBuildHeadersDeliveryIDIsPerAttempt(t *testing.T) {
	store := NewMemorySecretStore()
	store.Put("merchant-1", []byte("s3cret"))
	s := &signer{secrets: store}

	now := time.Unix(1700000000, 0)
	rec := newEntry(validEvent(), "evt-1", 1, now).snapshot()

	first, firstID, _ := s.buildHeaders(rec, now)
	second, secondID, _ := s.buildHeaders(rec, now)

	if firstID == secondID {
		t.Errorf("兩次嘗試的 Delivery-ID 相同: %s", firstID)
	}
	if first[headerEventID] != second[headerEventID] {
		t.Errorf("兩次嘗試的 Event-ID 不同: %s vs %s", first[headerEventID], second[headerEventID])
	}
}

// 時間戳納入簽章，讓 merchant 能拒絕過舊的重播請求。
func TestBuildHeadersSignatureCoversTimestamp(t *testing.T) {
	store := NewMemorySecretStore()
	store.Put("merchant-1", []byte("s3cret"))
	s := &signer{secrets: store}

	now := time.Unix(1700000000, 0)
	rec := newEntry(validEvent(), "evt-1", 1, now).snapshot()

	first, _, _ := s.buildHeaders(rec, now)
	second, _, _ := s.buildHeaders(rec, now.Add(time.Second))

	if first[headerSignature] == second[headerSignature] {
		t.Fatal("時間戳不同卻產生相同簽章，簽章未涵蓋時間戳")
	}
}

func TestBuildHeadersMissingSecret(t *testing.T) {
	s := &signer{secrets: NewMemorySecretStore()}
	rec := newEntry(validEvent(), "evt-1", 1, time.Now()).snapshot()

	if _, _, err := s.buildHeaders(rec, time.Now()); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("buildHeaders() = %v, 預期 ErrSecretNotFound", err)
	}
}

// 從 merchant 的角度驗證一次，確認我們送出的簽章真的可被驗證。
// 這裡刻意示範正確做法：用 hmac.Equal 做常數時間比較，不可用 == 或 bytes.Equal
// 之外的短路比較，否則簽章會被時序攻擊逐位元組猜出。
func TestSignatureVerifiableByMerchant(t *testing.T) {
	const secret = "s3cret"
	store := NewMemorySecretStore()
	store.Put("merchant-1", []byte(secret))
	s := &signer{secrets: store}

	now := time.Unix(1700000000, 0)
	rec := newEntry(validEvent(), "evt-1", 1, now).snapshot()
	headers, _, err := s.buildHeaders(rec, now)
	if err != nil {
		t.Fatalf("buildHeaders() 失敗: %v", err)
	}

	verify := func(sig string) bool {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(headers[headerTimestamp] + "." + string(rec.Event.Payload)))
		expected := mac.Sum(nil)
		got, err := hex.DecodeString(sig)
		if err != nil {
			return false
		}
		return hmac.Equal(expected, got)
	}

	if !verify(headers[headerSignature]) {
		t.Error("merchant 無法驗證我們送出的簽章")
	}
	if verify(hex.EncodeToString(make([]byte, sha256.Size))) {
		t.Error("錯誤的簽章竟然通過驗證")
	}

	// 順手確認時間戳可被解析成 Unix 秒，merchant 才能做重播窗口檢查。
	if _, err := strconv.ParseInt(headers[headerTimestamp], 10, 64); err != nil {
		t.Errorf("時間戳無法解析: %v", err)
	}
}
