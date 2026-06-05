package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/contember/tudy/cmd/shared"
)

type Config struct {
	ConfigDir    string
	EnvFile      string
	CaddyFile    string
	BinaryPath   string
	DefaultURL   string
	DashboardURL string
}

var configPaths = []string{
	"/usr/local/etc/tudy",
}

var binaryPaths = []string{
	"/usr/local/bin/tudy-bin",
}

// defaultConfigDir returns the preferred default config directory for the platform.
func defaultConfigDir() string {
	// Prefer /usr/local/etc/tudy on macOS and Linux
	return "/usr/local/etc/tudy"
}

// LoadConfig detects the installation and loads configuration.
// If no config directory exists, it picks a default and creates it.
// Supports TUDY_CONFIG_DIR and TUDY_BIN env vars for overrides.
func LoadConfig() (*Config, error) {
	config := &Config{
		DashboardURL: "https://proxy.localhost",
		DefaultURL:   "https://proxy.localhost",
	}

	// Resolve config directory
	if dir := os.Getenv("TUDY_CONFIG_DIR"); dir != "" {
		absDir, err := filepath.Abs(dir)
		if err != nil {
			return nil, fmt.Errorf("invalid TUDY_CONFIG_DIR: %w", err)
		}
		config.ConfigDir = absDir
	} else {
		// Try known paths first
		for _, path := range configPaths {
			if _, err := os.Stat(path); err == nil {
				config.ConfigDir = path
				break
			}
		}
		// Fall back to default — will be created by setup if needed
		if config.ConfigDir == "" {
			config.ConfigDir = defaultConfigDir()
		}
	}
	config.EnvFile = filepath.Join(config.ConfigDir, "env")
	config.CaddyFile = filepath.Join(config.ConfigDir, "Caddyfile")

	// Resolve binary path
	if bin := os.Getenv("TUDY_BIN"); bin != "" {
		absBin, err := filepath.Abs(bin)
		if err != nil {
			return nil, fmt.Errorf("invalid TUDY_BIN: %w", err)
		}
		config.BinaryPath = absBin
	} else {
		// Try known paths
		for _, path := range binaryPaths {
			if _, err := os.Stat(path); err == nil {
				config.BinaryPath = path
				break
			}
		}
		// Try tudy-bin or caddy next to the running tudy binary
		if config.BinaryPath == "" {
			if self, err := os.Executable(); err == nil {
				dir := filepath.Dir(self)
				for _, name := range []string{"tudy-bin", "caddy"} {
					sibling := filepath.Join(dir, name)
					if _, err := os.Stat(sibling); err == nil {
						config.BinaryPath = sibling
						break
					}
				}
			}
		}
		// Try finding caddy in PATH as last resort
		if config.BinaryPath == "" {
			if caddy, err := exec.LookPath("caddy"); err == nil {
				config.BinaryPath = caddy
			}
		}
		if config.BinaryPath == "" {
			return nil, fmt.Errorf("tudy-bin not found (install it or set TUDY_BIN)")
		}
	}

	// Load default URL from env if present
	if url := config.GetEnvValue("DEFAULT_URL"); url != "" {
		config.DefaultURL = url
	}

	return config, nil
}

// DataDir returns the data directory path for this config.
func (c *Config) DataDir() string {
	if strings.HasPrefix(c.ConfigDir, "/usr/local") {
		return "/usr/local/var/lib/tudy"
	}
	return filepath.Join(c.ConfigDir, "data")
}

// EnsureConfigDir creates the config directory, data directory, and default files if they don't exist.
func (c *Config) EnsureConfigDir() error {
	if err := os.MkdirAll(c.ConfigDir, 0755); err != nil {
		return fmt.Errorf("failed to create config dir %s: %w", c.ConfigDir, err)
	}

	dataDir := c.DataDir()
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data dir %s: %w", dataDir, err)
	}

	// Create or fix Caddyfile
	caddyfileContent, err := os.ReadFile(c.CaddyFile)
	needsRewrite := err != nil ||
		strings.Contains(string(caddyfileContent), "{env.CADDY_DATA_DIR}") ||
		!strings.Contains(string(caddyfileContent), "skip_install_trust")
	if needsRewrite {
		caddyfile := generateCaddyfile(dataDir)
		if err := os.WriteFile(c.CaddyFile, []byte(caddyfile), 0644); err != nil {
			return fmt.Errorf("failed to write Caddyfile: %w", err)
		}
	}

	// Create env file if missing
	if _, err := os.Stat(c.EnvFile); os.IsNotExist(err) {
		if err := os.WriteFile(c.EnvFile, []byte("# Tudy configuration\n"), 0644); err != nil {
			return fmt.Errorf("failed to write env file: %w", err)
		}
	}

	// Ensure CADDY_DATA_DIR is set in env file
	if c.GetEnvValue("CADDY_DATA_DIR") == "" {
		if err := c.SetEnvValue("CADDY_DATA_DIR", dataDir); err != nil {
			return fmt.Errorf("failed to set CADDY_DATA_DIR: %w", err)
		}
	}

	return nil
}

func generateCaddyfile(dataDir string) string {
	return fmt.Sprintf(`{
	storage file_system {
		root %s
	}
	local_certs
	skip_install_trust
	on_demand_tls {
		ask http://127.0.0.1:80/_tls_check
	}
	auto_https disable_redirects
}

:80 {
	@tls_check path /_tls_check
	handle @tls_check {
		respond "OK" 200
	}
	handle {
		redir https://{host}{uri} permanent
	}
}

:443 {
	tls {
		on_demand
	}

	llm_resolver {
		api_key {$LLM_API_KEY}
		llm_enabled {$LLM_ENABLED:true}
		cache_file %s/mappings.json
	}

	reverse_proxy {http.vars.upstream}
}
`, dataDir, dataDir)
}

func (c *Config) GetEnvValue(key string) string {
	return shared.GetEnvValue(c.EnvFile, key)
}

func (c *Config) SetEnvValue(key, value string) error {
	return shared.SetEnvValue(c.EnvFile, key, value, shared.CopyFileWithAdmin)
}

func (c *Config) GetAPIKey() string {
	return c.GetEnvValue("LLM_API_KEY")
}

func (c *Config) SetAPIKey(key string) error {
	return c.SetEnvValue("LLM_API_KEY", key)
}
