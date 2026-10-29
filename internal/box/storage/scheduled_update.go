package storage

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
)

// Scheduled errors are safe to expose: they contain no source URL or core output.
var (
	ErrScheduledInventory  = errors.New("subscription inventory unavailable")
	ErrScheduledSource     = errors.New("subscription source or profile unavailable")
	ErrScheduledDownload   = errors.New("subscription download failed")
	ErrScheduledConflict   = errors.New("subscription changed during download")
	ErrScheduledCore       = errors.New("executable core required for scheduled validation")
	ErrScheduledValidation = errors.New("downloaded configuration validation or commit failed")
)

func (r *Runtime) scheduledLock(ctx context.Context) (func(), error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		lock, err := base.AcquireServiceLock(r.RunDir(), 0)
		if err == nil {
			return func() { lock.Close() }, nil
		}
		if !errors.Is(err, base.ErrServiceLockTimeout) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// ScheduledUpdate updates subscriptions on disk without starting or reloading
// the service. HTTP runs without service.lock; commits require core validation.
func (r *Runtime) ScheduledUpdate(ctx context.Context) (int, error) {
	return r.ScheduledUpdateGuarded(ctx, nil)
}

// ScheduledUpdateGuarded holds guard only around each final rename, after
// download and validation. Successful profiles before interruption are counted.
func (r *Runtime) ScheduledUpdateGuarded(ctx context.Context, guard base.CommitGuard) (int, error) {
	names, err := r.scheduledNames(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 0, ErrScheduledInventory
	}
	updated := 0
	var failures error
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return updated, err
		}
		if err := r.scheduledProfile(ctx, name, guard); err != nil {
			if errors.Is(err, base.ErrAutomationChanged) {
				return updated, err
			}
			// Keep distinct safe causes while continuing with other subscriptions.
			if !errors.Is(failures, err) {
				failures = errors.Join(failures, err)
			}
		} else {
			updated++
		}
	}
	if err := ctx.Err(); err != nil {
		return updated, err
	}
	return updated, failures
}

func (r *Runtime) scheduledNames(ctx context.Context) ([]string, error) {
	unlock, err := r.scheduledLock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	current, err := r.profileDownloadRuntime()
	if err != nil {
		return nil, err
	}
	s, err := current.openProfileStorage()
	if err != nil {
		return nil, err
	}
	defer s.close()
	entries, err := s.profiles.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if !base.ValidProfileName(entry.Name()) {
			continue
		}
		source, err := s.source(entry.Name())
		if err != nil {
			return nil, err
		}
		if source != "" {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

func (r *Runtime) scheduledProfile(ctx context.Context, name string, guard base.CommitGuard) error {
	snapshot, err := r.prepareProfileDownloadContext(ctx, "profiles update", name, "")
	if err != nil {
		return ErrScheduledSource
	}
	defer snapshot.close()
	content, err := r.downloadStorageProfileContext(ctx, snapshot.source)
	if err != nil {
		return ErrScheduledDownload
	}
	unlock, err := r.scheduledLock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	current, err := r.profileDownloadRuntime()
	if err != nil {
		return ErrScheduledConflict
	}
	s, err := snapshot.reopen(current, name)
	if err != nil {
		return ErrScheduledConflict
	}
	defer s.close()
	if _, err := s.activeName(); err != nil {
		return ErrScheduledConflict
	}
	if err := current.installStoredProfileGuarded(ctx, s, name, content, true, guard); err != nil {
		if errors.Is(err, base.ErrAutomationChanged) || ctx.Err() != nil {
			return err
		}
		if errors.Is(err, ErrScheduledCore) {
			return ErrScheduledCore
		}
		return ErrScheduledValidation
	}
	return nil
}
