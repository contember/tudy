//go:build darwin

package llm_resolver

import (
	"net"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
)

// NetworkTunnel detects docker-mac-net-connect on macOS and verifies
// per-target reachability before routing requests to container IPs.
//
// dmnc makes Docker container IPs reachable from the host by adding
// routes for Docker network subnets through its own utun interface.
// We cannot reliably identify "dmnc's utun" from the process — lsof
// on macOS doesn't expose the kernel control socket — and a competing
// VPN (e.g. WireGuard) can install routes for the very same Docker
// default subnets (172.16.0.0/12, 192.168.0.0/16), silently winning
// the route lookup and black-holing container traffic.
//
// So instead of fingerprinting dmnc's interface, we treat IsRunning
// as a hint and ask the kernel per-target via a short TCP probe.
// A successful TCP handshake is the best signal we can get cheaply,
// but it isn't ironclad: a transparent middlebox (SYN-proxy, NAT
// gateway, network appliance) can ACK the SYN even when the real
// destination is unreachable. For local-dev workflows on personal
// machines that's rare; in heavily managed corporate networks it
// can produce false positives.
//
// See: https://github.com/chipmk/docker-mac-net-connect
type NetworkTunnel struct {
	logger *zap.Logger

	// running indicates the dmnc process was detected at Start time.
	// atomic.Bool guards against any future caller invoking Start()
	// outside the single-threaded Provision lifecycle.
	running atomic.Bool

	mu    sync.Mutex
	cache map[string]reachabilityEntry

	// dedupe concurrent probes for the same target
	probeGroup singleflight.Group

	// probe is the function used to test reachability — overridable for tests.
	// Production callers do not reassign this field after construction.
	probe func(ip string, port int) bool
}

type reachabilityEntry struct {
	reachable bool
	checkedAt time.Time
}

const (
	// reachableTTL is short on purpose: container IPs change on restart,
	// and we have no upstream-failure hook to invalidate on demand, so
	// the only recovery from a stale "reachable=true" entry is the TTL.
	reachableTTL             = 10 * time.Second
	unreachableTTL           = 5 * time.Second
	reachabilityProbeTimeout = 400 * time.Millisecond
	reachabilityCacheMax     = 256
)

// NewNetworkTunnel creates a NetworkTunnel
func NewNetworkTunnel(logger *zap.Logger) *NetworkTunnel {
	return &NetworkTunnel{
		logger: logger,
		cache:  make(map[string]reachabilityEntry),
		probe:  probeTCP,
	}
}

// Start records whether the dmnc process is running. This is only a
// hint — actual routing decisions use IsReachable, which probes the
// destination so we don't get fooled by a VPN claiming Docker subnets.
func (nt *NetworkTunnel) Start() error {
	running := detectDockerMacNetConnect()
	nt.running.Store(running)
	if running {
		nt.logger.Info("docker-mac-net-connect process detected; reachability will be verified per-target")
	} else {
		nt.logger.Debug("docker-mac-net-connect not detected, using published port detection")
	}
	return nil
}

// Stop is a no-op — we don't own the tunnel process
func (nt *NetworkTunnel) Stop() {}

// IsRunning reports whether the dmnc process appeared to be active
// when Start ran. Best-effort; use IsReachable for routing decisions.
func (nt *NetworkTunnel) IsRunning() bool {
	return nt.running.Load()
}

// IsReachable verifies the given (ip, port) is reachable from the host
// via a short TCP probe. Results are cached briefly: successful probes
// for 10s, failed probes for 5s so a fixed VPN config is picked up quickly.
//
// This is the source of truth for whether to use a container IP
// directly: dmnc may be running but a colliding VPN can still
// black-hole the destination subnet.
//
// Note: this call blocks for up to reachabilityProbeTimeout on a cold
// cache. There is no caller-context cancellation — if the inbound
// request times out, the probe still runs to completion. The cache
// dampens repeat cost; the singleflight dedupes concurrent probes
// for the same target.
func (nt *NetworkTunnel) IsReachable(ip string, port int) bool {
	if ip == "" || port == 0 {
		return false
	}
	key := ip + ":" + strconv.Itoa(port)

	if r, ok := nt.lookupCache(key); ok {
		return r
	}

	// Dedupe concurrent probes for the same target — a stampede on a
	// cold cache (e.g. when the dashboard opens many containers at once)
	// would otherwise fire one TCP probe per request.
	result, _, _ := nt.probeGroup.Do(key, func() (result interface{}, err error) {
		// Defensive: don't let a panicking probe kill every waiter.
		// singleflight propagates panics to all callers; we'd rather
		// degrade to "unreachable" than crash N request goroutines.
		defer func() {
			if rec := recover(); rec != nil {
				nt.logger.Warn("reachability probe panicked, treating as unreachable",
					zap.Any("recover", rec),
					zap.String("target", key),
				)
				result = false
				err = nil
			}
		}()
		if r, ok := nt.lookupCache(key); ok {
			return r, nil
		}
		reachable := nt.probe(ip, port)
		nt.storeCache(key, reachable)
		return reachable, nil
	})
	if b, ok := result.(bool); ok {
		return b
	}
	return false
}

