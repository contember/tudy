package main

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/contember/tudy/cmd/shared"
)

// llmProvider is one preset for the setup wizard. URL == "" means "ask the
// user to type it" (used for self-hosted gateways / custom OpenAI-compatible
// endpoints).
type llmProvider struct {
	Name    string
	URL     string
	KeyHint string
}

var llmProviders = []llmProvider{
	{Name: "OpenRouter", URL: "https://openrouter.ai/api/v1/chat/completions", KeyHint: "OpenRouter API key"},
	{Name: "OpenAI", URL: "https://api.openai.com/v1/chat/completions", KeyHint: "OpenAI API key"},
	{Name: "Cloudflare AI Gateway", URL: "", KeyHint: "API key for the routed provider"},
	{Name: "Custom (OpenAI-compatible)", URL: "", KeyHint: "API key"},
}

// detectProvider maps an existing LLM_API_URL value back to a preset for
// display purposes. Returns a "Custom" entry if nothing matches.
func detectProvider(url string) llmProvider {
	if url == "" {
		return llmProviders[0]
	}
	for _, p := range llmProviders {
		if p.URL != "" && p.URL == url {
			return p
		}
	}
	switch {
	case strings.Contains(url, "openrouter.ai"):
		return llmProviders[0]
	case strings.Contains(url, "api.openai.com"):
		return llmProviders[1]
	case strings.Contains(url, "gateway.ai.cloudflare.com"):
		return llmProviders[2]
	}
	return llmProvider{Name: "Custom", URL: url, KeyHint: "API key"}
}

// runSetup dispatches `tudy setup` and its sub-targets.
//
//	tudy setup                      → full interactive wizard
//	tudy setup llm-api-url [url]    → change LLM endpoint
//	tudy setup llm-model   [name]   → change LLM model
func runSetup(config *Config, args []string) int {
	if len(args) == 0 {
		return runSetupWizard(config)
	}
	switch args[0] {
	case "llm-api-url":
		return runSetupAPIURL(config, args[1:])
	case "llm-model":
		return runSetupModel(config, args[1:])
	default:
		printError(fmt.Sprintf("Unknown setup target: %s", args[0]))
		fmt.Fprintln(os.Stderr, "Available: llm-api-url, llm-model")
		return 1
	}
}

// runSetupWizard runs the full first-time interactive setup flow.
func runSetupWizard(config *Config) int {
	printHeader("Tudy Setup")

	// Ensure config directory and default files exist
	if err := config.EnsureConfigDir(); err != nil {
		printError(fmt.Sprintf("Failed to initialize config: %v", err))
		return 1
	}

	// Step 1: Configure LLM Provider
	printStep(1, 5, "Configure LLM Provider")

	currentKey := config.GetAPIKey()
	currentURL := config.GetEnvValue("LLM_API_URL")
	currentProvider := detectProvider(currentURL)

	if currentKey != "" {
		printDim(fmt.Sprintf("  Provider: %s", currentProvider.Name))
		printDim(fmt.Sprintf("  Key:      %s", shared.MaskAPIKey(currentKey)))
		if !promptYesNo("Update LLM provider?", false) {
			printOK("Provider unchanged")
		} else {
			if !configureProvider(config) {
				return 1
			}
		}
	} else {
		if !configureProvider(config) {
			return 1
		}
	}
	fmt.Println()

	// Step 2: Docker Networking (macOS only)
	printStep(2, 5, "Docker Networking")
	setupDockerNetworking()
	fmt.Println()

	// Step 3: Install System Service (one-time password prompt)
	printStep(3, 5, "Install System Service")
	if !isDaemonInstalled() {
		printDim("  Installing launchd service (one-time admin password)...")
		if err := installDaemon(config); err != nil {
			printWarning(fmt.Sprintf("Failed to install service: %v", err))
			printDim("  Falling back to direct start (will ask for password each time)")
		} else {
			printOK("System service installed (no password needed for start/stop)")
		}
	} else {
		printOK("System service already installed")
	}
	fmt.Println()

	// Step 4: Start Proxy
	printStep(4, 5, "Start Proxy")

	status := CheckProxyStatus(config)
	if status == StatusRunning {
		printDim("  Restarting proxy...")
		if err := RestartProxy(config); err != nil {
			printError(fmt.Sprintf("Failed to restart proxy: %v", err))
			return 1
		}
		printOK("Proxy restarted")
	} else {
		printDim("  Starting proxy...")
		if err := StartProxy(config); err != nil {
			printError(fmt.Sprintf("Failed to start proxy: %v", err))
			return 1
		}
		printOK("Proxy started")
	}
	fmt.Println()

	// Step 5: Trust HTTPS Certificate
	printStep(5, 5, "Trust HTTPS Certificate")

	if isCertTrustedCheck() {
		printOK("Certificate already trusted")
	} else {
		// Trigger an HTTPS request to generate the certificate
		printDim("  Generating certificate...")
		triggerCertGeneration(config)

		printDim("  Trusting certificate...")
		if err := TrustCertificate(config); err != nil {
			printWarning(fmt.Sprintf("Certificate trust: %v", err))
			printDim("  You can trust it later with: tudy trust")
		} else {
			printOK("Certificate trusted")
		}
	}

	// Print summary
	fmt.Println()
	printHeader("Setup Complete!")
	fmt.Printf("  Dashboard: %s\n", config.DashboardURL)
	fmt.Printf("  Config:    %s\n", config.ConfigDir)
	fmt.Println()
	fmt.Println("Try it out:")
	fmt.Println("  curl https://myapp.localhost")
	fmt.Println()

	return 0
}

