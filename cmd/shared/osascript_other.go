//go:build !darwin

package shared

import (
	"os"
)

// CopyFileWithAdmin copies a file directly on non-macOS platforms.
func CopyFileWithAdmin(src, dst string) error {
	input, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, input, 0644)
}
