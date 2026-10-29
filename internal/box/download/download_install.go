//go:build linux || android

package download

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
)

// Only read-only descriptors pin the old core while HTTP is in flight. A writable
// descriptor would prevent service and external starts with ETXTBSY.
type coreDownload struct {
	rootPath, root                              string
	rootDir, run, bin, lock, service, old, work *os.File
	oldInfo, absentInfo                         os.FileInfo
	workName                                    string
}

func downloadFDPath(dir *os.File) string {
	return filepath.Join("/proc/self/fd", strconv.Itoa(int(dir.Fd())))
}

func newCoreDownload(rootPath string) (*coreDownload, error) {
	absolute, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, err
	}
	root, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, err
	}
	d := &coreDownload{rootPath: absolute, root: root}
	ok := false
	defer func() {
		if !ok {
			d.close()
		}
	}()
	d.run, err = base.OpenDirectory(filepath.Join(root, "run"), true)
	if err != nil {
		return nil, err
	}
	d.lock, err = base.OpenRegularAt(d.run, "download.lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	// Always take download.lock before service.lock. Waiting for another
	// download must not prevent lifecycle operations.
	deadline := time.Now().Add(base.ServiceLockTimeout)
	for {
		err = syscall.Flock(int(d.lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, errors.New("timed out waiting for core download lock")
		}
		time.Sleep(20 * time.Millisecond)
	}
	ok = true
	return d, nil
}

func (d *coreDownload) close() {
	if d.bin != nil && d.workName != "" {
		os.RemoveAll(filepath.Join(downloadFDPath(d.bin), d.workName))
	}
	for _, f := range []*os.File{d.work, d.old, d.service, d.bin, d.rootDir, d.lock, d.run} {
		if f != nil {
			f.Close()
		}
	}
}

func downloadSameFile(a, b *os.File) bool {
	before, e1 := a.Stat()
	after, e2 := b.Stat()
	return e1 == nil && e2 == nil && os.SameFile(before, after)
}

func downloadSameMetadata(before, after os.FileInfo) bool {
	a := before.Sys().(*syscall.Stat_t)
	b := after.Sys().(*syscall.Stat_t)
	return os.SameFile(before, after) && a.Size == b.Size && a.Mode == b.Mode &&
		a.Nlink == b.Nlink && a.Mtim == b.Mtim && a.Ctim == b.Ctim
}

func downloadOpenDirectoryAt(dir *os.File, name string) (*os.File, error) {
	fd, err := syscall.Openat(int(dir.Fd()), name,
		syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), filepath.Join(downloadFDPath(dir), name)), nil
}

// Reopen the current paths without creating anything. A descriptor for a renamed
// directory must never redirect the final install away from the current root.
func (d *coreDownload) reopenBin() (*os.File, error) {
	conflict := errors.New("core download directory changed; retry the operation")
	root, err := filepath.EvalSymlinks(d.rootPath)
	if err != nil || root != d.root {
		return nil, conflict
	}
	var bin *os.File
	for _, pair := range []struct {
		path   string
		pinned *os.File
	}{
		{d.root, d.rootDir},
		{filepath.Join(d.root, "run"), d.run},
		{filepath.Join(d.root, "bin"), d.bin},
	} {
		current, err := base.OpenDirectory(pair.path, false)
		if err != nil {
			return nil, conflict
		}
		same := downloadSameFile(pair.pinned, current)
		if pair.pinned == d.bin && same {
			bin = current
		} else {
			current.Close()
		}
		if !same {
			return nil, conflict
		}
	}
	currentLock, err := base.OpenRegularAt(d.run, "download.lock", os.O_RDONLY, 0)
	if err != nil {
		bin.Close()
		return nil, conflict
	}
	defer currentLock.Close()
	if !downloadSameFile(d.lock, currentLock) {
		bin.Close()
		return nil, conflict
	}
	return bin, nil
}

