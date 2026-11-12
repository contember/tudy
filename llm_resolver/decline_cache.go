package llm_resolver

import (
	"sync"
	"time"
)

// defaultDeclineTTL is how long a decline ("nothing discovered plausibly
// serves this") is remembered before the model is asked again.
//
// Declines are deliberately not written to the mapping cache: there is no
// mapping to write, and inventing one is the exact failure the decline path
// exists to prevent. They still have to be remembered somewhere, because a
// declined hostname is usually a typo or a service that is not running, and
// clients retry those. A browser reload loop or a polling dev script would
// otherwise bill one LLM call per request.
//
// The TTL is short because the usual fix is starting the missing service. The
// picker re-runs discovery on every render, so a service started in the
// meantime shows up in its list right away; only automatic re-resolution waits
// out the TTL, and "?force" skips it entirely.
const defaultDeclineTTL = 30 * time.Second

// DeclineCache remembers recent declines per hostname. In-memory only: a
// decline describes what is running on this machine right now, which is not a
// decision worth persisting across restarts.
type DeclineCache struct {
	mu      sync.Mutex
	entries map[string]declineEntry
	ttl     time.Duration
}

type declineEntry struct {
	err     error
	expires time.Time
}

// NewDeclineCache creates a DeclineCache with the default TTL.
func NewDeclineCache() *DeclineCache {
	return &DeclineCache{
		entries: make(map[string]declineEntry),
		ttl:     defaultDeclineTTL,
	}
}

// Get returns the remembered decline for a hostname, or nil when there is
// none or it has expired.
func (c *DeclineCache) Get(hostname string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[hostname]
	if !ok {
		return nil
	}
	if time.Now().After(entry.expires) {
		delete(c.entries, hostname)
		return nil
	}
	return entry.err
}

// Set remembers a decline for the configured TTL.
func (c *DeclineCache) Set(hostname string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Sweep on write: declines are rare, and without this the map grows by one
	// entry per typo'd hostname for the life of the process.
	now := time.Now()
	for h, entry := range c.entries {
		if now.After(entry.expires) {
			delete(c.entries, h)
		}
	}

	c.entries[hostname] = declineEntry{err: err, expires: now.Add(c.ttl)}
}

// Delete forgets any decline for a hostname, e.g. once it has a real mapping.
func (c *DeclineCache) Delete(hostname string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, hostname)
}
