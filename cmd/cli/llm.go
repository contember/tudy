package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/contember/tudy/cmd/shared"
)

// llmRoutingEnabled reads the LLM_ENABLED toggle from the env file.
// Empty or unrecognized values mean enabled (matches the resolver default).
func llmRoutingEnabled(config *Config) bool {
	switch strings.ToLower(strings.TrimSpace(config.GetEnvValue("LLM_ENABLED"))) {
	case "false", "off", "0", "no":
		return false
	default:
		return true
	}
}

// runLLM dispatches `tudy llm` and its sub-targets.
//
//	tudy llm        → show whether LLM routing is on
//	tudy llm on     → enable the LLM resolution path
//	tudy llm off    → disable it (heuristic + picker only; key is kept)
func runLLM(config *Config, args []string) int {
	if len(args) == 0 {
		return printLLMStatus(config)
	}
	switch args[0] {
	case "on":
		return applyLLMEnabled(config, true)
	case "off":
		return applyLLMEnabled(config, false)
	case "status":
		return printLLMStatus(config)
	default:
		printError(fmt.Sprintf("Unknown llm subcommand: %s", args[0]))
		fmt.Fprintln(os.Stderr, "Usage: tudy llm [on|off|status]")
		return 1
	}
}

func printLLMStatus(config *Config) int {
	key := config.GetAPIKey()
	enabled := llmRoutingEnabled(config)

	switch {
	case key == "":
		fmt.Printf("LLM routing: %soff%s (no API key configured — run 'tudy setup' to add one)\n", colorYellow, colorReset)
	case !enabled:
		fmt.Printf("LLM routing: %soff%s (key %s kept — 'tudy llm on' to re-enable)\n", colorYellow, colorReset, shared.MaskAPIKey(key))
	default:
		fmt.Printf("LLM routing: %son%s (key %s)\n", colorGreen, colorReset, shared.MaskAPIKey(key))
	}
	printDim("  Off = hostnames resolve via heuristics; unresolved ones show a picker in the browser.")
	return 0
}

func applyLLMEnabled(config *Config, enabled bool) int {
	label := "off"
	value := "false"
	if enabled {
		label = "on"
		value = "true"
	}

	if enabled && config.GetAPIKey() == "" {
		printWarning("No API key configured — LLM routing stays inactive until you add one.")
		printDim("  Run: tudy setup")
	}

	if llmRoutingEnabled(config) == enabled {
		fmt.Printf("LLM routing already %s%s%s.\n", colorBold, label, colorReset)
		return 0
	}
	fmt.Printf("Turning LLM routing %s%s%s... ", colorBold, label, colorReset)
	if err := config.SetEnvValue("LLM_ENABLED", value); err != nil {
		fmt.Println()
		printError(fmt.Sprintf("Failed to update env file: %v", err))
		return 1
	}
	fmt.Println("done")
	return restartIfRunning(config)
}
