//go:build darwin

package llm_resolver

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

func newTestTunnel(probe func(ip string, port int) bool) *NetworkTunnel {
	nt := NewNetworkTunnel(zap.NewNop())
	if probe != nil {
		nt.probe = probe
	}
	return nt
}

func TestIsReachable_RejectsEmptyInput(t *testing.T) {
	nt := newTestTunnel(func(string, int) bool { return true })
	if nt.IsReachable("", 1234) {
		t.Fatal("empty IP should not be reachable")
	}
	if nt.IsReachable("1.2.3.4", 0) {
		t.Fatal("zero port should not be reachable")
	}
}

func TestIsReachable_CachesSuccess(t *testing.T) {
	var calls int32
	nt := newTestTunnel(func(string, int) bool {
		atomic.AddInt32(&calls, 1)
		return true
	})

	for i := 0; i < 5; i++ {
		if !nt.IsReachable("10.0.0.1", 80) {
			t.Fatalf("call %d: expected reachable", i)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 probe, got %d", got)
	}
}

func TestIsReachable_CachesFailure(t *testing.T) {
	var calls int32
	nt := newTestTunnel(func(string, int) bool {
		atomic.AddInt32(&calls, 1)
		return false
	})

	for i := 0; i < 3; i++ {
		if nt.IsReachable("10.0.0.1", 80) {
			t.Fatalf("call %d: expected unreachable", i)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 probe, got %d", got)
	}
}

func TestIsReachable_UnreachableTTLShorterThanReachable(t *testing.T) {
	if unreachableTTL >= reachableTTL {
		t.Fatalf("unreachableTTL (%s) should be shorter than reachableTTL (%s) so VPN fixes are picked up quickly", unreachableTTL, reachableTTL)
	}
}

func TestIsReachable_ExpiresAndReProbes(t *testing.T) {
	var calls int32
	nt := newTestTunnel(func(string, int) bool {
		atomic.AddInt32(&calls, 1)
		return false
	})

	nt.IsReachable("10.0.0.1", 80)
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 probe after first call, got %d", got)
	}

	// Force-expire the cache entry by rewinding its timestamp past the TTL.
	nt.mu.Lock()
	entry := nt.cache["10.0.0.1:80"]
	entry.checkedAt = time.Now().Add(-2 * unreachableTTL)
	nt.cache["10.0.0.1:80"] = entry
	nt.mu.Unlock()

	nt.IsReachable("10.0.0.1", 80)
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected 2 probes after expiry, got %d", got)
	}
}

func TestIsReachable_InvalidateForcesReprobe(t *testing.T) {
	var calls int32
	nt := newTestTunnel(func(string, int) bool {
		atomic.AddInt32(&calls, 1)
		return true
	})

	nt.IsReachable("10.0.0.1", 80)
	nt.InvalidateReachability("10.0.0.1", 80)
	nt.IsReachable("10.0.0.1", 80)

	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected 2 probes after invalidation, got %d", got)
	}
}

func TestIsReachable_DedupesConcurrentProbes(t *testing.T) {
	var calls int32
	gate := make(chan struct{})
	nt := newTestTunnel(func(string, int) bool {
		atomic.AddInt32(&calls, 1)
		<-gate
		return true
	})

	const concurrent = 20
	var wg sync.WaitGroup
	wg.Add(concurrent)
	for i := 0; i < concurrent; i++ {
		go func() {
			defer wg.Done()
			nt.IsReachable("10.0.0.1", 80)
		}()
	}

	// Let probes pile up on the singleflight gate, then release.
	time.Sleep(20 * time.Millisecond)
	close(gate)
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 probe due to singleflight, got %d", got)
	}
}

