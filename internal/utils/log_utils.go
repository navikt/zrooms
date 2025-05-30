package utils

import (
	"regexp"
	"strings"
	"unicode"
)

// MaxLogStringLength defines the maximum length for user-provided strings in logs
const MaxLogStringLength = 200

// SanitizeLogString sanitizes a user-controlled string for safe logging
// It replaces control characters, limits string length, and escapes format specifiers
func SanitizeLogString(input string) string {
	if input == "" {
		return ""
	}

	// Truncate long strings
	if len(input) > MaxLogStringLength {
		input = input[:MaxLogStringLength] + "... (truncated)"
	}

	// Pre-process CRLF to avoid double spaces
	input = strings.ReplaceAll(input, "\r\n", "\n")

	// Replace control characters with spaces
	sanitized := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			// Replace newlines, tabs, and other control chars with space
			return ' '
		}
		return r
	}, input)

	// Replace % with %% to prevent format string issues
	sanitized = strings.ReplaceAll(sanitized, "%", "%%")

	// Remove any remaining potentially problematic sequences
	// This regex removes any character that's not a letter, number, punctuation, symbol or whitespace
	re := regexp.MustCompile(`[^\p{L}\p{N}\p{P}\p{S}\p{Z}]`)
	sanitized = re.ReplaceAllString(sanitized, "")

	return sanitized
}
