//go:build !darwin

package discovery

// ensureDockerHost is a no-op on Linux where /var/run/docker.sock is standard.
func ensureDockerHost() {}
