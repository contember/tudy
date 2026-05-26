//go:build darwin

package llm_resolver

import (
	"errors"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
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

	// gens tracks a per-key generation counter, guarded by mu.
	// It exists to close a race between InvalidateReachability and an
	// in-flight probe: Forget on the singleflight group frees the slot
	// for new callers, but it cannot stop the in-flight closure from
	// calling storeCache. We snapshot the generation before the probe
	// runs and only persist the result if the generation is unchanged.
	//
	// gens is pruned alongside cache entries in evictLocked so the map
	// stays bounded with the cache itself.
	gens map[string]uint64

	// dedupe concurrent probes for the same target
	probeGroup singleflight.Group

	// probe is the function used to test reachability — overridable for tests.
	// Production callers do not reassign this field after construction.
	probe func(ip string, port int) bool

	// healthy reports whether dmnc's tunnel was reachable end-to-end on the
	// most recent health probe. Distinct from running: a dmnc process whose
	// VM-side WireGuard peer disappeared (typically after Docker Desktop
	// restart) keeps running but silently black-holes container traffic.
	// IsHealthy is the routing-time gate; running is just a coarse hint.
	healthy atomic.Bool

	// stopCh signals the health-probe goroutine to exit. stopOnce keeps
	// Stop idempotent under repeated calls.
	stopCh   chan struct{}
	stopOnce sync.Once

	// healthProbe is overridable for tests. Production callers do not
	// reassign this field after construction.
	healthProbe func() bool
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

	tunnelHealthProbeTimeout  = 500 * time.Millisecond
	tunnelHealthProbeInterval = 30 * time.Second
)

// tunnelHealthTarget is dmnc's WireGuard peer endpoint inside Docker
// Desktop's VM. A healthy tunnel rejects arbitrary ports with
// ECONNREFUSED (the VM kernel sends RST); a broken tunnel times out
// because packets reach an absent peer. Port 22 is conventional and
// not normally bound inside the VM.
//
// var instead of const so tests can redirect it to a known-closed port.
var tunnelHealthTarget = "10.33.33.2:22"

// NewNetworkTunnel creates a NetworkTunnel
func NewNetworkTunnel(logger *zap.Logger) *NetworkTunnel {
	return &NetworkTunnel{
		logger:      logger,
		cache:       make(map[string]reachabilityEntry),
		gens:        make(map[string]uint64),
		probe:       probeTCP,
		stopCh:      make(chan struct{}),
		healthProbe: probeDmncTunnel,
	}
}

// Start records whether the dmnc process is running and, if so, kicks
// off a periodic health probe against the tunnel's VM-side peer. The
// process being up is just a hint; routing decisions use IsHealthy plus
// the per-target IsReachable probe.
func (nt *NetworkTunnel) Start() error {
	running := detectDockerMacNetConnect()
	nt.running.Store(running)
	if !running {
		nt.logger.Debug("docker-mac-net-connect not detected, using published port detection")
		return nil
	}

	// Initial probe synchronously so the dashboard reflects state on first load.
	initial := nt.healthProbe()
	nt.healthy.Store(initial)
	if initial {
		nt.logger.Info("docker-mac-net-connect tunnel is healthy")
	} else {
		nt.logger.Warn("docker-mac-net-connect is running but the tunnel is broken; container IPs are unreachable until dmnc is restarted",
			zap.String("fix", "sudo brew services restart docker-mac-net-connect"),
		)
	}

	go nt.healthLoop()
	return nil
}

// Stop signals the health-probe goroutine to exit. The dmnc process
// itself is not owned by us.
func (nt *NetworkTunnel) Stop() {
	nt.stopOnce.Do(func() { close(nt.stopCh) })
}

// IsRunning reports whether the dmnc process appeared to be active
// when Start ran. Best-effort; use IsHealthy / IsReachable for routing.
func (nt *NetworkTunnel) IsRunning() bool {
	return nt.running.Load()
}

// IsHealthy reports whether the tunnel was reachable end-to-end on the
// most recent probe. A running-but-unhealthy tunnel happens after Docker
// Desktop restarts: dmnc keeps running on the host but its WireGuard
// peer inside the VM is gone, so packets leave but never return.
func (nt *NetworkTunnel) IsHealthy() bool {
	return nt.healthy.Load()
}

