package box

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	storageContentLimit = 16 << 20
	storageTextLimit    = 16 << 10
	storageTimeout      = 60 * time.Second
	storageTestTimeout  = 20 * time.Second
)

// ValidProfileName accepts a single YAML filename, including names from the
// WebUI normalizer. Names also appear unescaped in the legacy TSV protocol.
func ValidProfileName(name string) bool {
	if name == "" || len(name) > 240 || !utf8.ValidString(name) ||
		strings.TrimSpace(name) != name || strings.Contains(name, "..") ||
		strings.ContainsAny(name, "/\\:*?\"<>|") {
		return false
	}
	for _, ch := range name {
		if unicode.IsControl(ch) {
			return false
		}
	}
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".yaml" || ext == ".yml"
}

// storageDir pins each directory with openat(O_NOFOLLOW). All file operations
// use that descriptor, so replacing an ancestor with a symlink cannot redirect
// a read, atomic rename or removal outside the opened directory.
type storageDir struct{ *os.File }

func openStorageDir(path string, create bool) (*storageDir, error) {
	if path == "" {
		return nil, errors.New("storage directory is not configured")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, errors.New("invalid storage directory")
	}
	flags := syscall.O_RDONLY | syscall.O_DIRECTORY | syscall.O_NOFOLLOW | syscall.O_CLOEXEC
	fd, err := syscall.Open("/", flags, 0)
	if err != nil {
		return nil, errors.New("cannot open storage directory")
	}
	for _, part := range strings.Split(strings.TrimPrefix(absolute, "/"), "/") {
		if part == "" {
			continue
		}
		next, openErr := syscall.Openat(fd, part, flags, 0)
		if errors.Is(openErr, syscall.ENOENT) && create {
			mkdirErr := syscall.Mkdirat(fd, part, 0700)
			if mkdirErr == nil || errors.Is(mkdirErr, syscall.EEXIST) {
				next, openErr = syscall.Openat(fd, part, flags, 0)
			}
		}
		syscall.Close(fd)
		if openErr != nil {
			if errors.Is(openErr, syscall.ENOENT) {
				return nil, os.ErrNotExist
			}
			return nil, errors.New("unsafe or inaccessible storage directory")
		}
		fd = next
	}
	return &storageDir{os.NewFile(uintptr(fd), absolute)}, nil
}

func (d *storageDir) child(name string) (*storageDir, error) {
	flags := syscall.O_RDONLY | syscall.O_DIRECTORY | syscall.O_NOFOLLOW | syscall.O_CLOEXEC
	fd, err := syscall.Openat(int(d.Fd()), name, flags, 0)
	if errors.Is(err, syscall.ENOENT) {
		if mkdirErr := syscall.Mkdirat(int(d.Fd()), name, 0700); mkdirErr == nil || errors.Is(mkdirErr, syscall.EEXIST) {
			fd, err = syscall.Openat(int(d.Fd()), name, flags, 0)
		}
	}
	if err != nil {
		return nil, errors.New("unsafe or inaccessible storage directory")
	}
	return &storageDir{os.NewFile(uintptr(fd), name)}, nil
}

func (d *storageDir) path(name string) string {
	return filepath.Join("/proc/self/fd", fmt.Sprint(d.Fd()), name)
}

func (d *storageDir) open(name string, flags int) (*os.File, error) {
	fd, err := syscall.Openat(int(d.Fd()), name, flags|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0600)
	if err != nil {
		if errors.Is(err, syscall.ENOENT) {
			return nil, os.ErrNotExist
		}
		return nil, errors.New("unsafe or inaccessible storage file")
	}
	f := os.NewFile(uintptr(fd), name)
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, errors.New("cannot inspect storage file")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || !ok || stat.Nlink != 1 {
		f.Close()
		return nil, errors.New("storage file must be regular and have one link")
	}
	return f, nil
}

func (d *storageDir) read(name string, limit int64) ([]byte, error) {
	f, err := d.open(name, syscall.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, errors.New("cannot read storage file")
	}
	if int64(len(data)) > limit {
		return nil, errors.New("storage file exceeds size limit")
	}
	return data, nil
}

