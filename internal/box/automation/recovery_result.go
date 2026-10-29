package automation

import (
	"context"
	"errors"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
	"github.com/LufsX/mono-box/internal/box/service"
)

func (r *Runtime) finishRecovery(result taskResult, reason string, now time.Time) {
	r.finalize(func(rec *record) error {
		if !sameRecovery(*rec, result.generation) || rec.RecoveryAttempt != result.attempt || rec.IntentRevision != result.revision {
			return nil
		}
		switch {
		case result.err == nil:
			rec.Recovery.State = "stabilizing"
		case reason != "" || errors.Is(result.err, context.Canceled) || errors.Is(result.err, base.ErrAutomationChanged):
			if reason == "" {
				reason = "task cancelled"
			}
			rec.Recovery.State = "waiting"
			rec.Recovery.LastError = "core recovery interrupted: " + reason
			rec.Recovery.NextAttemptAt = now.Add(recoveryDelay(rec.Recovery.Attempts)).UnixMilli()
		case errors.Is(result.err, service.ErrRecoverySuperseded):
			rec.Recovery.State, rec.Recovery.NextAttemptAt = "waiting", 0
		default:
			rec.Recovery.LastError = "core recovery failed; check service diagnostics"
			if rec.Recovery.Attempts >= recoveryLimit {
				rec.Recovery.State, rec.Recovery.NextAttemptAt = "paused", 0
			} else {
				rec.Recovery.State = "backoff"
				rec.Recovery.NextAttemptAt = now.Add(recoveryDelay(rec.Recovery.Attempts)).UnixMilli()
			}
		}
		return nil
	})
}
