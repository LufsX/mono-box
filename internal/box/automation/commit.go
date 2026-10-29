package automation

import (
	"context"

	"github.com/LufsX/mono-box/internal/box/base"
)

// The callback is invoked under service.lock. No code holding config.lock
// may acquire service.lock. Settings changes serialize with the actual side
// effect, not only the later task result.
func (r *Runtime) commitGuard(update bool, generation, attempt uint64) base.CommitGuard {
	return func(ctx context.Context, commit func() error) error {
		_, err := r.locked(ctx, func(rec *record) error {
			valid := sameRecovery(*rec, generation) && rec.RecoveryAttempt == attempt
			if update {
				valid = sameUpdate(*rec, generation) && rec.UpdateAttempt == attempt
			}
			if !valid {
				return base.ErrAutomationChanged
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := commit(); err != nil {
				return err
			}
			// Persist progress with each successful rename. A process exit or
			// unresponsive HTTP transport cannot erase previously committed work.
			if update {
				rec.Update.Updated++
			}
			return nil
		})
		return err
	}
}
