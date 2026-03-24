//go:build darwin

package main

import (
	"os/exec"
	"strings"
)

func isDockerMacNetConnectRunning() bool {
	out, err := exec.Command("pgrep", "-f", "docker-mac-net-connect").Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

func setupDockerNetworking() {
	if isDockerMacNetConnectRunning() {
		printOK("Docker direct networking active (docker-mac-net-connect)")
		return
	}

	printDim("  docker-mac-net-connect allows Tudy to access Docker containers")
	printDim("  without publishing ports (-p). Containers become directly reachable.")
	printDim("  https://github.com/chipmk/docker-mac-net-connect")
	printDim("")
	printDim("  Skipped. Docker containers will need published ports (-p).")
}
