package automation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
	"github.com/LufsX/mono-box/internal/box/service"
	"github.com/LufsX/mono-box/internal/box/storage"
)

const recoveryLimit = 5
const stablePeriod = time.Minute

type workerDeps struct {
	poll     time.Duration
	now      func() time.Time
	snapshot func() (intentSnapshot, error)
	recover  func(context.Context, uint64, base.CommitGuard) error
	update   func(context.Context, base.CommitGuard) (int, error)
	wifi     func(context.Context) (bool, error)
}

type intentSnapshot struct {
	revision uint64
	running  bool
	pid      int
}

// Run owns both workers and waits for their cancellation before returning.
// wifi must report the current connection, not a cached startup observation.
func (r *Runtime) Run(ctx context.Context, wifi func(context.Context) (bool, error)) {
	deps := workerDeps{
		poll: time.Second, now: time.Now, wifi: wifi,
		snapshot: func() (intentSnapshot, error) {
			intent, pid, err := service.New(r.Runtime).RecoverySnapshot()
			return intentSnapshot{intent.Revision, intent.Running, pid}, err
		},
		recover: func(ctx context.Context, revision uint64, guard base.CommitGuard) error {
			return service.New(r.Runtime).RecoverGuarded(ctx, revision, guard)
		},
		update: func(ctx context.Context, guard base.CommitGuard) (int, error) {
			return storage.New(r.Runtime).ScheduledUpdateGuarded(ctx, guard)
		},
	}
	r.run(ctx, deps)
}

func (r *Runtime) run(ctx context.Context, deps workerDeps) {
	var workers sync.WaitGroup
	workers.Add(2)
	go func() { defer workers.Done(); r.recoveryWorker(ctx, deps) }()
	go func() { defer workers.Done(); r.updateWorker(ctx, deps) }()
	workers.Wait()
}

func (r *Runtime) snapshot(ctx context.Context) (record, error) {
	return r.read(ctx)
}

func (r *Runtime) moduleAvailable() bool {
	if r.ModuleDir == "" {
		return true
	}
	info, err := os.Lstat(r.ModuleDir)
	if err != nil || !info.IsDir() {
		return false
	}
	for _, marker := range []string{"disable", "remove"} {
		if _, err := os.Lstat(filepath.Join(r.ModuleDir, marker)); !errors.Is(err, os.ErrNotExist) {
			return false
		}
	}
	return true
}

type taskResult struct {
	generation uint64
	revision   uint64
	attempt    uint64
	updated    int
	err        error
}

func sameUpdateSettings(a, b Settings) bool {
	return a.UpdateEnabled == b.UpdateEnabled && a.UpdateIntervalHours == b.UpdateIntervalHours && a.UpdateWifiOnly == b.UpdateWifiOnly
}

func sameRecovery(rec record, generation uint64) bool {
	return rec.Settings.RecoveryEnabled && rec.RecoveryGeneration == generation
}

func sameUpdate(rec record, generation uint64) bool {
	return rec.Settings.UpdateEnabled && rec.UpdateGeneration == generation
}
