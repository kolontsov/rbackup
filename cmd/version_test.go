package cmd

import "testing"

func TestFormatVersion(t *testing.T) {
	tests := []struct {
		name     string
		version  string
		commit   string
		date     string
		expected string
	}{
		{
			name:     "release",
			version:  "v1.0.0",
			commit:   "abc1234",
			date:     "2026-02-28",
			expected: "rbackup v1.0.0 (abc1234, 2026-02-28)\n",
		},
		{
			name:     "dev_dirty",
			version:  "dev",
			commit:   "abc1234-dirty",
			date:     "2026-02-28",
			expected: "rbackup dev (abc1234-dirty, 2026-02-28)\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatVersion(tt.version, tt.commit, tt.date)
			if got != tt.expected {
				t.Errorf("got %q, want %q", got, tt.expected)
			}
		})
	}
}
