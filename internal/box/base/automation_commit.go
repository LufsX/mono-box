package base

import (
	"context"
	"errors"
)

// CommitGuard serializes a final side effect with persisted automation settings.
// The caller holds service.lock; the guard must never acquire service.lock.
type CommitGuard func(context.Context, func() error) error

var ErrAutomationChanged = errors.New("automation settings changed")