func (d *storageDir) check(name string) error {
	f, err := d.open(name, syscall.O_RDONLY)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return f.Close()
}

func (d *storageDir) write(name string, data []byte) error {
	if err := d.check(name); err != nil {
		return err
	}
	if err := AtomicWrite(d.path(name), data, 0600); err != nil {
		return errors.New("cannot atomically write storage file")
	}
	return nil
}

func (d *storageDir) remove(name string) error {
	if err := d.check(name); err != nil {
		return err
	}
	if err := os.Remove(d.path(name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.New("cannot remove storage file")
	}
	return nil
}

func (r *Runtime) storageLock() (func(), error) {
	// This is the same inode and flock protocol used by Service/start snapshots.
	run, err := openStorageDir(filepath.Join(r.Root, "run"), true)
	if err != nil {
		return nil, err
	}
	defer run.Close()
	f, err := run.open("service.lock", syscall.O_CREAT|syscall.O_RDWR)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, errors.New("cannot lock storage")
	}
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}

func decodeStorageText(encoded string, limit int) ([]byte, error) {
	if len(encoded) > base64.StdEncoding.EncodedLen(limit) {
		return nil, errors.New("storage argument exceeds size limit")
	}
	data, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(data) > limit || !utf8.Valid(data) || strings.IndexByte(string(data), 0) >= 0 {
		return nil, errors.New("invalid base64 text argument")
	}
	return data, nil
}

// Storage dispatches the config, module and profiles CLI command families.
// Arbitrary text arguments use standard UTF-8 base64; command enums are literal.
func (r *Runtime) Storage(args []string) error {
	// PatchConfig owns its read/merge/write lock.
	if len(args) == 3 && args[0] == "config" && args[1] == "patch" {
		return r.PatchConfig(args[2])
	}
	if len(args) < 2 {
		return errors.New("expected config, module or profiles subcommand")
	}
	command := args[0] + " " + args[1]
	want := 2
	switch command {
	case "config read", "module info", "profiles list":
	case "config write", "config patch", "profiles update", "profiles delete":
		want = 3
	case "profiles import", "profiles download", "profiles switch":
		want = 4
	default:
		return errors.New("unknown storage command")
	}
	if len(args) != want {
		return errors.New("invalid storage argument count")
	}
	var name, source, kind string
	var content []byte
	var err error
	if command == "config write" {
		content, err = decodeStorageText(args[2], storageContentLimit)
		if err != nil {
			return err
		}
		if _, err = ParseConfig(string(content)); err != nil {
			return errors.New("invalid box.config: expected valid literal assignments")
		}
	} else if len(args) > 2 {
		nameArg := args[2]
		if command == "profiles download" {
			nameArg = args[3]
		}
		decoded, decodeErr := decodeStorageText(nameArg, 240)
		if decodeErr != nil {
			return decodeErr
		}
		name = string(decoded)
		if !ValidProfileName(name) {
			return errors.New("invalid profile name")
		}
		switch command {
		case "profiles import":
			content, err = decodeStorageText(args[3], storageContentLimit)
		case "profiles download":
			var data []byte
			data, err = decodeStorageText(args[2], storageTextLimit)
			source = string(data)
			if err == nil {
				err = validStorageURL(source)
			}
		case "profiles switch":
			kind = args[3]
			if kind != "current" && kind != "profile" {
				return errors.New("invalid profile kind")
			}
			if kind == "current" && name != "config.yaml" {
				return errors.New("current config must be config.yaml")
			}
		}
		if err != nil {
			return err
		}
	}
	unlock, err := r.storageLock()
	if err != nil {
		return err
	}
	defer unlock()
	switch command {
	case "config read", "config write":
		dir, err := openStorageDir(filepath.Join(r.Root, "scripts"), command == "config write")
		if err != nil {
			return err
		}
		defer dir.Close()
		if command == "config write" {
			return dir.write("box.config", content)
		}
		data, err := dir.read("box.config", storageContentLimit)
		if err != nil {
			return err
		}
		return r.storageOutput(data)
	case "module info":
		dir, err := openStorageDir(r.ModuleDir, false)
		if err != nil {
			return err
		}
		defer dir.Close()
		data, err := dir.read("module.prop", storageContentLimit)
		if err != nil {
			return err
		}
		return r.storageOutput(data)
	default:
		return r.storageProfiles(command, name, source, kind, content)
	}
}

