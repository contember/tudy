//go:build !darwin

package main

// ensureDockerHost is a no-op on non-macOS platforms.
// On Linux, the Docker socket is at the standard /var/run/docker.sock.
func ensureDockerHost() {}

// setupDockerNetworking is a no-op on non-macOS platforms.
// On Linux, Docker container IPs are directly accessible.
func setupDockerNetworking() {}
