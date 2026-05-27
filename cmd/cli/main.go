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
  setup       Interactive first-time setup (provider, API key, Docker, TLS, start)
  status      Show proxy status with service discovery
  start       Start the proxy
  stop        Stop the proxy
  restart     Restart the proxy
  doctor      Diagnose configuration, LLM credentials, and proxy health
  trust       Trust the HTTPS certificate
  logs        Tail the proxy log file
  update      Update tudy to the latest version
  uninstall   Fully remove tudy from the system
  caddy       Pass-through to the underlying Caddy binary (run, list-modules, ...)

Setup subcommands:
  setup                          Run the full interactive setup wizard
  setup llm-api-url [url]        Change the LLM endpoint (provider chooser if no url)
  setup llm-model   [name]       Change the LLM model (prompts if no name)
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
		os.Exit(runSetup(config, os.Args[2:]))

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

	case "doctor":
		config, err := LoadConfig()
		if err != nil {
			printError(fmt.Sprintf("Failed to load configuration: %v", err))
			os.Exit(1)
		}
		os.Exit(runDoctor(config))

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

	case "caddy":
		// Pass-through to the underlying Caddy binary. The env file is sourced
		// first so {$LLM_API_KEY} et al. resolve when Caddy reads the Caddyfile.
		config, err := LoadConfig()
		if err != nil {
			printError(fmt.Sprintf("Failed to load configuration: %v", err))
			os.Exit(1)
		}
		caddyArgs := os.Args[2:]
		if len(caddyArgs) == 0 {
			caddyArgs = []string{"help"}
		}
		if err := delegateToCaddy(config, caddyArgs); err != nil {
			printError(fmt.Sprintf("Failed to exec caddy: %v", err))
			os.Exit(1)
		}

	default:
		// Backward-compatible fallback: anything we don't recognize is delegated
		// to the Caddy binary. The canonical form is 'tudy caddy <cmd>' (since
		// v0.8.1); keep this silent fallback so older launchd plists / brew
		// service definitions / third-party scripts using 'tudy run' keep
		// working without intervention.
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
