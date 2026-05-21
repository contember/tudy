package shared

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// ParseEnvFile reads a KEY=VALUE env file and returns all entries as a map.
func ParseEnvFile(envFile string) map[string]string {
	file, err := os.Open(envFile)
	if err != nil {
		return nil
	}
	defer file.Close()

	result := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			value = strings.Trim(value, "\"'")
			result[key] = value
		}
	}
	return result
}

// GetEnvValue reads a specific value from a KEY=VALUE env file.
func GetEnvValue(envFile, key string) string {
	return ParseEnvFile(envFile)[key]
}

// SetEnvValue updates or adds a KEY=VALUE entry in an env file.
// writeFunc is called with the temp file path and target path to perform the final write.
// Pass nil to use a simple os.Rename.
func SetEnvValue(envFile, key, value string, writeFunc func(src, dst string) error) error {
	content, err := os.ReadFile(envFile)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read env file: %w", err)
	}

	lines := strings.Split(string(content), "\n")
	found := false
	newLines := make([]string, 0, len(lines))

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			newLines = append(newLines, line)
			continue
		}
		parts := strings.SplitN(trimmed, "=", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) == key {
			newLines = append(newLines, fmt.Sprintf("%s=%s", key, value))
			found = true
		} else {
			newLines = append(newLines, line)
		}
	}

	if !found {
		newLines = append(newLines, fmt.Sprintf("%s=%s", key, value))
	}

	tempFile, err := os.CreateTemp("", "env-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)

	if _, err := tempFile.WriteString(strings.Join(newLines, "\n")); err != nil {
		tempFile.Close()
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	tempFile.Close()

	if writeFunc != nil {
		return writeFunc(tempPath, envFile)
	}
	return os.Rename(tempPath, envFile)
}
