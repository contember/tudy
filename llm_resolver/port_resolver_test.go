package llm_resolver

import (
	"testing"
	"time"
)

func cacheWith(processes ...LocalProcess) *ProcessCache {
	return &ProcessCache{
		processes:  processes,
		lastUpdate: time.Now(),
		ttl:        defaultProcessCacheTTL,
	}
}

func TestResolveProcessPort_ExactWorkdirBeatsHomeDirProcess(t *testing.T) {
	cache := cacheWith(
		proc(4096, "agent", "agent serve", "/home/dev"),
		proc(27821, "node", "dist/cli.js", "/home/dev/projects/dashboard"),
	)

	port, err := ResolveProcessPort(&ProcessIdentifier{Workdir: "/home/dev/projects/dashboard"}, cache)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if port != 27821 {
		t.Errorf("expected exact workdir match on 27821, got %d", port)
	}
}

func TestResolveProcessPort_HomeDirProcessIsNeverAFallback(t *testing.T) {
	cache := cacheWith(
		proc(4096, "agent", "agent serve", "/home/dev"),
		proc(3000, "node", "next dev", "/Users/dev"),
		proc(3001, "node", "next dev", "/root"),
	)

	if _, err := ResolveProcessPort(&ProcessIdentifier{Workdir: "/home/dev/projects/dashboard"}, cache); err == nil {
		t.Error("expected no match so the caller falls back to the cached port")
	}
}

func TestResolveProcessPort_ExactBeatsSubdirectory(t *testing.T) {
	cache := cacheWith(
		proc(3000, "node", "vite dev", "/home/dev/proj/web"),
		proc(5173, "node", "vite dev", "/home/dev/proj"),
	)

	port, err := ResolveProcessPort(&ProcessIdentifier{Workdir: "/home/dev/proj"}, cache)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if port != 5173 {
		t.Errorf("expected exact match on 5173, got %d", port)
	}
}

func TestResolveProcessPort_ClosestSubdirectoryWins(t *testing.T) {
	cache := cacheWith(
		proc(3000, "node", "vite dev", "/home/dev/proj/packages/web/spa"),
		proc(5173, "node", "vite dev", "/home/dev/proj/web"),
	)

	port, err := ResolveProcessPort(&ProcessIdentifier{Workdir: "/home/dev/proj"}, cache)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if port != 5173 {
		t.Errorf("expected the closest subdirectory (5173), got %d", port)
	}
}

func TestResolveProcessPort_ParentWorkdirWithinDistance(t *testing.T) {
	cache := cacheWith(proc(5173, "node", "vite dev", "/home/dev/proj"))

	port, err := ResolveProcessPort(&ProcessIdentifier{Workdir: "/home/dev/proj/apps/web"}, cache)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if port != 5173 {
		t.Errorf("expected parent workdir match on 5173, got %d", port)
	}
}

func TestResolveProcessPort_ParentWorkdirTooFarAway(t *testing.T) {
	cache := cacheWith(proc(5173, "node", "vite dev", "/home/dev/proj"))

	if _, err := ResolveProcessPort(&ProcessIdentifier{Workdir: "/home/dev/proj/apps/web/spa"}, cache); err == nil {
		t.Error("expected no match beyond maxParentDistance")
	}
}

func TestResolveProcessPort_LowestPortBreaksTies(t *testing.T) {
	cache := cacheWith(
		proc(5174, "node", "vite dev", "/home/dev/proj/api"),
		proc(5173, "node", "vite dev", "/home/dev/proj/web"),
	)

	port, err := ResolveProcessPort(&ProcessIdentifier{Workdir: "/home/dev/proj"}, cache)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if port != 5173 {
		t.Errorf("expected the lowest port (5173), got %d", port)
	}
}

func TestResolveProcessPort_CommandPatternFilters(t *testing.T) {
	cache := cacheWith(
		proc(3000, "node", "next dev", "/home/dev/proj"),
		proc(5432, "postgres", "postgres -D data", "/home/dev/proj"),
	)

	port, err := ResolveProcessPort(&ProcessIdentifier{Workdir: "/home/dev/proj", CommandPattern: "postgres"}, cache)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if port != 5432 {
		t.Errorf("expected the postgres process (5432), got %d", port)
	}
}
