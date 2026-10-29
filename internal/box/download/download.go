//go:build linux || android

package download

import (
	"context"
	"debug/elf"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
)

const (
	downloadLatestAPI           = "https://api.github.com/repos/MetaCubeX/mihomo/releases/latest"
	downloadSmartAPI            = "https://api.github.com/repos/vernesong/mihomo/releases/tags/Prerelease-Alpha"
	downloadSmartAssets         = "https://github.com/vernesong/mihomo/releases/expanded_assets/Prerelease-Alpha"
	downloadProxyPrefix         = "https://cors.isteed.cc/"
	downloadMetadataLimit int64 = 8 << 20
	downloadAssetLimit    int64 = 128 << 20
	downloadBinaryLimit   int64 = 256 << 20
	downloadExpandedLimit int64 = 512 << 20
	downloadEntryLimit          = 4096
)

type downloadAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

type downloadArchitecture struct {
	abi     string
	machine elf.Machine
	class   elf.Class
	pattern *regexp.Regexp
}

var downloadArchitectures = []downloadArchitecture{
	{"arm64-v8a", elf.EM_AARCH64, elf.ELFCLASS64, regexp.MustCompile(`(^|[^a-z0-9])(arm64-v8a|arm64|aarch64)([^a-z0-9]|$)`)},
	{"armeabi-v7a", elf.EM_ARM, elf.ELFCLASS32, regexp.MustCompile(`(^|[^a-z0-9])(armeabi-v7a|armv7a?)([^a-z0-9]|$)`)},
	{"x86", elf.EM_386, elf.ELFCLASS32, regexp.MustCompile(`(^|[^a-z0-9])(x86|386|i[3-6]86)([^a-z0-9]|$)`)},
	{"x86_64", elf.EM_X86_64, elf.ELFCLASS64, regexp.MustCompile(`(^|[^a-z0-9])(x86_64|amd64)([^a-z0-9]|$)`)},
}
var downloadUnsupportedARM = regexp.MustCompile(`(^|[^a-z0-9])(armv[4-69][a-z0-9]*|armeabi)([^a-z0-9]|$)`)
var downloadAdvancedAMD64 = regexp.MustCompile(`(^|[^a-z0-9])(amd64|x86_64)[-_]v[2-9]([^a-z0-9]|$)`)
var downloadForeignOS = regexp.MustCompile(`(^|[^a-z0-9])(windows|darwin|macos|freebsd|openbsd|netbsd|ios)([^a-z0-9]|$)`)
var downloadAssetHref = regexp.MustCompile(`(?i)\bhref\s*=\s*["']([^"'<>]+)["']`)

