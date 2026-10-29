package automation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
)

func recoveryDelay(attempts int) time.Duration {
	return 5 * time.Second * time.Duration(1<<max(0, min(attempts, recoveryLimit)-1))
}

func (r *Runtime) recoveryWorker(ctx context.Context, deps workerDeps) {
	r.resumeInterrupted(false, deps.now())
	ticker := time.NewTicker(deps.poll)
	defer ticker.Stop()
	results := make(chan taskResult, 1)
	var active *activeTask
	defer func() {
		if active != nil {
			result := active.drain(results)
			r.finishRecovery(result, active.reason, deps.now())
		}
	}()
	for {
		rec, err := r.snapshot(ctx)
		if err != nil && active != nil {
			active.stop("automation state unavailable")
		}
		if err == nil {
			_, manualErr := os.Lstat(filepath.Join(r.Root, "manual"))
			available := r.moduleAvailable() && errors.Is(manualErr, os.ErrNotExist)
			if active != nil {
				if !sameRecovery(rec, active.generation) {
					active.stop("settings changed")
				}
				if !available {
					active.stop("automatic start unavailable")
				}
			}
			if active == nil && rec.Settings.RecoveryEnabled && available {
				observed, observeErr := deps.snapshot()
				if observeErr == nil {
					start := false
					var attempt uint64
					_, err = r.locked(ctx, func(current *record) error {
						if !sameRecovery(*current, rec.RecoveryGeneration) {
							return nil
						}
						start = advanceRecovery(current, observed, deps.now())
						if start {
							current.RecoveryAttempt++
							attempt = current.RecoveryAttempt
						}
						return nil
					})
					if err == nil && start {
						taskCtx, stop := context.WithCancel(ctx)
						active = &activeTask{generation: rec.RecoveryGeneration, attempt: attempt, revision: observed.revision, cancel: stop}
						go func(generation, attempt, revision uint64) {
							err := deps.recover(taskCtx, revision, r.commitGuard(false, generation, attempt))
							results <- taskResult{generation: generation, attempt: attempt, revision: revision, err: err}
						}(active.generation, active.attempt, observed.revision)
					}
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case result := <-results:
			reason := active.reason
			active.cancel()
			active = nil
			r.finishRecovery(result, reason, deps.now())
		case <-ticker.C:
		}
	}
}

func advanceRecovery(rec *record, observed intentSnapshot, now time.Time) bool {
	recovery := &rec.Recovery
	if rec.IntentRevision != observed.revision {
		rec.IntentRevision = observed.revision
		*recovery = Recovery{State: "waiting"}
		rec.StableSince, rec.StablePID = 0, 0
	}
	if !observed.running || observed.revision == 0 {
		recovery.State, recovery.NextAttemptAt = "stopped", 0
		rec.StableSince, rec.StablePID = 0, 0
		return false
	}
	if observed.pid > 0 {
		if rec.StableSince == 0 || rec.StablePID != observed.pid {
			rec.StableSince, rec.StablePID = now.UnixMilli(), observed.pid
		}
		recovery.State, recovery.NextAttemptAt = "stabilizing", 0
		if now.UnixMilli()-rec.StableSince >= stablePeriod.Milliseconds() {
			*recovery = Recovery{State: "running"}
		}
		return false
	}
	rec.StableSince, rec.StablePID = 0, 0
	if recovery.Attempts >= recoveryLimit {
		recovery.State, recovery.NextAttemptAt = "paused", 0
		return false
	}
	if recovery.NextAttemptAt == 0 {
		recovery.State = "backoff"
		recovery.NextAttemptAt = now.Add(recoveryDelay(recovery.Attempts + 1)).UnixMilli()
	}
	if now.UnixMilli() < recovery.NextAttemptAt {
		recovery.State = "backoff"
		return false
	}
	recovery.State = "recovering"
	recovery.Attempts++
	recovery.NextAttemptAt = now.Add(recoveryDelay(recovery.Attempts)).UnixMilli()
	return true
}
