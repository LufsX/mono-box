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
	rec := record{State: State{Settings: defaultSettings(), Recovery: Recovery{State: "disabled"}, Update: Update{State: "disabled"}}}
	statusPath := filepath.Join(r.RunDir(), "automation-status.json")
	if err := base.ReadPrivateJSON(statusPath, &rec, stateLimit); err != nil && !errors.Is(err, os.ErrNotExist) {
		return State{}, errors.New("automation status unavailable or invalid")
	}
	before := rec
	rec.Settings, err = r.loadSettings()
	if err != nil {
		return State{}, err
	}
	if rec.Recovery.State == "" || rec.Update.State == "" || rec.Recovery.Attempts < 0 || rec.Recovery.Attempts > recoveryLimit {
		return State{}, errors.New("invalid automation status")
	}
	if before.Settings != rec.Settings {
		reconcileSettings(&rec, before.Settings, rec.Settings, time.Now())
	}
	if change != nil {
		if err := change(&rec); err != nil {
			return State{}, err
		}
	}
	if before != rec {
		if err := base.WritePrivateJSON(statusPath, rec, stateLimit); err != nil {
			return State{}, errors.New("cannot save automation status")
		}
	}
	if !rec.Settings.RecoveryEnabled {
		rec.Recovery.State = "disabled"
		rec.Recovery.NextAttemptAt = 0
	}
	if !rec.Settings.UpdateEnabled {
		rec.Update.State = "disabled"
		rec.Update.NextRunAt = 0
	}
	return rec.State, nil
}

func (r *Runtime) Get() (State, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return r.locked(ctx, nil)
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
