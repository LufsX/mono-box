package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"syscall"
	"unicode"

	"github.com/LufsX/mono-box/internal/box/base"
)

// A pinned inode cannot be reused after an unlink while HTTP is in flight.
// Keep its original metadata too, so in-place edits also invalidate the snapshot.
type profileDownloadFile struct {
	file *os.File
	info os.FileInfo // Parent directory metadata when file is missing.
}

func pinProfileDownloadFile(dir *storageDir, name string) (profileDownloadFile, error) {
	f, err := dir.open(name, syscall.O_RDONLY)
	if errors.Is(err, os.ErrNotExist) {
		// Absence has no inode to pin. Directory timestamps catch create/delete
		// ABA, conservatively including changes to unrelated entries.
		info, statErr := dir.Stat()
		if statErr != nil {
			return profileDownloadFile{}, errors.New("cannot inspect profile download directory")
		}
		return profileDownloadFile{info: info}, nil
	}
	if err != nil {
		return profileDownloadFile{}, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return profileDownloadFile{}, errors.New("cannot inspect profile download target")
	}
	return profileDownloadFile{file: f, info: info}, nil
}

func (p profileDownloadFile) matches(dir *storageDir, name string) bool {
	now, err := pinProfileDownloadFile(dir, name)
	if err != nil {
		return false
	}
	if now.file != nil {
		defer now.file.Close()
	}
	if p.file == nil || now.file == nil {
		if p.file != nil || now.file != nil {
			return false
		}
		before := p.info.Sys().(*syscall.Stat_t)
		after := now.info.Sys().(*syscall.Stat_t)
		return os.SameFile(p.info, now.info) && before.Mtim == after.Mtim && before.Ctim == after.Ctim
	}
	before := p.info.Sys().(*syscall.Stat_t)
	after := now.info.Sys().(*syscall.Stat_t)
	return os.SameFile(p.info, now.info) && before.Size == after.Size &&
		before.Mode == after.Mode && before.Mtim == after.Mtim && before.Ctim == after.Ctim
}

type profileDownload struct {
	storage             *profileStorage
	profile, sourceFile profileDownloadFile
	binName, source     string
	storedSource        string
}

func (d *profileDownload) close() {
	for _, f := range []*os.File{d.profile.file, d.sourceFile.file} {
		if f != nil {
			f.Close()
		}
	}
	d.storage.close()
}

// Reload under service.lock without mutating the caller. Missing config retains
// the embedder's Config. A valid disk config must not mask an unsafe embedder.
func (r *Runtime) profileDownloadRuntime() (*Runtime, error) {
	if err := validProfileCoreName(r.Config.BinName); err != nil {
		return nil, err
	}
	current := New(r.WithConfig(r.Config))
	dir, err := openStorageDir(filepath.Join(r.Root, "scripts"), false)
	if errors.Is(err, os.ErrNotExist) {
		return current, nil
	}
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	data, err := dir.read("box.config", base.ConfigSizeLimit)
	if errors.Is(err, os.ErrNotExist) {
		return current, nil
	}
	if err != nil {
		return nil, err
	}
	config, err := base.ParseConfig(string(data))
	if err != nil {
		return nil, errors.New("invalid box.config")
	}
	if err := validProfileCoreName(config.BinName); err != nil {
		return nil, err
	}
	current.Config = config
	return current, nil
}

func (r *Runtime) prepareProfileDownload(command, name, source string) (*profileDownload, error) {
	unlock, err := r.storageLock()
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
	d := &profileDownload{storage: s, binName: current.Config.BinName, source: source}
	ok := false
	defer func() {
		if !ok {
			d.close()
		}
	}()
	if _, err = s.activeName(); err != nil {
		return nil, err
	}
	if d.profile, err = pinProfileDownloadFile(s.profiles, name); err != nil {
		return nil, err
	}
	if d.sourceFile, err = pinProfileDownloadFile(s.sources, name+".url"); err != nil {
		return nil, err
	}
	if d.sourceFile.file != nil {
		data, readErr := io.ReadAll(io.LimitReader(d.sourceFile.file, storageTextLimit+1))
		if readErr != nil || len(data) > storageTextLimit {
			return nil, errors.New("cannot snapshot profile source")
		}
		d.storedSource = string(data)
	}
	if command == "profiles update" {
		if d.profile.file == nil {
			return nil, os.ErrNotExist
		}
		d.source = d.storedSource
		if d.source == "" {
			return nil, errors.New("config profile has no source URL")
		}
		if err := validStorageURL(d.source); err != nil {
			return nil, errors.New("invalid stored source URL")
		}
	}
	ok = true
	return d, nil
}