func (r *Runtime) storageOutput(data []byte) error {
	if r.Out == nil {
		return nil
	}
	_, err := r.Out.Write(data)
	return err
}

type profileStorage struct {
	core, meta, profiles, sources *storageDir
	corePath                      string
}

func (s *profileStorage) close() {
	for _, dir := range []*storageDir{s.sources, s.profiles, s.meta, s.core} {
		if dir != nil {
			dir.Close()
		}
	}
}

func (r *Runtime) openProfileStorage() (*profileStorage, error) {
	// Config can also be supplied directly by embedders instead of ParseConfig.
	if r.Config.BinName == "" || filepath.Base(r.Config.BinName) != r.Config.BinName ||
		strings.ContainsAny(r.Config.BinName, "/\\\x00") || r.Config.BinName == "." || r.Config.BinName == ".." {
		return nil, errors.New("invalid core directory")
	}
	for _, ch := range r.Config.BinName {
		if unicode.IsControl(ch) {
			return nil, errors.New("invalid core directory")
		}
	}
	s := &profileStorage{corePath: r.CoreDir()}
	var err error
	s.core, err = openStorageDir(s.corePath, true)
	if err == nil {
		s.meta, err = s.core.child(".mono-box")
	}
	if err == nil {
		s.profiles, err = s.meta.child("profiles")
	}
	if err == nil {
		s.sources, err = s.meta.child("sources")
	}
	if err != nil {
		s.close()
		return nil, err
	}
	return s, nil
}

func (s *profileStorage) activeName() (string, error) {
	data, err := s.meta.read("active-profile", 240)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	name := strings.TrimRight(string(data), "\r\n")
	if !ValidProfileName(name) {
		return "", nil
	}
	f, err := s.profiles.open(name, syscall.O_RDONLY)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	f.Close()
	return name, nil
}

func (s *profileStorage) source(name string) (string, error) {
	data, err := s.sources.read(name+".url", storageTextLimit)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	source := string(data)
	if err := validStorageURL(source); err != nil {
		return "", errors.New("invalid stored source URL")
	}
	return source, nil
}

func (s *profileStorage) row(kind, name, source string, active bool) ([]byte, error) {
	dir := s.profiles
	path := filepath.Join(s.corePath, ".mono-box", "profiles", name)
	if kind == "current" {
		dir = s.core
		path = filepath.Join(s.corePath, "config.yaml")
	}
	f, err := dir.open(name, syscall.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, errors.New("cannot inspect profile")
	}
	return []byte(fmt.Sprintf("%s\t%s\t%s\t%d\t%d\t%s\t%t\n",
		kind, name, path, info.Size(), info.ModTime().Unix(), base64.StdEncoding.EncodeToString([]byte(source)), active)), nil
}

