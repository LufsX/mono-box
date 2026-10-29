//go:build linux || android

package download

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"debug/elf"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
)

func downloadTestArch(t *testing.T, abi string) downloadArchitecture {
	t.Helper()
	arch, err := downloadResolveABI(abi)
	if err != nil {
		t.Fatal(err)
	}
	return arch
}

// A minimal, structurally valid executable with an executable PT_LOAD segment.
func downloadTestELF(arch downloadArchitecture) []byte {
	size, phsize := 64, 56
	if arch.class == elf.ELFCLASS32 {
		size, phsize = 52, 32
	}
	data := make([]byte, size+phsize+8)
	copy(data, "\x7fELF")
	data[4], data[5], data[6] = byte(arch.class), byte(elf.ELFDATA2LSB), 1
	binary.LittleEndian.PutUint16(data[16:], uint16(elf.ET_EXEC))
	binary.LittleEndian.PutUint16(data[18:], uint16(arch.machine))
	binary.LittleEndian.PutUint32(data[20:], 1)
	if arch.class == elf.ELFCLASS64 {
		binary.LittleEndian.PutUint64(data[32:], uint64(size))
		binary.LittleEndian.PutUint16(data[52:], uint16(size))
		binary.LittleEndian.PutUint16(data[54:], uint16(phsize))
		binary.LittleEndian.PutUint16(data[56:], 1)
		binary.LittleEndian.PutUint32(data[size:], uint32(elf.PT_LOAD))
		binary.LittleEndian.PutUint32(data[size+4:], uint32(elf.PF_R|elf.PF_X))
		binary.LittleEndian.PutUint64(data[size+32:], uint64(len(data)))
		binary.LittleEndian.PutUint64(data[size+40:], uint64(len(data)))
	} else {
		binary.LittleEndian.PutUint32(data[28:], uint32(size))
		binary.LittleEndian.PutUint16(data[40:], uint16(size))
		binary.LittleEndian.PutUint16(data[42:], uint16(phsize))
		binary.LittleEndian.PutUint16(data[44:], 1)
		binary.LittleEndian.PutUint32(data[size:], uint32(elf.PT_LOAD))
		binary.LittleEndian.PutUint32(data[size+16:], uint32(len(data)))
		binary.LittleEndian.PutUint32(data[size+20:], uint32(len(data)))
		binary.LittleEndian.PutUint32(data[size+24:], uint32(elf.PF_R|elf.PF_X))
	}
	return data
}

type downloadTestMember struct {
	name string
	data []byte
	mode os.FileMode
}

