package main

import (
	"os"
	"strings"
	"syscall"

	"github.com/contember/tudy/cmd/shared"
)

func sourceEnvFile(envFile string) {
	for key, value := range shared.ParseEnvFile(envFile) {
		os.Setenv(key, value)
	}
}

// ensurePath adds common binary directories to PATH.
// This is needed when running as a launchd service where PATH is minimal
// (/usr/bin:/bin:/usr/sbin:/sbin) and tools like docker, lsof, etc. may
// not be found.
func ensurePath() {
	extraPaths := []string{
		"/usr/local/bin",
	}
	current := os.Getenv("PATH")
	existing := make(map[string]bool)
	for _, p := range strings.Split(current, ":") {
		existing[p] = true
	}
	var toAdd []string
	for _, p := range extraPaths {
		if !existing[p] {
			toAdd = append(toAdd, p)
		}
	}
	if len(toAdd) > 0 {
		os.Setenv("PATH", current+":"+strings.Join(toAdd, ":"))
	}
}

// delegateToCaddy replaces the current process with the caddy binary
func delegateToCaddy(config *Config, args []string) error {
	// Source the env file
	sourceEnvFile(config.EnvFile)

	// Ensure PATH includes common binary directories for launchd services
	ensurePath()

	// Set CADDY_DATA_DIR if not already set
	if os.Getenv("CADDY_DATA_DIR") == "" {
		os.Setenv("CADDY_DATA_DIR", config.DataDir())
	}

	// Ensure DOCKER_HOST is set so the docker CLI can find the socket.
	// On macOS, Docker Desktop uses a context-based socket path
	// (e.g. ~/.docker/run/docker.sock) which is not available when
	// running as a launchd daemon under root.
	ensureDockerHost()

	// Build argv: binary path + remaining args
	argv := append([]string{config.BinaryPath}, args...)

	// Replace current process with caddy binary
	return syscall.Exec(config.BinaryPath, argv, os.Environ())
}
