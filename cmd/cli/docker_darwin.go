//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func isDockerMacNetConnectRunning() bool {
	out, err := exec.Command("pgrep", "-f", "docker-mac-net-connect").Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

// ensureDockerHost sets DOCKER_HOST if not already set.
// On macOS, Docker Desktop places the socket in the user's home directory
// rather than /var/run/docker.sock. When running as a launchd daemon (root),
// the Docker CLI context is unavailable, so we probe known socket paths.
func ensureDockerHost() {
	if os.Getenv("DOCKER_HOST") != "" {
		return
	}

	// Try docker context inspect first (works when running as the user)
	if out, err := exec.Command("docker", "context", "inspect", "--format", "{{.Endpoints.docker.Host}}").Output(); err == nil {
		if host := strings.TrimSpace(string(out)); host != "" {
			os.Setenv("DOCKER_HOST", host)
			return
		}
	}

	// Probe known Docker Desktop socket paths on macOS
	knownSockets := []string{
		os.ExpandEnv("$HOME/.docker/run/docker.sock"),
	}
	// When running as root, $HOME is /var/root — also check real user homes
	if entries, err := os.ReadDir("/Users"); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				knownSockets = append(knownSockets, "/Users/"+e.Name()+"/.docker/run/docker.sock")
			}
		}
	}
	for _, sock := range knownSockets {
		if fi, err := os.Stat(sock); err == nil && fi.Mode().Type() == os.ModeSocket {
			os.Setenv("DOCKER_HOST", "unix://"+sock)
			return
		}
	}
}

func setupDockerNetworking() {
	if isDockerMacNetConnectRunning() {
		printOK("Docker direct networking active (docker-mac-net-connect)")
		return
	}

	printDim("  docker-mac-net-connect allows Tudy to access Docker containers")
	printDim("  without publishing ports (-p). Containers become directly reachable.")
	fmt.Println()

	if !promptYesNo("Install docker-mac-net-connect?", true) {
		printDim("  Skipped. Docker containers will need published ports (-p).")
		printDim("  Install later: https://github.com/chipmk/docker-mac-net-connect")
		return
	}

	fmt.Print("  Installing... ")
	cmd := exec.Command("brew", "install", "chipmk/tap/docker-mac-net-connect")
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Println()
		printWarning(fmt.Sprintf("Failed to install: %s", strings.TrimSpace(string(out))))
		printDim("  Install manually: https://github.com/chipmk/docker-mac-net-connect")
		return
	}
	fmt.Println("done")

	fmt.Print("  Starting service... ")
	cmd = exec.Command("sudo", "brew", "services", "start", "chipmk/tap/docker-mac-net-connect")
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Println()
		printWarning(fmt.Sprintf("Failed to start: %s", strings.TrimSpace(string(out))))
		printDim("  Start manually: sudo brew services start chipmk/tap/docker-mac-net-connect")
	} else {
		fmt.Println("done")
		printOK("Docker direct networking active")
	}
}
