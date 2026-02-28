package fsutil

import (
	"fmt"
	"os"
)

// ValidateSecureFile checks that path is a regular file (not symlink)
// with no group/other permissions (mode & 0077 == 0).
func ValidateSecureFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("secure file check: %w", err)
	}

	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("secure file check: %s is a symlink", path)
	}

	if !info.Mode().IsRegular() {
		return fmt.Errorf("secure file check: %s is not a regular file", path)
	}

	if info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("secure file check: %s has insecure permissions %04o (group/other access not allowed)", path, info.Mode().Perm())
	}

	return nil
}

// ReadSecureFile reads a file after validating its permissions.
func ReadSecureFile(path string) ([]byte, error) {
	if err := ValidateSecureFile(path); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}
