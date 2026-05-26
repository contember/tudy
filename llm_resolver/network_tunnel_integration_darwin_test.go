//go:build darwin

package llm_resolver

import (
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"go.uber.org/zap"
)

// TestIntegration_RealProbeAgainstLocalListener spins up a real TCP
// listener and verifies probeTCP can reach it. Sanity check on the
// dial+timeout path. Always runs.
func TestIntegration_RealProbeAgainstLocalListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	addr := ln.Addr().(*net.TCPAddr)

	if !probeTCP("127.0.0.1", addr.Port) {
		t.Fatal("expected local listener to be reachable")
	}
}

// TestIntegration_RealProbeAgainstClosedPort confirms the probe
// returns false for a port nothing is listening on.
func TestIntegration_RealProbeAgainstClosedPort(t *testing.T) {
	// Pick a port by listening then closing — high chance nothing else grabs it.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	if probeTCP("127.0.0.1", port) {
		t.Fatalf("expected closed port %d to be unreachable", port)
	}
}

// TestIntegration_AgainstBrokenTarget verifies the probe correctly
// reports a known-unreachable Docker container IP (claimed by a VPN
// on the test machine). Gated by env var because it depends on the
// developer's local network config.
//
//	TUDY_INTEGRATION_BROKEN_TARGET=172.18.0.9:1480 go test -run TestIntegration_AgainstBrokenTarget ./...
func TestIntegration_AgainstBrokenTarget(t *testing.T) {
	target := os.Getenv("TUDY_INTEGRATION_BROKEN_TARGET")
	if target == "" {
		t.Skip("set TUDY_INTEGRATION_BROKEN_TARGET=ip:port to run")
	}
	ip, portStr, err := net.SplitHostPort(target)
	if err != nil {
		t.Fatalf("bad target %q: %v", target, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 {
		t.Fatalf("bad port %q: %v", portStr, err)
	}

	nt := NewNetworkTunnel(zap.NewNop())
	if nt.IsReachable(ip, port) {
		t.Fatalf("expected %s to be unreachable (was probe successful?)", target)
	}
}

// TestIntegration_DmncDetectionMatchesRealProcess verifies that
// detectDockerMacNetConnect agrees with `pgrep`. This catches
// regressions where we add bogus auxiliary checks.
func TestIntegration_DmncDetectionMatchesRealProcess(t *testing.T) {
	pgrepOut, err := exec.Command("pgrep", "docker-mac-net-connect").Output()
	processRunning := err == nil && strings.TrimSpace(string(pgrepOut)) != ""

	detected := detectDockerMacNetConnect()
	if detected != processRunning {
		t.Fatalf("detection mismatch: detect=%v pgrep=%v", detected, processRunning)
	}
}
