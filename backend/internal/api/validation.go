package api

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	uuidRegex = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)
	slugRegex = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
)

func validateUUID(value, fieldName string) (string, error) {
	clean := strings.TrimSpace(value)
	if clean == "" {
		return "", fmt.Errorf("%s is required", fieldName)
	}
	if !uuidRegex.MatchString(clean) {
		return "", fmt.Errorf("%s is invalid", fieldName)
	}
	return clean, nil
}

func validateTopic(value string, min, max int) (string, error) {
	clean := strings.TrimSpace(value)
	length := len([]rune(clean))
	if length < min {
		return "", fmt.Errorf("topic must be at least %d chars", min)
	}
	if length > max {
		return "", fmt.Errorf("topic must be <= %d chars", max)
	}
	return clean, nil
}

func validateSlug(value string) (string, error) {
	normalized := normalizePublicSlug(value)
	if normalized == "" {
		return "", fmt.Errorf("slug is invalid")
	}
	if len([]rune(normalized)) > 64 {
		return "", fmt.Errorf("slug is invalid")
	}
	if !slugRegex.MatchString(normalized) {
		return "", fmt.Errorf("slug is invalid")
	}
	return normalized, nil
}

func parsePaginationLimit(raw string, defaultValue, minValue, maxValue int) (int, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return defaultValue, nil
	}
	value, err := strconv.Atoi(clean)
	if err != nil {
		return 0, fmt.Errorf("limit must be a number")
	}
	if value < minValue || value > maxValue {
		return 0, fmt.Errorf("limit must be between %d and %d", minValue, maxValue)
	}
	return value, nil
}
