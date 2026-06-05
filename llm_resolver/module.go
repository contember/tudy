package llm_resolver

import (
	"crypto/rand"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/sync/singleflight"
)

func init() {
	caddy.RegisterModule(LLMResolver{})
	httpcaddyfile.RegisterHandlerDirective("llm_resolver", parseCaddyfile)
	httpcaddyfile.RegisterDirectiveOrder("llm_resolver", httpcaddyfile.Before, "reverse_proxy")
}

// LLMResolver is a Caddy HTTP handler module that resolves hostnames
// to upstream targets using an LLM (via OpenRouter API).
type LLMResolver struct {
	// APIKey is the API key for the LLM API
	APIKey string `json:"api_key,omitempty"`

	// APIURL is the URL for the LLM API (default: https://openrouter.ai/api/v1/chat/completions)
	APIURL string `json:"api_url,omitempty"`

	// Model is the LLM model to use (default: anthropic/claude-haiku-4.5)
	Model string `json:"model,omitempty"`

	// LLMEnabled toggles the LLM resolution path without dropping the API
	// key ("true"/"false"/"on"/"off"/"1"/"0"; default enabled). When off,
	// resolution uses heuristics + the picker only. Falls back to the
	// LLM_ENABLED env var when unset (covers Caddyfiles generated before
	// this directive existed).
	LLMEnabled string `json:"llm_enabled,omitempty"`

	// CacheFile is the path to store hostname mappings (default: /data/mappings.json)
	CacheFile string `json:"cache_file,omitempty"`

	// ComposeProject is the name of our own compose project to filter out
	ComposeProject string `json:"compose_project,omitempty"`

	// logger is the Caddy logger
	logger *zap.Logger

	// cache is the mapping cache
	cache *Cache

	// processCache is short-lived cache for process discovery
	processCache *ProcessCache

	// resolver handles LLM API calls
	resolver *Resolver

	// resolveGroup deduplicates concurrent LLM requests for the same hostname
	resolveGroup singleflight.Group

	// networkTunnel manages WireGuard tunnel to Docker VM on macOS
	networkTunnel *NetworkTunnel

	// logBuffer captures recent log entries for the debug dashboard
	logBuffer *LogBuffer

	// stats tracks per-hostname request counts for the dashboard activity view
	stats *StatsTracker

	// pickerSecret signs the per-hostname tokens embedded in the service
	// picker page (see picker.go). Regenerated on every start.
	pickerSecret []byte

	// llmEnabled is the parsed LLMEnabled value (default true)
	llmEnabled bool
}

// llmAvailable reports whether the LLM resolution path can be used:
// an API key is configured and the path is not toggled off.
func (m *LLMResolver) llmAvailable() bool {
	return m.llmEnabled && m.resolver.HasAPIKey()
}

// parseEnabledFlag parses a human-friendly boolean ("true"/"false", "on"/"off",
// "1"/"0", "yes"/"no"). ok=false for empty or unrecognized input.
func parseEnabledFlag(s string) (value, ok bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "on", "1", "yes":
		return true, true
	case "false", "off", "0", "no":
		return false, true
	default:
		return false, false
	}
}

// CaddyModule returns the Caddy module information.
func (LLMResolver) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.llm_resolver",
		New: func() caddy.Module { return new(LLMResolver) },
	}
}

