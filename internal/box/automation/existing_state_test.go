package automation

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/LufsX/mono-box/internal/box/base"
)

func TestDisabledSnapshotReconcilesExistingTasksWithoutLockFile(t *testing.T) {
	r := fixture(t)
	previous := defaultSettings()
	previous.RecoveryEnabled, previous.UpdateEnabled = true, true
	stored := record{
		State:              State{Settings: previous, Recovery: Recovery{State: "recovering", Attempts: 2}, Update: Update{State: "updating"}},
		RecoveryGeneration: 7, UpdateGeneration: 8, RecoveryAttempt: 3, UpdateAttempt: 4,
	}
	path := filepath.Join(r.RunDir(), "automation-status.json")
	if err := base.WritePrivateJSON(path, stored, stateLimit); err != nil {
		t.Fatal(err)
	}
	// Simulate an existing disabled installation with task history. The idle
	// optimization must not discard history or skip durable invalidation.
	rec, err := r.snapshot(context.Background())
	if err != nil || rec.RecoveryGeneration != 8 || rec.UpdateGeneration != 9 ||
		rec.Recovery.State != "disabled" || rec.Update.State != "disabled" ||
		rec.RecoveryAttempt != 3 || rec.UpdateAttempt != 4 {
		t.Fatalf("disabled history not reconciled: %+v %v", rec, err)
	}
	var persisted record
	if err := base.ReadPrivateJSON(path, &persisted, stateLimit); err != nil || persisted != rec {
		t.Fatalf("reconciliation not durable: %+v %v", persisted, err)
	}
	lock, err := base.AcquireExistingConfigLock(context.Background(), r.RunDir())
	if err != nil {
		t.Fatalf("existing task history bypassed config lock: %v", err)
	}
	lock.Close()
}