func downloadTestArchive(t *testing.T, format string, members ...downloadTestMember) []byte {
	t.Helper()
	var out bytes.Buffer
	switch format {
	case "raw":
		return members[0].data
	case "gz":
		w := gzip.NewWriter(&out)
		_, err := w.Write(members[0].data)
		if err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	case "zip":
		w := zip.NewWriter(&out)
		for _, m := range members {
			h := &zip.FileHeader{Name: m.name, Method: zip.Deflate}
			mode := m.mode
			if mode == 0 {
				mode = 0700
			}
			h.SetMode(mode)
			f, err := w.CreateHeader(h)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.Write(m.data); err != nil {
				t.Fatal(err)
			}
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	case "tar.gz":
		gz := gzip.NewWriter(&out)
		w := tar.NewWriter(gz)
		for _, m := range members {
			flag := byte(tar.TypeReg)
			if m.mode&os.ModeSymlink != 0 {
				flag = tar.TypeSymlink
			}
			if m.mode.IsDir() {
				flag = tar.TypeDir
			}
			h := &tar.Header{Name: m.name, Mode: 0700, Size: int64(len(m.data)), Typeflag: flag}
			if flag == tar.TypeSymlink {
				h.Linkname = "../../escape"
			}
			if err := w.WriteHeader(h); err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write(m.data); err != nil {
				t.Fatal(err)
			}
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("unsupported test archive")
	}
	return out.Bytes()
}

func TestDownloadAssetSelection(t *testing.T) {
	for _, tc := range []struct {
		abi, good string
		bad       []string
	}{
		{"arm64-v8a", "mihomo-android-arm64-v8a.gz", []string{"mihomo-android-armv5.gz", "mihomo-android-armeabi-v7a.gz", "mihomo-android-arm64-v8a.gz.sha256"}},
		{"armeabi-v7a", "mihomo-android-armv7.gz", []string{"mihomo-android-armv5.gz", "mihomo-android-arm.gz", "mihomo-android-arm64.gz"}},
		{"armeabi-v7a", "mihomo-android-armeabi-v7a.gz", []string{"mihomo-android-armeabi.gz"}},
		{"x86", "mihomo-android-x86.gz", []string{"mihomo-android-x86_64.gz", "mihomo-android-amd64.gz"}},
		{"x86_64", "mihomo-android-x86_64.gz", []string{"mihomo-android-x86.gz", "mihomo-android-amd64-v3.gz"}},
	} {
		t.Run(tc.good, func(t *testing.T) {
			arch := downloadTestArch(t, tc.abi)
			makeAsset := func(name string) downloadAsset {
				return downloadAsset{name, "https://github.com/r/releases/download/v/" + name}
			}
			var assets []downloadAsset
			for _, bad := range tc.bad {
				asset := makeAsset(bad)
				if _, err := downloadSelectAsset([]downloadAsset{asset}, arch); err == nil {
					t.Errorf("accepted %s", bad)
				}
				assets = append(assets, asset)
			}
			assets = append(assets, makeAsset(tc.good), makeAsset(strings.Replace(tc.good, "android", "linux", 1)))
			got, err := downloadSelectAsset(assets, arch)
			if err != nil || got.Name != tc.good {
				t.Fatalf("got %v, %v", got, err)
			}
			assets[0] = downloadAsset{tc.good, "https://github.com/wrong-armv5.gz"}
			if _, err := downloadSelectAsset(assets[:1], arch); err == nil {
				t.Fatal("accepted mismatched name and URL")
			}
		})
	}
}

func TestDownloadABI(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // Exercise GOARCH fallback without getprop.
	if arch, err := downloadResolveABI(""); err != nil || arch.machine == elf.EM_NONE {
		t.Fatalf("%v %v", arch, err)
	}
	if _, err := downloadResolveABI("mips"); err == nil {
		t.Fatal("accepted mips")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "getprop"), []byte("#!/bin/sh\nprintf 'armeabi-v7a\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	if arch, err := downloadResolveABI(""); err != nil || arch.abi != "armeabi-v7a" {
		t.Fatalf("%v %v", arch, err)
	}
}

func downloadTestUnpack(t *testing.T, payload []byte, name string, arch downloadArchitecture, limits downloadUnpackLimits) ([]byte, error) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "asset"), payload, 0600); err != nil {
		t.Fatal(err)
	}
	src, err := os.Open(filepath.Join(dir, "asset"))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	dst, err := os.Create(filepath.Join(dir, "core"))
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Close()
	err = downloadUnpack(src, dst, name, arch, limits)
	if err == nil {
		err = downloadVerifyELF(dst, arch)
	}
	data, readErr := os.ReadFile(dst.Name())
	if readErr != nil {
		t.Fatal(readErr)
	}
	return data, err
}

