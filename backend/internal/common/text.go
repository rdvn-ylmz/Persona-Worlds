package common

import "strings"

func TruncateRunes(value string, maxRunes int) string {
	trimmed := strings.TrimSpace(value)
	if maxRunes <= 0 {
		return trimmed
	}

	runes := []rune(trimmed)
	if len(runes) <= maxRunes {
		return trimmed
	}
	return strings.TrimSpace(string(runes[:maxRunes]))
}
