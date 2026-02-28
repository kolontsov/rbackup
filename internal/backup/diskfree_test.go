package backup

import "testing"

func TestDiskFreeBytesReturnsNonZero(t *testing.T) {
	avail, err := DiskFreeBytes(t.TempDir())
	if err != nil {
		t.Fatalf("DiskFreeBytes: %v", err)
	}
	if avail == 0 {
		t.Fatal("expected non-zero available bytes")
	}
}

func TestDiskFreeBytesInvalidPath(t *testing.T) {
	_, err := DiskFreeBytes("/nonexistent-path-that-should-not-exist")
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
}