func TestDownloadArchivesAndELF(t *testing.T) {
	for _, arch := range downloadArchitectures {
		t.Run(arch.abi, func(t *testing.T) {
			want := downloadTestELF(arch)
			for _, format := range []string{"raw", "gz", "zip", "tar.gz"} {
				t.Run(format, func(t *testing.T) {
					payload := downloadTestArchive(t, format, downloadTestMember{"nested/mihomo", want, 0})
					got, err := downloadTestUnpack(t, payload, "mihomo."+format, arch, downloadDefaultLimits())
					if err != nil || !bytes.Equal(got, want) {
						t.Fatalf("roundtrip: %v", err)
					}
				})
			}
			other := downloadArchitectures[0]
			if other.abi == arch.abi {
				other = downloadArchitectures[1]
			}
			if _, err := downloadTestUnpack(t, downloadTestELF(other), "mihomo", arch, downloadDefaultLimits()); err == nil {
				t.Fatal("accepted wrong architecture")
			}
			truncated := append([]byte(nil), want[:len(want)-1]...)
			if _, err := downloadTestUnpack(t, truncated, "mihomo", arch, downloadDefaultLimits()); err == nil {
				t.Fatal("accepted truncated ELF")
			}
		})
	}
}

func TestDownloadArchiveRejections(t *testing.T) {
	arch := downloadTestArch(t, "arm64-v8a")
	core := downloadTestELF(arch)
	for _, format := range []string{"zip", "tar.gz"} {
		withRootDirectory := downloadTestArchive(t, format,
			downloadTestMember{"./", nil, os.ModeDir | 0700},
			downloadTestMember{"./mihomo", core, 0})
		if _, err := downloadTestUnpack(t, withRootDirectory, "core."+format, arch, downloadDefaultLimits()); err != nil {
			t.Fatalf("root directory in %s: %v", format, err)
		}
		for _, name := range []string{"../mihomo", "/mihomo", "dir/../../mihomo", "dir\\mihomo", "C:/mihomo"} {
			t.Run(format+"/"+name, func(t *testing.T) {
				payload := downloadTestArchive(t, format, downloadTestMember{name, core, 0})
				if _, err := downloadTestUnpack(t, payload, "core."+format, arch, downloadDefaultLimits()); err == nil {
					t.Fatal("accepted traversal")
				}
			})
		}
		for _, tc := range []struct {
			name    string
			members []downloadTestMember
			limits  downloadUnpackLimits
		}{
			{"symlink", []downloadTestMember{{"mihomo", nil, os.ModeSymlink | 0700}}, downloadDefaultLimits()},
			{"missing", []downloadTestMember{{"README.md", []byte("hello"), 0}}, downloadDefaultLimits()},
			{"ambiguous", []downloadTestMember{{"a/mihomo", core, 0}, {"b/mihomo", core, 0}}, downloadDefaultLimits()},
			{"count", []downloadTestMember{{"a", nil, 0}, {"b", nil, 0}}, downloadUnpackLimits{1024, 4096, 1}},
			{"binary-size", []downloadTestMember{{"mihomo", core, 0}}, downloadUnpackLimits{32, 4096, 10}},
			{"total-size", []downloadTestMember{{"a", make([]byte, 600), 0}, {"mihomo", core, 0}}, downloadUnpackLimits{1024, 650, 10}},
		} {
			t.Run(format+"/"+tc.name, func(t *testing.T) {
				payload := downloadTestArchive(t, format, tc.members...)
				if _, err := downloadTestUnpack(t, payload, "core."+format, arch, tc.limits); err == nil {
					t.Fatal("accepted unsafe archive")
				}
			})
		}
		// A multi-ABI archive must choose the matching binary.
		members := []downloadTestMember{{"mihomo-android-armv5", []byte("wrong"), 0}, {"mihomo-android-arm64", core, 0}}
		payload := downloadTestArchive(t, format, members...)
		if _, err := downloadTestUnpack(t, payload, "core."+format, arch, downloadDefaultLimits()); err != nil {
			t.Fatal(err)
		}
		payload = downloadTestArchive(t, format,
			downloadTestMember{"android-armv5/mihomo", []byte("wrong architecture"), 0},
			downloadTestMember{"linux-arm64/mihomo", []byte("less preferred platform"), 0},
			downloadTestMember{"android-arm64/mihomo", core, 0})
		if _, err := downloadTestUnpack(t, payload, "core."+format, arch, downloadDefaultLimits()); err != nil {
			t.Fatal(err)
		}
	}
	for _, format := range []string{"gz", "tar.gz", "zip"} {
		payload := downloadTestArchive(t, format, downloadTestMember{"mihomo", core, 0})
		if format == "zip" {
			// Corrupt the CRC in the central directory.
			index := bytes.Index(payload, []byte{'P', 'K', 1, 2})
			payload[index+16] ^= 1
		} else {
			payload[len(payload)-5] ^= 1
		}
		if _, err := downloadTestUnpack(t, payload, "core."+format, arch, downloadDefaultLimits()); err == nil {
			t.Fatalf("accepted corrupt %s", format)
		}
	}
	bomb := downloadTestArchive(t, "gz", downloadTestMember{"mihomo", make([]byte, 4096), 0})
	if _, err := downloadTestUnpack(t, bomb, "core.gz", arch, downloadUnpackLimits{1024, 4096, 10}); err == nil {
		t.Fatal("accepted gzip bomb")
	}
	// Trailing tar padding is decompressed under the stream budget as well.
	var trailing bytes.Buffer
	gz := gzip.NewWriter(&trailing)
	_, _ = gz.Write(make([]byte, 8192))
	_ = gz.Close()
	if _, err := downloadTestUnpack(t, trailing.Bytes(), "core.tar.gz", arch, downloadUnpackLimits{1024, 2048, 1}); err == nil {
		t.Fatal("accepted tar padding bomb")
	}
}

