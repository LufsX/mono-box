package automation

import (
	"context"
	"time"
)

func (r *Runtime) updateWorker(ctx context.Context, deps workerDeps) {
	r.resumeInterrupted(true, deps.now())
	ticker := time.NewTicker(deps.poll)
	defer ticker.Stop()
	results := make(chan taskResult, 1)
	var active *activeTask
	defer func() {
		if active != nil {
			result := active.drain(results)
			r.finishUpdate(result, active.reason, deps.now())
		}
	}()
	for {
		rec, err := r.snapshot(ctx)
		if err != nil && active != nil {
			active.stop("automation state unavailable")
		}
		if err == nil {
			available := r.moduleAvailable()
			if active != nil {
				if !sameUpdate(rec, active.generation) {
					active.stop("settings changed")
				}
				if !available {
					active.stop("module unavailable")
				}
				if rec.Settings.UpdateWifiOnly && deps.wifi != nil {
					wifi, err := deps.wifi(ctx)
					if err != nil || !wifi {
						active.stop("Wi-Fi unavailable")
					}
				}
			}
			if active == nil && rec.Settings.UpdateEnabled && available {
				wifi := true
				var wifiErr error
				if rec.Settings.UpdateWifiOnly {
					if deps.wifi == nil {
						wifi = false
					} else {
						wifi, wifiErr = deps.wifi(ctx)
					}
				}
				start := false
				var attempt uint64
				_, err = r.locked(ctx, func(current *record) error {
					if !sameUpdate(*current, rec.UpdateGeneration) {
						return nil
					}
					update := &current.Update
					now := deps.now()
					if update.NextRunAt == 0 {
						anchor := update.LastAttemptAt
						if anchor == 0 {
							anchor = now.UnixMilli()
						}
						update.NextRunAt = anchor + interval(current.Settings).Milliseconds()
					}
					if now.UnixMilli() < update.NextRunAt {
						if update.State != "failed" && update.State != "interrupted" {
							update.State = "scheduled"
						}
						return nil
					}
					if !wifi || wifiErr != nil {
						update.State = "waiting_wifi"
						return nil
					}
					update.State, update.LastAttemptAt = "updating", now.UnixMilli()
					update.NextRunAt = now.Add(interval(current.Settings)).UnixMilli()
					update.LastError, update.Updated = "", 0
					current.UpdateAttempt++
					attempt = current.UpdateAttempt
					start = true
					return nil
				})
				if err == nil && start {
					taskCtx, stop := context.WithCancel(ctx)
					active = &activeTask{generation: rec.UpdateGeneration, attempt: attempt, cancel: stop}
					go func(generation, attempt uint64) {
						updated, err := deps.update(taskCtx, r.commitGuard(true, generation, attempt))
						results <- taskResult{generation: generation, attempt: attempt, updated: updated, err: err}
					}(active.generation, active.attempt)
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
			r.finishUpdate(result, reason, deps.now())
		case <-ticker.C:
		}
	}
}
