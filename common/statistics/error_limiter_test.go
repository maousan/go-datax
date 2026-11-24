package statistics

import (
	"testing"

	"github.com/longkeyy/go-datax/common/config"
)

func newLimiter(t *testing.T, raw string) *ErrorLimiter {
	t.Helper()

	cfg, err := config.FromJSON(raw)
	if err != nil {
		t.Fatalf("invalid test JSON: %v", err)
	}
	return NewErrorLimiter(cfg)
}

func TestErrorLimiterFractionPercentage(t *testing.T) {
	limiter := newLimiter(t, `{"job":{"setting":{"errorLimit":{"record":100,"percentage":0.02}}}}`)

	for i := 0; i < 100; i++ {
		limiter.AddTotalRecord(1)
	}

	// 1% and 2% are allowed; the limit only fails when it is exceeded.
	if err := limiter.AddErrorRecord(1); err != nil {
		t.Fatalf("1%% error rate should be allowed: %v", err)
	}
	if err := limiter.AddErrorRecord(1); err != nil {
		t.Fatalf("2%% error rate should be allowed: %v", err)
	}
	if err := limiter.AddErrorRecord(1); err == nil {
		t.Fatal("3% error rate should exceed the 2% limit")
	}
}

func TestErrorLimiterPercentStyle(t *testing.T) {
	limiter := newLimiter(t, `{"job":{"setting":{"errorLimit":{"record":100,"percentage":5}}}}`)

	for i := 0; i < 100; i++ {
		limiter.AddTotalRecord(1)
	}

	for i := 0; i < 5; i++ {
		if err := limiter.AddErrorRecord(1); err != nil {
			t.Fatalf("error %d should be allowed at a 5%% limit: %v", i+1, err)
		}
	}
	if err := limiter.AddErrorRecord(1); err == nil {
		t.Fatal("6% error rate should exceed the 5% limit")
	}
}

func TestErrorLimiterRecordLimit(t *testing.T) {
	limiter := newLimiter(t, `{"job":{"setting":{"errorLimit":{"record":2,"percentage":100}}}}`)

	limiter.AddTotalRecord(10)
	if err := limiter.AddErrorRecord(1); err != nil {
		t.Fatalf("first error should be allowed: %v", err)
	}
	if err := limiter.AddErrorRecord(1); err != nil {
		t.Fatalf("second error should be allowed: %v", err)
	}
	if err := limiter.AddErrorRecord(1); err == nil {
		t.Fatal("third error should exceed the record limit")
	}
}

func TestErrorLimiterWithoutLimits(t *testing.T) {
	limiter := newLimiter(t, `{"job":{"setting":{}}}`)

	limiter.AddTotalRecord(100)
	for i := 0; i < 100; i++ {
		if err := limiter.AddErrorRecord(1); err != nil {
			t.Fatalf("error %d should be allowed without limits: %v", i+1, err)
		}
	}
}

func TestParseErrorPercentage(t *testing.T) {
	tests := []struct {
		raw  interface{}
		want float64
	}{
		{raw: nil, want: 1.0},
		{raw: 0.02, want: 0.02},
		{raw: 0.5, want: 0.5},
		{raw: 1, want: 1.0},
		{raw: 5, want: 0.05},
		{raw: int64(10), want: 0.1},
		{raw: "0.05", want: 0.05},
		{raw: "5", want: 0.05},
		{raw: "abc", want: 1.0},
		{raw: -1.0, want: 1.0},
	}

	for _, tt := range tests {
		if got := parseErrorPercentage(tt.raw); got != tt.want {
			t.Fatalf("parseErrorPercentage(%v) = %v, want %v", tt.raw, got, tt.want)
		}
	}
}
