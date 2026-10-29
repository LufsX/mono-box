// Package box connects the native controller's command families.
package box

import (
	"io"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
	"github.com/LufsX/mono-box/internal/box/control"
	"github.com/LufsX/mono-box/internal/box/download"
	"github.com/LufsX/mono-box/internal/box/service"
	"github.com/LufsX/mono-box/internal/box/storage"
	"github.com/LufsX/mono-box/internal/box/volume"
)

var ErrVolumeTimeout = volume.ErrVolumeTimeout

type Runtime struct {
	*control.Runtime
}

func New(root, moduleDir string, out, errOut io.Writer) (*Runtime, error) {
	runtime, err := base.New(root, moduleDir, out, errOut)
	return &Runtime{Runtime: control.New(runtime)}, err
}

func (r *Runtime) Service(action string) error {
	return service.New(r.Runtime.Runtime).Service(action)
}

func (r *Runtime) LogsSize() error {
	return service.New(r.Runtime.Runtime).LogsSize()
}

func (r *Runtime) ClearLogs() error {
	return service.New(r.Runtime.Runtime).ClearLogs()
}

func (r *Runtime) ReadLog(path string) error {
	return service.New(r.Runtime.Runtime).ReadLog(path)
}

func (r *Runtime) OpenLog(path string) error {
	return service.New(r.Runtime.Runtime).OpenLog(path)
}

func (r *Runtime) Storage(args []string) error {
	return storage.New(r.Runtime.Runtime).Storage(args)
}

func (r *Runtime) Request(args []string, execute func([]string) error) error {
	return storage.New(r.Runtime.Runtime).Request(args, execute)
}

func (r *Runtime) DownloadCore(kind string, overwrite, proxy bool, abi string) error {
	return download.New(r.Runtime.Runtime).DownloadCore(kind, overwrite, proxy, abi)
}

func (r *Runtime) ChooseVolume(timeout time.Duration) error {
	return volume.New(r.Runtime.Runtime).ChooseVolume(timeout)
}