// Provision sets up the module.
func (m *LLMResolver) Provision(ctx caddy.Context) error {
	m.logBuffer = NewLogBuffer(500)
	m.logger = ctx.Logger().WithOptions(zap.WrapCore(func(c zapcore.Core) zapcore.Core {
		return zapcore.NewTee(c, NewBufferCore(m.logBuffer))
	}))

	// Set defaults
	if m.Model == "" {
		m.Model = "anthropic/claude-haiku-4.5"
	}
	if m.CacheFile == "" {
		m.CacheFile = "/data/mappings.json"
	}

	// Initialize cache
	m.cache = NewCache(m.CacheFile, m.logger)
	if err := m.cache.Load(); err != nil {
		m.logger.Warn("failed to load cache, starting fresh", zap.Error(err))
	}

	// Initialize process cache for dynamic port resolution
	m.processCache = NewProcessCache()

	// Initialize per-route request stats
	m.stats = NewStatsTracker()

	// Initialize resolver
	m.resolver = NewResolver(m.APIKey, m.APIURL, m.Model, m.ComposeProject, m.logger)

	// Parse the LLM on/off toggle (directive value, then env var, then on)
	rawEnabled := m.LLMEnabled
	if rawEnabled == "" {
		rawEnabled = os.Getenv("LLM_ENABLED")
	}
	m.llmEnabled = true
	if v, ok := parseEnabledFlag(rawEnabled); ok {
		m.llmEnabled = v
	} else if rawEnabled != "" {
		m.logger.Warn("unrecognized llm_enabled value, defaulting to enabled",
			zap.String("value", rawEnabled))
	}

	// Secret for picker form tokens (no-LLM / fallback routing UI)
	m.pickerSecret = make([]byte, 32)
	if _, err := rand.Read(m.pickerSecret); err != nil {
		return fmt.Errorf("failed to generate picker secret: %v", err)
	}

	// Initialize network tunnel for Docker VM access on macOS
	m.networkTunnel = NewNetworkTunnel(m.logger)
	if err := m.networkTunnel.Start(); err != nil {
		m.logger.Warn("failed to start network tunnel", zap.Error(err))
		// Non-fatal: proxy will still work with published ports
	}

	m.logger.Info("LLM resolver provisioned",
		zap.String("model", m.Model),
		zap.String("cache_file", m.CacheFile),
		zap.Bool("llm_available", m.llmAvailable()),
	)
	switch {
	case !m.resolver.HasAPIKey():
		m.logger.Info("no LLM API key configured — running in heuristic + picker mode")
	case !m.llmEnabled:
		m.logger.Info("LLM routing toggled off — running in heuristic + picker mode")
	}

	return nil
}

// Validate validates the module configuration.
func (m *LLMResolver) Validate() error {
	// Validate API URL if set
	if m.APIURL != "" {
		if _, err := url.Parse(m.APIURL); err != nil {
			return fmt.Errorf("invalid api_url: %v", err)
		}
	}

	// Validate cache file path is writable
	dir := filepath.Dir(m.CacheFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("cache directory not writable: %v", err)
	}

	// Test write access by creating a unique temp file (avoids races
	// between concurrent reloads or multiple tudy instances).
	f, err := os.CreateTemp(dir, ".tudy-write-test-*")
	if err != nil {
		return fmt.Errorf("cache file not writable: %v", err)
	}
	name := f.Name()
	f.Close()
	os.Remove(name)

	return nil
}

// Cleanup is called when the module is being unloaded.
func (m *LLMResolver) Cleanup() error {
	if m.networkTunnel != nil {
		m.networkTunnel.Stop()
	}
	return nil
}

// UnmarshalCaddyfile implements caddyfile.Unmarshaler.
func (m *LLMResolver) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		for d.NextBlock(0) {
			switch d.Val() {
			case "api_key":
				// Optional: `api_key {$LLM_API_KEY}` expands to no argument
				// when the env var is unset, which means no-LLM mode.
				if d.NextArg() {
					m.APIKey = d.Val()
				}
			case "api_url":
				if !d.NextArg() {
					return d.ArgErr()
				}
				m.APIURL = d.Val()
			case "model":
				if !d.NextArg() {
					return d.ArgErr()
				}
				m.Model = d.Val()
			case "llm_enabled":
				// Optional: `llm_enabled {$LLM_ENABLED:}` expands to no
				// argument when the env var is unset (default: enabled).
				if d.NextArg() {
					m.LLMEnabled = d.Val()
				}
			case "cache_file":
				if !d.NextArg() {
					return d.ArgErr()
				}
				m.CacheFile = d.Val()
			case "compose_project":
				if d.NextArg() {
					m.ComposeProject = d.Val()
				}
			default:
				return d.Errf("unknown subdirective '%s'", d.Val())
			}
		}
	}
	return nil
}

// parseCaddyfile sets up the handler from Caddyfile tokens.
func parseCaddyfile(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	var m LLMResolver
	err := m.UnmarshalCaddyfile(h.Dispenser)
	return &m, err
}

// Interface guards
var (
	_ caddy.Provisioner           = (*LLMResolver)(nil)
	_ caddy.Validator             = (*LLMResolver)(nil)
	_ caddy.CleanerUpper          = (*LLMResolver)(nil)
	_ caddyhttp.MiddlewareHandler = (*LLMResolver)(nil)
	_ caddyfile.Unmarshaler       = (*LLMResolver)(nil)
)
