//go:build linux || android

package box

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
)

// Validate and count central-directory headers before archive/zip allocates its
// File slice. Checking len(Reader.File) afterward does not bound those allocations.
// ZIP64 and multi-disk archives are unnecessary under our 128 MiB download limit.
func downloadCheckZipDirectory(src io.ReaderAt, size int64, maxEntries int) error {
	const endSize = 22
	if size < endSize {
		return errors.New("truncated ZIP")
	}
	tailSize := int64(65535 + endSize)
	if size < tailSize {
		tailSize = size
	}
	tail := make([]byte, int(tailSize))
	if _, err := src.ReadAt(tail, size-tailSize); err != nil {
		return err
	}
	var end []byte
	var endOffset int64
	for i := len(tail) - endSize; i >= 0; i-- {
		if bytes.Equal(tail[i:i+4], []byte{'P', 'K', 5, 6}) &&
			i+endSize+int(binary.LittleEndian.Uint16(tail[i+20:])) == len(tail) {
			end, endOffset = tail[i:], size-tailSize+int64(i)
			break
		}
	}
	if end == nil {
		return errors.New("ZIP end directory not found")
	}
	disk := binary.LittleEndian.Uint16(end[4:])
	dirDisk := binary.LittleEndian.Uint16(end[6:])
	diskEntries := binary.LittleEndian.Uint16(end[8:])
	entries := binary.LittleEndian.Uint16(end[10:])
	dirSize := int64(binary.LittleEndian.Uint32(end[12:]))
	offset := int64(binary.LittleEndian.Uint32(end[16:]))
	if disk != 0 || dirDisk != 0 || diskEntries != entries || entries == 0xffff ||
		dirSize == 0xffffffff || offset == 0xffffffff {
		return errors.New("ZIP64 or multi-disk ZIP is not supported")
	}
	if int(entries) > maxEntries {
		return errors.New("too many archive entries")
	}
	if offset > endOffset || dirSize != endOffset-offset {
		return errors.New("invalid ZIP directory bounds")
	}
	var header [46]byte
	count := 0
	for offset < endOffset {
		count++
		if count > maxEntries {
			return errors.New("too many archive entries")
		}
		if endOffset-offset < int64(len(header)) {
			return errors.New("truncated ZIP directory entry")
		}
		if _, err := src.ReadAt(header[:], offset); err != nil {
			return err
		}
		if !bytes.Equal(header[:4], []byte{'P', 'K', 1, 2}) {
			return errors.New("invalid ZIP directory entry")
		}
		next := offset + int64(len(header)) + int64(binary.LittleEndian.Uint16(header[28:])) +
			int64(binary.LittleEndian.Uint16(header[30:])) + int64(binary.LittleEndian.Uint16(header[32:]))
		if next > endOffset {
			return errors.New("invalid ZIP directory entry bounds")
		}
		offset = next
	}
	if count != int(entries) {
		return errors.New("ZIP directory entry count mismatch")
	}
	return nil
}
