//go:build linux || android

package control

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
	"github.com/LufsX/mono-box/internal/box/service"
	"github.com/LufsX/mono-box/internal/box/testutil"
)

func bootTestIntent(t *testing.T, r *base.Runtime) base.ServiceIntent {
	t.Helper()
	lock, err := base.AcquireServiceLock(r.RunDir(), time.Second)
	testutil.Require(t, err)
	defer lock.Close()
	intent, err := r.ReadServiceIntent()
	testutil.Require(t, err)
	return intent
}

func bootTestSaveIntent(t *testing.T, r *base.Runtime, intent base.ServiceIntent) {
	t.Helper()
	lock, err := base.AcquireServiceLock(r.RunDir(), time.Second)
	testutil.Require(t, err)
	defer lock.Close()
	testutil.Require(t, base.WritePrivateJSON(filepath.Join(r.RunDir(), "service-intent.json"), intent, 4096))
}

func TestBootStartFailedAttemptClaimsIntentOnce(t *testing.T) {
	for _, mode := range []string{"invalid", "delayed-exit", "missing-config", "missing-core"} {
		t.Run(mode, func(t *testing.T) {
			r := testutil.NewRuntime(t)
			s := service.New(r)
			switch mode {
			case "missing-config":
				testutil.Require(t, os.Remove(filepath.Join(r.CoreDir(), "config.yaml")))
			case "missing-core":
				testutil.Require(t, os.Remove(r.CoreBinary()))
			default:
				testutil.Write(t, filepath.Join(r.CoreDir(), "config.yaml"), []byte(mode), 0600)
			}
			revision, err := s.BootPlan()
			testutil.Require(t, err)
			done, err := s.BootStart(revision)
			if !done || err == nil {
				t.Fatalf("failed startup must consume boot opportunity: done=%v err=%v", done, err)
			}
			first := bootTestIntent(t, r)
			bootID, err := base.CurrentBootID()
			testutil.Require(t, err)
			if !first.Running || first.Revision != 1 || first.BootID != bootID {
				t.Fatalf("boot intent: %+v", first)
			}
			for i := 0; i < 3; i++ {
				done, err := s.BootStart(revision)
				if !done || err != nil {
					t.Fatalf("completed plan: done=%v err=%v", done, err)
				}
				freshRevision, err := s.BootPlan()
				testutil.Require(t, err)
				done, err = s.BootStart(freshRevision)
				if !done || err != nil {
					t.Fatalf("new plan: done=%v err=%v", done, err)
				}
			}
			if after := bootTestIntent(t, r); after != first {
				t.Fatalf("intent republished: before=%+v after=%+v", first, after)
			}
			if mode == "invalid" || mode == "delayed-exit" {
				want := 1
				if mode == "delayed-exit" {
					want = 2
				}
				if events := testutil.Events(t, r); len(events) != want {
					t.Fatalf("repeated actual startup: %+v", events)
				}
			}
		})
	}
}

func TestBootStartRespectsLifecycleBeforeOrAfterPlan(t *testing.T) {
	for _, action := range []string{"stop", "policy-stop", "start"} {
		for _, before := range []bool{true, false} {
			name := action + "/after-plan"
			if before {
				name = action + "/before-plan"
			}
			t.Run(name, func(t *testing.T) {
				r := testutil.NewRuntime(t)
				s := service.New(r)
				var revision uint64
				if !before {
					var err error
					revision, err = s.BootPlan()
					testutil.Require(t, err)
				}
				switch action {
				case "policy-stop":
					testutil.Require(t, s.EnsureStopped())
				case "start":
					testutil.Write(t, filepath.Join(r.CoreDir(), "config.yaml"), []byte("invalid"), 0600)
					if err := s.Service("start"); err == nil {
						t.Fatal("expected explicit start failure")
					}
				default:
					testutil.Require(t, s.Service("stop"))
				}
				intent := bootTestIntent(t, r)
				bootID, err := base.CurrentBootID()
				testutil.Require(t, err)
				if intent.BootID != bootID {
					t.Fatalf("lifecycle did not record current boot: %+v", intent)
				}
				if before {
					revision, err = s.BootPlan()
					testutil.Require(t, err)
				}
				testutil.Write(t, filepath.Join(r.CoreDir(), "config.yaml"), []byte("valid"), 0600)
				done, err := s.BootStart(revision)
				if !done || err != nil || bootTestIntent(t, r) != intent {
					t.Fatalf("lifecycle intent replaced: done=%v err=%v", done, err)
				}
				if _, err := s.RunningPID(); !errors.Is(err, service.ErrStopped) {
					t.Fatalf("boot resurrected core: %v", err)
				}
			})
		}
	}
}