type downloadTestTransport func(*http.Request) (*http.Response, error)

func (f downloadTestTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// Preserve production URLs for assertions; route their requests to httptest.
func downloadTestClient(server *httptest.Server, observe func(*http.Request)) *http.Client {
	base := server.Client().Transport
	u, _ := url.Parse(server.URL)
	return &http.Client{Transport: downloadTestTransport(func(req *http.Request) (*http.Response, error) {
		if observe != nil {
			observe(req)
		}
		copy := req.Clone(req.Context())
		target := *req.URL
		target.Scheme, target.Host = u.Scheme, u.Host
		copy.URL = &target
		return base.RoundTrip(copy)
	})}
}

func downloadTestRuntime(t *testing.T, client *http.Client, original []byte) (*Runtime, *bytes.Buffer) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	if original != nil {
		if err := os.WriteFile(filepath.Join(root, "bin", "mihomo"), original, 0700); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	return &Runtime{Runtime: &base.Runtime{Root: root, Config: base.Config{BinName: "different"}, Out: &out, HTTP: client}}, &out
}

func TestDownloadCoreRequestTimeouts(t *testing.T) {
	for _, timeout := range []time.Duration{60 * time.Second, 20 * time.Millisecond, 0, 10 * time.Minute} {
		t.Run(timeout.String(), func(t *testing.T) {
			r, _ := downloadTestRuntime(t, nil, nil)
			shared, err := base.New(r.Root, t.TempDir(), io.Discard, io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			client := shared.HTTP
			client.Timeout = timeout
			r.HTTP = client
			arch := downloadTestArch(t, "arm64-v8a")
			core := downloadTestELF(arch)
			asset := downloadAsset{"mihomo-android-arm64", "https://github.com/release/mihomo-android-arm64"}
			var requests, redirects int
			client.CheckRedirect = func(*http.Request, []*http.Request) error {
				redirects++
				return nil
			}
			client.Transport = downloadTestTransport(func(req *http.Request) (*http.Response, error) {
				requests++
				want := 30 * time.Second
				if req.URL.String() == asset.URL {
					want = 5 * time.Minute
				}
				deadline, ok := req.Context().Deadline()
				if remaining := time.Until(deadline); !ok || remaining > want || remaining < want-time.Second {
					t.Errorf("%s: deadline remaining %v, want %v", req.URL, remaining, want)
				}
				response := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), ContentLength: -1}
				switch req.URL.String() {
				case downloadLatestAPI:
					response.StatusCode = http.StatusFound
					response.Header.Set("Location", "https://api.github.com/metadata")
					response.Body = io.NopCloser(strings.NewReader(""))
				case "https://api.github.com/metadata":
					data, err := json.Marshal(map[string]any{"assets": []downloadAsset{asset}})
					if err != nil {
						return nil, err
					}
					response.Body = io.NopCloser(bytes.NewReader(data))
				case asset.URL:
					if timeout == 20*time.Millisecond {
						timer := time.NewTimer(50 * time.Millisecond)
						defer timer.Stop()
						select {
						case <-timer.C:
						case <-req.Context().Done():
							return nil, req.Context().Err()
						}
					}
					response.Body = io.NopCloser(bytes.NewReader(core))
				default:
					return nil, fmt.Errorf("unexpected URL: %s", req.URL)
				}
				return response, nil
			})
			if err := r.DownloadCore("mihomo", true, false, arch.abi); err != nil {
				t.Fatal(err)
			}
			if requests != 3 || redirects != 1 {
				t.Fatalf("requests=%d redirects=%d", requests, redirects)
			}
			if r.HTTP != client || client.Timeout != timeout {
				t.Fatal("download changed the shared HTTP client")
			}
			// The original callback still accepts this URL; the download wrapper rejects it.
			probe, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := client.CheckRedirect(probe, nil); err != nil || redirects != 2 {
				t.Fatalf("shared redirect callback changed: %v", err)
			}
			got, err := os.ReadFile(filepath.Join(r.Root, "bin", "mihomo"))
			if err != nil || !bytes.Equal(got, core) {
				t.Fatalf("core was not installed: %v", err)
			}
		})
	}
}

