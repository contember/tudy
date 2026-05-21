package main

import (
	"fmt"
	"os"
	"os/exec"
)

// Version is set at build time via -ldflags
var Version = "dev"

const usage = `Tudy - AI-powered local development proxy

Usage:
  tudy <command> [args...]

Commands:
  setup       Interactive first-time setup (API key, certificate, Docker, start)
  status      Show proxy status
  start       Start the proxy
  stop        Stop the proxy
  restart     Restart the proxy
  trust       Trust the HTTPS certificate
  update      Update tudy to the latest version
  uninstall   Fully remove tudy from the system
  logs        Tail the proxy log file

All other commands (run, version, etc.) are passed through to Caddy.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(0)
	}

	command := os.Args[1]

	switch command {
	case "help", "--help", "-h":
		fmt.Print(usage)
		os.Exit(0)

	case "version", "--version", "-v":
		fmt.Printf("tudy v%s\n", Version)
		os.Exit(0)

	case "setup":
		config, err := LoadConfig()
		if err != nil {
			printError(fmt.Sprintf("Failed to load configuration: %v", err))
			os.Exit(1)
		}
		os.Exit(runSetup(config))

	case "status":
		config, err := LoadConfig()
		if err != nil {
			printError(fmt.Sprintf("Failed to load configuration: %v", err))
			os.Exit(1)
		}
		printRichStatus(config)

	case "start":
		config, err := LoadConfig()
		if err != nil {
			printError(fmt.Sprintf("Failed to load configuration: %v", err))
			os.Exit(1)
		}
		status := CheckProxyStatus(config)
		if status == StatusRunning {
			printRichStatus(config, status)
			os.Exit(0)
		}
		fmt.Print("Starting proxy... ")
		if err := StartProxy(config); err != nil {
			fmt.Println()
			printError(fmt.Sprintf("Failed to start proxy: %v", err))
			os.Exit(1)
		}
		fmt.Println("done")
		fmt.Println()
		printRichStatus(config)

	case "stop":
		config, err := LoadConfig()
		if err != nil {
			printError(fmt.Sprintf("Failed to load configuration: %v", err))
			os.Exit(1)
		}
		status := CheckProxyStatus(config)
		if status == StatusStopped {
			fmt.Println("Proxy is already stopped.")
			os.Exit(0)
		}
		fmt.Print("Stopping proxy... ")
		if err := StopProxy(config); err != nil {
			fmt.Println()
			printError(fmt.Sprintf("Failed to stop proxy: %v", err))
			os.Exit(1)
		}
		fmt.Println("done")

	case "restart":
		config, err := LoadConfig()
		if err != nil {
			printError(fmt.Sprintf("Failed to load configuration: %v", err))
			os.Exit(1)
		}
		fmt.Print("Restarting proxy... ")
		if err := RestartProxy(config); err != nil {
			fmt.Println()
			printError(fmt.Sprintf("Failed to restart proxy: %v", err))
			os.Exit(1)
		}
		fmt.Println("done")

	case "trust":
		config, err := LoadConfig()
		if err != nil {
			printError(fmt.Sprintf("Failed to load configuration: %v", err))
			os.Exit(1)
		}
		fmt.Print("Trusting certificate... ")
		if err := TrustCertificate(config); err != nil {
			fmt.Println()
			printWarning(err.Error())
			os.Exit(1)
		}
		fmt.Println("done")

	case "update":
		runUpdate()

	case "uninstall":
		runUninstall()

	case "logs":
		logFile := getLogFile()
		if _, err := os.Stat(logFile); os.IsNotExist(err) {
			printError("No log file found. Start the proxy first to generate logs.")
			os.Exit(1)
		}
		fmt.Printf("Tailing %s (Ctrl+C to stop)\n\n", logFile)
		cmd := exec.Command("tail", "-f", logFile)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			// tail -f is normally interrupted by Ctrl+C, which is expected
			if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 130 {
				os.Exit(0)
			}
		}

	default:
		// Delegate everything else to the caddy binary
		config, err := LoadConfig()
		if err != nil {
			printError(fmt.Sprintf("Failed to load configuration: %v", err))
			os.Exit(1)
		}
		if err := delegateToCaddy(config, os.Args[1:]); err != nil {
			printError(fmt.Sprintf("Failed to exec caddy: %v", err))
			os.Exit(1)
		}
	}
}