// DownloadCore installs a verified core at Root/bin/mihomo. It shares the service
// lock so a service start cannot race the final running-process check and rename.
func (r *Runtime) DownloadCore(kind string, overwrite bool, proxy bool, abi string) error {
	if kind != "mihomo" && kind != "mihomo_smart" {
		return fmt.Errorf("unsupported core kind %q", kind)
	}
	if r.Root == "" {
		return errors.New("empty runtime root")
	}
	root, err := filepath.Abs(r.Root)
	if err != nil {
		return err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	lock, err := base.AcquireServiceLock(filepath.Join(root, "run"), base.ServiceLockTimeout)
	if err != nil {
		return fmt.Errorf("download core lock: %w", err)
	}
	defer lock.Close()
	dir, err := base.OpenDirectory(filepath.Join(root, "bin"), true)
	if err != nil {
		return err
	}
	defer dir.Close()
	// Keep every write, including the rename, anchored to this directory.
	bin := filepath.Join("/proc/self/fd", strconv.Itoa(int(dir.Fd())))
	target := filepath.Join(bin, "mihomo")
	info, err := os.Lstat(target)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("core target is not a regular file")
		}
		if info.Size() > 0 && !overwrite {
			r.downloadMessage("Core already exists, skip download")
			return nil
		}
	}
	// Linux refuses a writable handle for an executing inode (ETXTBSY).
	// Retain this handle until commit to also prevent an external exec racing
	// our procfs checks; no bytes in the installed core are changed.
	old, err := base.OpenRegularAt(dir, "mihomo", os.O_WRONLY, 0)
	if errors.Is(err, syscall.ETXTBSY) {
		return errors.New("core is running; stop it before overwrite")
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if old != nil {
		defer old.Close()
	}
	arch, err := downloadResolveABI(abi)
	if err != nil {
		return err
	}
	corePath := filepath.Join(root, "bin", "mihomo")
	if err := downloadCheckStopped(corePath); err != nil {
		return err
	}
	r.downloadMessage("Downloading %s; Device ABI: %s", kind, arch.abi)
	asset, err := r.downloadFindAsset(kind, proxy, arch)
	if err != nil {
		return err
	}
	work, err := os.MkdirTemp(bin, ".monobox-download-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	archive, err := os.OpenFile(filepath.Join(work, "asset"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer archive.Close()
	source := downloadWrapURL(asset.URL, proxy)
	r.downloadMessage("Download URL: %s", source)
	if err := r.downloadGet(source, archive, downloadAssetLimit, 5*time.Minute); err != nil {
		return fmt.Errorf("download asset: %w", err)
	}
	staged, err := os.OpenFile(filepath.Join(work, "core"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer staged.Close()
	if err := downloadUnpack(archive, staged, asset.Name, arch, downloadDefaultLimits()); err != nil {
		return fmt.Errorf("unpack core: %w", err)
	}
	if err := downloadVerifyELF(staged, arch); err != nil {
		return fmt.Errorf("verify core: %w", err)
	}
	if err := staged.Chmod(0700); err != nil {
		return err
	}
	if err := staged.Sync(); err != nil {
		return err
	}
	if err := staged.Close(); err != nil {
		return err
	}
	// Repeat after network and extraction, including manually launched cores
	// without a service PID file. Never signal or stop any process.
	if err := downloadCheckStopped(corePath); err != nil {
		return err
	}
	if err := os.Rename(staged.Name(), target); err != nil {
		return err
	}
	r.downloadMessage("Installed %s to %s", kind, corePath)
	return nil
}

func (r *Runtime) downloadMessage(format string, args ...any) {
	if r.Out != nil {
		fmt.Fprintf(r.Out, format+"\n", args...)
	}
}

func downloadResolveABI(abi string) (downloadArchitecture, error) {
	abi = strings.TrimSpace(abi)
	if abi == "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "getprop", "ro.product.cpu.abi")
		cmd.WaitDelay = time.Second
		// A fixed-size output writer also bounds a faulty getprop replacement.
		out := &downloadSmallOutput{}
		cmd.Stdout = out
		if err := cmd.Run(); err == nil {
			abi = strings.TrimSpace(string(out.data))
		}
		if abi == "" {
			abi = runtime.GOARCH
		}
	}
	switch abi {
	case "arm64", "aarch64":
		abi = "arm64-v8a"
	case "arm", "armv7", "armv7a":
		abi = "armeabi-v7a"
	case "amd64":
		abi = "x86_64"
	case "386", "i386", "i686":
		abi = "x86"
	}
	for _, arch := range downloadArchitectures {
		if abi == arch.abi {
			return arch, nil
		}
	}
	return downloadArchitecture{}, fmt.Errorf("unsupported device ABI %q", abi)
}

type downloadSmallOutput struct{ data []byte }

func (w *downloadSmallOutput) Write(p []byte) (int, error) {
	if len(w.data)+len(p) > 256 {
		return 0, errors.New("getprop output too large")
	}
	w.data = append(w.data, p...)
	return len(p), nil
}

func downloadWrapURL(source string, proxy bool) string {
	if proxy {
		return downloadProxyPrefix + source
	}
	return source
}

func downloadValidURL(source string) bool {
	u, err := url.Parse(source)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.Fragment == ""
}

func (r *Runtime) downloadGet(source string, dst io.Writer, limit int64, timeout time.Duration) error {
	if !downloadValidURL(source) {
		return errors.New("invalid HTTPS download URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "MonoBox")
	req.Header.Set("Accept-Encoding", "identity")
	var client http.Client
	if r.HTTP != nil {
		client = *r.HTTP
	}
	client.Timeout = timeout
	previousRedirect := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 || !downloadValidURL(req.URL.String()) {
			return errors.New("unsafe or excessive download redirects")
		}
		if previousRedirect != nil {
			return previousRedirect(req, via)
		}
		return nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP status %d", resp.StatusCode)
	}
	if resp.ContentLength > limit {
		return fmt.Errorf("response exceeds %d bytes", limit)
	}
	if resp.Header.Get("Content-Encoding") != "" && resp.Header.Get("Content-Encoding") != "identity" {
		return errors.New("unexpected HTTP content encoding")
	}
	n, err := io.Copy(dst, io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return err
	}
	if n > limit {
		return fmt.Errorf("response exceeds %d bytes", limit)
	}
	if n == 0 {
		return errors.New("empty HTTP response")
	}
	if resp.ContentLength >= 0 && n != resp.ContentLength {
		return errors.New("incomplete HTTP response")
	}
	return nil
}

func (r *Runtime) downloadMetadata(source string, proxy bool) ([]byte, error) {
	var out strings.Builder
	err := r.downloadGet(downloadWrapURL(source, proxy), &out, downloadMetadataLimit, 30*time.Second)
	return []byte(out.String()), err
}

func (r *Runtime) downloadFindAsset(kind string, proxy bool, arch downloadArchitecture) (downloadAsset, error) {
	api := downloadLatestAPI
	if kind == "mihomo_smart" {
		api = downloadSmartAPI
	}
	data, apiErr := r.downloadMetadata(api, proxy)
	var release struct {
		Assets []downloadAsset `json:"assets"`
	}
	if apiErr == nil {
		apiErr = json.Unmarshal(data, &release)
	}
	if apiErr == nil {
		var asset downloadAsset
		asset, apiErr = downloadSelectAsset(release.Assets, arch)
		if apiErr == nil {
			return asset, nil
		}
	}
	if kind != "mihomo_smart" {
		return downloadAsset{}, fmt.Errorf("release API: %w", apiErr)
	}
	data, err := r.downloadMetadata(downloadSmartAssets, proxy)
	if err != nil {
		return downloadAsset{}, fmt.Errorf("smart API: %v; expanded_assets: %w", apiErr, err)
	}
	assets := downloadExpandedAssets(string(data))
	asset, err := downloadSelectAsset(assets, arch)
	if err != nil {
		return downloadAsset{}, fmt.Errorf("smart API: %v; expanded_assets: %w", apiErr, err)
	}
	return asset, nil
}

func downloadExpandedAssets(page string) []downloadAsset {
	var assets []downloadAsset
	seen := make(map[string]bool)
	base, _ := url.Parse("https://github.com")
	const prefix = "/vernesong/mihomo/releases/download/Prerelease-Alpha/"
	for _, match := range downloadAssetHref.FindAllStringSubmatch(page, -1) {
		ref, err := url.Parse(html.UnescapeString(match[1]))
		if err != nil {
			continue
		}
		u := base.ResolveReference(ref)
		if u.Host != "github.com" || !downloadValidURL(u.String()) || !strings.HasPrefix(u.Path, prefix) {
			continue
		}
		name := strings.TrimPrefix(u.Path, prefix)
		if name == "" || strings.Contains(name, "/") || seen[u.String()] {
			continue
		}
		seen[u.String()] = true
		assets = append(assets, downloadAsset{Name: name, URL: u.String()})
	}
	return assets
}

// Names must identify this architecture explicitly. Android wins over Linux;
// compatible/baseline builds win over optional newer CPU instruction sets.
func downloadNameScore(name string, arch downloadArchitecture, generic bool) (int, bool) {
	name = strings.ToLower(name)
	if !strings.HasPrefix(name, "mihomo") || (len(name) > 6 && name[6] != '-' && name[6] != '_' && name[6] != '.') {
		return 0, false
	}
	for _, suffix := range []string{".txt", ".md", ".json", ".sig", ".sha256", ".sha256sum", ".sha512", ".sum", ".asc", ".exe", ".deb", ".rpm", ".xz", ".zst", ".tar", ".bz2", ".7z", ".apk", ".so"} {
		if strings.HasSuffix(name, suffix) {
			return 0, false
		}
	}
	if strings.Contains(name, "sha256") || strings.Contains(name, "sha512") || strings.Contains(name, "checksum") ||
		downloadForeignOS.MatchString(name) || downloadUnsupportedARM.MatchString(strings.ReplaceAll(name, "armeabi-v7a", "armv7")) || downloadAdvancedAMD64.MatchString(name) {
		return 0, false
	}
	// Remove x86_64 before checking the standalone x86 spelling.
	scan := strings.ReplaceAll(name, "x86_64", "amd64")
	matched := false
	for _, candidate := range downloadArchitectures {
		if candidate.pattern.MatchString(scan) {
			if candidate.abi != arch.abi {
				return 0, false
			}
			matched = true
		}
	}
	if !matched && !generic {
		return 0, false
	}
	// A generic "arm" build does not establish ARMv7 compatibility.
	if !matched && strings.Contains(name, "arm") {
		return 0, false
	}
	score := 0
	if matched {
		score += 20
	}
	if strings.Contains(name, "android") {
		score += 100
	}
	if strings.Contains(name, "compatible") {
		score += 5
	}
	if strings.HasSuffix(name, ".gz") || strings.HasSuffix(name, ".tgz") || strings.HasSuffix(name, ".zip") {
		score++
	}
	return score, true
}

func downloadSelectAsset(assets []downloadAsset, arch downloadArchitecture) (downloadAsset, error) {
	type ranked struct {
		asset downloadAsset
		score int
	}
	var candidates []ranked
	for _, asset := range assets {
		if !downloadValidURL(asset.URL) {
			continue
		}
		u, _ := url.Parse(asset.URL)
		name := path.Base(u.Path)
		if asset.Name != "" && asset.Name != name {
			continue
		}
		score, ok := downloadNameScore(name, arch, false)
		if ok {
			asset.Name = name
			candidates = append(candidates, ranked{asset, score})
		}
	}
	if len(candidates) == 0 {
		return downloadAsset{}, fmt.Errorf("no matching core asset for %s", arch.abi)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].asset.URL < candidates[j].asset.URL
	})
	return candidates[0].asset, nil
}

// Match both the canonical executable path and device/inode. This also catches
// hardlinks and a deleted executable formerly installed at this exact path.
func downloadCheckStopped(target string) error {
	current, err := os.Stat(target)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		exePath := filepath.Join("/proc", entry.Name(), "exe")
		exe, err := os.Readlink(exePath)
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.EINVAL) {
			continue
		}
		if errors.Is(err, os.ErrPermission) && os.Geteuid() != 0 {
			// Unprivileged callers cannot inspect other users' procfs entries.
			// The writable old-core handle still protects the installed inode.
			continue
		}
		if err != nil {
			return fmt.Errorf("cannot inspect process %s: %w", entry.Name(), err)
		}
		matching := strings.TrimSuffix(exe, " (deleted)") == target
		if !matching && current != nil {
			info, err := os.Stat(exePath)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return err
			}
			matching = os.SameFile(current, info)
		}
		if matching {
			return fmt.Errorf("core is running (PID %s); stop it before overwrite", entry.Name())
		}
	}
	return nil
}

