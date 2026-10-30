package automation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
)

const stateLimit = 16 << 10

type record struct {
	State
	RecoveryGeneration uint64 `json:"recoveryGeneration"`
	UpdateGeneration   uint64 `json:"updateGeneration"`
	RecoveryAttempt    uint64 `json:"recoveryAttempt"`
	UpdateAttempt      uint64 `json:"updateAttempt"`
	IntentRevision     uint64 `json:"intentRevision"`
	StableSince        int64  `json:"stableSince"`
	StablePID          int    `json:"stablePID"`
}

func (r *Runtime) locked(ctx context.Context, change func(*record) error) (State, error) {
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	lock, err := base.AcquireConfigLock(ctx, r.RunDir())
	if err != nil {
		if ctx.Err() != nil {
			return State{}, ctx.Err()
		}
		return State{}, errors.New("automation configuration lock unavailable")
	}
	defer lock.Close()
	rec, err := r.recordLocked(change)
	return rec.visibleState(), err
}

// Reconciliation must remain durable: even a Get observing a direct config
// edit invalidates active tasks, including disable/enable ABA between polls.
func (r *Runtime) recordLocked(change func(*record) error) (record, error) {
	rec, before, _, err := r.readRecord()
	if err != nil {
		return record{}, err
	}
	if change != nil {
		if err := change(&rec); err != nil {
			return record{}, err
		}
	}
	if before != rec {
		if err := base.WritePrivateJSON(filepath.Join(r.RunDir(), "automation-status.json"), rec, stateLimit); err != nil {
			return record{}, errors.New("cannot save automation status")
		}
	}
	return rec, nil
}

func (r *Runtime) readRecord() (record, record, bool, error) {
	rec := record{State: State{Settings: defaultSettings(), Recovery: Recovery{State: "disabled"}, Update: Update{State: "disabled"}}}
	err := base.ReadPrivateJSON(filepath.Join(r.RunDir(), "automation-status.json"), &rec, stateLimit)
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return record{}, record{}, exists, errors.New("automation status unavailable or invalid")
	}
	before := rec
	settings, err := r.loadSettings()
	if err != nil {
		return record{}, record{}, exists, err
	}
	if rec.Recovery.State == "" || rec.Update.State == "" || rec.Recovery.Attempts < 0 || rec.Recovery.Attempts > recoveryLimit {
		return record{}, record{}, exists, errors.New("invalid automation status")
	}
	if before.Settings != settings {
		reconcileSettings(&rec, before.Settings, settings, time.Now())
	}
	return rec, before, exists, nil
}

func (rec record) visibleState() State {
	if !rec.Settings.RecoveryEnabled {
		rec.Recovery.State = "disabled"
		rec.Recovery.NextAttemptAt = 0
	}
	if !rec.Settings.UpdateEnabled {
		rec.Update.State = "disabled"
		rec.Update.NextRunAt = 0
	}
	return rec.State
}

func (r *Runtime) Get() (State, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	rec, err := r.read(ctx)
	return rec.visibleState(), err
}

// Get and idle worker snapshots share the no-state fast path. Existing state
// always reconciles under config.lock, even when both features are disabled.
func (r *Runtime) read(ctx context.Context) (record, error) {
	lock, err := base.AcquireExistingConfigLock(ctx, r.RunDir())
	if errors.Is(err, os.ErrNotExist) {
		// Writers create config.lock before changing either file and never
		// unlink it. If it appeared during the read, discard this snapshot.
		rec, _, exists, readErr := r.readRecord()
		lock, err = base.AcquireExistingConfigLock(ctx, r.RunDir())
		if errors.Is(err, os.ErrNotExist) {
			if readErr != nil {
				return record{}, readErr
			}
			if !exists && !rec.Settings.RecoveryEnabled && !rec.Settings.UpdateEnabled {
				return rec, nil
			}
			lock, err = base.AcquireConfigLock(ctx, r.RunDir())
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			return record{}, ctx.Err()
		}
		return record{}, errors.New("automation configuration lock unavailable")
	}
	defer lock.Close()
	return r.recordLocked(nil)
}

func (r *Runtime) set(settings Settings) (State, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return r.locked(ctx, func(rec *record) error {
		if err := r.saveSettings(settings); err != nil {
			return err
		}
		if settings == rec.Settings {
			return nil
		}
		reconcileSettings(rec, rec.Settings, settings, time.Now())
		return nil
	})
}

func reconcileSettings(rec *record, previous, settings Settings, now time.Time) {
	if previous.RecoveryEnabled != settings.RecoveryEnabled {
		rec.RecoveryGeneration++
	}
	updateChanged := !sameUpdateSettings(previous, settings)
	if updateChanged {
		rec.UpdateGeneration++
	}
	rec.Settings = settings
	if !previous.RecoveryEnabled && settings.RecoveryEnabled {
		rec.Recovery = Recovery{State: "waiting"}
		rec.StableSince = 0
	}
	if !settings.RecoveryEnabled {
		rec.Recovery.State = "disabled"
		rec.Recovery.NextAttemptAt = 0
	}
	if !settings.UpdateEnabled {
		rec.Update.State, rec.Update.NextRunAt = "disabled", 0
	} else if updateChanged && rec.Update.State == "updating" {
		rec.Update.State = "interrupted"
		rec.Update.LastError = "subscription update interrupted: settings changed"
		rec.Update.NextRunAt = now.Add(interruptionRetry).UnixMilli()
	} else if !previous.UpdateEnabled || previous.UpdateIntervalHours != settings.UpdateIntervalHours {
		anchor := rec.Update.LastAttemptAt
		if anchor == 0 {
			anchor = now.UnixMilli()
		}
		rec.Update.State = "scheduled"
		rec.Update.NextRunAt = anchor + interval(settings).Milliseconds()
	}
}

func interval(settings Settings) time.Duration {
	return time.Duration(settings.UpdateIntervalHours) * time.Hour
}
