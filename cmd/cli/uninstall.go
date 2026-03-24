package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func runUninstall() {
	printHeader("Uninstall Tudy")

	fmt.Println("This will remove:")
	fmt.Println("  - Tudy binaries (tudy, tudy-bin)")
	fmt.Println("  - Configuration files (Caddyfile, env)")
	fmt.Println("  - TLS certificates and Caddy data")
	fmt.Println("  - Log files")
	fmt.Println()

	if !promptYesNo("Are you sure you want to uninstall Tudy?", false) {
		fmt.Println("Cancelled.")
		return
	}
	fmt.Println()

	// Step 1: Stop the proxy
	fmt.Print("Stopping proxy... ")
	if isProcessRunning() {
		config, err := LoadConfig()
		if err == nil {
			StopProxy(config)
		} else {
			exec.Command("pkill", "-f", "tudy-bin").Run()
		}
	}
	fmt.Println("done")

	// Step 2: Remove certificates from Keychain
	uninstallCertificates()

	// Step 3: Remove binaries
	fmt.Print("Removing binaries... ")
	binaries := []string{
		"/usr/local/bin/tudy",
		"/usr/local/bin/tudy-bin",
	}
	removePaths(binaries)
	fmt.Println("done")

	// Step 4: Remove config
	fmt.Print("Removing configuration... ")
	configDirs := []string{
		"/usr/local/etc/tudy",
	}
	removePaths(configDirs)
	fmt.Println("done")

	// Step 5: Remove data directories (Caddy data, certs, etc.)
	fmt.Print("Removing data and certificates... ")
	home, _ := os.UserHomeDir()
	dataDirs := []string{
		"/usr/local/var/lib/tudy",
		"/var/lib/tudy",
		filepath.Join(home, "Library", "Application Support", "Caddy"),
	}
	removePaths(dataDirs)
	fmt.Println("done")

	// Step 6: Remove logs
	fmt.Print("Removing logs... ")
	logFiles := []string{
		filepath.Join(home, "Library", "Logs", "tudy.log"),
		"/var/log/tudy.log",
	}
	removePaths(logFiles)
	fmt.Println("done")

	fmt.Println()
	printOK("Tudy has been fully removed from your system.")
}

func removePaths(paths []string) {
	for _, p := range paths {
		if err := os.RemoveAll(p); err == nil {
			continue
		}
		exec.Command("sudo", "rm", "-rf", p).Run()
	}
}

func uninstallCertificates() {
	fmt.Print("Removing certificates from trust store... ")
	removeCertFromTrustStore()
	fmt.Println("done")
}