func downloadVerifyELF(file *os.File, arch downloadArchitecture) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() == 0 || info.Size() > downloadBinaryLimit {
		return errors.New("invalid core size")
	}
	// Only fixed-size ELF and program headers are needed. Avoid parsing
	// untrusted section tables or compressed debug/name sections.
	var header [64]byte
	headerSize, programSize := 64, 56
	if arch.class == elf.ELFCLASS32 {
		headerSize, programSize = 52, 32
	}
	if _, err := file.ReadAt(header[:headerSize], 0); err != nil {
		return err
	}
	if string(header[:4]) != "\x7fELF" || header[6] != 1 {
		return errors.New("invalid ELF header")
	}
	order := binary.LittleEndian
	machine, class := elf.Machine(order.Uint16(header[18:])), elf.Class(header[4])
	if machine != arch.machine || class != arch.class || header[5] != byte(elf.ELFDATA2LSB) {
		return fmt.Errorf("ELF architecture %s/%s does not match %s", machine, class, arch.abi)
	}
	kind := elf.Type(order.Uint16(header[16:]))
	if kind != elf.ET_EXEC && kind != elf.ET_DYN {
		return errors.New("ELF is not an executable")
	}
	if order.Uint32(header[20:]) != 1 {
		return errors.New("invalid ELF version")
	}
	var offset, entry uint64
	var count, entrySize, declaredHeader uint16
	if class == elf.ELFCLASS64 {
		entry, offset = order.Uint64(header[24:]), order.Uint64(header[32:])
		declaredHeader, entrySize, count = order.Uint16(header[52:]), order.Uint16(header[54:]), order.Uint16(header[56:])
	} else {
		entry, offset = uint64(order.Uint32(header[24:])), uint64(order.Uint32(header[28:]))
		declaredHeader, entrySize, count = order.Uint16(header[40:]), order.Uint16(header[42:]), order.Uint16(header[44:])
	}
	size := uint64(info.Size())
	if int(declaredHeader) != headerSize || int(entrySize) != programSize || count == 0 || count > 1024 ||
		offset < uint64(headerSize) || offset > size || uint64(count)*uint64(programSize) > size-offset {
		return errors.New("invalid ELF program headers")
	}
	var program [56]byte
	load := false
	for i := uint16(0); i < count; i++ {
		if _, err := file.ReadAt(program[:programSize], int64(offset)+int64(i)*int64(programSize)); err != nil {
			return err
		}
		programType := elf.ProgType(order.Uint32(program[:]))
		var flags elf.ProgFlag
		var start, address, fileSize, memorySize uint64
		if class == elf.ELFCLASS64 {
			flags = elf.ProgFlag(order.Uint32(program[4:]))
			start, address = order.Uint64(program[8:]), order.Uint64(program[16:])
			fileSize, memorySize = order.Uint64(program[32:]), order.Uint64(program[40:])
		} else {
			start, address = uint64(order.Uint32(program[4:])), uint64(order.Uint32(program[8:]))
			fileSize, memorySize = uint64(order.Uint32(program[16:])), uint64(order.Uint32(program[20:]))
			flags = elf.ProgFlag(order.Uint32(program[24:]))
		}
		if fileSize > size || start > size-fileSize {
			return errors.New("truncated ELF segment")
		}
		if programType == elf.PT_LOAD {
			if memorySize < fileSize {
				return errors.New("invalid ELF load segment")
			}
			if flags&elf.PF_X != 0 && fileSize > 0 && entry >= address && entry-address < fileSize {
				load = true
			}
		}
	}
	if !load {
		return errors.New("ELF has no executable load segment containing its entry point")
	}
	return nil
}
