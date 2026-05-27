package llm_resolver

import (
	"sync"
	"time"
)

const (
	statsBucketDuration = 10 * time.Second
	statsBuckets        = 30
	statsMaxHostnames   = 200
)

// RouteBucket holds request counts for a single time bucket.
type RouteBucket struct {
	Requests uint32 `json:"requests"`
	Errors   uint32 `json:"errors"`
}

// RouteStatsSnapshot is a point-in-time view of one route's stats.
type RouteStatsSnapshot struct {
	Hostname  string        `json:"hostname"`
	Buckets   []RouteBucket `json:"buckets"`
	WindowReq uint64        `json:"window_req"`
	WindowErr uint64        `json:"window_err"`
	TotalReq  uint64        `json:"total_req"`
	TotalErr  uint64        `json:"total_err"`
	LastSeen  time.Time     `json:"last_seen"`
}

// routeStats tracks per-hostname request stats over a sliding window.
// Each bucket records counts within statsBucketDuration; the slot is
// indexed by the absolute bucket number (unix-seconds / duration) so
// stale slots can be detected and zeroed lazily on first write.
type routeStats struct {
	mu            sync.Mutex
	buckets       [statsBuckets]RouteBucket
	bucketIdx     [statsBuckets]int64
	totalRequests uint64
	totalErrors   uint64
	lastSeen      time.Time
}

func bucketIndex(t time.Time) int64 {
	return t.Unix() / int64(statsBucketDuration.Seconds())
}

func slotFor(idx int64) int {
	n := int64(statsBuckets)
	return int(((idx % n) + n) % n)
}

func (s *routeStats) record(now time.Time, isErr bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := bucketIndex(now)
	slot := slotFor(idx)
	if s.bucketIdx[slot] != idx {
		s.buckets[slot] = RouteBucket{}
		s.bucketIdx[slot] = idx
	}
	s.buckets[slot].Requests++
	if isErr {
		s.buckets[slot].Errors++
	}
	s.totalRequests++
	if isErr {
		s.totalErrors++
	}
	s.lastSeen = now
}

func (s *routeStats) snapshot(hostname string, now time.Time) RouteStatsSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	currentIdx := bucketIndex(now)
	oldestIdx := currentIdx - int64(statsBuckets) + 1
	out := make([]RouteBucket, statsBuckets)
	var windowReq, windowErr uint64
	for i := 0; i < statsBuckets; i++ {
		idx := oldestIdx + int64(i)
		slot := slotFor(idx)
		if s.bucketIdx[slot] == idx {
			out[i] = s.buckets[slot]
			windowReq += uint64(s.buckets[slot].Requests)
			windowErr += uint64(s.buckets[slot].Errors)
		}
	}
	return RouteStatsSnapshot{
		Hostname:  hostname,
		Buckets:   out,
		WindowReq: windowReq,
		WindowErr: windowErr,
		TotalReq:  s.totalRequests,
		TotalErr:  s.totalErrors,
		LastSeen:  s.lastSeen,
	}
}

// StatsTracker holds per-hostname request stats.
type StatsTracker struct {
	mu     sync.Mutex
	routes map[string]*routeStats
}

func NewStatsTracker() *StatsTracker {
	return &StatsTracker{routes: make(map[string]*routeStats)}
}

// Record adds an event for hostname. A status code of 0 or >=500
// counts as an error; anything else counts as a successful request.
func (t *StatsTracker) Record(hostname string, statusCode int) {
	if hostname == "" {
		return
	}
	now := time.Now()
	isErr := statusCode == 0 || statusCode >= 500

	t.mu.Lock()
	rs, ok := t.routes[hostname]
	if !ok {
		if len(t.routes) >= statsMaxHostnames {
			t.evictOldestLocked()
		}
		rs = &routeStats{}
		t.routes[hostname] = rs
	}
	t.mu.Unlock()

	rs.record(now, isErr)
}

// Forget drops stats for a hostname (used when a mapping is deleted).
func (t *StatsTracker) Forget(hostname string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.routes, hostname)
}

// evictOldestLocked drops the route with the oldest lastSeen. Caller holds t.mu.
func (t *StatsTracker) evictOldestLocked() {
	var oldestKey string
	var oldestTime time.Time
	first := true
	for k, v := range t.routes {
		v.mu.Lock()
		last := v.lastSeen
		v.mu.Unlock()
		if first || last.Before(oldestTime) {
			oldestKey = k
			oldestTime = last
			first = false
		}
	}
	if oldestKey != "" {
		delete(t.routes, oldestKey)
	}
}

// Snapshot returns the current per-hostname snapshots.
func (t *StatsTracker) Snapshot() map[string]RouteStatsSnapshot {
	t.mu.Lock()
	routes := make(map[string]*routeStats, len(t.routes))
	for k, v := range t.routes {
		routes[k] = v
	}
	t.mu.Unlock()

	now := time.Now()
	out := make(map[string]RouteStatsSnapshot, len(routes))
	for k, v := range routes {
		out[k] = v.snapshot(k, now)
	}
	return out
}

// BucketSeconds returns the bucket duration in seconds (for client rendering).
func BucketSeconds() int { return int(statsBucketDuration.Seconds()) }

// BucketCount returns the number of buckets in the window.
func BucketCount() int { return statsBuckets }
