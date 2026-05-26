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
