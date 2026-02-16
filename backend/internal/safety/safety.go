package safety

import (
	"errors"
	"regexp"
	"strings"
)

var (
	profanityPattern = regexp.MustCompile(`(?i)\b(fuck|shit|bitch|asshole|dick)\b`)
	linkPattern      = regexp.MustCompile(`(?i)https?://|www\.`)
)

func ValidateContent(content string, maxLen int) error {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return errors.New("content cannot be empty")
	}
	if len([]rune(trimmed)) > maxLen {
		return errors.New("content exceeds max length")
	}
	if profanityPattern.MatchString(trimmed) {
		return errors.New("content failed profanity check")
	}
	if len(linkPattern.FindAllStringIndex(trimmed, -1)) > 2 {
		return errors.New("content failed link spam check")
	}
	return nil
}
