package llm_resolver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
)

// ServeHTTP implements caddyhttp.MiddlewareHandler.
func (m *LLMResolver) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	hostname := extractHostname(r)

	m.logger.Debug("handling request",
		zap.String("host", hostname),
		zap.String("path", r.URL.Path),
		zap.String("method", r.Method),
	)

	// Handle special paths

	// TLS check endpoint for on-demand TLS
	if r.URL.Path == "/_tls_check" {
		domain := r.URL.Query().Get("domain")
		if domain == "" {
			domain = hostname
		}
		if strings.HasSuffix(domain, ".localhost") {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
			return nil
		}
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("Not allowed"))
		return nil
	}

	// API endpoints for mapping management
	if strings.HasPrefix(r.URL.Path, "/_api/mappings/") {
		return m.handleMappingsAPI(w, r)
	}

	// proxy.localhost — dashboard / sub-pages / SSE
	if hostname == "proxy.localhost" {
		return m.handleProxyLocalhost(w, r)
	}
	if r.URL.Path == "/_debug" {
		return m.handleDebug(w, r)
	}

	// Second-level proxy for inter-service communication
	if strings.HasPrefix(r.URL.Path, "/_proxy/") {
		return m.handleSecondLevelProxy(w, r, hostname, next)
	}

	// Service picker form submission (no-LLM / fallback routing UI)
	if r.URL.Path == pickerSelectPath {
		return m.handlePickerSelect(w, r, hostname)
	}

	// Ignore common browser requests
	if r.URL.Path == "/favicon.ico" || r.URL.Path == "/robots.txt" {
		w.WriteHeader(http.StatusNotFound)
		return nil
	}

	// From here on we're proxying a real *.localhost request — record stats
	// for the dashboard activity view, and emit one Info log per request so
	// the dashboard's logs page shows actual traffic (not just resolver
	// chatter on cache misses).
	start := time.Now()
	ww := newStatusWriter(w)
	w = ww
	defer func() {
		dur := time.Since(start)
		if m.stats != nil {
			m.stats.Record(hostname, ww.statusCode)
		}
		m.logger.Info("request",
			zap.String("host", hostname),
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.Int("status", ww.statusCode),
			zap.Duration("duration", dur),
		)
	}()

	// Check for force refresh and custom prompt
	force := r.URL.Query().Has("force")
	userPrompt := r.URL.Query().Get("prompt")

	// Get or resolve target
	var mapping *RouteMapping

	if !force {
		mapping = m.cache.Get(hostname)
	}

	if mapping == nil {
		m.logger.Info("resolving target",
			zap.String("hostname", hostname),
			zap.Bool("forced", force),
		)

		// Use singleflight to deduplicate concurrent requests for same hostname.
		// Trade-off: the call is owned by whichever request arrived first
		// ("the leader"); if that request's context is canceled, the LLM call
		// is aborted and all current followers also fail. That's acceptable
		// here — followers will simply retry on the next request, which is
		// cheaper than letting a stalled call block every waiter for the full
		// HTTP timeout.
		result, err, shared := m.resolveGroup.Do(hostname, func() (interface{}, error) {
			// Double-check cache inside singleflight (another request may have just finished)
			if cached := m.cache.Get(hostname); cached != nil && !force {
				return cached, nil
			}

			resolved, err := m.resolveTargetChain(r.Context(), hostname, userPrompt, force)
			if err != nil {
				return nil, err
			}

			// Cache the result
			m.cache.Set(hostname, resolved)
			if err := m.cache.Save(); err != nil {
				m.logger.Warn("failed to save cache", zap.Error(err))
			}

			return resolved, nil
		})

		if err != nil {
			m.logger.Error("failed to resolve target",
				zap.String("hostname", hostname),
				zap.Error(err),
			)
			return m.serveResolutionFailure(w, r, hostname, err)
		}

		mapping, _ = result.(*RouteMapping)
		if mapping == nil {
			m.logger.Error("singleflight returned nil mapping", zap.String("hostname", hostname))
			http.Error(w, "internal error", http.StatusInternalServerError)
			return nil
		}

		m.logger.Info("resolved target",
			zap.String("hostname", hostname),
			zap.String("type", mapping.Type),
			zap.String("target", mapping.Target),
			zap.Int("port", mapping.Port),
			zap.String("reason", mapping.LLMReason),
			zap.Bool("shared", shared),
		)
	}

	// Build upstream URL
	upstream, err := m.buildUpstreamURL(mapping)
	if err != nil {
		m.logger.Error("failed to build upstream URL",
			zap.String("hostname", hostname),
			zap.Error(err),
		)
		http.Error(w, fmt.Sprintf("Failed to build upstream: %v", err), http.StatusBadGateway)
		return nil
	}

	m.logger.Debug("proxying request",
		zap.String("hostname", hostname),
		zap.String("upstream", upstream),
	)

	// Set the upstream variable for reverse_proxy to use
	caddyhttp.SetVar(r.Context(), "upstream", upstream)

	return next.ServeHTTP(w, r)
}

