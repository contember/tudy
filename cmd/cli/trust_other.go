//go:build !darwin

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// TrustCertificate prints manual trust instructions for non-macOS platforms
func TrustCertificate(config *Config) error {
	home, _ := os.UserHomeDir()

	certPaths := []string{
		filepath.Join(home, ".local", "share", "caddy", "pki", "authorities", "local", "root.crt"),
		"/var/lib/tudy/pki/authorities/local/root.crt",
	}

	var certPath string
	for _, p := range certPaths {
		if _, err := os.Stat(p); err == nil {
			certPath = p
			break
		}
	}

	if certPath == "" {
		return fmt.Errorf("certificate not found - start the proxy first and make an HTTPS request")
	}

	fmt.Printf("\nCaddy root CA certificate found at:\n  %s\n\n", certPath)

	switch runtime.GOOS {
	case "linux":
		fmt.Println("To trust this certificate on Linux:")
		fmt.Println("  sudo cp", certPath, "/usr/local/share/ca-certificates/caddy-root-ca.crt")
		fmt.Println("  sudo update-ca-certificates")
		fmt.Println()
		fmt.Println("For browser trust, import the certificate in your browser settings.")
	default:
		fmt.Println("To trust this certificate, import it into your system trust store.")
	}

	return nil
}

// isCertTrustedCheck returns whether the certificate is already trusted
// On non-darwin platforms, we can't easily check, so we return false
func isCertTrustedCheck() bool {
	return false
}

