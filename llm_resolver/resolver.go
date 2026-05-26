package llm_resolver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"go.uber.org/zap"
)

const defaultAPIURL = "https://openrouter.ai/api/v1/chat/completions"

var (
	markdownPrefixRe = regexp.MustCompile(`^` + "```" + `(?:json)?\s*`)
	markdownSuffixRe = regexp.MustCompile(`\s*` + "```" + `$`)
)

// Resolver handles LLM-based target resolution
type Resolver struct {
	apiKey         string
	apiURL         string
	model          string
	composeProject string
	logger         *zap.Logger
	httpClient     *http.Client
}

// NewResolver creates a new resolver instance
func NewResolver(apiKey, apiURL, model, composeProject string, logger *zap.Logger) *Resolver {
	if apiURL == "" {
		apiURL = defaultAPIURL
	}
	return &Resolver{
		apiKey:         apiKey,
		apiURL:         apiURL,
		model:          model,
		composeProject: composeProject,
		logger:         logger,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// LLMResponse represents the expected response from the LLM
type LLMResponse struct {
	Type           string `json:"type"`
	Target         string `json:"target"`
	Port           int    `json:"port"`
	Reason         string `json:"reason"`
	Workdir        string `json:"workdir,omitempty"`        // Working directory for process identification
	CommandPattern string `json:"commandPattern,omitempty"` // Optional regex to match command
}

// ResolveTarget resolves a hostname to a target using the LLM.
// The provided context is plumbed down to the outbound LLM HTTP call so that
// cancellation of the inbound request (e.g. client disconnect) aborts the
// outbound request instead of waiting out the full client timeout.
func (r *Resolver) ResolveTarget(ctx context.Context, hostname, userPrompt string, existingMappings Mappings) (*RouteMapping, error) {
	if r.apiKey == "" {
		return nil, fmt.Errorf("API key is not set")
	}

	// Gather context
	processes, err := DiscoverLocalProcesses()
	if err != nil {
		r.logger.Warn("failed to discover processes", zap.Error(err))
	}

	containers, err := DiscoverDockerContainers(r.composeProject)
	if err != nil {
		r.logger.Warn("failed to discover containers", zap.Error(err))
	}

	prompt := r.buildPrompt(hostname, processes, containers, existingMappings, userPrompt)
	systemPrompt := r.getSystemPrompt()

	response, err := r.callLLM(ctx, systemPrompt, prompt)
	if err != nil {
		return nil, err
	}

	mapping := &RouteMapping{
		Type:      response.Type,
		Target:    response.Target,
		Port:      response.Port,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		LLMReason: response.Reason,
	}

	// For process type, create ProcessIdentifier for dynamic port resolution
	if response.Type == "process" && response.Workdir != "" {
		mapping.ProcessIdentifier = &ProcessIdentifier{
			Workdir:        response.Workdir,
			CommandPattern: response.CommandPattern,
		}
	}

	return mapping, nil
}

// ResolveRelatedService resolves a related service for an origin hostname.
// The provided context is plumbed down to the outbound LLM HTTP call so that
// cancellation of the inbound request (e.g. client disconnect) aborts the
// outbound request instead of waiting out the full client timeout.
func (r *Resolver) ResolveRelatedService(
	ctx context.Context,
	originHostname string,
	originMapping *RouteMapping,
	serviceName string,
	userPrompt string,
	existingMappings Mappings,
) (*RouteMapping, error) {
	if r.apiKey == "" {
		return nil, fmt.Errorf("API key is not set")
	}

	// Gather context
	processes, err := DiscoverLocalProcesses()
	if err != nil {
		r.logger.Warn("failed to discover processes", zap.Error(err))
	}

	containers, err := DiscoverDockerContainers(r.composeProject)
	if err != nil {
		r.logger.Warn("failed to discover containers", zap.Error(err))
	}

	prompt := r.buildRelatedServicePrompt(originHostname, originMapping, serviceName, processes, containers, existingMappings, userPrompt)
	systemPrompt := r.getRelatedServiceSystemPrompt()

	response, err := r.callLLM(ctx, systemPrompt, prompt)
	if err != nil {
		return nil, err
	}

	mapping := &RouteMapping{
		Type:      response.Type,
		Target:    response.Target,
		Port:      response.Port,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		LLMReason: response.Reason,
	}

	// For process type, create ProcessIdentifier for dynamic port resolution
	if response.Type == "process" && response.Workdir != "" {
		mapping.ProcessIdentifier = &ProcessIdentifier{
			Workdir:        response.Workdir,
			CommandPattern: response.CommandPattern,
		}
	}

	return mapping, nil
}

func (r *Resolver) getSystemPrompt() string {
	return `You are a routing resolver for a local development proxy. Your job is to determine which local service a request should be forwarded to based on the hostname.

You will receive:
1. The hostname from the request (e.g., "myapp.localhost", "api.project.localhost")
2. A list of locally running processes with their ports, commands, arguments, and working directories
3. A list of Docker containers with their names, images, exposed ports, IP addresses, and working directories
4. Current routing mappings for context

Your task is to analyze the hostname and determine the best matching service. Consider:
- Hostname patterns (e.g., "vite.myproject.localhost" might match a Vite process running in a "myproject" directory)
- Service types (e.g., a hostname containing "api" might route to a backend service)
- Project names in the hostname vs working directories
- Container names vs hostname parts

Respond with a JSON object:
{
  "type": "process" | "docker",
  "target": "localhost" for process, or container name for docker,
  "port": the port number to connect to,
  "reason": "brief explanation of why this target was chosen",
  "workdir": "working directory of the matched process (REQUIRED for type=process, omit for docker)"
}

IMPORTANT: For type="process", you MUST include the "workdir" field with the full working directory path of the matched process. This is used for dynamic port resolution when the process restarts on a different port.

If no suitable target is found, still provide your best guess with explanation.`
}

func (r *Resolver) getRelatedServiceSystemPrompt() string {
	return `You are a routing resolver for a local development proxy. Your job is to find a related service for a given origin service.

You will receive:
1. The origin hostname and where it routes to (e.g., "app.mapeditor.localhost" -> process on port 5173)
2. The service name being requested (e.g., "api", "backend", "db")
3. A list of locally running processes with their ports, commands, arguments, and working directories
4. A list of Docker containers with their names, images, exposed ports, IP addresses, and working directories
5. Current routing mappings for context

Your task is to find the related service. Consider:
- If origin is "app.mapeditor.localhost" and service is "api", look for an API/backend service in the same project (mapeditor)
- Working directories are key - look for services in the same project folder
- Docker compose services often have related names (app, api, db, redis, etc.)
- Common patterns: frontend+backend, app+api, web+server

Respond with a JSON object:
{
  "type": "process" | "docker",
  "target": "localhost" for process, or container name for docker,
  "port": the port number to connect to,
  "reason": "brief explanation of why this target was chosen",
  "workdir": "working directory of the matched process (REQUIRED for type=process, omit for docker)"
}

IMPORTANT: For type="process", you MUST include the "workdir" field with the full working directory path of the matched process. This is used for dynamic port resolution when the process restarts on a different port.

If no suitable target is found, still provide your best guess with explanation.`
}

func (r *Resolver) buildPrompt(
	hostname string,
	processes []LocalProcess,
	containers []DockerContainer,
	mappings Mappings,
	userPrompt string,
) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("Hostname to resolve: %s\n\n", hostname))

	b.WriteString("## Local Processes\n")
	if len(processes) == 0 {
		b.WriteString("No local processes with open ports found.\n")
	} else {
		for _, proc := range processes {
			b.WriteString(fmt.Sprintf("- Port %d: %s", proc.Port, proc.Command))
			if proc.Args != "" {
				b.WriteString(fmt.Sprintf(" (args: %s)", proc.Args))
			}
			if proc.Workdir != "" {
				b.WriteString(fmt.Sprintf(" [workdir: %s]", proc.Workdir))
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("\n## Docker Containers\n")
	if len(containers) == 0 {
		b.WriteString("No Docker containers found.\n")
	} else {
		for _, container := range containers {
			b.WriteString(fmt.Sprintf("- %s (image: %s)", container.Name, container.Image))
			if len(container.Ports) > 0 {
				ports := make([]string, len(container.Ports))
				for i, p := range container.Ports {
					ports[i] = fmt.Sprintf("%d", p)
				}
				b.WriteString(fmt.Sprintf(" ports: %s", strings.Join(ports, ", ")))
			}
			if container.IP != "" {
				b.WriteString(fmt.Sprintf(" [ip: %s]", container.IP))
			}
			if container.Workdir != "" {
				b.WriteString(fmt.Sprintf(" [workdir: %s]", container.Workdir))
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("\n## Current Mappings\n")
	if len(mappings) == 0 {
		b.WriteString("No existing mappings.\n")
	} else {
		for host, mapping := range mappings {
			b.WriteString(fmt.Sprintf("- %s -> %s:%s:%d", host, mapping.Type, mapping.Target, mapping.Port))
			if mapping.LLMReason != "" {
				b.WriteString(fmt.Sprintf(" (%s)", mapping.LLMReason))
			}
			b.WriteString("\n")
		}
	}

	if userPrompt != "" {
		b.WriteString(fmt.Sprintf("\n## Additional Context from User\n%s\n", userPrompt))
	}

	return b.String()
}

func (r *Resolver) buildRelatedServicePrompt(
	originHostname string,
	originMapping *RouteMapping,
	serviceName string,
	processes []LocalProcess,
	containers []DockerContainer,
	mappings Mappings,
	userPrompt string,
) string {
	var b strings.Builder

	b.WriteString("## Request Context\n")
	b.WriteString(fmt.Sprintf("Origin hostname: %s\n", originHostname))
	if originMapping != nil {
		b.WriteString(fmt.Sprintf("Origin routes to: %s:%s:%d\n", originMapping.Type, originMapping.Target, originMapping.Port))
	}
	b.WriteString(fmt.Sprintf("Looking for related service: \"%s\"\n\n", serviceName))

	b.WriteString("## Local Processes\n")
	if len(processes) == 0 {
		b.WriteString("No local processes with open ports found.\n")
	} else {
		for _, proc := range processes {
			b.WriteString(fmt.Sprintf("- Port %d: %s", proc.Port, proc.Command))
			if proc.Args != "" {
				b.WriteString(fmt.Sprintf(" (args: %s)", proc.Args))
			}
			if proc.Workdir != "" {
				b.WriteString(fmt.Sprintf(" [workdir: %s]", proc.Workdir))
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("\n## Docker Containers\n")
	if len(containers) == 0 {
		b.WriteString("No Docker containers found.\n")
	} else {
		for _, container := range containers {
			b.WriteString(fmt.Sprintf("- %s (image: %s)", container.Name, container.Image))
			if len(container.Ports) > 0 {
				ports := make([]string, len(container.Ports))
				for i, p := range container.Ports {
					ports[i] = fmt.Sprintf("%d", p)
				}
				b.WriteString(fmt.Sprintf(" ports: %s", strings.Join(ports, ", ")))
			}
			if container.IP != "" {
				b.WriteString(fmt.Sprintf(" [ip: %s]", container.IP))
			}
			if container.Workdir != "" {
				b.WriteString(fmt.Sprintf(" [workdir: %s]", container.Workdir))
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("\n## Current Mappings\n")
	if len(mappings) == 0 {
		b.WriteString("No existing mappings.\n")
	} else {
		for host, mapping := range mappings {
			b.WriteString(fmt.Sprintf("- %s -> %s:%s:%d", host, mapping.Type, mapping.Target, mapping.Port))
			if mapping.LLMReason != "" {
				b.WriteString(fmt.Sprintf(" (%s)", mapping.LLMReason))
			}
			b.WriteString("\n")
		}
	}

	if userPrompt != "" {
		b.WriteString(fmt.Sprintf("\n## Additional Context from User\n%s\n", userPrompt))
	}

	return b.String()
}

func (r *Resolver) callLLM(ctx context.Context, systemPrompt, userPrompt string) (*LLMResponse, error) {
	requestBody := map[string]interface{}{
		"model": r.model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"response_format": map[string]string{"type": "json_object"},
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", r.apiURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.apiKey)

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error: %d - %s", resp.StatusCode, string(body))
	}

	var apiResponse struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(body, &apiResponse); err != nil {
		return nil, fmt.Errorf("failed to parse API response: %w", err)
	}

	if len(apiResponse.Choices) == 0 || apiResponse.Choices[0].Message.Content == "" {
		return nil, fmt.Errorf("no response from LLM")
	}

	content := apiResponse.Choices[0].Message.Content

	// Strip markdown code blocks if present
	content = stripMarkdownCodeBlocks(content)

	var llmResponse LLMResponse
	if err := json.Unmarshal([]byte(content), &llmResponse); err != nil {
		return nil, fmt.Errorf("failed to parse LLM response: %s - %w", content, err)
	}

	// Validate response
	if err := validateLLMResponse(&llmResponse); err != nil {
		return nil, fmt.Errorf("invalid LLM response: %w", err)
	}

	return &llmResponse, nil
}

// stripMarkdownCodeBlocks removes markdown code block markers
func stripMarkdownCodeBlocks(content string) string {
	// Remove leading ```json or ```
	content = markdownPrefixRe.ReplaceAllString(content, "")

	// Remove trailing ```
	content = markdownSuffixRe.ReplaceAllString(content, "")

	return strings.TrimSpace(content)
}

// validateLLMResponse validates the LLM response structure
func validateLLMResponse(r *LLMResponse) error {
	if r.Type != "process" && r.Type != "docker" {
		return fmt.Errorf("type must be 'process' or 'docker', got '%s'", r.Type)
	}
	if r.Target == "" {
		return fmt.Errorf("target must be a non-empty string")
	}
	if r.Port < 1 || r.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535, got %d", r.Port)
	}
	return nil
}
