package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/contember/tudy/cmd/shared"
	"github.com/contember/tudy/llm_resolver/discovery"
)

type statusData struct {
	Mappings     map[string]*routeMapping   `json:"mappings"`
	Processes    []discovery.LocalProcess    `json:"processes"`
	Containers   []discovery.DockerContainer `json:"containers"`
	DockerTunnel bool                        `json:"docker_tunnel"`
}

// routeMapping mirrors llm_resolver.RouteMapping without importing the Caddy-dependent package
type routeMapping struct {
	Type      string `json:"type"`
	Target    string `json:"target"`
	Port      int    `json:"port"`
	CreatedAt string `json:"createdAt"`
	LLMReason string `json:"llmReason"`
}

// fetchStatusData fetches status data from the proxy debug endpoint
func fetchStatusData(dashboardURL string) (*statusData, error) {
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	req, err := http.NewRequest("GET", dashboardURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var data statusData
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	return &data, nil
}

// printRichStatus prints the rich dashboard-style status output.
// If no status is provided, it checks the proxy status.
func printRichStatus(config *Config, statuses ...ProxyStatus) {
	// Header
	fmt.Printf("%study%s %sv%s%s\n", colorBold, colorReset, colorDim, Version, colorReset)
	fmt.Println()

	// Proxy status
	status := StatusStopped
	if len(statuses) > 0 {
		status = statuses[0]
	} else {
		status = CheckProxyStatus(config)
	}
	switch status {
	case StatusRunning:
		fmt.Printf("  %s●%s Proxy running on %s:443%s\n", colorGreen, colorReset, colorBold, colorReset)
	case StatusStarting:
		fmt.Printf("  %s●%s Proxy is starting...\n", colorYellow, colorReset)
	default:
		printStopped(config)
		return
	}

	// Dashboard
	fmt.Printf("  %s●%s Dashboard at %sproxy.localhost%s\n", colorGreen, colorReset, colorBold, colorReset)
	fmt.Println()

	// Fetch status data and check TLS concurrently
	var data *statusData
	var certTrusted bool
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		d, err := fetchStatusData(config.DashboardURL)
		if err == nil {
			data = d
		}
	}()
	go func() {
		defer wg.Done()
		certTrusted = isCertTrustedCheck()
	}()
	wg.Wait()

	if data != nil {
		printDiscoveredServices(data)
		printRouting(data)
	}

	// Status checks
	if certTrusted {
		fmt.Printf("  %s●%s TLS certificate trusted\n", colorGreen, colorReset)
	} else {
		fmt.Printf("  %s●%s TLS certificate %snot trusted%s %s(run: tudy trust)%s\n",
			colorYellow, colorReset, colorYellow, colorReset, colorDim, colorReset)
	}
	if data != nil {
		printDockerTunnelStatus(data)
	}

	// Footer
	fmt.Println()
	printDim("  Watching for new services...")
	fmt.Println()
}

func printSectionHeader(title string) {
	fmt.Printf("  %s%s%s\n", colorDim, title, colorReset)
}

func printDiscoveredServices(data *statusData) {
	if len(data.Processes) == 0 && len(data.Containers) == 0 {
		return
	}

	printSectionHeader("Discovered services")
	fmt.Println()

	for _, proc := range data.Processes {
		label := shortenPath(proc.Workdir)
		if label == "" {
			label = proc.Command
		}
		fmt.Printf("    %s●%s :%d  %s%s%s\n", colorGreen, colorReset, proc.Port, colorDim, label, colorReset)
	}

	for _, c := range data.Containers {
		var portNums []int
		if len(c.PortMappings) > 0 {
			for _, pm := range c.PortMappings {
				portNums = append(portNums, pm.HostPort)
			}
		} else {
			portNums = c.Ports
		}
		ports := formatPorts(portNums)
		label := c.Name
		if c.Image != "" {
			label += fmt.Sprintf(" %s(%s)%s", colorDim, c.Image, colorReset)
		}
		if ports != "" {
			fmt.Printf("    %s●%s %s  %s\n", colorYellow, colorReset, ports, label)
		} else {
			fmt.Printf("    %s●%s %s\n", colorYellow, colorReset, label)
		}
	}

	fmt.Println()
}

func formatPorts(ports []int) string {
	if len(ports) == 0 {
		return ""
	}
	strs := make([]string, len(ports))
	for i, p := range ports {
		strs[i] = fmt.Sprintf(":%d", p)
	}
	return strings.Join(strs, ", ")
}

func printRouting(data *statusData) {
	if len(data.Mappings) == 0 {
		return
	}

	printSectionHeader("Routing")
	fmt.Println()

	// Sort hostnames for consistent output
	hostnames := make([]string, 0, len(data.Mappings))
	for hostname := range data.Mappings {
		hostnames = append(hostnames, hostname)
	}
	sort.Strings(hostnames)

	for _, hostname := range hostnames {
		m := data.Mappings[hostname]
		if m == nil {
			continue
		}
		fmt.Printf("    %s→%s %s%s%s  →  :%d\n", colorCyan, colorReset, colorBold, hostname, colorReset, m.Port)
		if m.LLMReason != "" {
			fmt.Printf("      %s%s%s\n", colorDim, m.LLMReason, colorReset)
		}
	}

	fmt.Println()
}

func printDockerTunnelStatus(data *statusData) {
	if len(data.Containers) == 0 {
		return // Only relevant when Docker containers are present
	}
	if data.DockerTunnel {
		fmt.Printf("  %s●%s Docker networking %s(docker-mac-net-connect detected; reachability verified per-target)%s\n",
			colorGreen, colorReset, colorDim, colorReset)
	} else {
		fmt.Printf("  %s●%s Docker networking %svia published ports only%s\n",
			colorYellow, colorReset, colorDim, colorReset)
		printDim("    Install: brew install chipmk/tap/docker-mac-net-connect")
	}
}

func printStopped(config *Config) {
	fmt.Printf("  %s●%s Proxy is stopped\n", colorRed, colorReset)
	fmt.Println()
	fmt.Printf("  Config:    %s\n", config.ConfigDir)
	fmt.Printf("  Dashboard: %s\n", config.DashboardURL)

	if key := config.GetAPIKey(); key == "" {
		fmt.Printf("  API Key:   %snot set — no-LLM mode (heuristic + picker routing)%s\n", colorYellow, colorReset)
	} else if !llmRoutingEnabled(config) {
		fmt.Printf("  API Key:   %s %s(LLM routing off — tudy llm on)%s\n", shared.MaskAPIKey(key), colorYellow, colorReset)
	} else {
		fmt.Printf("  API Key:   %s\n", shared.MaskAPIKey(key))
	}

	fmt.Println()
	printDim("  Run 'tudy start' to start the proxy.")
	fmt.Println()
}

var homeDir = sync.OnceValue(func() string {
	home, _ := os.UserHomeDir()
	return home
})

func shortenPath(path string) string {
	if path == "" {
		return ""
	}
	if home := homeDir(); home != "" && strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}
