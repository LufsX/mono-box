package automation

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
)

func TestDisabledWorkersAndReadsLeaveNoRuntimeState(t *testing.T) {
	for _, config := range []string{"", "auto_recovery=false\nauto_update_config=false\nconfig_update_interval_hours=3\nconfig_update_wifi_only=false\n"} {
		t.Run(config, func(t *testing.T) {
			r := fixture(t)
			if config != "" {
				writeConfigFixture(t, r, config)
			}
			for i := 0; i < 3; i++ {
				if _, err := r.Get(); err != nil {
					t.Fatal(err)
				}
				if _, err := r.snapshot(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			defer cancel()
			r.run(ctx, workerDeps{poll: time.Millisecond, now: time.Now,
				snapshot: func() (intentSnapshot, error) {
					t.Error("disabled recovery observed service")
					return intentSnapshot{}, nil
				},
				update: func(context.Context, base.CommitGuard) (int, error) { t.Error("disabled update ran"); return 0, nil },
			})
			if _, err := os.Stat(r.RunDir()); !os.IsNotExist(err) {
				t.Fatalf("disabled workers created runtime artifacts: %v", err)
			}
		})
	}
}

func TestIdleReadWithExistingRuntimeDirectory(t *testing.T) {
	r := fixture(t)
	if err := os.Mkdir(r.RunDir(), 0700); err != nil {
		t.Fatal(err)
	}
	// Other runtime owners may already have created run/ and unrelated files.
	path := filepath.Join(r.RunDir(), "watch.log")
	if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	r.resumeInterrupted(false, time.Now())
	r.resumeInterrupted(true, time.Now())
	if _, err := r.Get(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(r.RunDir())
	if err != nil || len(entries) != 1 || entries[0].Name() != "watch.log" {
		t.Fatalf("idle read changed run/: %v, %v", entries, err)
	}
}

func TestIdleReadConcurrentFirstSettingsWrite(t *testing.T) {
	r := fixture(t)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 12; j++ {
				state, err := r.Get()
				if err != nil {
					t.Error(err)
					return
				}
				if state.Settings.UpdateEnabled && state.Update.NextRunAt == 0 {
					t.Error("enabled snapshot lost its schedule")
				}
			}
		}()
	}
	close(start)
	settings := defaultSettings()
	settings.UpdateEnabled = true
	if _, err := r.set(settings); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	state, err := r.Get()
	if err != nil || !state.Settings.UpdateEnabled || state.Update.NextRunAt == 0 {
		t.Fatalf("first activation lost: %+v %v", state, err)
	}
}

func TestGetPreservesUpdateABAInvalidation(t *testing.T) {
	r := fixture(t)
	settings := defaultSettings()
	settings.UpdateEnabled = true
	if _, err := r.set(settings); err != nil {
		t.Fatal(err)
	}
	initial, guard := beginAttempt(t, r, true)
	settings.UpdateEnabled = false
	writeSettingsConfig(t, r, settings)
	if _, err := r.Get(); err != nil {
		t.Fatal(err)
	}
	settings.UpdateEnabled = true
	writeSettingsConfig(t, r, settings)
	rec, err := r.snapshot(context.Background())
	if err != nil || rec.UpdateGeneration != initial.UpdateGeneration+2 {
		t.Fatalf("lost observed ABA: %+v %v", rec, err)
	}
	called := false
	if err := guard(context.Background(), func() error { called = true; return nil }); err != base.ErrAutomationChanged || called {
		t.Fatalf("stale task committed: called=%v, err=%v", called, err)
	}
}
