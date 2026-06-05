package llm_resolver

import (
	"github.com/contember/tudy/llm_resolver/discovery"
)

// Re-export types from discovery package
type LocalProcess = discovery.LocalProcess
type DockerContainer = discovery.DockerContainer
type PortMapping = discovery.PortMapping

// DiscoverLocalProcesses discovers locally running processes with open ports
func DiscoverLocalProcesses() ([]LocalProcess, error) {
	return discovery.DiscoverLocalProcesses()
}

// DiscoverDockerContainers discovers running Docker containers
func DiscoverDockerContainers(ownComposeProject string) ([]DockerContainer, error) {
	return discovery.DiscoverDockerContainers(ownComposeProject)
}

// GetContainerIP gets the IP address of a container by name or ID
func GetContainerIP(containerIDOrName string) (string, error) {
	return discovery.GetContainerIP(containerIDOrName)
}

// GetContainerHostAddress returns the host-accessible address for a container port
func GetContainerHostAddress(containerIDOrName string, containerPort int) (string, int, bool) {
	return discovery.GetContainerHostAddress(containerIDOrName, containerPort)
}
