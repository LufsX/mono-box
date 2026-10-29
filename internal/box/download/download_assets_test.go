//go:build linux || android

package download

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

// Names observed in the upstream latest v1.19.33 and Prerelease-Alpha metadata.
func TestDownloadActualReleaseNames(t *testing.T) {
	devices := []struct{ abi, assetArch string }{
		{"arm64-v8a", "arm64-v8"},
		{"armeabi-v7a", "armv7"},
		{"x86", "386"},
		{"x86_64", "amd64"},
	}
	for _, suffix := range []string{"v1.19.33", "alpha-smart-4624551"} {
		t.Run(suffix, func(t *testing.T) {
			var assets []downloadAsset
			for _, device := range devices {
				for _, platform := range []string{"linux", "android"} {
					name := "mihomo-" + platform + "-" + device.assetArch + "-" + suffix + ".gz"
					assets = append(assets, downloadAsset{Name: name, URL: "https://github.com/releases/download/tag/" + name})
				}
			}
			// Unrelated Android ARM and x86 assets must never beat a match.
			for _, name := range []string{"mihomo-android-armv5-" + suffix + ".gz", "mihomo-android-amd64-v3-" + suffix + ".gz"} {
				assets = append(assets, downloadAsset{Name: name, URL: "https://github.com/releases/download/tag/" + name})
			}
			for _, device := range devices {
				t.Run(device.abi, func(t *testing.T) {
					got, err := downloadSelectAsset(assets, downloadTestArch(t, device.abi))
					want := "mihomo-android-" + device.assetArch + "-" + suffix + ".gz"
					if err != nil || got.Name != want {
						t.Fatalf("want %s, got %s, %v", want, got.Name, err)
					}
					got, err = downloadSelectAsset([]downloadAsset{{Name: want + ".sha256", URL: "https://github.com/" + want + ".sha256"}}, downloadTestArch(t, device.abi))
					if err == nil {
						t.Fatalf("accepted checksum %s", got.Name)
					}
				})
			}
		})
	}
}

func TestDownloadZipDirectoryBounds(t *testing.T) {
	archive := downloadTestArchive(t, "zip",
		downloadTestMember{"a", []byte("a"), 0},
		downloadTestMember{"b", []byte("b"), 0})
	end := bytes.LastIndex(archive, []byte{'P', 'K', 5, 6})
	for _, tc := range []struct {
		name   string
		mutate func([]byte)
	}{
		{"entry-count", func(data []byte) {
			binary.LittleEndian.PutUint16(data[end+8:], 1)
			binary.LittleEndian.PutUint16(data[end+10:], 1)
		}},
		{"zip64", func(data []byte) { binary.LittleEndian.PutUint16(data[end+10:], 0xffff) }},
		{"offset", func(data []byte) { binary.LittleEndian.PutUint32(data[end+16:], 0xfffffffe) }},
		{"multidisk", func(data []byte) { binary.LittleEndian.PutUint16(data[end+4:], 1) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := append([]byte(nil), archive...)
			tc.mutate(data)
			if err := downloadCheckZipDirectory(bytes.NewReader(data), int64(len(data)), 10); err == nil {
				t.Fatal("accepted invalid ZIP directory")
			}
		})
	}
	if err := downloadCheckZipDirectory(bytes.NewReader(archive), int64(len(archive)), 1); err == nil || !strings.Contains(err.Error(), "too many") {
		t.Fatalf("entry budget: %v", err)
	}
}
