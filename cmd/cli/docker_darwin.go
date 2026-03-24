//go:build darwin

package main

import (
	"fmt"
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
