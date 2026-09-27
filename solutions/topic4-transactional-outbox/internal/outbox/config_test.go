package outbox

import (
	"errors"
	"testing"
	"time"
)

func TestConfigApplyDefaults(t *testing.T) {
	cfg := Config{}
	cfg.applyDefaults()

	if cfg.Workers <= 0 {
		t.Errorf("Workers = %d, 預期正數", cfg.Workers)
	}
	if cfg.QueueSize < cfg.Workers {
		t.Errorf("QueueSize = %d, 預期至少等於 Workers %d", cfg.QueueSize, cfg.Workers)
	}
	if cfg.MaxRetries <= 0 {
		t.Errorf("MaxRetries = %d, 預期正數", cfg.MaxRetries)
	}
	if cfg.BaseBackoff <= 0 || cfg.MaxBackoff < cfg.BaseBackoff {
		t.Errorf("退避區間不合理: base=%v max=%v", cfg.BaseBackoff, cfg.MaxBackoff)
	}
	if cfg.MaxInflightPerMerchant <= 0 {
		t.Errorf("MaxInflightPerMerchant = %d, 預期正數", cfg.MaxInflightPerMerchant)
	}
	if cfg.Clock == nil || cfg.IsRetryable == nil || cfg.Rand == nil || cfg.NewEventID == nil {
		t.Error("可注入的依賴應有預設實作")
	}
}

// MaxRetries 為 0 代表「未設定」而套用預設；要真正關掉重試須明確給負值。
func TestConfigMaxRetriesDisabled(t *testing.T) {
	cfg := Config{MaxRetries: -1}
	cfg.applyDefaults()

	if cfg.MaxRetries != 0 {
		t.Fatalf("MaxRetries = %d, 預期 0（不重試）", cfg.MaxRetries)
	}
}

func TestConfigValidate(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantErr error
	}{
		{"缺 Sender", Config{Secrets: NewMemorySecretStore()}, ErrInvalidConfig},
		{"缺 Secrets", Config{Sender: newFakeSender()}, ErrInvalidConfig},
		{"齊備", Config{Sender: newFakeSender(), Secrets: NewMemorySecretStore()}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := c.cfg
			cfg.applyDefaults()
			if err := cfg.validate(); !errors.Is(err, c.wantErr) {
				t.Fatalf("validate() = %v, 預期 %v", err, c.wantErr)
			}
		})
	}
}

// 退避必須隨次數指數成長、受 MaxBackoff 封頂，且永遠保留一段真實等待
// ——webhook 收到 503 後若在 0ms 就重送，只是再打一次已經在喘的下游。
func TestBackoffForGrowsAndCaps(t *testing.T) {
	cfg := Config{
		BaseBackoff: time.Second,
		MaxBackoff:  8 * time.Second,
		// 取 jitter 區間的最大值，讓成長與封頂行為可精確斷言。
		Rand: func(n int64) int64 { return n },
	}
	cfg.applyDefaults()

	want := []time.Duration{
		time.Second,     // attempt 1: 1s
		2 * time.Second, // attempt 2: 2s
		4 * time.Second, // attempt 3: 4s
		8 * time.Second, // attempt 4: 8s
		8 * time.Second, // attempt 5: 封頂
		8 * time.Second,
	}
	for i, w := range want {
		attempt := i + 1
		if got := cfg.backoffFor(attempt); got != w {
			t.Errorf("backoffFor(%d) = %v, 預期 %v", attempt, got, w)
		}
	}
}

func TestBackoffForKeepsMinimumWait(t *testing.T) {
	cfg := Config{
		BaseBackoff: 4 * time.Second,
		MaxBackoff:  time.Minute,
		// 取 jitter 區間的最小值。
		Rand: func(int64) int64 { return 0 },
	}
	cfg.applyDefaults()

	got := cfg.backoffFor(1)
	if got < cfg.BaseBackoff/2 {
		t.Fatalf("backoffFor(1) = %v, 預期至少 %v（jitter 不得把等待縮到零）", got, cfg.BaseBackoff/2)
	}
}

func TestDefaultIsRetryable(t *testing.T) {
	cases := []struct {
		name string
		resp HTTPResponse
		err  error
		want bool
	}{
		{"網路層錯誤", HTTPResponse{}, errors.New("connection refused"), true},
		{"500", HTTPResponse{StatusCode: 500}, nil, true},
		{"503", HTTPResponse{StatusCode: 503}, nil, true},
		{"429 限流", HTTPResponse{StatusCode: 429}, nil, true},
		{"408 逾時", HTTPResponse{StatusCode: 408}, nil, true},
		{"400 請求有誤", HTTPResponse{StatusCode: 400}, nil, false},
		{"401 未授權", HTTPResponse{StatusCode: 401}, nil, false},
		{"404", HTTPResponse{StatusCode: 404}, nil, false},
		// 2xx 不會走到重試判斷，但仍不該被判為可重試。
		{"200", HTTPResponse{StatusCode: 200}, nil, false},
		{"3xx 重導向未被跟隨", HTTPResponse{StatusCode: 302}, nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DefaultIsRetryable(c.resp, c.err); got != c.want {
				t.Fatalf("DefaultIsRetryable(%d, %v) = %v, 預期 %v", c.resp.StatusCode, c.err, got, c.want)
			}
		})
	}
}