// configureProvider walks the user through picking an LLM provider preset
// (or entering a custom OpenAI-compatible endpoint), then prompts for the
// matching API key and writes both to the env file.
//
// The LLM is optional: the last choice skips it entirely. Without a key the
// proxy resolves hostnames heuristically and serves an interactive service
// picker on unresolved hostnames.
func configureProvider(config *Config) bool {
	names := make([]string, 0, len(llmProviders)+1)
	for _, p := range llmProviders {
		names = append(names, p.Name)
	}
	names = append(names, "Skip — run without LLM")
	idx := promptChoice("Pick a provider", names, 0)
	if idx == len(llmProviders) {
		printOK("Running without LLM")
		printDim("  Hostnames are matched heuristically against running services;")
		printDim("  unresolved hostnames show a service picker in the browser.")
		printDim("  Enable LLM routing later with: tudy setup")
		return true
	}
	p := llmProviders[idx]

	url := p.URL
	if url == "" {
		// Cloudflare AI Gateway / custom — let the user paste the endpoint.
		if p.Name == "Cloudflare AI Gateway" {
			printDim("  Example: https://gateway.ai.cloudflare.com/v1/<account>/<gateway>/compat/chat/completions")
		} else {
			printDim("  Must speak the OpenAI chat-completions protocol.")
		}
		url = promptString("Endpoint URL", "")
		if url == "" {
			printError("Endpoint URL is required")
			return false
		}
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			printError("URL must start with http:// or https://")
			return false
		}
	}
	if err := config.SetEnvValue("LLM_API_URL", url); err != nil {
		printError(fmt.Sprintf("Failed to save API URL: %v", err))
		return false
	}

	key := promptString("Enter your "+p.KeyHint, "")
	if key == "" {
		printError("API key is required")
		return false
	}
	if err := config.SetAPIKey(key); err != nil {
		printError(fmt.Sprintf("Failed to save API key: %v", err))
		return false
	}

	printOK("Provider configured")
	return true
}

// runSetupAPIURL is `tudy setup llm-api-url [url]`. With a positional URL it
// writes immediately; without one, it walks the provider preset chooser
// (URL only — key is left alone) and restarts the proxy if running.
func runSetupAPIURL(config *Config, args []string) int {
	if len(args) > 0 {
		return applyAPIURL(config, strings.TrimSpace(args[0]))
	}

	printHeader("Change LLM Endpoint")
	current := config.GetEnvValue("LLM_API_URL")
	if current == "" {
		printDim(fmt.Sprintf("  Current: %s (default)", defaultAPIURL))
	} else {
		printDim(fmt.Sprintf("  Current: %s", current))
	}

	names := make([]string, len(llmProviders))
	for i, p := range llmProviders {
		names[i] = p.Name
	}
	idx := promptChoice("Pick a provider", names, 0)
	p := llmProviders[idx]
	url := p.URL
	if url == "" {
		if p.Name == "Cloudflare AI Gateway" {
			printDim("  Example: https://gateway.ai.cloudflare.com/v1/<account>/<gateway>/compat/chat/completions")
		} else {
			printDim("  Must speak the OpenAI chat-completions protocol.")
		}
		url = promptString("Endpoint URL", "")
	}
	return applyAPIURL(config, url)
}

