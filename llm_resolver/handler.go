package llm_resolver

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"sort"
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

	// Debug endpoint
	if hostname == "proxy.localhost" || r.URL.Path == "/_debug" {
		return m.handleDebug(w, r)
	}

	// Second-level proxy for inter-service communication
	if strings.HasPrefix(r.URL.Path, "/_proxy/") {
		return m.handleSecondLevelProxy(w, r, hostname, next)
	}

	// Ignore common browser requests
	if r.URL.Path == "/favicon.ico" || r.URL.Path == "/robots.txt" {
		w.WriteHeader(http.StatusNotFound)
		return nil
	}

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

		// Use singleflight to deduplicate concurrent requests for same hostname
		result, err, shared := m.resolveGroup.Do(hostname, func() (interface{}, error) {
			// Double-check cache inside singleflight (another request may have just finished)
			if cached := m.cache.Get(hostname); cached != nil && !force {
				return cached, nil
			}

			resolved, err := m.resolver.ResolveTarget(hostname, userPrompt, m.cache.GetAll())
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
			http.Error(w, fmt.Sprintf("Failed to resolve target: %v", err), http.StatusBadGateway)
			return nil
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
		// Use singleflight to deduplicate concurrent requests for same cache key
		result, err, shared := m.resolveGroup.Do(cacheKey, func() (interface{}, error) {
			// Double-check cache inside singleflight
			if cached := m.cache.Get(cacheKey); cached != nil && !force {
				return cached, nil
			}

			// Get origin mapping for context
			originMapping := m.cache.Get(originHostname)

			resolved, err := m.resolver.ResolveRelatedService(
				originHostname,
				originMapping,
				serviceName,
				userPrompt,
				m.cache.GetAll(),
			)
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

// handleDebug returns debug information
func (m *LLMResolver) handleDebug(w http.ResponseWriter, r *http.Request) error {
	// Check if HTML is requested
	acceptsHTML := strings.Contains(r.Header.Get("Accept"), "text/html")

	if acceptsHTML {
		return m.handleDebugHTML(w, r)
	}

	// Return JSON
	processes, _ := m.processCache.Get()
	containers, _ := DiscoverDockerContainers(m.ComposeProject)

	data := map[string]interface{}{
		"mappings":       m.cache.GetAll(),
		"model":          m.Model,
		"cache_file":     m.CacheFile,
		"processes":      processes,
		"containers":     containers,
		"docker_tunnel":  m.networkTunnel != nil && m.networkTunnel.IsRunning(),
	}

	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(data)
}

// debugMappingRow is a flattened mapping entry for template rendering
type debugMappingRow struct {
	Hostname  string
	Type      string
	Target    string
	Port      int
	LLMReason string
	TagClass  string
	PortEdit  bool
}

// debugLogRow is a flattened log entry for template rendering
type debugLogRow struct {
	Time     string
	Level    string
	TagClass string
	Message  string
	Details  string
}

// debugProcessRow is a flattened process entry for template rendering
type debugProcessRow struct {
	Port    int
	Command string
	Workdir string
}

// debugContainerRow is a flattened container entry for template rendering
type debugContainerRow struct {
	Name    string
	Image   string
	Ports   string
	IP      string
	Workdir string
}

// debugPageData is the data passed to debugTmpl
type debugPageData struct {
	Model                string
	CacheFile            string
	MappingCount         int
	ProcessCount         int
	ContainerCount       int
	LogCount             int
	Mappings             []debugMappingRow
	Processes            []debugProcessRow
	Containers           []debugContainerRow
	LogEntries           []debugLogRow
	AvailableTargetsJSON template.JS
}

// debugTmpl renders the dashboard HTML with contextual auto-escaping.
var debugTmpl = template.Must(template.New("debug").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>Tudy</title>
    <link rel="preconnect" href="https://fonts.googleapis.com">
    <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
    <link href="https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;500;600;700&family=DM+Sans:ital,wght@0,400;0,500;0,600;0,700;1,400&display=swap" rel="stylesheet">
    <style>
        :root {
            --bg: #0a0a08;
            --surface: #111110;
            --surface-raised: #18181a;
            --border: #222220;
            --border-subtle: #1a1a18;
            --text: #d4d0c8;
            --text-secondary: #8a8780;
            --text-muted: #5a5850;
            --accent: #d4a843;
            --accent-dim: #a07e30;
            --accent-glow: rgba(212, 168, 67, 0.08);
            --green: #7ec47e;
            --green-bg: rgba(126, 196, 126, 0.08);
            --blue: #7eaac4;
            --blue-bg: rgba(126, 170, 196, 0.08);
            --red: #c47e7e;
            --red-bg: rgba(196, 126, 126, 0.06);
            --mono: 'JetBrains Mono', 'SF Mono', 'Cascadia Code', monospace;
            --sans: 'DM Sans', system-ui, sans-serif;
        }
        *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
        body {
            font-family: var(--sans);
            background: var(--bg);
            color: var(--text);
            line-height: 1.6;
            -webkit-font-smoothing: antialiased;
            min-height: 100vh;
        }
        body::before {
            content: '';
            position: fixed;
            inset: 0;
            background:
                radial-gradient(ellipse 80% 60% at 50% 0%, rgba(212, 168, 67, 0.03) 0%, transparent 70%),
                radial-gradient(circle at 20% 80%, rgba(126, 170, 196, 0.02) 0%, transparent 50%);
            pointer-events: none;
            z-index: 0;
        }

        .layout {
            position: relative;
            z-index: 1;
            max-width: 1060px;
            margin: 0 auto;
            padding: 48px 32px 80px;
        }

        /* ---- Header ---- */
        .header {
            display: flex;
            align-items: center;
            justify-content: space-between;
            margin-bottom: 48px;
            animation: fadeIn 0.5s ease-out;
        }
        .header-left { display: flex; align-items: baseline; gap: 16px; }
        .wordmark {
            font-family: var(--mono);
            font-size: 15px;
            font-weight: 700;
            color: var(--accent);
            letter-spacing: -0.02em;
        }
        .wordmark span { color: var(--text-muted); font-weight: 400; }
        .status-dot {
            width: 7px; height: 7px;
            background: var(--green);
            border-radius: 50%;
            display: inline-block;
            margin-left: 2px;
            box-shadow: 0 0 6px rgba(126, 196, 126, 0.4);
            animation: pulse 3s ease-in-out infinite;
        }
        @keyframes pulse {
            0%, 100% { opacity: 1; }
            50% { opacity: 0.5; }
        }
        .header-actions { display: flex; gap: 6px; align-items: center; }
        .btn {
            display: inline-flex; align-items: center; gap: 6px;
            padding: 6px 12px;
            font-family: var(--mono);
            font-size: 11px;
            font-weight: 500;
            letter-spacing: 0.02em;
            border: 1px solid var(--border);
            border-radius: 6px;
            background: var(--surface);
            color: var(--text-secondary);
            cursor: pointer;
            transition: all 0.15s;
            text-transform: uppercase;
        }
        .btn:hover { border-color: var(--accent-dim); color: var(--accent); background: var(--accent-glow); }
        .btn svg { width: 12px; height: 12px; }

        /* ---- Config strip ---- */
        .config-strip {
            display: flex;
            gap: 24px;
            padding: 12px 0;
            margin-bottom: 40px;
            border-top: 1px solid var(--border-subtle);
            border-bottom: 1px solid var(--border-subtle);
            animation: fadeIn 0.5s ease-out 0.05s both;
        }
        .config-pair {
            display: flex; align-items: center; gap: 8px;
        }
        .config-key {
            font-family: var(--mono);
            font-size: 10px;
            font-weight: 600;
            color: var(--text-muted);
            text-transform: uppercase;
            letter-spacing: 0.08em;
        }
        .config-val {
            font-family: var(--mono);
            font-size: 12px;
            color: var(--text-secondary);
        }
        .config-sep {
            width: 1px;
            height: 16px;
            background: var(--border);
        }

        /* ---- Stats ---- */
        .stats {
            display: grid;
            grid-template-columns: repeat(4, 1fr);
            gap: 1px;
            background: var(--border-subtle);
            border: 1px solid var(--border);
            border-radius: 10px;
            overflow: hidden;
            margin-bottom: 48px;
            animation: fadeIn 0.5s ease-out 0.1s both;
        }
        .stat {
            background: var(--surface);
            padding: 20px 24px;
            position: relative;
        }
        .stat-num {
            font-family: var(--mono);
            font-size: 32px;
            font-weight: 700;
            color: #fff;
            letter-spacing: -0.03em;
            line-height: 1;
            margin-bottom: 4px;
        }
        .stat-label {
            font-family: var(--mono);
            font-size: 10px;
            font-weight: 500;
            color: var(--text-muted);
            text-transform: uppercase;
            letter-spacing: 0.1em;
        }

        /* ---- Sections ---- */
        .section {
            margin-bottom: 40px;
            animation: slideUp 0.4s ease-out both;
        }
        .section:nth-child(4) { animation-delay: 0.15s; }
        .section:nth-child(5) { animation-delay: 0.2s; }
        .section:nth-child(6) { animation-delay: 0.25s; }

        .section-head {
            display: flex;
            align-items: baseline;
            gap: 10px;
            margin-bottom: 10px;
            padding-bottom: 8px;
        }
        .section-title {
            font-family: var(--mono);
            font-size: 11px;
            font-weight: 700;
            color: var(--accent);
            text-transform: uppercase;
            letter-spacing: 0.1em;
        }
        .section-count {
            font-family: var(--mono);
            font-size: 11px;
            color: var(--text-muted);
        }
        .section-line {
            flex: 1;
            height: 1px;
            background: var(--border-subtle);
            margin-left: 8px;
        }

        /* ---- Tables ---- */
        .table-container {
            border: 1px solid var(--border);
            border-radius: 10px;
            overflow: hidden;
            background: var(--surface);
        }
        table { width: 100%; border-collapse: collapse; }
        thead th {
            font-family: var(--mono);
            font-size: 10px;
            font-weight: 600;
            color: var(--text-muted);
            text-transform: uppercase;
            letter-spacing: 0.08em;
            padding: 10px 16px;
            text-align: left;
            background: rgba(0,0,0,0.2);
            border-bottom: 1px solid var(--border);
        }
        thead th:first-child { padding-left: 20px; }
        tbody td {
            font-size: 13px;
            padding: 9px 16px;
            border-bottom: 1px solid var(--border-subtle);
            vertical-align: middle;
        }
        tbody td:first-child { padding-left: 20px; }
        tbody tr:last-child td { border-bottom: none; }
        tbody tr { transition: background 0.1s; }
        tbody tr:hover { background: rgba(255,255,255,0.015); }

        .cell-hostname {
            font-family: var(--mono);
            font-size: 12px;
            font-weight: 600;
            color: #fff;
        }
        .cell-hostname a {
            color: inherit;
            text-decoration: none;
            border-bottom: 1px solid transparent;
            transition: border-color 0.15s;
        }
        .cell-hostname a:hover { border-color: var(--accent-dim); }
        .cell-mono {
            font-family: var(--mono);
            font-size: 12px;
            color: var(--text-secondary);
        }
        .cell-dim {
            font-family: var(--mono);
            font-size: 12px;
            color: var(--text-muted);
        }
        .cell-reason {
            font-size: 12px;
            color: var(--text-muted);
            font-style: italic;
            max-width: 240px;
            overflow: hidden;
            text-overflow: ellipsis;
            white-space: nowrap;
        }
        .cell-cmd {
            font-family: var(--mono);
            font-size: 11px;
            color: var(--text-secondary);
            max-width: 380px;
            overflow: hidden;
            text-overflow: ellipsis;
            white-space: nowrap;
        }
        .cell-dir {
            font-family: var(--mono);
            font-size: 11px;
            color: var(--text-muted);
            max-width: 240px;
            overflow: hidden;
            text-overflow: ellipsis;
            white-space: nowrap;
            direction: rtl;
            text-align: left;
        }

        .tag {
            display: inline-flex;
            align-items: center;
            gap: 5px;
            font-family: var(--mono);
            font-size: 10px;
            font-weight: 600;
            padding: 3px 8px;
            border-radius: 4px;
            text-transform: uppercase;
            letter-spacing: 0.04em;
        }
        .tag::before {
            content: '';
            width: 5px; height: 5px;
            border-radius: 50%;
        }
        .tag-process { background: var(--green-bg); color: var(--green); }
        .tag-process::before { background: var(--green); }
        .tag-docker { background: var(--blue-bg); color: var(--blue); }
        .tag-docker::before { background: var(--blue); }
        .tag-info { background: var(--green-bg); color: var(--green); }
        .tag-info::before { background: var(--green); }
        .tag-warn { background: rgba(212, 168, 67, 0.1); color: var(--accent); }
        .tag-warn::before { background: var(--accent); }
        .tag-error { background: var(--red-bg); color: var(--red); }
        .tag-error::before { background: var(--red); }
        .tag-debug { background: rgba(90, 88, 80, 0.1); color: var(--text-muted); }
        .tag-debug::before { background: var(--text-muted); }
        .cell-details {
            font-family: var(--mono);
            font-size: 11px;
            color: var(--text-muted);
            max-width: 340px;
            overflow: hidden;
            text-overflow: ellipsis;
            white-space: nowrap;
        }

        .btn-del {
            display: inline-flex; align-items: center; justify-content: center;
            width: 26px; height: 26px;
            background: transparent;
            border: 1px solid transparent;
            border-radius: 5px;
            color: var(--text-muted);
            cursor: pointer;
            transition: all 0.15s;
        }
        .btn-del:hover {
            background: var(--red-bg);
            border-color: rgba(196, 126, 126, 0.15);
            color: var(--red);
        }
        .btn-del svg { width: 13px; height: 13px; }

        .cell-editable { cursor: pointer; position: relative; }
        .cell-editable:hover { background: rgba(212, 168, 67, 0.06); }
        .cell-editable input, .cell-editable select {
            font-family: var(--mono);
            font-size: 12px;
            background: var(--surface-raised);
            border: 1px solid var(--accent-dim);
            border-radius: 4px;
            color: var(--text);
            padding: 4px 8px;
            outline: none;
        }
        .cell-editable input { width: 80px; }
        .cell-editable select { width: 100%; min-width: 200px; }
        .cell-editable input:focus, .cell-editable select:focus {
            border-color: var(--accent);
            box-shadow: 0 0 0 2px rgba(212, 168, 67, 0.15);
        }

        .empty {
            padding: 36px 20px;
            text-align: center;
            font-family: var(--mono);
            font-size: 12px;
            color: var(--text-muted);
        }

        @keyframes fadeIn {
            from { opacity: 0; }
            to { opacity: 1; }
        }
        @keyframes slideUp {
            from { opacity: 0; transform: translateY(12px); }
            to { opacity: 1; transform: translateY(0); }
        }

        @media (max-width: 768px) {
            .layout { padding: 24px 16px 60px; }
            .stats { grid-template-columns: 1fr; }
            .config-strip { flex-direction: column; gap: 8px; }
            .config-sep { display: none; }
            .header { flex-direction: column; align-items: flex-start; gap: 12px; }
        }
    </style>
</head>
<body>
<div class="layout">
    <div class="header">
        <div class="header-left">
            <div class="wordmark">tudy <span>//</span> dashboard</div>
            <div class="status-dot" title="Running"></div>
        </div>
        <div class="header-actions">
            <button class="btn" onclick="location.reload()">
                <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M2.5 8a5.5 5.5 0 0 1 9.3-4"/><path d="M13.5 8a5.5 5.5 0 0 1-9.3 4"/><path d="M11.5 1.5v3h3"/><path d="M4.5 14.5v-3h-3"/></svg>
                Reload
            </button>
        </div>
    </div>

    <div class="config-strip">
        <div class="config-pair">
            <span class="config-key">Model</span>
            <span class="config-val">{{ .Model }}</span>
        </div>
        <div class="config-sep"></div>
        <div class="config-pair">
            <span class="config-key">Cache</span>
            <span class="config-val">{{ .CacheFile }}</span>
        </div>
    </div>

    <div class="stats">
        <div class="stat">
            <div class="stat-num">{{ .MappingCount }}</div>
            <div class="stat-label">Routes</div>
        </div>
        <div class="stat">
            <div class="stat-num">{{ .ProcessCount }}</div>
            <div class="stat-label">Processes</div>
        </div>
        <div class="stat">
            <div class="stat-num">{{ .ContainerCount }}</div>
            <div class="stat-label">Containers</div>
        </div>
        <div class="stat">
            <div class="stat-num">{{ .LogCount }}</div>
            <div class="stat-label">Logs</div>
        </div>
    </div>

    <div class="section">
        <div class="section-head">
            <span class="section-title">Route Mappings</span>
            <span class="section-count">{{ .MappingCount }}</span>
            <div class="section-line"></div>
        </div>
        <div class="table-container">
{{- if eq .MappingCount 0 }}
            <div class="empty">No mappings yet. Visit a *.localhost domain to create one.</div>
{{- else }}
            <table>
                <thead><tr><th>Hostname</th><th>Type</th><th>Target</th><th>Port</th><th>Reason</th><th></th></tr></thead>
                <tbody>
{{- range .Mappings }}
                <tr data-hostname="{{ .Hostname }}" data-type="{{ .Type }}" data-target="{{ .Target }}" data-port="{{ .Port }}">
                    <td class="cell-hostname"><a href="https://{{ .Hostname }}" target="_blank">{{ .Hostname }}</a></td>
                    <td><span class="tag {{ .TagClass }}">{{ .Type }}</span></td>
                    <td class="cell-mono cell-editable" onclick="editTarget(this)">{{ .Target }}</td>
                    {{- if .PortEdit }}
                    <td class="cell-dim cell-editable" onclick="editPort(this)">{{ .Port }}</td>
                    {{- else }}
                    <td class="cell-dim">{{ .Port }}</td>
                    {{- end }}
                    <td class="cell-reason" title="{{ .LLMReason }}">{{ .LLMReason }}</td>
                    <td><button class="btn-del" data-hostname="{{ .Hostname }}" onclick="deleteMapping(this.dataset.hostname)" title="Remove"><svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5"><line x1="4" y1="4" x2="12" y2="12"/><line x1="12" y1="4" x2="4" y2="12"/></svg></button></td>
                </tr>
{{- end }}
                </tbody>
            </table>
{{- end }}
        </div>
    </div>

    <div class="section">
        <div class="section-head">
            <span class="section-title">Local Processes</span>
            <span class="section-count">{{ .ProcessCount }}</span>
            <div class="section-line"></div>
        </div>
        <div class="table-container">
{{- if eq .ProcessCount 0 }}
            <div class="empty">No local processes detected.</div>
{{- else }}
            <table>
                <thead><tr><th>Port</th><th>Command</th><th>Directory</th></tr></thead>
                <tbody>
{{- range .Processes }}
                <tr>
                    <td class="cell-mono">{{ .Port }}</td>
                    <td class="cell-cmd" title="{{ .Command }}">{{ .Command }}</td>
                    <td class="cell-dir" title="{{ .Workdir }}">{{ .Workdir }}</td>
                </tr>
{{- end }}
                </tbody>
            </table>
{{- end }}
        </div>
    </div>

    <div class="section">
        <div class="section-head">
            <span class="section-title">Docker Containers</span>
            <span class="section-count">{{ .ContainerCount }}</span>
            <div class="section-line"></div>
        </div>
        <div class="table-container">
{{- if eq .ContainerCount 0 }}
            <div class="empty">No Docker containers detected.</div>
{{- else }}
            <table>
                <thead><tr><th>Name</th><th>Image</th><th>Ports</th><th>IP</th><th>Directory</th></tr></thead>
                <tbody>
{{- range .Containers }}
                <tr>
                    <td class="cell-hostname">{{ .Name }}</td>
                    <td class="cell-mono">{{ .Image }}</td>
                    <td class="cell-dim">{{ .Ports }}</td>
                    <td class="cell-mono">{{ .IP }}</td>
                    <td class="cell-dir" title="{{ .Workdir }}">{{ .Workdir }}</td>
                </tr>
{{- end }}
                </tbody>
            </table>
{{- end }}
        </div>
    </div>

    <div class="section">
        <div class="section-head">
            <span class="section-title">Recent Logs</span>
            <span class="section-count">{{ .LogCount }}</span>
            <div class="section-line"></div>
        </div>
        <div class="table-container">
{{- if eq .LogCount 0 }}
            <div class="empty">No log entries yet.</div>
{{- else }}
            <table>
                <thead><tr><th>Time</th><th>Level</th><th>Message</th><th>Details</th></tr></thead>
                <tbody>
{{- range .LogEntries }}
                <tr>
                    <td class="cell-dim">{{ .Time }}</td>
                    <td><span class="tag {{ .TagClass }}">{{ .Level }}</span></td>
                    <td class="cell-mono">{{ .Message }}</td>
                    <td class="cell-details" title="{{ .Details }}">{{ .Details }}</td>
                </tr>
{{- end }}
                </tbody>
            </table>
{{- end }}
        </div>
    </div>
</div>

<script>
    const availableTargets = {{ .AvailableTargetsJSON }};

    function editTarget(td) {
        if (td.querySelector('select')) return;
        const row = td.closest('tr');
        const current = row.dataset.target;
        const currentType = row.dataset.type;
        const originalText = td.textContent;

        const select = document.createElement('select');

        const currentOpt = document.createElement('option');
        currentOpt.value = '';
        currentOpt.textContent = current;
        currentOpt.selected = true;
        select.appendChild(currentOpt);

        const procs = availableTargets.filter(t => t.type === 'process');
        if (procs.length > 0) {
            const group = document.createElement('optgroup');
            group.label = 'Processes';
            procs.forEach(t => {
                const opt = document.createElement('option');
                opt.value = JSON.stringify(t);
                opt.textContent = t.label;
                group.appendChild(opt);
            });
            select.appendChild(group);
        }

        const dockers = availableTargets.filter(t => t.type === 'docker');
        if (dockers.length > 0) {
            const group = document.createElement('optgroup');
            group.label = 'Containers';
            dockers.forEach(t => {
                const opt = document.createElement('option');
                opt.value = JSON.stringify(t);
                opt.textContent = t.label;
                group.appendChild(opt);
            });
            select.appendChild(group);
        }

        td.textContent = '';
        td.appendChild(select);
        select.focus();

        const cancel = () => { td.textContent = originalText; };
        select.onchange = () => {
            if (select.value) saveMapping(row, JSON.parse(select.value));
            else cancel();
        };
        select.onkeydown = (e) => { if (e.key === 'Escape') cancel(); };
        select.onblur = () => setTimeout(() => { if (td.contains(select)) cancel(); }, 150);
    }

    function editPort(td) {
        if (td.querySelector('input')) return;
        const row = td.closest('tr');
        const current = parseInt(row.dataset.port);
        const originalText = td.textContent;

        const input = document.createElement('input');
        input.type = 'number';
        input.value = current;
        input.min = 1;
        input.max = 65535;

        td.textContent = '';
        td.appendChild(input);
        input.focus();
        input.select();

        const save = () => {
            const newPort = parseInt(input.value);
            if (newPort && newPort !== current) {
                saveMapping(row, {type: row.dataset.type, target: row.dataset.target, port: newPort});
            } else {
                td.textContent = originalText;
            }
        };
        input.onkeydown = (e) => {
            if (e.key === 'Enter') { e.preventDefault(); save(); }
            if (e.key === 'Escape') td.textContent = originalText;
        };
        input.onblur = save;
    }

    async function saveMapping(row, data) {
        const hostname = row.dataset.hostname;
        row.style.opacity = '0.5';
        row.style.transition = 'opacity 0.2s';
        const resp = await fetch('/_api/mappings/' + encodeURIComponent(hostname), {
            method: 'PUT',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({type: data.type, target: data.target, port: data.port}),
        });
        if (resp.ok) location.reload();
        else { row.style.opacity = '1'; alert('Failed to update mapping'); }
    }

    async function deleteMapping(hostname) {
        if (!confirm('Remove route mapping for ' + hostname + '?')) return;
        const row = event.target.closest('tr');
        if (row) { row.style.opacity = '0.3'; row.style.transition = 'opacity 0.2s'; }
        const resp = await fetch('/_api/mappings/' + encodeURIComponent(hostname), { method: 'DELETE' });
        if (resp.ok) location.reload();
        else { if (row) row.style.opacity = '1'; alert('Failed to delete mapping'); }
    }
</script>
</body>
</html>`))

// handleDebugHTML returns an HTML debug page
func (m *LLMResolver) handleDebugHTML(w http.ResponseWriter, r *http.Request) error {
	// Get discovery data for the page
	processes, _ := DiscoverLocalProcesses()
	containers, _ := DiscoverDockerContainers(m.ComposeProject)
	mappings := m.cache.GetAll()
	logEntries := m.logBuffer.Entries()

	// Sort processes by port ascending for deterministic display
	sortedProcesses := make([]LocalProcess, len(processes))
	copy(sortedProcesses, processes)
	sort.Slice(sortedProcesses, func(i, j int) bool {
		return sortedProcesses[i].Port < sortedProcesses[j].Port
	})

	// Sort containers by name for deterministic display
	sortedContainers := make([]DockerContainer, len(containers))
	copy(sortedContainers, containers)
	sort.Slice(sortedContainers, func(i, j int) bool {
		return sortedContainers[i].Name < sortedContainers[j].Name
	})

	// Build available targets for inline editing dropdown
	var availableTargets []map[string]interface{}
	for _, proc := range sortedProcesses {
		label := proc.Command
		if proc.Workdir != "" {
			label = proc.Workdir
		}
		availableTargets = append(availableTargets, map[string]interface{}{
			"type":   "process",
			"target": proc.Workdir,
			"port":   proc.Port,
			"label":  fmt.Sprintf(":%d  %s", proc.Port, label),
		})
	}
	for _, container := range sortedContainers {
		port := 0
		if len(container.Ports) > 0 {
			port = container.Ports[0]
		}
		availableTargets = append(availableTargets, map[string]interface{}{
			"type":   "docker",
			"target": container.Name,
			"port":   port,
			"label":  fmt.Sprintf("%s (%s)", container.Name, container.Image),
		})
	}
	// json.Marshal HTML-escapes <, >, & to </>/& by default,
	// which prevents </script> breakouts when embedded in an inline script.
	availableTargetsJSON, _ := json.Marshal(availableTargets)

	// Sort mappings by hostname for deterministic display.
	hostnames := make([]string, 0, len(mappings))
	for h := range mappings {
		hostnames = append(hostnames, h)
	}
	sort.Strings(hostnames)

	mappingRows := make([]debugMappingRow, 0, len(hostnames))
	for _, hostname := range hostnames {
		mapping := mappings[hostname]
		tagClass := "tag-process"
		if mapping.Type == "docker" {
			tagClass = "tag-docker"
		}
		mappingRows = append(mappingRows, debugMappingRow{
			Hostname:  hostname,
			Type:      mapping.Type,
			Target:    mapping.Target,
			Port:      mapping.Port,
			LLMReason: mapping.LLMReason,
			TagClass:  tagClass,
			PortEdit:  mapping.Type != "process",
		})
	}

	processRows := make([]debugProcessRow, 0, len(sortedProcesses))
	for _, proc := range sortedProcesses {
		cmd := proc.Args
		if cmd == "" {
			cmd = proc.Command
		}
		if len(cmd) > 100 {
			cmd = cmd[:100] + "..."
		}
		processRows = append(processRows, debugProcessRow{
			Port:    proc.Port,
			Command: cmd,
			Workdir: proc.Workdir,
		})
	}

	containerRows := make([]debugContainerRow, 0, len(sortedContainers))
	for _, container := range sortedContainers {
		ports := ""
		for i, p := range container.Ports {
			if i > 0 {
				ports += ", "
			}
			ports += fmt.Sprintf("%d", p)
		}
		containerRows = append(containerRows, debugContainerRow{
			Name:    container.Name,
			Image:   container.Image,
			Ports:   ports,
			IP:      container.IP,
			Workdir: container.Workdir,
		})
	}

	// Log entries: keep chronological order in the buffer, but emit newest first.
	logRows := make([]debugLogRow, 0, len(logEntries))
	for i := len(logEntries) - 1; i >= 0; i-- {
		entry := logEntries[i]
		tagClass := "tag-info"
		switch entry.Level {
		case "warn":
			tagClass = "tag-warn"
		case "error":
			tagClass = "tag-error"
		case "debug":
			tagClass = "tag-debug"
		}

		// Sort field keys so the rendered details are deterministic.
		keys := make([]string, 0, len(entry.Fields))
		for k := range entry.Fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		details := ""
		for _, k := range keys {
			if details != "" {
				details += " "
			}
			details += fmt.Sprintf("%s=%v", k, entry.Fields[k])
		}

		logRows = append(logRows, debugLogRow{
			Time:     entry.Time.Format(time.RFC3339),
			Level:    entry.Level,
			TagClass: tagClass,
			Message:  entry.Message,
			Details:  details,
		})
	}

	data := debugPageData{
		Model:                m.Model,
		CacheFile:            m.CacheFile,
		MappingCount:         len(mappings),
		ProcessCount:         len(processes),
		ContainerCount:       len(containers),
		LogCount:             len(logEntries),
		Mappings:             mappingRows,
		Processes:            processRows,
		Containers:           containerRows,
		LogEntries:           logRows,
		AvailableTargetsJSON: template.JS(availableTargetsJSON),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return debugTmpl.Execute(w, data)
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

	// Docker container — if network tunnel is active AND the destination is
	// actually reachable, use container IP directly. We probe reachability
	// because a competing VPN can claim Docker subnets and silently steal
	// traffic; the dmnc process being up isn't proof packets get there.
	var containerIP string
	var containerIPErr error

	if m.networkTunnel != nil && m.networkTunnel.IsRunning() && mapping.Port != 0 {
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
	if m.networkTunnel != nil && m.networkTunnel.IsRunning() && mapping.Port != 0 && !m.networkTunnel.IsReachable(containerIP, mapping.Port) {
		return "", fmt.Errorf("container %s at %s:%d not reachable (no published port and container IP not routable; a VPN may be claiming this Docker subnet)", mapping.Target, containerIP, mapping.Port)
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
