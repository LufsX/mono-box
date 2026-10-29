//go:build linux || android

package service

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/LufsX/mono-box/internal/box/base"
)

const serviceStartupLogLimit = 4096

// Keep the original error and include bounded tails from this startup's logs.
func serviceStartupError(cause error, paths ...string) error {
	for _, path := range paths {
		file, err := base.OpenRegular(path, os.O_RDONLY, 0)
		if err != nil {
			continue
		}
		info, err := file.Stat()
		if err != nil {
			file.Close()
			continue
		}
		offset := max(int64(0), info.Size()-serviceStartupLogLimit)
		data := make([]byte, min(int64(serviceStartupLogLimit), info.Size()))
		n, err := file.ReadAt(data, offset)
		file.Close()
		if err != nil && err != io.EOF {
			continue
		}
		text := strings.TrimSpace(strings.ToValidUTF8(string(data[:n]), "�"))
		if text == "" {
			continue
		}
		if offset > 0 {
			text = "[truncated]\n" + text
		}
		cause = fmt.Errorf("%w\nCore log (%s):\n%s", cause, path, text)
	}
	return cause
}
