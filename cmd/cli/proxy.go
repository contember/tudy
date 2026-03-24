package main

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const adminAPI = "http://localhost:2019"

type ProxyStatus int

const (
	StatusStopped ProxyStatus = iota
	StatusRunning
	StatusStarting
	StatusStopping
)

func (s ProxyStatus) String() string {
	switch s {
	case StatusRunning:
		return "Running"
	case StatusStarting:
		return "Starting..."
	case StatusStopping:
		return "Stopping..."
	default:
		return "Stopped"
	}
}

func CheckProxyStatus(config *Config) ProxyStatus {
	if isAdminAPIResponding() {
		return StatusRunning
	}
	if isProcessRunning() {
		return StatusStarting
	}
	return StatusStopped
}

func isAdminAPIResponding() bool {
	client := &http.Client{
		Timeout: 2 * time.Second,
	}
	resp, err := client.Get(adminAPI + "/config/")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func isProcessRunning() bool {
	cmd := exec.Command("pgrep", "-f", "tudy")
	err := cmd.Run()
	return err == nil
}

func StartProxy(config *Config) error {
	home, _ := os.UserHomeDir()
	logDir := filepath.Join(home, "Library", "Logs")
	os.MkdirAll(logDir, 0755)
	logFile := filepath.Join(logDir, "tudy.log")

	shellCmd := fmt.Sprintf(
		"set -a; source '%s'; '%s' run --config '%s' >> '%s' 2>&1 &",
		config.EnvFile,
		config.BinaryPath,
		config.CaddyFile,
		logFile,
	)

	script := fmt.Sprintf(
		`do shell script "%s" with administrator privileges`,
		escapeAppleScript(shellCmd),
	)

	cmd := exec.Command("osascript", "-e", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to start proxy: %s", strings.TrimSpace(string(output)))
	}
	return waitForStart()
}

func waitForStart() error {
	for i := 0; i < 20; i++ {
		time.Sleep(500 * time.Millisecond)
		if isAdminAPIResponding() || isProcessRunning() {
			return nil
		}
	}
	return fmt.Errorf("proxy did not start within timeout")
}

func StopProxy(config *Config) error {
	if isAdminAPIResponding() {
		client := &http.Client{
			Timeout: 10 * time.Second,
		}

		req, err := http.NewRequest("POST", adminAPI+"/stop", nil)
		if err != nil {
			return err
		}

		resp, err := client.Do(req)
		if err != nil {
			return stopViaKill()
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			for i := 0; i < 20; i++ {
				if !isProcessRunning() {
					return nil
				}
				time.Sleep(250 * time.Millisecond)
			}
		}
	}

	return stopViaKill()
}

func stopViaKill() error {
	if isProcessRunning() {
		exec.Command("pkill", "-f", "tudy").Run()
		time.Sleep(500 * time.Millisecond)

		if isProcessRunning() {
			script := `do shell script "pkill -f tudy; exit 0" with administrator privileges`
			exec.Command("osascript", "-e", script).Run()
		}
	}

	for i := 0; i < 10; i++ {
		if !isProcessRunning() {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}

	if isProcessRunning() {
		return fmt.Errorf("proxy did not stop within timeout")
	}
	return nil
}

func RestartProxy(config *Config) error {
	// Try hot reload first (preserves state, faster)
	if isAdminAPIResponding() {
		if reloaded := tryHotReload(config); reloaded {
			return nil
		}
	}

	// Hot reload failed or not possible — full stop+start to pick up env changes
	if err := StopProxy(config); err != nil {
		return fmt.Errorf("failed to stop: %w", err)
	}

	for i := 0; i < 10; i++ {
		if !isProcessRunning() {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	return StartProxy(config)
}

func tryHotReload(config *Config) bool {
	caddyfileContent, err := os.ReadFile(config.CaddyFile)
	if err != nil {
		return false
	}

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("POST", adminAPI+"/load", bytes.NewReader(caddyfileContent))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "text/caddyfile")

	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func escapeAppleScript(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return s
}

func getLogFile() string {
	home, _ := os.UserHomeDir()
	locations := []string{
		filepath.Join(home, "Library", "Logs", "tudy.log"),
		"/var/log/tudy.log",
	}
	for _, loc := range locations {
		if _, err := os.Stat(loc); err == nil {
			return loc
		}
	}
	return filepath.Join(home, "Library", "Logs", "tudy.log")
}