// InvalidateReachability clears any cached probe result for the given
// target and forgets any in-flight singleflight probe, so the next
// IsReachable call performs a fresh probe rather than receiving the
// pre-invalidation leader's result.
func (nt *NetworkTunnel) InvalidateReachability(ip string, port int) {
	key := ip + ":" + strconv.Itoa(port)
	nt.mu.Lock()
	delete(nt.cache, key)
	nt.mu.Unlock()
	// Forget the singleflight key so concurrent waiters don't receive
	// the pre-invalidate leader's result, and so a concurrent storeCache
	// from the in-flight probe can't re-populate the entry we just cleared.
	nt.probeGroup.Forget(key)
}

func (nt *NetworkTunnel) lookupCache(key string) (bool, bool) {
	nt.mu.Lock()
	defer nt.mu.Unlock()
	entry, ok := nt.cache[key]
	if !ok {
		return false, false
	}
	ttl := reachableTTL
	if !entry.reachable {
		ttl = unreachableTTL
	}
	if time.Since(entry.checkedAt) >= ttl {
		delete(nt.cache, key)
		return false, false
	}
	return entry.reachable, true
}

func (nt *NetworkTunnel) storeCache(key string, reachable bool) {
	nt.mu.Lock()
	defer nt.mu.Unlock()

	// Sweep expired entries when we approach the cap, and evict the
	// oldest if still over after the sweep. Keeps the map bounded even
	// if many distinct targets are probed over a long-running proxy.
	if len(nt.cache) >= reachabilityCacheMax {
		nt.evictLocked()
	}

	nt.cache[key] = reachabilityEntry{reachable: reachable, checkedAt: time.Now()}
}

// evictLocked drops expired entries and, if still over the cap, the
// oldest entry. Caller must hold nt.mu.
func (nt *NetworkTunnel) evictLocked() {
	now := time.Now()
	for k, e := range nt.cache {
		ttl := reachableTTL
		if !e.reachable {
			ttl = unreachableTTL
		}
		if now.Sub(e.checkedAt) >= ttl {
			delete(nt.cache, k)
		}
	}
	if len(nt.cache) < reachabilityCacheMax {
		return
	}
	var oldestKey string
	var oldestAt time.Time
	for k, e := range nt.cache {
		if oldestKey == "" || e.checkedAt.Before(oldestAt) {
			oldestKey = k
			oldestAt = e.checkedAt
		}
	}
	if oldestKey != "" {
		delete(nt.cache, oldestKey)
	}
}

// probeTCP attempts a TCP handshake to (ip, port) with a short timeout.
// Returns true if the handshake completes — see the NetworkTunnel doc
// comment for the caveat that this can be fooled by transparent
// middleboxes that SYN-ACK on behalf of the real destination.
func probeTCP(ip string, port int) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(ip, strconv.Itoa(port)), reachabilityProbeTimeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// detectDockerMacNetConnect returns true if the dmnc process is running.
//
// We use `pgrep` (without -f) so we match on the executable name only,
// not on substrings in unrelated processes' argv (e.g. an editor with
// a file named docker-mac-net-connect.go open). A previous version
// also inspected the routing table to filter false positives, but
// that gave the opposite false positive when a VPN had installed
// utun routes for 172.16.0.0/12 — exactly the subnets Docker defaults
// into — and we now defer the "is the tunnel actually usable" question
// to per-target probes in IsReachable.
func detectDockerMacNetConnect() bool {
	out, err := exec.Command("pgrep", "docker-mac-net-connect").Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return false
	}
	return true
}
