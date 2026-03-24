//go:build !darwin

package main

// setupDockerNetworking is a no-op on non-macOS platforms.
// On Linux, Docker container IPs are directly accessible.
func setupDockerNetworking() {}
