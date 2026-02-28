package cmd

import (
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input string
		want  time.Duration
	}{
		{"25h", 25 * time.Hour},
		{"30m", 30 * time.Minute},
		{"3d", 72 * time.Hour},
		{"1d12h", 36 * time.Hour},
		{"7d", 168 * time.Hour},
		{"0d", 0},
		{"500ms", 500 * time.Millisecond},
	}
	for _, tt := range tests {
		got, err := parseDuration(tt.input)
		if err != nil {
			t.Errorf("parseDuration(%q) error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("parseDuration(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestParseDurationInvalid(t *testing.T) {
	for _, input := range []string{"", "abc", "3x", "1d-bogus"} {
		_, err := parseDuration(input)
		if err == nil {
			t.Errorf("parseDuration(%q) should fail", input)
		}
	}
}

func TestFormatSize(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{4521984, "4.3 MB"},
		{1073741824, "1.0 GB"},
	}
	for _, tt := range tests {
		got := formatSize(tt.bytes)
		if got != tt.want {
			t.Errorf("formatSize(%d) = %q, want %q", tt.bytes, got, tt.want)
		}
	}
}

func TestFormatAge(t *testing.T) {
	now := time.Now()

	tests := []struct {
		t    time.Time
		want string
	}{
		{time.Time{}, "unknown"},
		{now.Add(-30 * time.Second), "30s ago"},
		{now.Add(-45 * time.Minute), "45m ago"},
		{now.Add(-12 * time.Hour), "12h ago"},
		{now.Add(-72 * time.Hour), "3d ago"},
	}
	for _, tt := range tests {
		got := formatAge(tt.t)
		if got != tt.want {
			t.Errorf("formatAge(%v) = %q, want %q", tt.t, got, tt.want)
		}
	}
}

func TestTruncateVersion(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"", ""},
		{"abc", "abc"},
		{"12345678", "12345678"},
		{"abcdef1234567890", "...34567890"},
	}
	for _, tt := range tests {
		got := truncateVersion(tt.input)
		if got != tt.want {
			t.Errorf("truncateVersion(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
