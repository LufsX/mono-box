package automation

import (
	"context"
	"errors"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
)

func (r *Runtime) finishUpdate(result taskResult, reason string, now time.Time) {
	r.finalize(func(rec *record) error {
		if rec.UpdateAttempt != result.attempt {
			return nil
		}
		rec.Update.Updated = max(rec.Update.Updated, result.updated)
		changed := rec.UpdateGeneration != result.generation
		interrupted := reason != "" || changed || errors.Is(result.err, context.Canceled) || errors.Is(result.err, base.ErrAutomationChanged)
		switch {
		case result.err == nil:
			rec.Update.State = "scheduled"
			rec.Update.LastSuccessAt = now.UnixMilli()
			rec.Update.LastError = ""
		case interrupted:
			if changed {
				reason = "settings changed"
			}
			if reason == "" {
				reason = "task cancelled"
			}
			rec.Update.State = "interrupted"
			rec.Update.LastError = "subscription update interrupted: " + reason
			rec.Update.NextRunAt = now.Add(interruptionRetry).UnixMilli()
		default:
			rec.Update.State = "failed"
			rec.Update.LastError = safeUpdateError(result.err)
		}
		if !rec.Settings.UpdateEnabled {
			rec.Update.State, rec.Update.NextRunAt = "disabled", 0
		}
		return nil
	})
}
