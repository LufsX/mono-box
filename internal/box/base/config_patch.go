package base

import (
	"sort"
	"strings"
)

// MergeConfigPatch replaces the last assignment of each updated key, preserving
// comments and unrelated lines. Update values must already be shell literals.
func MergeConfigPatch(content string, updates map[string]string) string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	last := map[string]int{}
	for i, line := range lines {
		text := strings.TrimSpace(line)
		if strings.HasPrefix(text, "#") {
			continue
		}
		key, _, ok := strings.Cut(strings.TrimPrefix(text, "export "), "=")
		if ok {
			last[strings.TrimSpace(key)] = i
		}
	}
	keys := make([]string, 0, len(updates))
	for key := range updates {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := updates[key]
		if i, ok := last[key]; ok {
			eq := strings.IndexByte(lines[i], '=')
			raw := lines[i][eq+1:]
			comment := configComment(raw)
			lines[i] = lines[i][:eq+1] + value
			if comment != "" {
				lines[i] += " " + comment
			}
		} else {
			if len(lines) > 0 && lines[len(lines)-1] == "" {
				lines = lines[:len(lines)-1]
			}
			lines = append(lines, key+"="+value, "")
		}
	}
	return strings.Join(lines, "\n")
}

func configComment(raw string) string {
	var quote byte
	escaped := false
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			continue
		}
		if ch == '#' && (i == 0 || raw[i-1] == ' ' || raw[i-1] == '\t') {
			return raw[i:]
		}
	}
	return ""
}
