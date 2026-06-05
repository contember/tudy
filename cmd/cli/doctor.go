package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/contember/tudy/cmd/shared"
)

// runDoctor diagnoses a tudy install: config files, LLM credentials,
// endpoint reachability, proxy status, TLS trust, and Docker networking.
// Exits 1 if any check fails so it's usable in scripts.
func runDoctor(config *Config) int {
	printHeader("tudy doctor")

	failed := 0
	failed += runSection("Configuration", []check{
		checkConfigDir(config),
		checkCaddyfile(config),
		checkEnvFile(config),
		checkCaddyBinary(config),
	})
	fmt.Println()
	llmFailed := runSection("LLM provider", llmChecks(config))
	failed += llmFailed
	fmt.Println()
	failed += runSection("Proxy", proxyChecks(config))

	if runtime.GOOS == "darwin" {
		fmt.Println()
		failed += runSection("Docker (macOS)", dockerChecks())
	}

	fmt.Println()
	if failed == 0 {
		printOK("All checks passed")
		return 0
	}
	fmt.Printf("%s%d check(s) failed.%s\n", colorRed, failed, colorReset)
	return 1
}

// ----- presentation -----

type checkStatus int

const (
	checkPass checkStatus = iota
	checkFail
	checkWarn
	checkSkip
)

type check struct {
	status checkStatus
	name   string
	detail string // current value / status text
	hint   string // optional fix suggestion (shown on next line, indented)
}

func runSection(title string, checks []check) int {
	fmt.Printf("  %s%s%s\n", colorBold, title, colorReset)
	failed := 0
	for _, c := range checks {
		printCheck(c)
		if c.status == checkFail {
			failed++
		}
	}
	return failed
}