func TestBootStartAdoptsOnlyOldBootOrLegacyIntent(t *testing.T) {
	currentBoot, err := base.CurrentBootID()
	testutil.Require(t, err)
	oldBoot := "00000000-0000-0000-0000-000000000000"
	if oldBoot == currentBoot {
		oldBoot = "00000000-0000-0000-0000-000000000001"
	}
	for _, bootID := range []string{"", oldBoot} {
		for _, running := range []bool{false, true} {
			t.Run(bootID+"/"+map[bool]string{false: "stopped", true: "running"}[running], func(t *testing.T) {
				r := testutil.NewRuntime(t)
				s := service.New(r)
				bootTestSaveIntent(t, r, base.ServiceIntent{BootID: bootID, Running: running, Revision: 8, AttemptedAt: 1})
				revision, err := s.BootPlan()
				testutil.Require(t, err)
				done, err := s.BootStart(revision)
				if !done || err != nil {
					t.Fatalf("old installation boot: done=%v err=%v", done, err)
				}
				intent := bootTestIntent(t, r)
				if intent.Revision != 9 || intent.BootID != currentBoot || !intent.Running {
					t.Fatalf("old boot not adopted: %+v", intent)
				}
				if events := testutil.Events(t, r); len(events) != 2 || !events[0].Test || events[1].Test {
					t.Fatalf("normal startup flow not used: %+v", events)
				}
				testutil.Require(t, s.Service("stop"))
				if intent := bootTestIntent(t, r); intent.Running || intent.BootID != currentBoot {
					t.Fatalf("stop lost boot ID: %+v", intent)
				}
			})
		}
	}
}

func TestBootStartConcurrentClaims(t *testing.T) {
	r := testutil.NewRuntime(t)
	s := service.New(r)
	testutil.Write(t, filepath.Join(r.CoreDir(), "config.yaml"), []byte("invalid"), 0600)
	revision, err := s.BootPlan()
	testutil.Require(t, err)
	var group sync.WaitGroup
	var failures atomic.Int32
	for i := 0; i < 6; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			done, err := s.BootStart(revision)
			if !done {
				t.Error("concurrent boot failed to finish")
			}
			if err != nil {
				failures.Add(1)
			}
		}()
	}
	group.Wait()
	if intent := bootTestIntent(t, r); intent.Revision != 1 || failures.Load() != 1 {
		t.Fatalf("boot claimed more than once: intent=%+v failures=%d", intent, failures.Load())
	}
	if events := testutil.Events(t, r); len(events) != 1 || !events[0].Test {
		t.Fatalf("concurrent boot repeated startup: %+v", events)
	}
}

func TestBootStartRespectsPolicyStopOnOldStoppedIntent(t *testing.T) {
	for _, beforePlan := range []bool{true, false} {
		t.Run(map[bool]string{true: "before-plan", false: "after-plan"}[beforePlan], func(t *testing.T) {
			r := testutil.NewRuntime(t)
			s := service.New(r)
			bootTestSaveIntent(t, r, base.ServiceIntent{Running: false, Revision: 6, AttemptedAt: 1})
			var revision uint64
			var err error
			if !beforePlan {
				revision, err = s.BootPlan()
				testutil.Require(t, err)
			}
			testutil.Require(t, s.EnsureStopped())
			intent := bootTestIntent(t, r)
			currentBoot, err := base.CurrentBootID()
			testutil.Require(t, err)
			if intent.Running || intent.BootID != currentBoot {
				t.Fatalf("policy stop did not adopt current boot: %+v", intent)
			}
			if beforePlan {
				revision, err = s.BootPlan()
				testutil.Require(t, err)
			}
			done, err := s.BootStart(revision)
			if !done || err != nil || bootTestIntent(t, r) != intent {
				t.Fatalf("old stopped intent resurrected: done=%v err=%v", done, err)
			}
		})
	}
}