// errNoLLM signals that automatic resolution is exhausted: no unambiguous
// heuristic match, and no LLM available (not configured, or toggled off)
// to take over.
var errNoLLM = errors.New("no LLM available and no unambiguous heuristic match")

// resolveTargetChain resolves a hostname through the full chain:
// heuristic match first (free, instant), then the LLM when configured.
//
// A force refresh (or an explicit user prompt) means the user is overriding
// a previous answer, so the heuristic is skipped — it would just reproduce
// the mapping they're trying to replace. With an LLM that falls through to
// the LLM; without one it falls through to the picker.
func (m *LLMResolver) resolveTargetChain(ctx context.Context, hostname, userPrompt string, force bool) (*RouteMapping, error) {
	processes, err := DiscoverLocalProcesses()
	if err != nil {
		m.logger.Warn("failed to discover processes", zap.Error(err))
	}
	containers, err := DiscoverDockerContainers(m.ComposeProject)
	if err != nil {
		m.logger.Warn("failed to discover containers", zap.Error(err))
	}

	skipHeuristic := force || userPrompt != ""
	if !skipHeuristic {
		if mapping := ResolveHeuristically(hostname, processes, containers); mapping != nil {
			m.logger.Info("resolved heuristically",
				zap.String("hostname", hostname),
				zap.String("reason", mapping.LLMReason),
			)
			return mapping, nil
		}
	}

	if !m.llmAvailable() {
		return nil, errNoLLM
	}

	return m.resolver.ResolveTarget(ctx, hostname, userPrompt, processes, containers, m.cache.GetAll())
}

// serveResolutionFailure answers a request whose hostname could not be
// resolved: browsers get the interactive service picker, other clients get a
// plain-text error with discovered services and next steps.
func (m *LLMResolver) serveResolutionFailure(w http.ResponseWriter, r *http.Request, hostname string, resolveErr error) error {
	msg := resolveErr.Error()
	if errors.Is(resolveErr, errNoLLM) {
		reason := "no LLM is configured"
		if m.resolver.HasAPIKey() && !m.llmEnabled {
			reason = "LLM routing is turned off"
		}
		msg = fmt.Sprintf("Automatic resolution unavailable: no unambiguous match among discovered services, and %s.", reason)
	}

	if wantsHTML(r) {
		return m.renderPickerPage(w, hostname, msg, r.URL.RequestURI())
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusBadGateway)
	w.Write([]byte(m.pickerSuggestionText(hostname, msg)))
	return nil
}

