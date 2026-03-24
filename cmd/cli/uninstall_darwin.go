//go:build darwin

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// removeCertFromTrustStore removes Caddy root CA certificates from macOS Keychain
func removeCertFromTrustStore() {
	home, _ := os.UserHomeDir()
	loginKeychain := filepath.Join(home, "Library", "Keychains", "login.keychain-db")

	// Find and delete all Caddy Local Authority certificates from login keychain
	out, err := exec.Command("security", "find-certificate", "-c", "Caddy Local Authority", "-a", "-Z", loginKeychain).Output()
	if err != nil {
		return
	}

	// Extract SHA-1 hashes and delete each certificate
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "SHA-1 hash:") {
			hash := strings.TrimSpace(strings.TrimPrefix(line, "SHA-1 hash:"))
			if hash != "" {
				exec.Command("security", "delete-certificate", "-Z", hash, loginKeychain).Run()
			}
		}
	}

	// Also remove from system keychain trust settings
	exec.Command("sudo", "security", "remove-trusted-cert", "-d",
		"/Library/Keychains/System.keychain").Run()
}
