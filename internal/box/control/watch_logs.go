package control

import (
	"context"
	"time"
)

// Log maintenance runs independently of boot and network actions. The service
// layer skips a busy lifecycle lock, so this worker never queues behind a start.
func watchLogMaintenance(ctx context.Context, interval time.Duration, maintain func() error) (<-chan error, <-chan struct{}) {
	results := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			if ctx.Err() != nil {
				return
			}
			err := maintain()
			select {
			case results <- err:
			default:
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return results, done
}
