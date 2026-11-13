package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/port"
)

func TestStreamRecoveryWaitHonorsHintAndCancelsImmediately(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := streamRecovery{}
	start := time.Now()
	err := r.wait(ctx, &port.BackendError{Kind: port.KindRateLimit, RetryAfter: time.Minute}, func(d time.Duration, n int) {
		if d != time.Minute || n != 1 {
			t.Fatalf("wrong wait %s attempt %d", d, n)
		}
		cancel()
	})
	if !errors.Is(err, context.Canceled) || time.Since(start) > time.Second {
		t.Fatalf("wait did not cancel: %v", err)
	}
}
func TestStreamRecoveryDoesNotShortenServerWaitBeyondBudget(t *testing.T) {
	r := streamRecovery{}
	err := r.wait(context.Background(), &port.BackendError{Kind: port.KindRateLimit, RetryAfter: transientWaitBudget + time.Second}, func(time.Duration, int) { t.Fatal("should stop without retrying early") })
	if err == nil || r.attempts != 0 {
		t.Fatalf("budget not enforced: %v %+v", err, r)
	}
}
