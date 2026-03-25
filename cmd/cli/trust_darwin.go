//go:build darwin

package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/contember/tudy/cmd/shared"
)

// TrustCertificate installs Caddy's root CA certificate to the system trust store
func TrustCertificate(config *Config) error {
	home, _ := os.UserHomeDir()

	// Try multiple possible certificate locations
	certPaths := []string{
		filepath.Join(config.DataDir(), "pki", "authorities", "local", "root.crt"),
		"/usr/local/var/lib/tudy/pki/authorities/local/root.crt",
		filepath.Join(home, "Library", "Application Support", "Caddy", "pki", "authorities", "local", "root.crt"),
		"/var/lib/tudy/pki/authorities/local/root.crt",
	}

	loginKeychain := filepath.Join(home, "Library", "Keychains", "login.keychain-db")
	tempCert := filepath.Join(os.TempDir(), "caddy-root-ca.crt")

	// Try to copy certificate from each path (may need admin privileges for root-owned files)
	var copyErr error
	for _, certPath := range certPaths {
		if err := shared.CopyFileWithAdmin(certPath, tempCert); err == nil {
			copyErr = nil
			break
		}
		copyErr = fmt.Errorf("certificate not found at %s", certPath)
	}

	if copyErr != nil {
		return fmt.Errorf("certificate not found - start the proxy first and make an HTTPS request")
	}

	// Clean up temp file when done
	defer os.Remove(tempCert)

	// Import to login keychain
	if output, err := exec.Command("security", "import", tempCert, "-k", loginKeychain).CombinedOutput(); err != nil {
		if !strings.Contains(string(output), "already exists") {
			fmt.Fprintf(os.Stderr, "warning: certificate import failed: %s\n", strings.TrimSpace(string(output)))
		}
	}

	if isCertTrustedCheck() {
		return nil
	}

	// Try to add as trusted root certificate with SSL policy
	if output, err := exec.Command("security", "add-trusted-cert", "-r", "trustRoot", "-p", "ssl", "-k", loginKeychain, tempCert).CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: add-trusted-cert failed: %s\n", strings.TrimSpace(string(output)))
	}

	if isCertTrustedCheck() {
		return nil
	}

	// Trust settings not properly set - open certificate for manual trust via macOS UI
	exec.Command("open", tempCert).Run()
	time.Sleep(2 * time.Second)
	return fmt.Errorf("certificate opened in Keychain Access - set 'Always Trust' for SSL, then restart your browser")
}

// isCertTrustedCheck tests if the proxy's TLS certificate is actually trusted
// by making a real HTTPS connection. This is more reliable than parsing keychain
// output, which can have stale certs from previous installations.
func isCertTrustedCheck() bool {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("https://proxy.localhost/_tls_check")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return true
}


