package volume

import "github.com/LufsX/mono-box/internal/box/base"

type Runtime struct {
	*base.Runtime
}

func New(runtime *base.Runtime) *Runtime {
	return &Runtime{Runtime: runtime}
}
