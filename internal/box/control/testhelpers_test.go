//go:build linux || android

package control

import (
	"os"
	"testing"

	"github.com/LufsX/mono-box/internal/box/service"
	"github.com/LufsX/mono-box/internal/box/testutil"
)

func serviceTestRuntime(t *testing.T) *Runtime {
	t.Helper()
	r := New(testutil.NewRuntime(t))
	t.Cleanup(func() { _ = service.New(r.Runtime).Service("stop") })
	return r
}

func serviceTestWrite(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	testutil.Write(t, path, data, mode)
}

func serviceTestRequire(t *testing.T, err error) {
	t.Helper()
	testutil.Require(t, err)
}

func serviceTestState(t *testing.T, r *Runtime, state string) {
	t.Helper()
	testutil.State(t, r.Runtime, state)
}

func serviceTestEvents(t *testing.T, r *Runtime) []testutil.Event {
	t.Helper()
	return testutil.Events(t, r.Runtime)
}