func TestDownloadHTTPBounds(t *testing.T) {
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		limit   int64
		timeout time.Duration
	}{
		{"status", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) }, 64, time.Second},
		{"partial", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(206); _, _ = w.Write([]byte("partial")) }, 64, time.Second},
		{"declared-size", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "100")
			_, _ = w.Write([]byte("x"))
		}, 64, time.Second},
		{"stream-size", func(w http.ResponseWriter, r *http.Request) {
			w.(http.Flusher).Flush()
			_, _ = w.Write(make([]byte, 100))
		}, 64, time.Second},
		{"truncated", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "10")
			_, _ = w.Write([]byte("x"))
		}, 64, time.Second},
		{"empty", func(w http.ResponseWriter, r *http.Request) {}, 64, time.Second},
		{"timeout", func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }, 64, 20 * time.Millisecond},
		{"downgrade", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "http://example.com/file", 302) }, 64, time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(tc.handler)
			defer server.Close()
			r := &Runtime{Runtime: &base.Runtime{HTTP: downloadTestClient(server, nil)}}
			if err := r.downloadGet("https://github.com/file", io.Discard, tc.limit, tc.timeout); err == nil {
				t.Fatal("accepted invalid response")
			}
		})
	}
}

func TestDownloadCoreInstallAndPreserve(t *testing.T) {
	arch := downloadTestArch(t, "arm64-v8a")
	core := downloadTestELF(arch)
	for _, tc := range []struct {
		name    string
		status  int
		payload []byte
		badJSON bool
		fail    bool
		format  string
	}{
		{"success-gzip", 200, downloadTestArchive(t, "gz", downloadTestMember{"mihomo", core, 0}), false, false, "gz"},
		{"success-raw", 200, core, false, false, "raw"},
		{"success-zip", 200, downloadTestArchive(t, "zip", downloadTestMember{"nested/mihomo", core, 0}), false, false, "zip"},
		{"success-tar", 200, downloadTestArchive(t, "tar.gz", downloadTestMember{"nested/mihomo", core, 0}), false, false, "tar.gz"},
		{"HTTP-error", 500, []byte("error"), false, true, "gz"},
		{"wrong-ELF", 200, downloadTestArchive(t, "gz", downloadTestMember{"mihomo", downloadTestELF(downloadTestArch(t, "x86")), 0}), false, true, "gz"},
		{"invalid-gzip", 200, []byte("corrupt"), false, true, "gz"},
		{"bad-JSON", 200, nil, true, true, "gz"},
		{"zip-traversal", 200, downloadTestArchive(t, "zip", downloadTestMember{"../mihomo", core, 0}), false, true, "zip"},
		{"tar-traversal", 200, downloadTestArchive(t, "tar.gz", downloadTestMember{"../mihomo", core, 0}), false, true, "tar.gz"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			name := "mihomo-android-arm64-v8-v1.19.33"
			if tc.format != "raw" {
				name += "." + tc.format
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if strings.Contains(req.URL.Path, "/releases/latest") {
					if tc.badJSON {
						_, _ = w.Write([]byte("bad"))
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"assets": []downloadAsset{{name, "https://github.com/release/" + name}}})
					return
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write(tc.payload)
			}))
			defer server.Close()
			original := []byte("existing installed core")
			r, out := downloadTestRuntime(t, downloadTestClient(server, nil), original)
			err := r.DownloadCore("mihomo", true, false, arch.abi)
			if (err != nil) != tc.fail {
				t.Fatalf("error: %v", err)
			}
			got, readErr := os.ReadFile(filepath.Join(r.Root, "bin", "mihomo"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			want := core
			if tc.fail {
				want = original
			}
			if !bytes.Equal(got, want) {
				t.Fatal("incorrect installed content or original damaged")
			}
			entries, _ := os.ReadDir(filepath.Join(r.Root, "bin"))
			if len(entries) != 1 {
				t.Fatalf("temporary files leaked: %v", entries)
			}
			if !tc.fail {
				info, _ := os.Stat(filepath.Join(r.Root, "bin", "mihomo"))
				if info.Mode().Perm() != 0700 {
					t.Fatalf("permissions %v", info.Mode())
				}
				if !strings.Contains(out.String(), "Installed") {
					t.Fatal(out.String())
				}
			}
		})
	}
}