// Open the current paths again, without creating missing directories. The old
// descriptors only pin identity; all validation and writes use these new ones.
func (d *profileDownload) reopen(current *Runtime, name string) (*profileStorage, error) {
	conflict := errors.New("profile download target changed; retry the operation")
	if current.Config.BinName != d.binName {
		return nil, errors.New("core directory changed during profile download")
	}
	s := &profileStorage{corePath: current.CoreDir()}
	pairs := []struct {
		target **storageDir
		pinned *storageDir
		path   string
	}{
		{&s.core, d.storage.core, s.corePath},
		{&s.meta, d.storage.meta, filepath.Join(s.corePath, ".mono-box")},
		{&s.profiles, d.storage.profiles, filepath.Join(s.corePath, ".mono-box", "profiles")},
		{&s.sources, d.storage.sources, filepath.Join(s.corePath, ".mono-box", "sources")},
	}
	for _, pair := range pairs {
		dir, err := openStorageDir(pair.path, false)
		if err != nil {
			s.close()
			return nil, conflict
		}
		*pair.target = dir
		before, beforeErr := pair.pinned.Stat()
		after, afterErr := dir.Stat()
		if beforeErr != nil || afterErr != nil || !os.SameFile(before, after) {
			s.close()
			return nil, conflict
		}
	}
	if !d.profile.matches(s.profiles, name) || !d.sourceFile.matches(s.sources, name+".url") {
		s.close()
		return nil, conflict
	}
	if d.sourceFile.file != nil {
		data, err := s.sources.read(name+".url", storageTextLimit)
		if err != nil || string(data) != d.storedSource {
			s.close()
			return nil, conflict
		}
	}
	return s, nil
}

func (r *Runtime) storageDownloadProfile(command, name, source string) error {
	snapshot, err := r.prepareProfileDownload(command, name, source)
	if err != nil {
		return err
	}
	defer snapshot.close()
	content, err := r.downloadStorageProfile(snapshot.source)
	if err != nil {
		return err
	}
	unlock, err := r.storageLock()
	if err != nil {
		return err
	}
	defer unlock()
	current, err := r.profileDownloadRuntime()
	if err != nil {
		return err
	}
	s, err := snapshot.reopen(current, name)
	if err != nil {
		return err
	}
	defer s.close()
	// Selection can legitimately change during HTTP. Report its latest value.
	active, err := s.activeName()
	if err != nil {
		return err
	}
	return current.commitStoredProfile(s, name, snapshot.source, content, active)
}

func validStorageURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.Opaque != "" {
		return errors.New("only valid HTTP or HTTPS source URLs are allowed")
	}
	for _, ch := range raw {
		if unicode.IsControl(ch) || unicode.IsSpace(ch) {
			return errors.New("invalid source URL")
		}
	}
	return nil
}

func (r *Runtime) downloadStorageProfile(source string) ([]byte, error) {
	if err := validStorageURL(source); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), storageTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, errors.New("invalid source request")
	}
	client := http.Client{}
	if r.HTTP != nil {
		client = *r.HTTP
	}
	// Bound even clients supplied without a timeout. Keep injected transports.
	if client.Timeout <= 0 || client.Timeout > storageTimeout {
		client.Timeout = storageTimeout
	}
	client.Jar = nil
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		if err := validStorageURL(req.URL.String()); err != nil {
			return err
		}
		if via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return errors.New("insecure redirect")
		}
		return nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("profile download failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("profile download: HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > storageContentLimit {
		return nil, errors.New("downloaded profile exceeds size limit")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, storageContentLimit+1))
	if err != nil {
		return nil, errors.New("cannot read downloaded profile")
	}
	if len(data) > storageContentLimit {
		return nil, errors.New("downloaded profile exceeds size limit")
	}
	if len(data) == 0 {
		return nil, errors.New("downloaded profile is empty")
	}
	return data, nil
}
