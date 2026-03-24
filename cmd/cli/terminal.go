package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorCyan   = "\033[36m"
	colorBold   = "\033[1m"
	colorDim    = "\033[2m"
)

// printHeader prints a bold header line
func printHeader(msg string) {
	fmt.Printf("\n%s%s%s\n\n", colorBold, msg, colorReset)
}

// printStep prints a step indicator like "[1/3] Configure API Key"
func printStep(step, total int, msg string) {
	fmt.Printf("%s[%d/%d]%s %s\n", colorCyan, step, total, colorReset, msg)
}

// printOK prints a success message
func printOK(msg string) {
	fmt.Printf("%s  OK%s %s\n", colorGreen, colorReset, msg)
}

// printError prints an error message
func printError(msg string) {
	fmt.Fprintf(os.Stderr, "%serror:%s %s\n", colorRed, colorReset, msg)
}

// printWarning prints a warning message
func printWarning(msg string) {
	fmt.Fprintf(os.Stderr, "%swarning:%s %s\n", colorYellow, colorReset, msg)
}

// printDim prints dimmed text
func printDim(msg string) {
	fmt.Printf("%s%s%s\n", colorDim, msg, colorReset)
}

// promptString prompts the user for a string value
func promptString(prompt, defaultValue string) string {
	reader := bufio.NewReader(os.Stdin)
	if defaultValue != "" {
		fmt.Printf("  %s [%s]: ", prompt, defaultValue)
	} else {
		fmt.Printf("  %s: ", prompt)
	}
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultValue
	}
	return line
}

// promptYesNo prompts the user for a yes/no answer
func promptYesNo(prompt string, defaultYes bool) bool {
	reader := bufio.NewReader(os.Stdin)
	hint := "y/N"
	if defaultYes {
		hint = "Y/n"
	}
	fmt.Printf("  %s [%s]: ", prompt, hint)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	if line == "" {
		return defaultYes
	}
	return line == "y" || line == "yes"
}