func TestDownloadCoreSkip(t *testing.T) {
	client := &http.Client{Transport: downloadTestTransport(func(*http.Request) (*http.Response, error) {
		t.Error("unexpected HTTP")
		return nil, errors.New("unexpected HTTP")
	})}
	r, out := downloadTestRuntime(t, client, []byte("existing"))
	if err := r.DownloadCore("mihomo", false, false, "invalid"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "skip") {
		t.Fatal(out.String())
	}
	if err := r.DownloadCore("unknown", true, false, "arm64-v8a"); err == nil {
		t.Fatal("accepted invalid kind")
	}
}

func TestDownloadSmartFallbackAndProxy(t *testing.T) {
	arch := downloadTestArch(t, "arm64-v8a")
	core := downloadTestELF(arch)
	name := "mihomo-android-arm64-v8-alpha-smart-4624551.gz"
	for _, fallback := range []bool{false, true} {
		t.Run(fmt.Sprint(fallback), func(t *testing.T) {
			var seenAPI, seenFallback, seenAsset atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				switch {
				case strings.Contains(req.URL.Path, "api.github.com/"):
					seenAPI.Store(true)
					if fallback {
						w.WriteHeader(403)
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"assets": []downloadAsset{{name, "https://github.com/vernesong/mihomo/releases/download/Prerelease-Alpha/" + name}}})
				case strings.Contains(req.URL.Path, "expanded_assets"):
					seenFallback.Store(true)
					_, _ = fmt.Fprintf(w, "<a href='/vernesong/mihomo/releases/download/Prerelease-Alpha/%s?x=1&amp;y=2'>core</a>", name)
				default:
					seenAsset.Store(true)
					_, _ = w.Write(downloadTestArchive(t, "gz", downloadTestMember{"mihomo", core, 0}))
				}
			}))
			defer server.Close()
			client := downloadTestClient(server, func(req *http.Request) {
				if !strings.HasPrefix(req.URL.String(), downloadProxyPrefix+"https://") {
					t.Errorf("missing proxy: %s", req.URL)
				}
			})
			r, _ := downloadTestRuntime(t, client, nil)
			if err := r.DownloadCore("mihomo_smart", true, true, arch.abi); err != nil {
				t.Fatal(err)
			}
			if !seenAPI.Load() || seenFallback.Load() != fallback || !seenAsset.Load() {
				t.Fatalf("API=%v fallback=%v asset=%v", seenAPI.Load(), seenFallback.Load(), seenAsset.Load())
			}
		})
	}
	page := "<a href='https://evil.example/vernesong/mihomo/releases/download/Prerelease-Alpha/mihomo-android-arm64.gz'>bad</a>"
	if assets := downloadExpandedAssets(page); len(assets) != 0 {
		t.Fatal(assets)
	}
}