func TestIsReachable_InvalidateDuringInFlightProbeDoesNotRepopulate(t *testing.T) {
	// This test exercises the race the gens counter is designed to close:
	//
	//   1. Goroutine A enters probeGroup.Do — the probe blocks on `release`.
	//   2. The test calls InvalidateReachability, which bumps gens[key],
	//      deletes the cache entry, and Forgets the singleflight slot.
	//   3. The probe is unblocked; its (now-stale) result must NOT
	//      repopulate the cache, because the generation no longer matches.
	//
	// Before the fix, storeCache would unconditionally write back the
	// pre-invalidation result and the next caller would observe stale data.
	probing := make(chan struct{}, 8)
	release := make(chan bool, 8) // buffered so we can preload responses
	var calls int32
	nt := newTestTunnel(func(string, int) bool {
		atomic.AddInt32(&calls, 1)
		probing <- struct{}{}
		return <-release
	})

	// Kick off the in-flight probe (returns "true" once released).
	probeDone := make(chan bool, 1)
	go func() {
		probeDone <- nt.IsReachable("10.0.0.1", 80)
	}()

	// Wait until the probe is actually running inside the singleflight.
	<-probing

	// Invalidate while the probe is mid-flight. The probe captured the
	// old generation; the bump should make its eventual storeCacheIfGen
	// a no-op.
	nt.InvalidateReachability("10.0.0.1", 80)

	// Let the (stale) probe complete with result=true.
	release <- true
	if got := <-probeDone; !got {
		t.Fatal("expected first probe to return true (the value it produced before invalidation)")
	}

	// The cache must be empty — the stale write was dropped.
	nt.mu.Lock()
	_, present := nt.cache["10.0.0.1:80"]
	nt.mu.Unlock()
	if present {
		t.Fatal("cache was repopulated by an invalidated in-flight probe")
	}

	// Preload the second probe's response so the next IsReachable runs
	// to completion. Returning false distinguishes it from the stale
	// "true" we just discarded.
	release <- false
	if got := nt.IsReachable("10.0.0.1", 80); got {
		t.Fatal("expected fresh probe result (false), got the stale pre-invalidate result (true)")
	}
	<-probing // drain the second probe's signal
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected 2 probes (one in-flight, one fresh after invalidate), got %d", got)
	}
}

func TestIsReachable_GensPrunedOnEviction(t *testing.T) {
	// When a cache entry is evicted, its companion gens entry must be
	// dropped too — otherwise the gens map would leak entries for every
	// key that's ever been invalidated and re-probed over the lifetime
	// of the proxy.
	nt := newTestTunnel(func(string, int) bool { return true })

	// Seed both maps for a single key: probe (cache=present),
	// invalidate (gens[key]++, cache cleared), probe again
	// (cache=present, gens still bumped).
	nt.IsReachable("10.50.0.1", 80)
	nt.InvalidateReachability("10.50.0.1", 80)
	nt.IsReachable("10.50.0.1", 80)

	nt.mu.Lock()
	if _, ok := nt.cache["10.50.0.1:80"]; !ok {
		nt.mu.Unlock()
		t.Fatal("expected cache entry after probe")
	}
	if _, ok := nt.gens["10.50.0.1:80"]; !ok {
		nt.mu.Unlock()
		t.Fatal("expected gens entry after InvalidateReachability")
	}
	// Force the cache entry to look expired so the TTL sweep in
	// evictLocked will reap it.
	entry := nt.cache["10.50.0.1:80"]
	entry.checkedAt = time.Now().Add(-2 * reachableTTL)
	nt.cache["10.50.0.1:80"] = entry
	nt.mu.Unlock()

	// Fill enough to trigger evictLocked (which runs when len >= cap).
	for i := 0; i < reachabilityCacheMax; i++ {
		nt.IsReachable(fmt.Sprintf("10.51.%d.%d", i/256, i%256), 80)
	}

	nt.mu.Lock()
	_, cacheStillHas := nt.cache["10.50.0.1:80"]
	_, gensStillHas := nt.gens["10.50.0.1:80"]
	nt.mu.Unlock()

	if cacheStillHas {
		t.Fatal("expected cache entry to be evicted after TTL sweep")
	}
	if gensStillHas {
		t.Fatal("expected gens entry to be pruned alongside cache eviction")
	}
}

func TestIsReachable_CacheBoundedAndEvictsOldest(t *testing.T) {
	nt := newTestTunnel(func(string, int) bool { return true })

	// Fill cache past the cap.
	for i := 0; i < reachabilityCacheMax+50; i++ {
		nt.IsReachable(fmt.Sprintf("10.0.%d.%d", i/256, i%256), 80)
	}

	nt.mu.Lock()
	size := len(nt.cache)
	nt.mu.Unlock()

	if size > reachabilityCacheMax {
		t.Fatalf("cache exceeded cap: size=%d cap=%d", size, reachabilityCacheMax)
	}
}