func printCheck(c check) {
	var icon string
	switch c.status {
	case checkPass:
		icon = fmt.Sprintf("%s✓%s", colorGreen, colorReset)
	case checkFail:
		icon = fmt.Sprintf("%s✗%s", colorRed, colorReset)
	case checkWarn:
		icon = fmt.Sprintf("%s⚠%s", colorYellow, colorReset)
	case checkSkip:
		icon = fmt.Sprintf("%s○%s", colorDim, colorReset)
	}
	name := padRight(c.name, 22)
	if c.detail == "" {
		fmt.Printf("    %s %s\n", icon, name)
	} else {
		fmt.Printf("    %s %s  %s%s%s\n", icon, name, colorDim, c.detail, colorReset)
	}
	if c.hint != "" {
		fmt.Printf("        %s→ %s%s\n", colorDim, c.hint, colorReset)
	}
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

// ----- Configuration checks -----

func checkConfigDir(config *Config) check {
	if _, err := os.Stat(config.ConfigDir); err != nil {
		return check{checkFail, "Config directory", config.ConfigDir, "Run: tudy setup"}
	}
	return check{checkPass, "Config directory", config.ConfigDir, ""}
}

func checkCaddyfile(config *Config) check {
	if _, err := os.Stat(config.CaddyFile); err != nil {
		return check{checkFail, "Caddyfile", config.CaddyFile, "Reinstall tudy (Caddyfile missing)"}
	}
	return check{checkPass, "Caddyfile", config.CaddyFile, ""}
}

func checkEnvFile(config *Config) check {
	if _, err := os.Stat(config.EnvFile); err != nil {
		return check{checkFail, "Env file", config.EnvFile, "Run: tudy setup"}
	}
	return check{checkPass, "Env file", config.EnvFile, ""}
}

func checkCaddyBinary(config *Config) check {
	if config.BinaryPath == "" {
		return check{checkFail, "Caddy binary", "not found", "Reinstall tudy"}
	}
	if _, err := os.Stat(config.BinaryPath); err != nil {
		return check{checkFail, "Caddy binary", config.BinaryPath, "Reinstall tudy"}
	}
	return check{checkPass, "Caddy binary", config.BinaryPath, ""}
}

// ----- LLM checks -----

func llmChecks(config *Config) []check {
	url := config.GetEnvValue("LLM_API_URL")
	displayURL := url
	if url == "" {
		url = defaultAPIURL
		displayURL = url + " (default)"
	}
	key := config.GetAPIKey()
	model := config.GetEnvValue("MODEL")
	displayModel := model
	if model == "" {
		model = defaultModel
		displayModel = model + " (default)"
	}

	out := []check{
		{checkPass, "Endpoint", displayURL, ""},
	}
	switch {
	case key == "":
		// Not a failure: tudy runs without an LLM (heuristic matching +
		// browser picker on unresolved hostnames).
		out = append(out,
			check{checkWarn, "API key", "not set — no-LLM mode (heuristic + picker routing)", "Optional: run 'tudy setup' to enable LLM resolution"},
			check{checkSkip, "Ping endpoint", "skipped (no key)", ""},
		)
	case !llmRoutingEnabled(config):
		out = append(out,
			check{checkPass, "API key", shared.MaskAPIKey(key), ""},
			check{checkWarn, "LLM routing", "off (heuristic + picker routing)", "Re-enable with: tudy llm on"},
			check{checkSkip, "Ping endpoint", "skipped (LLM routing off)", ""},
		)
	default:
		out = append(out,
			check{checkPass, "API key", shared.MaskAPIKey(key), ""},
			pingLLM(url, key, model),
		)
	}
	out = append(out, check{checkPass, "Model", displayModel, ""})
	return out
}

// pingLLM sends a tiny chat-completions request to verify the key works
// against the configured endpoint. Catches the "rotated provider, forgot to
// update key" footgun before it shows up as a 502 on a real route.
func pingLLM(apiURL, apiKey, model string) check {
	body, _ := json.Marshal(map[string]interface{}{
		"model":      model,
		"messages":   []map[string]string{{"role": "user", "content": "ping"}},
		"max_tokens": 1,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(body))
	if err != nil {
		return check{checkFail, "Ping endpoint", err.Error(), ""}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return check{checkFail, "Ping endpoint", err.Error(), "Check network and tudy setup llm-api-url"}
	}
	defer resp.Body.Close()
	dur := time.Since(start).Round(time.Millisecond)

	if resp.StatusCode == http.StatusOK {
		return check{checkPass, "Ping endpoint", fmt.Sprintf("%d %s · %s", resp.StatusCode, resp.Status[4:], dur), ""}
	}

	// Read a snippet of the error body for context.
	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	snippet := strings.TrimSpace(string(bodyBytes))
	if len(snippet) > 100 {
		snippet = snippet[:97] + "..."
	}
	hint := ""
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		hint = "Check your API key: tudy setup llm-api-url"
	case http.StatusNotFound:
		hint = "Wrong endpoint path? Check: tudy setup llm-api-url"
	case http.StatusBadRequest:
		hint = "Model id may be invalid for this provider: tudy setup llm-model"
	case http.StatusTooManyRequests:
		hint = "Rate-limited by the provider"
	}
	detail := fmt.Sprintf("%d %s", resp.StatusCode, strings.TrimSpace(resp.Status[4:]))
	if snippet != "" {
		detail += " · " + snippet
	}
	return check{checkFail, "Ping endpoint", detail, hint}
}

// ----- Proxy checks -----

func proxyChecks(config *Config) []check {
	out := []check{}

	status := CheckProxyStatus(config)
	switch status {
	case StatusRunning:
		out = append(out, check{checkPass, "Proxy", "running on :443", ""})
	case StatusStarting:
		out = append(out, check{checkWarn, "Proxy", "starting", "Wait a moment, or check: tudy logs"})
	default:
		out = append(out, check{checkFail, "Proxy", "stopped", "Run: tudy start"})
	}

	if isDaemonInstalled() {
		out = append(out, check{checkPass, "Launchd service", "installed", ""})
	} else {
		hint := ""
		if runtime.GOOS == "darwin" {
			hint = "Run: tudy setup (installs launchd service)"
		}
		out = append(out, check{checkWarn, "Launchd service", "not installed", hint})
	}

	if isCertTrustedCheck() {
		out = append(out, check{checkPass, "TLS certificate", "trusted", ""})
	} else {
		out = append(out, check{checkFail, "TLS certificate", "not trusted", "Run: tudy trust"})
	}

	// Dashboard reachable? Only meaningful when the proxy is up.
	if status == StatusRunning {
		out = append(out, pingDashboard(config))
	}
	return out
}

func pingDashboard(config *Config) check {
	client := &http.Client{
		Timeout:   3 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}
	resp, err := client.Get(config.DashboardURL)
	if err != nil {
		return check{checkFail, "Dashboard", err.Error(), "Restart the proxy: tudy restart"}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return check{checkFail, "Dashboard", fmt.Sprintf("HTTP %d", resp.StatusCode), ""}
	}
	return check{checkPass, "Dashboard", config.DashboardURL, ""}
}

// ----- Docker checks (macOS only) -----

func dockerChecks() []check {
	if !commandExists("docker") {
		return []check{
			{checkSkip, "Docker CLI", "not installed", ""},
		}
	}

	out := []check{
		{checkPass, "Docker CLI", "installed", ""},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "docker", "info").Run(); err != nil {
		out = append(out, check{checkWarn, "Docker daemon", "not running", "Start Docker Desktop if you use containers"})
		return out
	}
	out = append(out, check{checkPass, "Docker daemon", "running", ""})

	if isDockerMacNetConnectRunning() {
		out = append(out, check{checkPass, "Network tunnel", "running (containers reachable by IP)", ""})
	} else {
		out = append(out, check{
			checkWarn,
			"Network tunnel",
			"not running",
			"Install for direct container IP routing: brew install chipmk/tap/docker-mac-net-connect",
		})
	}
	return out
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
