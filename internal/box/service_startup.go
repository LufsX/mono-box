//go:build linux || android

package box

import (
	"errors"
	"fmt"
	"time"
)

// A newly exec'd process may briefly have no readable argument vector. Retry
// only that state for this child, retaining all executable/start-time checks.
func serviceAwaitStartupArguments(deadline time.Time, done <-chan error, verify func() error) error {
	for {
		err := verify()
		if !errors.Is(err, errServiceArgumentsPending) {
			return err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf("timed out waiting for core arguments: %w", err)
		}
		timer := time.NewTimer(min(20*time.Millisecond, remaining))
		select {
		case <-timer.C:
		case exitErr := <-done:
			timer.Stop()
			if exitErr != nil {
				return fmt.Errorf("core exited while waiting for startup arguments: %w", exitErr)
			}
			return errors.New("core exited while waiting for startup arguments")
		}
	}
}
