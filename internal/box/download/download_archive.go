//go:build linux || android

package download

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

type downloadUnpackLimits struct {
	binary, expanded int64
	entries          int
}

func downloadDefaultLimits() downloadUnpackLimits {
	return downloadUnpackLimits{downloadBinaryLimit, downloadExpandedLimit, downloadEntryLimit}
}

// Archives never supply filesystem paths: all payloads stream into a single
// private staging file, but every member name and type is still validated.
func downloadSafeMember(name string) bool {
	if name == "" || strings.ContainsAny(name, "\\\x00:") || strings.HasPrefix(name, "/") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return false
		}
	}
	return path.Clean(name) != "."
}

type downloadUnpacker struct {
	dst       *os.File
	arch      downloadArchitecture
	limits    downloadUnpackLimits
	total     int64
	count     int
	best      int
	found     bool
	ambiguous bool
}

func (u *downloadUnpacker) member(name string, size int64, directory bool, src io.Reader) error {
	u.count++
	if u.count > u.limits.entries {
		return errors.New("too many archive entries")
	}
	if !downloadSafeMember(name) && !(directory && (name == "." || name == "./")) {
		return fmt.Errorf("unsafe archive path %q", name)
	}
	if size < 0 || size > u.limits.expanded-u.total {
		return errors.New("archive expanded size exceeds limit")
	}
	if directory {
		if size != 0 {
			return errors.New("archive directory contains data")
		}
		return nil
	}
	score, candidate := downloadNameScore(path.Base(name), u.arch, true)
	if candidate {
		// Architecture/platform directories count as well as the basename.
		// Validate basename suffixes first so appending a directory cannot hide
		// a checksum or documentation extension.
		score, candidate = downloadNameScore(path.Base(name)+"-"+path.Dir(name), u.arch, true)
	}
	// A nested binary has no archive suffix. Reject apparent nested archives.
	lower := strings.ToLower(name)
	for _, suffix := range []string{".gz", ".zip", ".tgz", ".tar"} {
		if strings.HasSuffix(lower, suffix) {
			candidate = false
		}
	}
	if candidate && size > u.limits.binary {
		return errors.New("core exceeds size limit")
	}
	var dst io.Writer = io.Discard
	if candidate && (!u.found || score > u.best) {
		if err := u.dst.Truncate(0); err != nil {
			return err
		}
		if _, err := u.dst.Seek(0, io.SeekStart); err != nil {
			return err
		}
		dst = u.dst
		u.found, u.best, u.ambiguous = true, score, false
	} else if candidate && score == u.best {
		u.ambiguous = true
	}
	// Read to EOF so zip CRC errors cannot be hidden by trusting header sizes.
	n, err := io.Copy(dst, io.LimitReader(src, size+1))
	if err != nil {
		return err
	}
	if n != size {
		return errors.New("archive member size mismatch")
	}
	u.total += n
	return nil
}

func downloadUnpack(src, dst *os.File, name string, arch downloadArchitecture, limits downloadUnpackLimits) error {
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if limits.binary <= 0 || limits.expanded <= 0 || limits.entries <= 0 {
		return errors.New("invalid extraction limits")
	}
	if err := dst.Truncate(0); err != nil {
		return err
	}
	if _, err := dst.Seek(0, io.SeekStart); err != nil {
		return err
	}
	name = strings.ToLower(name)
	u := downloadUnpacker{dst: dst, arch: arch, limits: limits}
	switch {
	case strings.HasSuffix(name, ".zip"):
		info, err := src.Stat()
		if err != nil {
			return err
		}
		if err := downloadCheckZipDirectory(src, info.Size(), limits.entries); err != nil {
			return err
		}
		archive, err := zip.NewReader(src, info.Size())
		if err != nil {
			return err
		}
		if len(archive.File) > limits.entries {
			return errors.New("too many archive entries")
		}
		for _, member := range archive.File {
			mode := member.Mode()
			if !mode.IsRegular() && !mode.IsDir() {
				return errors.New("archive contains a link or special file")
			}
			if member.UncompressedSize64 > uint64(limits.expanded-u.total) {
				return errors.New("archive expanded size exceeds limit")
			}
			reader, err := member.Open()
			if err != nil {
				return err
			}
			err = u.member(member.Name, int64(member.UncompressedSize64), mode.IsDir(), reader)
			closeErr := reader.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
		}
	case strings.HasSuffix(name, ".tar.gz"), strings.HasSuffix(name, ".tgz"):
		gz, err := gzip.NewReader(src)
		if err != nil {
			return err
		}
		defer gz.Close()
		// Bound the entire tar stream, including headers, PAX metadata and padding.
		// A payload-only budget would allow metadata-only decompression bombs.
		maxStream := limits.expanded + int64(limits.entries)*1024 + 1024
		budget := &io.LimitedReader{R: gz, N: maxStream + 1}
		archive := tar.NewReader(budget)
		for {
			member, err := archive.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
			if member.Typeflag != tar.TypeReg && member.Typeflag != tar.TypeRegA && member.Typeflag != tar.TypeDir {
				return errors.New("archive contains a link or special file")
			}
			if err := u.member(member.Name, member.Size, member.Typeflag == tar.TypeDir, archive); err != nil {
				return err
			}
		}
		// Drain the remaining gzip stream to validate its checksum and enforce the
		// same bound on trailing padding or concatenated gzip members.
		if _, err := io.Copy(io.Discard, budget); err != nil {
			return err
		}
		if budget.N == 0 {
			return errors.New("tar stream exceeds size limit")
		}
	case strings.HasSuffix(name, ".gz"):
		gz, err := gzip.NewReader(src)
		if err != nil {
			return err
		}
		defer gz.Close()
		if err := downloadCopyCore(dst, gz, limits.binary); err != nil {
			return err
		}
		return nil
	default:
		return downloadCopyCore(dst, src, limits.binary)
	}
	if !u.found {
		return errors.New("mihomo binary not found in archive")
	}
	if u.ambiguous {
		return errors.New("ambiguous mihomo binaries in archive")
	}
	return nil
}

func downloadCopyCore(dst io.Writer, src io.Reader, limit int64) error {
	n, err := io.Copy(dst, io.LimitReader(src, limit+1))
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.New("empty core")
	}
	if n > limit {
		return errors.New("core exceeds size limit")
	}
	return nil
}
