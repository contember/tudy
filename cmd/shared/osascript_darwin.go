//go:build darwin

package shared

import (
	"fmt"
	"os"
	"os/exec"
)

// CopyFileWithAdmin copies a file, using admin privileges if direct copy fails.
func CopyFileWithAdmin(src, dst string) error {
	// Try direct copy first
	if input, err := os.ReadFile(src); err == nil {
		if err := os.WriteFile(dst, input, 0644); err == nil {
			return nil
		}
	}
	// Fall back to admin copy via osascript
	cmd := exec.Command("osascript",
		"-e", `on run argv`,
		"-e", `do shell script "cp " & quoted form of item 1 of argv & " " & quoted form of item 2 of argv with administrator privileges`,
		"-e", `end run`,
		"--", src, dst)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to copy file: %s: %w", string(output), err)
	}
	return nil
}