// runSetupModel is `tudy setup llm-model [name]`. With a positional name it
// writes immediately; without one, prompts (default = current value).
func runSetupModel(config *Config, args []string) int {
	if len(args) > 0 {
		return applyModel(config, strings.TrimSpace(args[0]))
	}

	printHeader("Change LLM Model")
	current := config.GetEnvValue("MODEL")
	if current == "" {
		printDim(fmt.Sprintf("  Current: %s (default)", defaultModel))
	} else {
		printDim(fmt.Sprintf("  Current: %s", current))
	}
	printDim("  Browse model ids at https://openrouter.ai/models")
	name := promptString("Model id", current)
	return applyModel(config, name)
}

// defaults must stay in sync with the resolver defaults in
// llm_resolver/module.go and llm_resolver/resolver.go.
const (
	defaultModel  = "anthropic/claude-haiku-4.5"
	defaultAPIURL = "https://openrouter.ai/api/v1/chat/completions"
)

func applyAPIURL(config *Config, url string) int {
	if url == "" {
		printError("Endpoint URL is required")
		return 1
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		printError("URL must start with http:// or https://")
		return 1
	}
	if config.GetEnvValue("LLM_API_URL") == url {
		fmt.Printf("API URL already set to %s%s%s.\n", colorBold, url, colorReset)
		return 0
	}
	fmt.Printf("Setting API URL to %s%s%s... ", colorBold, url, colorReset)
	if err := config.SetEnvValue("LLM_API_URL", url); err != nil {
		fmt.Println()
		printError(fmt.Sprintf("Failed to update env file: %v", err))
		return 1
	}
	fmt.Println("done")
	return restartIfRunning(config)
}

func applyModel(config *Config, name string) int {
	if name == "" {
		printError("Model id is required")
		return 1
	}
	if config.GetEnvValue("MODEL") == name {
		fmt.Printf("Model already set to %s%s%s.\n", colorBold, name, colorReset)
		return 0
	}
	fmt.Printf("Setting model to %s%s%s... ", colorBold, name, colorReset)
	if err := config.SetEnvValue("MODEL", name); err != nil {
		fmt.Println()
		printError(fmt.Sprintf("Failed to update env file: %v", err))
		return 1
	}
	fmt.Println("done")
	return restartIfRunning(config)
}

// restartIfRunning applies pending env changes to a live proxy by restarting
// it. Caddy only reads MODEL / LLM_API_URL at startup, so a no-op is
// surprising — we either restart, or tell the user how to start manually.
func restartIfRunning(config *Config) int {
	if CheckProxyStatus(config) != StatusRunning {
		printDim("  Proxy is not running. Start it with: tudy start")
		return 0
	}
	fmt.Print("Restarting proxy... ")
	if err := RestartProxy(config); err != nil {
		fmt.Println()
		printError(fmt.Sprintf("Failed to restart proxy: %v", err))
		return 1
	}
	fmt.Println("done")
	return 0
}

// triggerCertGeneration makes an HTTPS request to force Caddy to generate the local CA cert
func triggerCertGeneration(config *Config) {
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	// Request any .localhost domain to trigger on-demand TLS cert generation
	client.Get("https://proxy.localhost/")
	// Poll for cert file to appear (Caddy writes it asynchronously)
	certPath := filepath.Join(config.DataDir(), "pki", "authorities", "local", "root.crt")
	for i := 0; i < 20; i++ {
		time.Sleep(200 * time.Millisecond)
		if _, err := os.Stat(certPath); err == nil {
			return
		}
	}
}
