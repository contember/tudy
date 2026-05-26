package llm_resolver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.uber.org/zap"
)

// ProcessIdentifier identifies a process for dynamic port resolution
type ProcessIdentifier struct {
	Workdir        string `json:"workdir,omitempty"`        // Process working directory
	CommandPattern string `json:"commandPattern,omitempty"` // Optional regex to match command
}

// RouteMapping represents a hostname to target mapping
type RouteMapping struct {
	Type      string `json:"type"`      // "process" or "docker"
	Target    string `json:"target"`    // For process: "localhost", for docker: container name
	Port      int    `json:"port"`      // Target port number (hint/fallback for process type)
	CreatedAt string `json:"createdAt"` // ISO timestamp
	LLMReason string `json:"llmReason"` // AI reasoning for the mapping

	// ProcessIdentifier for dynamic port resolution (process type only)
	ProcessIdentifier *ProcessIdentifier `json:"processIdentifier,omitempty"`
}

// Mappings is a map of hostname to RouteMapping
type Mappings map[string]*RouteMapping

// Cache manages hostname to target mappings with persistence
type Cache struct {
	mu       sync.RWMutex
	saveMu   sync.Mutex
	mappings Mappings
	filePath string
	logger   *zap.Logger
}

// NewCache creates a new cache instance
func NewCache(filePath string, logger *zap.Logger) *Cache {
	return &Cache{
		mappings: make(Mappings),
		filePath: filePath,
		logger:   logger,
	}
}

// Load reads mappings from the cache file
func (c *Cache) Load() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	data, err := os.ReadFile(c.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			c.mappings = make(Mappings)
			return nil
		}
		return err
	}

	return json.Unmarshal(data, &c.mappings)
}

// Save writes mappings to the cache file atomically
func (c *Cache) Save() error {
	// Take a snapshot under the data lock so readers don't stall on disk I/O.
	c.mu.RLock()
	snapshot := make(Mappings, len(c.mappings))
	for k, v := range c.mappings {
		// Copy the mapping to avoid race conditions
		copy := *v
		snapshot[k] = &copy
	}
	c.mu.RUnlock()

	// Serialize concurrent Save calls so they don't race on the same .tmp path,
	// without blocking readers.
	c.saveMu.Lock()
	defer c.saveMu.Unlock()

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}

	// Ensure directory exists
	dir := filepath.Dir(c.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Write to temp file first, then rename for atomicity
	tmpFile := c.filePath + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		// Best-effort cleanup of any partial tmp file.
		_ = os.Remove(tmpFile)
		return err
	}

	if err := os.Rename(tmpFile, c.filePath); err != nil {
		// Best-effort cleanup; rename may have left the tmp file behind.
		_ = os.Remove(tmpFile)
		return err
	}
	return nil
}

// Get retrieves a mapping by hostname
func (c *Cache) Get(hostname string) *RouteMapping {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.mappings[hostname]
}

// Set stores a mapping for a hostname
func (c *Cache) Set(hostname string, mapping *RouteMapping) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if mapping.CreatedAt == "" {
		mapping.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	c.mappings[hostname] = mapping
}

// Delete removes a mapping
func (c *Cache) Delete(hostname string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.mappings, hostname)
}

// GetAll returns a copy of all mappings
func (c *Cache) GetAll() Mappings {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(Mappings, len(c.mappings))
	for k, v := range c.mappings {
		// Copy the mapping to avoid race conditions
		copy := *v
		result[k] = &copy
	}
	return result
}