func TestDownloadConcurrent(t *testing.T) {
	arch := downloadTestArch(t, "arm64-v8a")
	core := downloadTestELF(arch)
	name := "mihomo-android-arm64.gz"
	var hits atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.Contains(req.URL.Path, "/releases/latest") {
			hits.Add(1)
			close(entered)
			<-release
			_ = json.NewEncoder(w).Encode(map[string]any{"assets": []downloadAsset{{name, "https://github.com/" + name}}})
		} else {
			_, _ = w.Write(downloadTestArchive(t, "gz", downloadTestMember{"mihomo", core, 0}))
		}
	}))
	defer server.Close()
	r, _ := downloadTestRuntime(t, downloadTestClient(server, nil), nil)
	// Independent Runtime values model separate CLI invocations and avoid writer races.
	other := *r
	r.Out, other.Out = io.Discard, io.Discard
	var wg sync.WaitGroup
	results := make(chan error, 2)
	wg.Add(2)
	go func() { defer wg.Done(); results <- r.DownloadCore("mihomo", false, false, arch.abi) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("download did not start")
	}
	go func() { defer wg.Done(); results <- other.DownloadCore("mihomo", false, false, arch.abi) }()
	close(release)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("API requests: %d", hits.Load())
	}
}

func TestDownloadRunningCoreAndOtherDirectory(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux process executable identity")
	}
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep unavailable")
	}
	payload, err := os.ReadFile(sleep)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := downloadTestRuntime(t, nil, payload)
	target := filepath.Join(r.Root, "bin", "mihomo")
	cmd := exec.Command(target, "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	if err := r.DownloadCore("mihomo", true, false, "arm64-v8a"); err == nil || !strings.Contains(err.Error(), "running") {
		t.Fatalf("expected running error: %v", err)
	}
	got, _ := os.ReadFile(target)
	if !bytes.Equal(got, payload) {
		t.Fatal("running binary changed")
	}
	// The same process must not block installations under another root.
	other, _ := downloadTestRuntime(t, nil, []byte("idle"))
	if err := downloadCheckStopped(filepath.Join(other.Root, "bin", "mihomo")); err != nil {
		t.Fatal(err)
	}
}

func TestDownloadRejectsUnsafeTarget(t *testing.T) {
	for _, component := range []string{"bin", "mihomo"} {
		t.Run(component, func(t *testing.T) {
			r, _ := downloadTestRuntime(t, nil, nil)
			victim := t.TempDir()
			link := filepath.Join(r.Root, "bin")
			if component == "bin" {
				if err := os.Remove(link); err != nil {
					t.Fatal(err)
				}
			} else {
				link = filepath.Join(link, "mihomo")
				victim = filepath.Join(victim, "victim")
				if err := os.WriteFile(victim, []byte("preserve"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(victim, link); err != nil {
				t.Fatal(err)
			}
			if err := r.DownloadCore("mihomo", true, false, "arm64-v8a"); err == nil {
				t.Fatal("accepted symlink target")
			}
			if component == "mihomo" {
				data, _ := os.ReadFile(victim)
				if string(data) != "preserve" {
					t.Fatal("symlink victim changed")
				}
			}
		})
	}
}
