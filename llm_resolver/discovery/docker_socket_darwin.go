//go:build darwin

package discovery

import (
	"os"
	"strings"
	"sync"
)

var dockerHostOnce sync.Once

// ensureDockerHost sets DOCKER_HOST if not already set by probing known
// Docker Desktop socket paths. On macOS, Docker Desktop places the socket
// in the user's home directory (~/.docker/run/docker.sock) rather than
// /var/run/docker.sock. When running as root (e.g. launchd daemon), the
// Docker CLI context is unavailable, so we detect the socket directly.
func ensureDockerHost() {
	dockerHostOnce.Do(func() {
		// If DOCKER_HOST is set, verify the socket actually exists
		if host := os.Getenv("DOCKER_HOST"); host != "" {
			if path, ok := strings.CutPrefix(host, "unix://"); ok && !isSocket(path) {
				// Socket doesn't exist — clear it so we can detect the real one
				os.Unsetenv("DOCKER_HOST")
			} else {
				return
			}
		}

		// Check the default socket path first
		if isSocket("/var/run/docker.sock") {
			return
		}

		// Scan user home directories for Docker Desktop socket
		entries, err := os.ReadDir("/Users")
		if err != nil {
			return
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			sock := "/Users/" + e.Name() + "/.docker/run/docker.sock"
			if isSocket(sock) {
				os.Setenv("DOCKER_HOST", "unix://"+sock)
				return
			}
		}
	})
}

func isSocket(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().Type() == os.ModeSocket
}