func TestBootStartAutomaticModuleGuards(t *testing.T) {
	for _, marker := range []string{"manual", "disable", "remove"} {
		t.Run(marker, func(t *testing.T) {
			r := testutil.NewRuntime(t)
			s := service.New(r)
			revision, err := s.BootPlan()
			testutil.Require(t, err)
			dir := r.ModuleDir
			if marker == "manual" {
				dir = r.Root
			}
			testutil.Write(t, filepath.Join(dir, marker), nil, 0600)
			done, err := s.BootStart(revision)
			if !done || err != nil || bootTestIntent(t, r).Revision != 0 {
				t.Fatalf("automatic guard ignored: done=%v err=%v", done, err)
			}
			if _, err := os.Stat(filepath.Join(r.CoreDir(), "invocations.jsonl")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("guarded boot invoked core: %v", err)
			}
		})
	}
}

func TestBootStartEnvironmentFailuresRemainRetryable(t *testing.T) {
	for _, failure := range []string{"environment", "intent-read", "module-missing", "module-unsafe"} {
		t.Run(failure, func(t *testing.T) {
			r := testutil.NewRuntime(t)
			s := service.New(r)
			revision, err := s.BootPlan()
			testutil.Require(t, err)
			var repair func()
			switch failure {
			case "environment":
				path := filepath.Join(r.Root, "scripts", "box.config")
				testutil.Require(t, os.MkdirAll(path, 0700))
				repair = func() { testutil.Require(t, os.Remove(path)) }
			case "intent-read":
				path := filepath.Join(r.RunDir(), "service-intent.json")
				testutil.Write(t, path, []byte("{"), 0600)
				repair = func() { testutil.Require(t, os.Remove(path)) }
			default:
				moved := r.ModuleDir + ".moved"
				testutil.Require(t, os.Rename(r.ModuleDir, moved))
				if failure == "module-unsafe" {
					testutil.Require(t, os.Symlink(moved, r.ModuleDir))
				}
				repair = func() {
					if failure == "module-unsafe" {
						testutil.Require(t, os.Remove(r.ModuleDir))
					}
					testutil.Require(t, os.Rename(moved, r.ModuleDir))
				}
			}
			done, err := s.BootStart(revision)
			if done || err == nil {
				t.Fatalf("environment error consumed opportunity: done=%v err=%v", done, err)
			}
			repair()
			if intent := bootTestIntent(t, r); intent.Revision != 0 {
				t.Fatalf("environment error published intent: %+v", intent)
			}
			done, err = s.BootStart(revision)
			if !done || err != nil {
				t.Fatalf("repaired environment not retried: done=%v err=%v", done, err)
			}
			if intent := bootTestIntent(t, r); intent.Revision != 1 {
				t.Fatalf("retry wrote extra intent: %+v", intent)
			}
			testutil.Require(t, s.Service("stop"))
		})
	}
}

func TestBootPlanSupersededByNetworkStopDuringEnvironmentRetry(t *testing.T) {
	r := New(testutil.NewRuntime(t))
	testutil.Write(t, filepath.Join(r.Root, "scripts", "box.config"),
		[]byte("CORE_USER_GROUP="+r.Config.CoreUserGroup+"\nctr_mode=switch\ntarget_wifi=stop\n"), 0600)
	deps := defaultControllerDeps()
	deps.netPath = filepath.Join(r.Root, "rt_tables")
	testutil.Write(t, deps.netPath, []byte("100 wlan0"), 0600)
	moved := r.ModuleDir + ".moved"
	testutil.Require(t, os.Rename(r.ModuleDir, moved))
	var plan controllerBootPlan
	done, err := r.bootController(deps, &plan)
	if done || err == nil || !plan.captured || plan.revision != 0 {
		t.Fatalf("boot plan not retained on environment failure: done=%v err=%v plan=%+v", done, err, plan)
	}
	testutil.Require(t, os.Rename(moved, r.ModuleDir))
	testutil.Require(t, r.controller(context.Background(), []string{"w", deps.netPath}, deps))
	stopped := bootTestIntent(t, r.Runtime)
	if stopped.Running || stopped.Revision != 1 {
		t.Fatalf("network policy did not stop intent: %+v", stopped)
	}
	done, err = r.bootController(deps, &plan)
	if !done || err != nil || plan.revision != 0 || bootTestIntent(t, r.Runtime) != stopped {
		t.Fatalf("boot replaced network stop: done=%v err=%v plan=%+v", done, err, plan)
	}
	if _, err := os.Stat(filepath.Join(r.CoreDir(), "invocations.jsonl")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("superseded plan invoked core: %v", err)
	}
}

