package automation

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/LufsX/mono-box/internal/box/base"
	"github.com/LufsX/mono-box/internal/box/storage"
)

func patchStorageConfig(r *Runtime, values map[string]string) error {
	data, err := json.Marshal(values)
	if err != nil {
		return err
	}
	return storage.New(r.Runtime).PatchConfig(base64.StdEncoding.EncodeToString(data))
}

func assertGuardRejected(t *testing.T, guard base.CommitGuard) {
	t.Helper()
	called := false
	err := guard(context.Background(), func() error { called = true; return nil })
	if !errors.Is(err, base.ErrAutomationChanged) || called {
		t.Fatalf("stale task committed: called=%v err=%v", called, err)
	}
}

func assertGuardValid(t *testing.T, guard base.CommitGuard) {
	t.Helper()
	called := false
	err := guard(context.Background(), func() error { called = true; return nil })
	if err != nil || !called {
		t.Fatalf("unrelated feature invalidated: called=%v err=%v", called, err)
	}
}

func TestStorageConfigABAInvalidatesOnlyChangedFeatureGuard(t *testing.T) {
	for _, operation := range []string{"patch", "write"} {
		t.Run(operation, func(t *testing.T) {
			r := fixture(t)
			setJSON(t, r, `{"recoveryEnabled":true,"updateEnabled":true,"updateWifiOnly":false}`)
			change := func(values map[string]string) {
				t.Helper()
				var err error
				if operation == "patch" {
					err = patchStorageConfig(r, values)
				} else {
					content := base.MergeConfigPatch(readConfigFixture(t, r), values)
					err = storage.New(r.Runtime).Storage([]string{"config", "write", base64.StdEncoding.EncodeToString([]byte(content))})
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			_, recoveryGuard := beginAttempt(t, r, false)
			_, updateGuard := beginAttempt(t, r, true)
			// No automation reads between off and on: equality with the cached
			// settings must not allow the captured task to commit after ABA.
			change(map[string]string{"auto_recovery": "false"})
			change(map[string]string{"auto_recovery": "true"})
			assertGuardRejected(t, recoveryGuard)
			assertGuardValid(t, updateGuard)

			_, recoveryGuard = beginAttempt(t, r, false)
			_, updateGuard = beginAttempt(t, r, true)
			change(map[string]string{"auto_update_config": "false"})
			change(map[string]string{"auto_update_config": "true"})
			assertGuardRejected(t, updateGuard)
			assertGuardValid(t, recoveryGuard)

			_, recoveryGuard = beginAttempt(t, r, false)
			_, updateGuard = beginAttempt(t, r, true)
			change(map[string]string{"clash_api_secret": "'new secret'", "clash_api_port": "9091"})
			assertGuardValid(t, recoveryGuard)
			assertGuardValid(t, updateGuard)
		})
	}
}

func TestConcurrentAutomationSetAndStoragePatchPreserveBothChanges(t *testing.T) {
	r := fixture(t)
	writeConfigFixture(t, r, "# preserve header\nbin_name=mihomo\nCORE_USER_GROUP='root:net_admin'\ncustom='keep me' # retained\n")
	setJSON(t, r, "{}")
	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(2)
	finalSettings := Settings{RecoveryEnabled: true, UpdateEnabled: true, UpdateIntervalHours: 32, UpdateWifiOnly: false}
	go func() {
		defer wg.Done()
		<-start
		for i := 1; i <= 32; i++ {
			if _, err := r.set(Settings{RecoveryEnabled: true, UpdateEnabled: true, UpdateIntervalHours: i, UpdateWifiOnly: false}); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 32; i++ {
			secret := "'temporary'"
			if i == 31 {
				secret = "'final secret # literal'"
			}
			if err := patchStorageConfig(r, map[string]string{"clash_api_secret": secret, "clash_api_port": "9092"}); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	close(start)
	wg.Wait()
	content := readConfigFixture(t, r)
	config, err := base.ParseConfig(content)
	if err != nil || config.APISecret != "final secret # literal" || config.APIPort != 9092 {
		t.Fatalf("lost storage updates: %+v %v", config, err)
	}
	state, err := r.Get()
	if err != nil || state.Settings != finalSettings {
		t.Fatalf("lost automation updates: %+v %v", state, err)
	}
	if !strings.HasPrefix(content, "# preserve header\nbin_name=mihomo\nCORE_USER_GROUP='root:net_admin'\ncustom='keep me' # retained\n") {
		t.Fatal("concurrent merge lost unrelated fields or comments")
	}
}

func TestUnrelatedRawConfigWritePreservesAutomation(t *testing.T) {
	r := fixture(t)
	setJSON(t, r, `{"recoveryEnabled":true,"updateEnabled":true,"updateIntervalHours":12,"updateWifiOnly":false}`)
	_, recoveryGuard := beginAttempt(t, r, false)
	_, updateGuard := beginAttempt(t, r, true)
	content := base.MergeConfigPatch(readConfigFixture(t, r), map[string]string{
		"clash_api_port": "9095", "clash_api_secret": "'saved from another settings page'",
	})
	if err := storage.New(r.Runtime).Storage([]string{"config", "write", base64.StdEncoding.EncodeToString([]byte(content))}); err != nil {
		t.Fatal(err)
	}
	state, err := r.Get()
	if err != nil || state.Settings != (Settings{RecoveryEnabled: true, UpdateEnabled: true, UpdateIntervalHours: 12, UpdateWifiOnly: false}) {
		t.Fatalf("unrelated raw write lost automation settings: %+v %v", state, err)
	}
	assertGuardValid(t, recoveryGuard)
	assertGuardValid(t, updateGuard)
}
