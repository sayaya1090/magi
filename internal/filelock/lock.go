// Package filelock provides cancellable kernel-owned cross-process locks.
// Lock files remain in place: unlinking would allow two locked inodes for one path.
package filelock

import (
	"context"
	"os"
	"time"
)

func Acquire(ctx context.Context, path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			f.Close()
			return nil, err
		}
		held, err := tryLock(f)
		if err != nil {
			f.Close()
			return nil, err
		}
		if held {
			return func() { f.Close() }, nil
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			f.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