func (d *coreDownload) prepare(overwrite bool, abi string) (downloadArchitecture, bool, error) {
	var arch downloadArchitecture
	lock, err := base.AcquireServiceLock(filepath.Join(d.root, "run"), base.ServiceLockTimeout)
	if err != nil {
		return arch, false, fmt.Errorf("download core service lock: %w", err)
	}
	d.service = lock
	// Retain the unlocked descriptor to pin the lock inode during the download.
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	d.rootDir, err = base.OpenDirectory(d.root, false)
	if err != nil {
		return arch, false, err
	}
	d.bin, err = base.OpenDirectory(filepath.Join(d.root, "bin"), true)
	if err != nil {
		return arch, false, err
	}
	currentBin, err := d.reopenBin()
	if err != nil {
		return arch, false, err
	}
	currentBin.Close()
	d.old, err = base.OpenRegularAt(d.bin, "mihomo", os.O_RDONLY, 0)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return arch, false, err
	}
	if d.old != nil {
		d.oldInfo, err = d.old.Stat()
		if err != nil {
			return arch, false, err
		}
		if d.oldInfo.Size() > 0 && !overwrite {
			return arch, true, nil
		}
	}
	guard, err := d.writeGuard(d.bin)
	if err != nil {
		return arch, false, err
	}
	if guard != nil {
		defer guard.Close()
	}
	if err := downloadCheckStopped(filepath.Join(d.root, "bin", "mihomo")); err != nil {
		return arch, false, err
	}
	arch, err = downloadResolveABI(abi)
	if err != nil {
		return arch, false, err
	}
	work, err := os.MkdirTemp(downloadFDPath(d.bin), ".monobox-download-")
	if err != nil {
		return arch, false, err
	}
	d.workName = filepath.Base(work)
	d.work, err = downloadOpenDirectoryAt(d.bin, d.workName)
	if err != nil {
		return arch, false, err
	}
	// Snapshot absence after our own staging directory creation. Parent
	// timestamps detect create/remove ABA while the target has no inode to pin.
	d.absentInfo, err = d.bin.Stat()
	return arch, false, err
}

func (d *coreDownload) matchesTarget(bin *os.File) bool {
	now, err := base.OpenRegularAt(bin, "mihomo", os.O_RDONLY, 0)
	if errors.Is(err, os.ErrNotExist) && d.old == nil {
		info, err := bin.Stat()
		if err != nil {
			return false
		}
		a := d.absentInfo.Sys().(*syscall.Stat_t)
		b := info.Sys().(*syscall.Stat_t)
		return os.SameFile(d.absentInfo, info) && a.Mtim == b.Mtim && a.Ctim == b.Ctim
	}
	if err != nil {
		return false
	}
	defer now.Close()
	if d.old == nil {
		return false
	}
	info, err := now.Stat()
	return err == nil && downloadSameMetadata(d.oldInfo, info)
}

// The writable handle protects the installed inode from external exec through
// the last process check and rename. Never retain it across HTTP or extraction.
func (d *coreDownload) writeGuard(bin *os.File) (*os.File, error) {
	guard, err := base.OpenRegularAt(bin, "mihomo", os.O_WRONLY, 0)
	if errors.Is(err, syscall.ETXTBSY) {
		return nil, errors.New("core is running; stop it before overwrite")
	}
	if errors.Is(err, os.ErrNotExist) && d.old == nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	info, err := guard.Stat()
	if err != nil || d.old == nil || !downloadSameMetadata(d.oldInfo, info) {
		guard.Close()
		return nil, errors.New("core download target changed; retry the operation")
	}
	return guard, nil
}

func (d *coreDownload) commit(overwrite bool, stagedInfo os.FileInfo) error {
	lock, err := base.AcquireServiceLock(filepath.Join(d.root, "run"), base.ServiceLockTimeout)
	if err != nil {
		return fmt.Errorf("download core service lock: %w", err)
	}
	defer lock.Close()
	if !downloadSameFile(d.service, lock) {
		return errors.New("core download service lock changed; retry the operation")
	}
	bin, err := d.reopenBin()
	if err != nil {
		return err
	}
	defer bin.Close()
	conflict := errors.New("core download target changed; retry the operation")
	if !d.matchesTarget(bin) {
		return conflict
	}
	// A changed target is a conflict even when overwrite is true. Without
	// overwrite only an unchanged empty file or unchanged absence may commit.
	if !overwrite && d.oldInfo != nil && d.oldInfo.Size() > 0 {
		return conflict
	}
	guard, err := d.writeGuard(bin)
	if err != nil {
		return err
	}
	if guard != nil {
		defer guard.Close()
	}
	if err := downloadCheckStopped(filepath.Join(d.root, "bin", "mihomo")); err != nil {
		return err
	}
	work, err := downloadOpenDirectoryAt(bin, d.workName)
	if err != nil {
		return err
	}
	defer work.Close()
	if !downloadSameFile(d.work, work) {
		return errors.New("core download staging directory changed")
	}
	staged, err := base.OpenRegularAt(work, "core", os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer staged.Close()
	info, err := staged.Stat()
	if err != nil || !downloadSameMetadata(stagedInfo, info) {
		return errors.New("core download staged file changed")
	}
	// Keep validation and rename in the same service critical section, with
	// anchored descriptors for both sides of the rename.
	if !d.matchesTarget(bin) {
		return conflict
	}
	return syscall.Renameat(int(work.Fd()), "core", int(bin.Fd()), "mihomo")
}
