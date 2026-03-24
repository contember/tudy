//go:build darwin

package llm_resolver

import (
	"os/exec"
	"strings"

	"go.uber.org/zap"
)

// NetworkTunnel detects docker-mac-net-connect on macOS.
// When running, Docker container IPs are directly reachable
// from the host, so published ports are not required.
// See: https://github.com/chipmk/docker-mac-net-connect
type NetworkTunnel struct {
	logger  *zap.Logger
	running bool
}

// NewNetworkTunnel checks for docker-mac-net-connect on macOS
func NewNetworkTunnel(logger *zap.Logger) *NetworkTunnel {
	return &NetworkTunnel{logger: logger}
}

// Start checks if docker-mac-net-connect is running
func (nt *NetworkTunnel) Start() error {
	nt.running = detectDockerMacNetConnect()
	if nt.running {
		nt.logger.Info("docker-mac-net-connect detected, container IPs are directly reachable")
	} else {
		nt.logger.Debug("docker-mac-net-connect not detected, using published port detection")
	}
	return nil
}

// Stop is a no-op — we don't own the tunnel process
func (nt *NetworkTunnel) Stop() {}

// IsRunning returns true if docker-mac-net-connect is active
func (nt *NetworkTunnel) IsRunning() bool {
	return nt.running
}

// detectDockerMacNetConnect checks if docker-mac-net-connect is running
// by looking for its process and verifying Docker subnet routes exist.
func detectDockerMacNetConnect() bool {
	// Check for the process
	out, err := exec.Command("pgrep", "-f", "docker-mac-net-connect").Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return false
	}

	// Verify Docker subnet routes through utun interface exist
	out, err = exec.Command("netstat", "-rnf", "inet").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		// Look for Docker subnet routes through a utun interface.
		// Docker can use any private range: 172.x, 192.168.x, or 10.x
		if !strings.Contains(line, "utun") {
			continue
		}
		if strings.HasPrefix(line, "172.") || strings.HasPrefix(line, "192.168.") || strings.HasPrefix(line, "10.") {
			return true
		}
	}

	return false
}