// handleSecondLevelProxy handles /_proxy/serviceName/path requests
func (m *LLMResolver) handleSecondLevelProxy(w http.ResponseWriter, r *http.Request, originHostname string, next caddyhttp.Handler) error {
	// Parse /_proxy/serviceName/path
	pathParts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/_proxy/"), "/", 2)
	if len(pathParts) == 0 || pathParts[0] == "" {
		http.Error(w, "Invalid proxy path", http.StatusBadRequest)
		return nil
	}

	serviceName := pathParts[0]
	remainingPath := "/"
	if len(pathParts) > 1 {
		remainingPath = "/" + pathParts[1]
	}

	m.logger.Info("second-level proxy request",
		zap.String("origin", originHostname),
		zap.String("service", serviceName),
		zap.String("path", remainingPath),
	)

	force := r.URL.Query().Has("force")
	userPrompt := r.URL.Query().Get("prompt")

	// Cache key for related service
	cacheKey := fmt.Sprintf("%s:%s", originHostname, serviceName)

	var mapping *RouteMapping

	if !force {
		mapping = m.cache.Get(cacheKey)
	}

	if mapping == nil {
		// Use singleflight to deduplicate concurrent requests for same cache key.
		// Trade-off: the call is owned by whichever request arrived first
		// ("the leader"); if that request's context is canceled, the LLM call
		// is aborted and all current followers also fail. That's acceptable
		// here — followers will simply retry on the next request, which is
		// cheaper than letting a stalled call block every waiter for the full
		// HTTP timeout.
		result, err, shared := m.resolveGroup.Do(cacheKey, func() (interface{}, error) {
			// Double-check cache inside singleflight
			if cached := m.cache.Get(cacheKey); cached != nil && !force {
				return cached, nil
			}

			// Get origin mapping for context
			originMapping := m.cache.Get(originHostname)

			resolved, err := m.resolveRelatedChain(r.Context(), originHostname, originMapping, serviceName, userPrompt, force)
			if err != nil {
				return nil, err
			}

			// Cache the result
			m.cache.Set(cacheKey, resolved)
			if err := m.cache.Save(); err != nil {
				m.logger.Warn("failed to save cache", zap.Error(err))
			}

			return resolved, nil
		})

		if err != nil {
			m.logger.Error("failed to resolve related service",
				zap.String("origin", originHostname),
				zap.String("service", serviceName),
				zap.Error(err),
			)
			http.Error(w, fmt.Sprintf("Failed to resolve service: %v", err), http.StatusBadGateway)
			return nil
		}

		mapping, _ = result.(*RouteMapping)
		if mapping == nil {
			m.logger.Error("singleflight returned nil mapping", zap.String("cacheKey", cacheKey))
			http.Error(w, "internal error", http.StatusInternalServerError)
			return nil
		}

		m.logger.Info("resolved related service",
			zap.String("origin", originHostname),
			zap.String("service", serviceName),
			zap.String("type", mapping.Type),
			zap.String("target", mapping.Target),
			zap.Int("port", mapping.Port),
			zap.Bool("shared", shared),
		)
	}

	// Build upstream URL
	upstream, err := m.buildUpstreamURL(mapping)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to build upstream: %v", err), http.StatusBadGateway)
		return nil
	}

	// Modify request path to remove /_proxy/serviceName prefix
	r.URL.Path = remainingPath

	// Set upstream for reverse_proxy
	caddyhttp.SetVar(r.Context(), "upstream", upstream)

	return next.ServeHTTP(w, r)
}

// resolveRelatedChain resolves a related service (second-level proxy):
// compose-aware heuristic first, then the LLM when configured.
//
// Unlike resolveTargetChain, force without an LLM still re-runs the
// heuristic: /_proxy/ calls are programmatic (fetch, not a browser
// navigation), so there's no picker to fall through to — a heuristic rerun
// beats a guaranteed error.
func (m *LLMResolver) resolveRelatedChain(
	ctx context.Context,
	originHostname string,
	originMapping *RouteMapping,
	serviceName, userPrompt string,
	force bool,
) (*RouteMapping, error) {
	processes, err := DiscoverLocalProcesses()
	if err != nil {
		m.logger.Warn("failed to discover processes", zap.Error(err))
	}
	containers, err := DiscoverDockerContainers(m.ComposeProject)
	if err != nil {
		m.logger.Warn("failed to discover containers", zap.Error(err))
	}

	skipHeuristic := (force || userPrompt != "") && m.llmAvailable()
	if !skipHeuristic {
		if mapping := ResolveRelatedHeuristically(originMapping, serviceName, processes, containers); mapping != nil {
			m.logger.Info("resolved related service heuristically",
				zap.String("origin", originHostname),
				zap.String("service", serviceName),
				zap.String("reason", mapping.LLMReason),
			)
			return mapping, nil
		}
	}

	if !m.llmAvailable() {
		return nil, errNoLLM
	}

	return m.resolver.ResolveRelatedService(ctx, originHostname, originMapping, serviceName, userPrompt, processes, containers, m.cache.GetAll())
}

