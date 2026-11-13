package app

import (
	"context"
	"fmt"
	"time"
)

// A continuation uses saved output, but still needs a bounded, cancelable wait
// while the transport recovers. Keep Retry-After even after partial output.
type streamRecovery struct {
	attempts int
	waited   time.Duration
}

func (r *streamRecovery) wait(ctx context.Context, cause error, announce func(time.Duration, int)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	left := transientWaitBudget - r.waited
	if r.attempts >= maxTransientRetries || left <= 0 {
		return fmt.Errorf("connection repeatedly interrupted the response; saved progress is retained, retry after the connection recovers")
	}
	hint := retryAfterHint(cause)
	if hint > left {
		return fmt.Errorf("connection recovery requires waiting %s beyond the remaining %s budget; saved progress is retained", hint, left)
	}
	delay := transientDelay(r.attempts, hint)
	if delay > left {
		delay = left
	}
	r.attempts++
	r.waited += delay
	announce(delay, r.attempts)
	if !sleepCtx(ctx, delay) {
		return ctx.Err()
	}
	return nil
}
