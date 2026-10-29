package automation

import (
	"context"
	"time"
)

const interruptionRetry = time.Minute
const taskStopTimeout = 3 * time.Second
const finalizationTimeout = 2 * time.Second

type activeTask struct {
	generation uint64
	attempt    uint64
	revision   uint64
	cancel     context.CancelFunc
	reason     string
}

func (task *activeTask) stop(reason string) {
	if task.reason == "" {
		task.reason = reason
	}
	task.cancel()
}

// Tasks use cancellable HTTP/validation. This bound also prevents a broken
// injected transport from keeping the watcher alive; its cancelled commit
// context and attempt token prevent any later side effect or stale result.
func (task *activeTask) drain(results <-chan taskResult) taskResult {
	task.stop("watch stopped")
	timer := time.NewTimer(taskStopTimeout)
	defer timer.Stop()
	select {
	case result := <-results:
		return result
	case <-timer.C:
		return taskResult{generation: task.generation, attempt: task.attempt, revision: task.revision, err: context.Canceled}
	}
}

func (r *Runtime) finalize(change func(*record) error) {
	ctx, cancel := context.WithTimeout(context.Background(), finalizationTimeout)
	defer cancel()
	_, _ = r.locked(ctx, change)
}

func (r *Runtime) resumeInterrupted(update bool, now time.Time) {
	r.finalize(func(rec *record) error {
		if update && rec.Update.State == "updating" {
			rec.Update.State = "interrupted"
			rec.Update.LastError = "subscription update interrupted: previous watcher exited"
			rec.Update.NextRunAt = now.Add(interruptionRetry).UnixMilli()
			if !rec.Settings.UpdateEnabled {
				rec.Update.State, rec.Update.NextRunAt = "disabled", 0
			}
		}
		if !update && rec.Recovery.State == "recovering" {
			rec.Recovery.State = "waiting"
			rec.Recovery.LastError = "core recovery interrupted: previous watcher exited"
			rec.Recovery.NextAttemptAt = now.Add(recoveryDelay(rec.Recovery.Attempts)).UnixMilli()
			if !rec.Settings.RecoveryEnabled {
				rec.Recovery.State, rec.Recovery.NextAttemptAt = "disabled", 0
			}
		}
		return nil
	})
}
