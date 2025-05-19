package utils

import (
	"testing"
)

func TestSanitizeLogString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "Normal string",
			input:    "Regular meeting",
			expected: "Regular meeting",
		},
		{
			name:     "String with format specifiers",
			input:    "Meeting with %s and %d",
			expected: "Meeting with %%s and %%d",
		},
		{
			name:     "String with newlines",
			input:    "First line\nSecond line\r\nThird line",
			expected: "First line Second line Third line",
		},
		{
			name:     "Long string truncation",
			input:    createLongString(300),
			expected: createLongString(MaxLogStringLength) + "... (truncated)",
		},
		{
			name:     "String with control characters",
			input:    "Meeting\twith\x00control\x1Fcharacters",
			expected: "Meeting with control characters",
		},
		{
			name:     "String with script tags",
			input:    "Meeting <script>alert('hacked!');</script>",
			expected: "Meeting <script>alert('hacked!');</script>",
		},
		{
			name:     "String with multiple format specifiers",
			input:    "ID=%d%s Type=%v%%",
			expected: "ID=%%d%%s Type=%%v%%%%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeLogString(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeLogString(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

// Helper function to create a string of the specified length
func createLongString(length int) string {
	result := make([]byte, length)
	for i := 0; i < length; i++ {
		result[i] = 'A'
	}
	return string(result)
}
