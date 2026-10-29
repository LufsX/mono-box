package control

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/LufsX/mono-box/internal/box/automation"
)

func (r *Runtime) watchAutomation(ctx context.Context, netPath string, ready <-chan struct{}) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		select {
		case <-ctx.Done():
			return
		case <-ready:
		}
		automation.New(r.Runtime).Run(ctx, func(ctx context.Context) (bool, error) {
			if err := ctx.Err(); err != nil {
				return false, err
			}
			data, err := os.ReadFile(netPath)
			if err != nil {
				return false, errors.New("network state unavailable")
			}
			return strings.Contains(string(data), "wlan"), nil
		})
	}()
	return done
}