func TestControllerBootKeepsPlanAndHonorsDoneWithError(t *testing.T) {
	for _, watch := range []bool{false, true} {
		t.Run(map[bool]string{false: "command", true: "watch"}[watch], func(t *testing.T) {
			r := controllerTestRuntime(t, "")
			var output bytes.Buffer
			r.Err = &output
			deps := controllerTestDeps(t)
			deps.interval = 5 * time.Millisecond
			var captures, starts atomic.Int32
			finalErr := errors.New("startup failed after intent persisted")
			completed := make(chan struct{})
			deps.bootPlan = func(*Runtime) (uint64, error) {
				if captures.Add(1) == 1 {
					return 0, errors.New("temporary plan environment failure")
				}
				return 17, nil
			}
			deps.bootStart = func(_ *Runtime, revision uint64) (bool, error) {
				if revision != 17 {
					t.Errorf("plan revision changed: %d", revision)
				}
				if starts.Add(1) < 3 {
					return false, errors.New("temporary startup environment failure")
				}
				close(completed)
				return true, finalErr
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			exited := make(chan error, 1)
			go func() {
				if watch {
					exited <- r.watch(ctx, deps)
				} else {
					exited <- r.controller(ctx, []string{"boot"}, deps)
				}
			}()
			select {
			case <-completed:
			case <-time.After(2 * time.Second):
				t.Fatal("boot retries did not finish")
			}
			if watch {
				time.Sleep(10 * deps.interval)
				cancel()
			}
			err := <-exited
			if watch && err != nil || !watch && !errors.Is(err, finalErr) {
				t.Fatalf("completed boot error: %v", err)
			}
			if !strings.Contains(output.String(), "temporary startup environment failure") || watch && !strings.Contains(output.String(), finalErr.Error()) {
				t.Fatalf("boot errors were not reported: %s", output.String())
			}
			if captures.Load() != 2 || starts.Load() != 3 {
				t.Fatalf("plan recaptured or completed startup retried: captures=%d starts=%d", captures.Load(), starts.Load())
			}
		})
	}
}

func TestWatcherRestartDoesNotRepublishFailedBoot(t *testing.T) {
	r := New(testutil.NewRuntime(t))
	testutil.Write(t, filepath.Join(r.CoreDir(), "config.yaml"), []byte("delayed-exit"), 0600)
	deps := defaultControllerDeps()
	deps.interval = 5 * time.Millisecond
	deps.netPath = filepath.Join(r.Root, "missing-rt-tables")
	deps.ready = func(context.Context) (bool, error) { return true, nil }
	deps.maintainLogs = func(*Runtime) error { return nil }
	deps.service = func(*Runtime, string) error { t.Error("boot used ordinary Service"); return nil }
	deps.bootPlan, deps.bootStart = nil, nil
	var first base.ServiceIntent
	for run := 0; run < 2; run++ {
		ctx, cancel := context.WithCancel(context.Background())
		exited := make(chan error, 1)
		go func() { exited <- r.watch(ctx, deps) }()
		controllerTestWait(t, func() bool {
			return bootTestIntent(t, r.Runtime).Revision == 1 &&
				base.Exists(filepath.Join(r.RunDir(), "watcher.pid"))
		})
		time.Sleep(150 * time.Millisecond)
		cancel()
		testutil.Require(t, <-exited)
		intent := bootTestIntent(t, r.Runtime)
		if run == 0 {
			first = intent
		} else if first != intent {
			t.Fatalf("watch restart republished intent: first=%+v next=%+v", first, intent)
		}
	}
	if events := testutil.Events(t, r.Runtime); len(events) != 2 {
		t.Fatalf("watch retried failed boot with recovery disabled: %+v", events)
	}
	testutil.Require(t, service.New(r.Runtime).Service("stop"))
	testutil.Require(t, r.controller(context.Background(), []string{"boot"}, deps))
	if intent := bootTestIntent(t, r.Runtime); intent.Running || intent.Revision != 2 {
		t.Fatalf("fresh boot command replaced manual stop: %+v", intent)
	}
}