func (r *Runtime) storageProfiles(command, name, source, kind string, content []byte) error {
	s, err := r.openProfileStorage()
	if err != nil {
		return err
	}
	defer s.close()
	active, err := s.activeName()
	if err != nil {
		return err
	}
	switch command {
	case "profiles list":
		var rows []byte
		row, err := s.row("current", "config.yaml", "", active == "")
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		rows = append(rows, row...)
		entries, err := s.profiles.ReadDir(-1)
		if err != nil {
			return errors.New("cannot list profiles")
		}
		// ReadDir on File does not sort, unlike os.ReadDir.
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			if ValidProfileName(entry.Name()) {
				names = append(names, entry.Name())
			}
		}
		sort.Strings(names)
		for _, profile := range names {
			source, err := s.source(profile)
			if err != nil {
				return err
			}
			row, err := s.row("profile", profile, source, profile == active)
			if err != nil {
				return err
			}
			rows = append(rows, row...)
		}
		return r.storageOutput(rows)
	case "profiles delete":
		if name == active {
			return errors.New("active config cannot be deleted")
		}
		f, err := s.profiles.open(name, syscall.O_RDONLY)
		if err != nil {
			return err
		}
		f.Close()
		if err := s.sources.check(name + ".url"); err != nil {
			return err
		}
		if err := s.profiles.remove(name); err != nil {
			return err
		}
		return s.sources.remove(name + ".url")
	case "profiles switch":
		dir := s.profiles
		if kind == "current" {
			dir = s.core
		} else {
			source, err = s.source(name)
			if err != nil {
				return err
			}
		}
		if err := r.validateStoredProfile(s, dir, name); err != nil {
			return err
		}
		if kind == "current" {
			err = s.meta.remove("active-profile")
		} else {
			err = s.meta.write("active-profile", []byte(name))
		}
		if err != nil {
			return err
		}
		row, err := s.row(kind, name, source, true)
		if err != nil {
			return err
		}
		return r.storageOutput(row)
	case "profiles update":
		f, err := s.profiles.open(name, syscall.O_RDONLY)
		if err != nil {
			return err
		}
		f.Close()
		source, err = s.source(name)
		if err != nil {
			return err
		}
		if source == "" {
			return errors.New("config profile has no source URL")
		}
	}
	if err := s.profiles.check(name); err != nil {
		return err
	}
	if err := s.sources.check(name + ".url"); err != nil {
		return err
	}
	if command == "profiles download" || command == "profiles update" {
		content, err = r.downloadStorageProfile(source)
		if err != nil {
			return err
		}
	}
	if len(content) == 0 {
		return errors.New("config profile is empty")
	}
	if err := r.installStoredProfile(s, name, content); err != nil {
		return err
	}
	if source == "" {
		err = s.sources.remove(name + ".url")
	} else {
		err = s.sources.write(name+".url", []byte(source))
	}
	if err != nil {
		return err
	}
	row, err := s.row("profile", name, source, name == active)
	if err != nil {
		return err
	}
	return r.storageOutput(row)
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

func (r *Runtime) validateStoredProfile(s *profileStorage, dir *storageDir, name string) error {
	f, err := dir.open(name, syscall.O_RDONLY)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() == 0 || info.Size() > storageContentLimit {
		return errors.New("invalid profile size")
	}
	binaryDir, err := openStorageDir(filepath.Dir(r.CoreBinary()), false)
	// Configs can be managed before the optional core download. Existing but
	// unsafe or broken binaries must still fail rather than bypass validation.
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.New("core binary is unavailable")
	}
	defer binaryDir.Close()
	binary, err := binaryDir.open(filepath.Base(r.CoreBinary()), syscall.O_RDONLY)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.New("core binary is unavailable")
	}
	defer binary.Close()
	ctx, cancel := context.WithTimeout(context.Background(), storageTestTimeout)
	defer cancel()
	// Descriptor paths keep validation on the exact files/directories we opened.
	// The core's diagnostics may contain subscription tokens or config secrets.
	cmd := exec.CommandContext(ctx, "/proc/self/fd/5", "-t", "-d", "/proc/self/fd/4", "-f", "/proc/self/fd/3")
	cmd.ExtraFiles = []*os.File{f, s.core.File, binary}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		return errors.New("config validation failed")
	}
	return nil
}

func (r *Runtime) installStoredProfile(s *profileStorage, name string, data []byte) error {
	f, err := os.CreateTemp(s.profiles.path(""), ".monobox-*.yaml")
	if err != nil {
		return errors.New("cannot stage profile")
	}
	tempPath := f.Name()
	defer os.Remove(tempPath)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return errors.New("cannot stage profile")
	}
	if err := r.validateStoredProfile(s, s.profiles, filepath.Base(tempPath)); err != nil {
		return err
	}
	if err := s.profiles.check(name); err != nil {
		return err
	}
	if err := os.Rename(tempPath, s.profiles.path(name)); err != nil {
		return errors.New("cannot atomically replace profile")
	}
	return nil
}
