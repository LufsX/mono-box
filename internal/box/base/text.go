package base

import (
	"encoding/base64"
	"errors"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
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

func DecodeText(encoded string, limit int) ([]byte, error) {
	if len(encoded) > base64.StdEncoding.EncodedLen(limit) {
		return nil, errors.New("storage argument exceeds size limit")
	}
	data, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(data) > limit || !utf8.Valid(data) || strings.IndexByte(string(data), 0) >= 0 {
		return nil, errors.New("invalid base64 text argument")
	}
	return data, nil
}
