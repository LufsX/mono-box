package control

import (
	"github.com/LufsX/mono-box/internal/box/base"
	"github.com/LufsX/mono-box/internal/box/service"
)

type Runtime struct {
	*base.Runtime
}

func New(runtime *base.Runtime) *Runtime {
	return &Runtime{Runtime: runtime}
}

var ErrStopped = service.ErrStopped
