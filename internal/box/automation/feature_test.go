package automation

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
)

func TestFeatureGenerationsAndABAIndependent(t *testing.T) {
	r := fixture(t)
	settings := defaultSettings()
	settings.RecoveryEnabled = true
	settings.UpdateEnabled = true
	if _, err := r.set(settings); err != nil {
		t.Fatal(err)
	}
	initial, err := r.snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	settings.RecoveryEnabled = false
	if _, err := r.set(settings); err != nil {
		t.Fatal(err)
	}
	settings.RecoveryEnabled = true
	if _, err := r.set(settings); err != nil {
		t.Fatal(err)
	}
	recoveryChanged, err := r.snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if recoveryChanged.UpdateGeneration != initial.UpdateGeneration || recoveryChanged.RecoveryGeneration != initial.RecoveryGeneration+2 {
		t.Fatal("recovery ABA changed update generation")
	}
	settings.UpdateEnabled = false
	if _, err := r.set(settings); err != nil {
		t.Fatal(err)
	}
	settings.UpdateEnabled = true
	if _, err := r.set(settings); err != nil {
		t.Fatal(err)
	}
	updateChanged, err := r.snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if updateChanged.RecoveryGeneration != recoveryChanged.RecoveryGeneration || updateChanged.UpdateGeneration != recoveryChanged.UpdateGeneration+2 {
		t.Fatal("update ABA changed recovery generation")
	}
}

func TestRecoverySettingsDoNotCancelActiveUpdate(t *testing.T) {
	r := fixture(t)
	settings := defaultSettings()
	settings.UpdateEnabled = true
	settings.UpdateWifiOnly = false
	if _, err := r.set(settings); err != nil {
		t.Fatal(err)
	}
	_, err := r.locked(context.Background(), func(rec *record) error { rec.Update.NextRunAt = 1; return nil })
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	complete := make(chan struct{})
	var cancelled atomic.Bool
	deps := workerDeps{poll: time.Millisecond, now: time.Now, update: func(ctx context.Context, _ base.CommitGuard) (int, error) {
		close(started)
		select {
		case <-ctx.Done():
			cancelled.Store(true)
			return 0, ctx.Err()
		case <-complete:
			return 2, nil
		}
	}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); r.updateWorker(ctx, deps) }()
	defer func() { cancel(); <-done }()
	<-started
	settings.RecoveryEnabled = true
	if _, err := r.set(settings); err != nil {
		t.Fatal(err)
	}
	settings.RecoveryEnabled = false
	if _, err := r.set(settings); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if cancelled.Load() {
		t.Fatal("recovery toggle cancelled update")
	}
	close(complete)
	waitFor(t, func() bool { state, _ := r.Get(); return state.Update.LastSuccessAt > 0 })
}

func TestUpdateSettingsDoNotCancelActiveRecovery(t *testing.T) {
	r := fixture(t)
	settings := defaultSettings()
	settings.RecoveryEnabled = true
	if _, err := r.set(settings); err != nil {
		t.Fatal(err)
	}
	var clock atomic.Int64
	clock.Store(time.Now().UnixMilli())
	started := make(chan struct{})
	complete := make(chan struct{})
	var cancelled atomic.Bool
	deps := workerDeps{poll: time.Millisecond, now: func() time.Time { return time.UnixMilli(clock.Load()) },
		snapshot: func() (intentSnapshot, error) { return intentSnapshot{revision: 1, running: true}, nil },
		recover: func(ctx context.Context, _ uint64, _ base.CommitGuard) error {
			close(started)
			select {
			case <-ctx.Done():
				cancelled.Store(true)
				return ctx.Err()
			case <-complete:
				return errors.New("finished test recovery")
			}
		}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); r.recoveryWorker(ctx, deps) }()
	defer func() { cancel(); <-done }()
	waitFor(t, func() bool { state, _ := r.Get(); return state.Recovery.NextAttemptAt > 0 })
	clock.Add(10000)
	<-started
	settings.UpdateEnabled = true
	settings.UpdateIntervalHours = 168
	settings.UpdateWifiOnly = false
	if _, err := r.set(settings); err != nil {
		t.Fatal(err)
	}
	settings.UpdateIntervalHours = 1
	settings.UpdateWifiOnly = true
	if _, err := r.set(settings); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if cancelled.Load() {
		t.Fatal("update settings cancelled recovery")
	}
	close(complete)
	waitFor(t, func() bool { state, _ := r.Get(); return state.Recovery.LastError != "" })
}
