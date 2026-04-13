package httpapi

import (
	"testing"
	"time"
)

func TestLoginLimiterBlocksAfterThreshold(t *testing.T) {
	limiter := newLoginLimiter(3, time.Minute, 2*time.Minute)

	if _, ok := limiter.Allow("127.0.0.1", "admin"); !ok {
		t.Fatalf("expected initial allow")
	}

	limiter.RecordFailure("127.0.0.1", "admin")
	limiter.RecordFailure("127.0.0.1", "admin")
	limiter.RecordFailure("127.0.0.1", "admin")

	retryAfter, ok := limiter.Allow("127.0.0.1", "admin")
	if ok {
		t.Fatalf("expected limiter to block after threshold")
	}
	if retryAfter <= 0 {
		t.Fatalf("expected positive retryAfter, got %v", retryAfter)
	}
}

func TestLoginLimiterClearsOnSuccess(t *testing.T) {
	limiter := newLoginLimiter(2, time.Minute, time.Minute)
	limiter.RecordFailure("127.0.0.1", "admin")
	limiter.RecordSuccess("127.0.0.1", "admin")

	if _, ok := limiter.Allow("127.0.0.1", "admin"); !ok {
		t.Fatalf("expected allow after success reset")
	}
}
