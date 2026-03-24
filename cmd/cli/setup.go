package main

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/contember/tudy/cmd/shared"
)

// runSetup runs the interactive setup flow
func runSetup(config *Config) int {
	printHeader("Tudy Setup")

	// Ensure config directory and default files exist
	if err := config.EnsureConfigDir(); err != nil {
		printError(fmt.Sprintf("Failed to initialize config: %v", err))
		return 1
	}

	// Step 1: Configure API Key
	printStep(1, 4, "Configure API Key")

	currentKey := config.GetAPIKey()
	if currentKey != "" {
		printDim(fmt.Sprintf("  Current key: %s", shared.MaskAPIKey(currentKey)))
		if !promptYesNo("Update API key?", false) {
			printOK("API key unchanged")
		} else {
			if !configureAPIKey(config) {
				return 1
			}
		}
	} else {
		if !configureAPIKey(config) {
			return 1
		}
	}
	fmt.Println()

	// Step 2: Docker Networking (macOS only)
	printStep(2, 4, "Docker Networking")
	setupDockerNetworking()
	fmt.Println()

	// Step 3: Start Proxy
	printStep(3, 4, "Start Proxy")

	status := CheckProxyStatus(config)
	if status == StatusRunning {
		printDim("  Restarting proxy...")
		if err := RestartProxy(config); err != nil {
			printError(fmt.Sprintf("Failed to restart proxy: %v", err))
			return 1
		}
		printOK("Proxy restarted")
	} else {
		printDim("  Starting proxy...")
		if err := StartProxy(config); err != nil {
			printError(fmt.Sprintf("Failed to start proxy: %v", err))
			return 1
		}
		printOK("Proxy started")
	}
	fmt.Println()

	// Step 4: Trust HTTPS Certificate
	printStep(4, 4, "Trust HTTPS Certificate")

	if isCertTrustedCheck() {
		printOK("Certificate already trusted")
	} else {
		// Trigger an HTTPS request to generate the certificate
		printDim("  Generating certificate...")
		triggerCertGeneration(config)

		printDim("  Trusting certificate...")
		if err := TrustCertificate(config); err != nil {
			printWarning(fmt.Sprintf("Certificate trust: %v", err))
			printDim("  You can trust it later with: tudy trust")
		} else {
			printOK("Certificate trusted")
		}
	}

	// Print summary
	fmt.Println()
	printHeader("Setup Complete!")
	fmt.Printf("  Dashboard: %s\n", config.DashboardURL)
	fmt.Printf("  Config:    %s\n", config.ConfigDir)
	fmt.Println()
	fmt.Println("Try it out:")
	fmt.Println("  curl https://myapp.localhost")
	fmt.Println()

	return 0
}

// configureAPIKey prompts for and saves an API key
func configureAPIKey(config *Config) bool {
	key := promptString("Enter your OpenRouter API key", "")
	if key == "" {
		printError("API key is required")
		return false
	}

	if err := config.SetAPIKey(key); err != nil {
		printError(fmt.Sprintf("Failed to save API key: %v", err))
		return false
	}

	printOK("API key saved")
	return true
}

// triggerCertGeneration makes an HTTPS request to force Caddy to generate the local CA cert
func triggerCertGeneration(config *Config) {
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	// Request any .localhost domain to trigger on-demand TLS cert generation
	client.Get("https://proxy.localhost/")
	// Poll for cert file to appear (Caddy writes it asynchronously)
	certPath := filepath.Join(config.DataDir(), "pki", "authorities", "local", "root.crt")
	for i := 0; i < 20; i++ {
		time.Sleep(200 * time.Millisecond)
		if _, err := os.Stat(certPath); err == nil {
			return
		}
	}
}