// handleMappingsAPI handles CRUD operations for mappings
func (m *LLMResolver) handleMappingsAPI(w http.ResponseWriter, r *http.Request) error {
	remainder := strings.TrimPrefix(r.URL.Path, "/_api/mappings/")

	// Collection endpoint: list all mappings on GET, 404 otherwise.
	if remainder == "" || remainder == "/" {
		if r.Method != http.MethodGet {
			http.Error(w, "Not found", http.StatusNotFound)
			return nil
		}
		w.Header().Set("Content-Type", "application/json")
		return json.NewEncoder(w).Encode(m.cache.GetAll())
	}

	// Strip a single trailing slash, then URL-decode the hostname segment.
	remainder = strings.TrimSuffix(remainder, "/")
	hostname, err := url.PathUnescape(remainder)
	if err != nil {
		http.Error(w, "Invalid hostname encoding", http.StatusBadRequest)
		return nil
	}
	// Reject anything that still looks like a path (e.g. "/_api/mappings//foo"
	// reduced to "/foo", or path-traversal-style inputs).
	if hostname == "" || strings.Contains(hostname, "/") {
		http.Error(w, "Invalid hostname", http.StatusBadRequest)
		return nil
	}

	// Lightweight auth for state-changing methods: the dashboard at
	// proxy.localhost is the only legitimate caller, so we require the
	// request's Host to be proxy.localhost. A malicious page on
	// foo.localhost would arrive with Host: foo.localhost and be rejected.
	// We also require the Origin header (when present) to be proxy.localhost
	// — some browsers omit Origin on same-origin requests, so a missing
	// Origin is allowed when Host already matches. GET is left open: it's
	// read-only and CORS prevents cross-origin pages from reading the
	// response without a preflight we never grant.
	if r.Method == http.MethodPut || r.Method == http.MethodDelete {
		if extractHostname(r) != "proxy.localhost" {
			http.Error(w, "Forbidden: mappings API can only be modified from proxy.localhost", http.StatusForbidden)
			return nil
		}
		if origin := r.Header.Get("Origin"); origin != "" &&
			origin != "http://proxy.localhost" && origin != "https://proxy.localhost" {
			http.Error(w, "Forbidden: invalid Origin", http.StatusForbidden)
			return nil
		}
	}

	switch r.Method {
	case http.MethodGet:
		// Get specific mapping
		mapping := m.cache.Get(hostname)
		if mapping == nil {
			http.Error(w, "Not found", http.StatusNotFound)
			return nil
		}
		w.Header().Set("Content-Type", "application/json")
		return json.NewEncoder(w).Encode(mapping)

	case http.MethodPut:
		var body struct {
			Type   string `json:"type"`
			Target string `json:"target"`
			Port   int    `json:"port"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return nil
		}
		if body.Type != "process" && body.Type != "docker" {
			http.Error(w, "Invalid type", http.StatusBadRequest)
			return nil
		}
		mapping := &RouteMapping{
			Type:      body.Type,
			Target:    body.Target,
			Port:      body.Port,
			CreatedAt: timeNow(),
			LLMReason: "Manually edited",
		}
		m.cache.Set(hostname, mapping)
		if err := m.cache.Save(); err != nil {
			http.Error(w, "Failed to save", http.StatusInternalServerError)
			return nil
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Updated"))
		return nil

	case http.MethodDelete:
		m.cache.Delete(hostname)
		if err := m.cache.Save(); err != nil {
			http.Error(w, "Failed to save", http.StatusInternalServerError)
			return nil
		}
		if m.stats != nil {
			m.stats.Forget(hostname)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Deleted"))
		return nil

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return nil
	}
}

// buildUpstreamURL creates the upstream URL for the reverse proxy
func (m *LLMResolver) buildUpstreamURL(mapping *RouteMapping) (string, error) {
	if mapping.Type == "process" {
		port := mapping.Port

		// Try dynamic port resolution if ProcessIdentifier is available
		if mapping.ProcessIdentifier != nil && m.processCache != nil {
			resolvedPort, err := ResolveProcessPort(mapping.ProcessIdentifier, m.processCache)
			if err != nil {
				m.logger.Warn("dynamic port resolution failed, using cached port",
					zap.String("workdir", mapping.ProcessIdentifier.Workdir),
					zap.Int("fallbackPort", mapping.Port),
					zap.Error(err),
				)
			} else {
				port = resolvedPort
			}
		}

		return fmt.Sprintf("127.0.0.1:%d", port), nil
	}

	// Docker container — if network tunnel is healthy AND the destination
	// is actually reachable, use container IP directly. We gate on
	// IsHealthy (background tunnel probe) before doing the per-target
	// IsReachable probe — when the tunnel is known-broken, skipping
	// straight to published-port lookup saves the 400ms cold-cache hit
	// per request.
	var containerIP string
	var containerIPErr error
	tunnelUsable := m.networkTunnel != nil && m.networkTunnel.IsRunning() && m.networkTunnel.IsHealthy()

	if tunnelUsable && mapping.Port != 0 {
		containerIP, containerIPErr = GetContainerIP(mapping.Target)
		if containerIPErr == nil && containerIP != "" && m.networkTunnel.IsReachable(containerIP, mapping.Port) {
			return net.JoinHostPort(containerIP, strconv.Itoa(mapping.Port)), nil
		}
		if containerIPErr != nil || containerIP == "" {
			m.logger.Warn("tunnel active but cannot resolve container IP, trying published port",
				zap.String("container", mapping.Target),
				zap.Error(containerIPErr),
			)
		} else {
			m.logger.Warn("tunnel active but container IP not reachable, trying published port (another VPN may be claiming this Docker subnet)",
				zap.String("container", mapping.Target),
				zap.String("ip", containerIP),
				zap.Int("port", mapping.Port),
			)
		}
	}

	// Try published port (required on macOS/Windows without tunnel)
	if hostIP, hostPort, found := GetContainerHostAddress(mapping.Target, mapping.Port); found {
		return net.JoinHostPort(hostIP, strconv.Itoa(hostPort)), nil
	}

	// Fall back to container IP (works on Linux or inside Docker on same network).
	// Reuse the lookup from above if we already did it.
	if containerIP == "" && containerIPErr == nil {
		containerIP, containerIPErr = GetContainerIP(mapping.Target)
	}
	if containerIPErr != nil || containerIP == "" {
		return "", fmt.Errorf("cannot resolve IP for container %s (no published port and container IP not reachable): %v", mapping.Target, containerIPErr)
	}

	// If the tunnel is up, only return the container IP when we've actually
	// confirmed reachability — otherwise we'd hand Caddy an IP we already
	// know is unreachable (e.g. VPN-claimed subnet) and the request would
	// eat a multi-second reverse_proxy dial timeout for nothing.
	if tunnelUsable && mapping.Port != 0 && !m.networkTunnel.IsReachable(containerIP, mapping.Port) {
		return "", fmt.Errorf("container %s at %s:%d not reachable (no published port and container IP not routable; a VPN may be claiming this Docker subnet, or dmnc tunnel needs restart)", mapping.Target, containerIP, mapping.Port)
	}

	return net.JoinHostPort(containerIP, strconv.Itoa(mapping.Port)), nil
}

// extractHostname extracts the hostname from the request, removing the port
func extractHostname(r *http.Request) string {
	host := r.Host
	if host == "" {
		host = r.Header.Get("Host")
	}
	// Handle IPv6 addresses: [::1]:port or [2001:db8::1]:8080
	if strings.HasPrefix(host, "[") {
		// IPv6 address in brackets
		if idx := strings.LastIndex(host, "]:"); idx != -1 {
			// Has port, extract just the bracketed address
			host = host[:idx+1]
		}
		// Remove brackets for cleaner hostname
		host = strings.TrimPrefix(host, "[")
		host = strings.TrimSuffix(host, "]")
		return strings.ToLower(host)
	}
	// IPv4 or hostname: remove port if present
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		host = host[:idx]
	}
	// Hostnames are case-insensitive per RFC; normalize to lowercase so
	// "Myapp.LOCALHOST" and "myapp.localhost" share a cache key.
	return strings.ToLower(host)
}

// timeNow returns the current time as ISO string
func timeNow() string {
	return time.Now().UTC().Format(time.RFC3339)
}
