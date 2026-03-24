package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"time"
)

const (
	githubRepo     = "contember/tudy"
	installScriptURL = "https://raw.githubusercontent.com/contember/tudy/main/install.sh"
)

// runUpdate checks for a new version and updates tudy
func runUpdate() {
	fmt.Printf("%study%s %sv%s%s\n", colorBold, colorReset, colorDim, Version, colorReset)
	fmt.Println()

	fmt.Print("Checking for updates... ")
	latest, err := getLatestVersion()
	if err != nil {
		fmt.Println()
		printError(fmt.Sprintf("Failed to check for updates: %v", err))
		os.Exit(1)
	}

	if latest == Version {
		fmt.Println("already up to date.")
		return
	}

	fmt.Printf("v%s available\n", latest)
	fmt.Println()
	fmt.Printf("Updating tudy v%s → v%s...\n", Version, latest)
	fmt.Println()

	// Run the install script via curl | bash
	cmd := exec.Command("bash", "-c", fmt.Sprintf("curl -fsSL %s | bash", installScriptURL))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), fmt.Sprintf("VERSION=%s", latest))

	if err := cmd.Run(); err != nil {
		printError(fmt.Sprintf("Update failed: %v", err))
		os.Exit(1)
	}
}

// getLatestVersion fetches the latest release version from GitHub
func getLatestVersion() (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", githubRepo))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}

	// Strip leading "v" from tag
	version := release.TagName
	if len(version) > 0 && version[0] == 'v' {
		version = version[1:]
	}

	return version, nil
}