// healthLoop reassesses tunnel health periodically and logs state
// transitions so the breakage is visible the moment it happens — without
// waiting for a request to fail.
func (nt *NetworkTunnel) healthLoop() {
	t := time.NewTicker(tunnelHealthProbeInterval)
	defer t.Stop()
	for {
		select {
		case <-nt.stopCh:
			return
		case <-t.C:
			now := nt.healthProbe()
			prev := nt.healthy.Swap(now)
			if now == prev {
				continue
			}
			if now {
				nt.logger.Info("docker-mac-net-connect tunnel recovered")
			} else {
				nt.logger.Warn("docker-mac-net-connect tunnel broken; container IPs are unreachable until dmnc is restarted",
					zap.String("fix", "sudo brew services restart docker-mac-net-connect"),
				)
			}
		}
	}
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
		// Snapshot the generation before probing. If InvalidateReachability
		// runs while the probe is in flight, it will bump gens[key] and
		// storeCacheIfGen will drop our (now stale) result instead of
		// repopulating the entry we just cleared.
		startGen := nt.currentGen(key)
		reachable := nt.probe(ip, port)
		nt.storeCacheIfGen(key, reachable, startGen)
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
//
// Bumping gens[key] under the same lock that guards the cache delete is
// what closes the in-flight-probe race: an already-running probe captured
// the pre-bump generation, so its eventual storeCacheIfGen will see the
// mismatch and discard the (potentially stale) result instead of
// repopulating the entry we just cleared.
func (nt *NetworkTunnel) InvalidateReachability(ip string, port int) {
	key := ip + ":" + strconv.Itoa(port)
	nt.mu.Lock()
	nt.gens[key]++
	delete(nt.cache, key)
	nt.mu.Unlock()
	// Forget the singleflight key so new callers don't attach to the
	// pre-invalidate leader's result. The generation guard above is what
	// prevents the in-flight probe from repopulating the cache.
	nt.probeGroup.Forget(key)
}

// currentGen returns the generation counter for key. Cheap helper so the
// caller doesn't have to take the mutex inline at the probe site.
func (nt *NetworkTunnel) currentGen(key string) uint64 {
	nt.mu.Lock()
	defer nt.mu.Unlock()
	return nt.gens[key]
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

// storeCacheIfGen stores the probe result for key only when the per-key
// generation is still equal to startGen. A mismatch means
// InvalidateReachability ran while the probe was in flight, so the
// result is potentially stale and must not repopulate the entry the
// invalidator just cleared.
func (nt *NetworkTunnel) storeCacheIfGen(key string, reachable bool, startGen uint64) {
	nt.mu.Lock()
	defer nt.mu.Unlock()
	if nt.gens[key] != startGen {
		// Invalidated while we were probing — drop the result.
		return
	}
	nt.storeCacheLocked(key, reachable)
}

// storeCacheLocked is the shared cache-write path. Caller must hold nt.mu.
func (nt *NetworkTunnel) storeCacheLocked(key string, reachable bool) {
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
//
// gens entries are pruned alongside their cache counterparts so the
// generation map stays bounded together with the cache. A key with no
// cache entry has no in-flight probe attached (the probe runs to
// completion and either writes the cache or is discarded by the gen
// check), so dropping its gen counter cannot cause a stale write to
// slip through later.
func (nt *NetworkTunnel) evictLocked() {
	now := time.Now()
	for k, e := range nt.cache {
		ttl := reachableTTL
		if !e.reachable {
			ttl = unreachableTTL
		}
		if now.Sub(e.checkedAt) >= ttl {
			delete(nt.cache, k)
			delete(nt.gens, k)
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
		delete(nt.gens, oldestKey)
	}
}

// probeDmncTunnel tests whether dmnc's WireGuard tunnel is carrying
// traffic end-to-end. ECONNREFUSED proves the SYN reached the VM kernel
// and got back a RST; any other failure (timeout, no route) means the
// tunnel is broken. A successful connect would also count as alive, but
// the target port is intentionally one the VM doesn't normally serve.
func probeDmncTunnel() bool {
	conn, err := net.DialTimeout("tcp", tunnelHealthTarget, tunnelHealthProbeTimeout)
	if err == nil {
		_ = conn.Close()
		return true
	}
	return errors.Is(err, syscall.ECONNREFUSED)
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
