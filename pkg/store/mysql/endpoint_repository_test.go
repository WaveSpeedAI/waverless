package mysql

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsImageRelatedFailure(t *testing.T) {
	tests := []struct {
		name           string
		healthMessage  string
		expectedResult bool
	}{
		{
			name:           "English image keyword lowercase",
			healthMessage:  "image pull failed",
			expectedResult: true,
		},
		{
			name:           "English image keyword uppercase",
			healthMessage:  "IMAGE_PULL_FAILED",
			expectedResult: true,
		},
		{
			name:           "English image keyword capitalized",
			healthMessage:  "Image not found",
			expectedResult: true,
		},
		{
			name:           "Worker startup failed message",
			healthMessage:  "All workers failed to start, please check image configuration",
			expectedResult: true,
		},
		{
			name:           "Unrelated error message",
			healthMessage:  "Network connection timeout",
			expectedResult: false,
		},
		{
			name:           "Empty message",
			healthMessage:  "",
			expectedResult: false,
		},
		{
			name:           "Resource limit error",
			healthMessage:  "Out of memory",
			expectedResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isImageRelatedFailure(tt.healthMessage)
			assert.Equal(t, tt.expectedResult, result)
		})
	}
}

func TestContains(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		substr   string
		expected bool
	}{
		{
			name:     "substring exists",
			s:        "hello world",
			substr:   "world",
			expected: true,
		},
		{
			name:     "substring at start",
			s:        "hello world",
			substr:   "hello",
			expected: true,
		},
		{
			name:     "substring not found",
			s:        "hello world",
			substr:   "foo",
			expected: false,
		},
		{
			name:     "empty substring",
			s:        "hello",
			substr:   "",
			expected: true,
		},
		{
			name:     "empty string",
			s:        "",
			substr:   "hello",
			expected: false,
		},
		{
			name:     "both empty",
			s:        "",
			substr:   "",
			expected: true,
		},
		{
			name:     "exact match",
			s:        "hello",
			substr:   "hello",
			expected: true,
		},
		{
			name:     "image keyword",
			s:        "image pull failed",
			substr:   "image",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := contains(tt.s, tt.substr)
			assert.Equal(t, tt.expected, result)
		})
	}
}
