package verify

import (
	"strings"
	"testing"
)

func TestValidateKey(t *testing.T) {
	tests := []struct {
		input   string
		wantKey string
		wantErr string
	}{
		{"host01/pocket-id.db.age", "host01/pocket-id.db.age", ""},
		{"  host01/pocket-id.db.age  ", "host01/pocket-id.db.age", ""},
		{"", "", "must not be empty"},
		{"/leading/slash", "", "must not start with '/'"},
		{"foo//bar", "", "must not contain empty path components"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			key, err := ValidateKey(tt.input)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if key != tt.wantKey {
				t.Errorf("key = %q, want %q", key, tt.wantKey)
			}
		})
	}
}

func TestIsSealed(t *testing.T) {
	if !IsSealed("host01/backup.1pux.age2") {
		t.Error("expected .age2 to be sealed")
	}
	if IsSealed("host01/backup.db.age") {
		t.Error("expected .age to not be sealed")
	}
}
