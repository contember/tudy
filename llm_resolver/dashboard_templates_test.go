package llm_resolver

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
)

func makeChrome(page, headline string) pageChrome {
	return pageChrome{
		Page:         page,
		Model:        "anthropic/claude-haiku-4.5",
		CacheFile:    "/data/mappings.json",
		TunnelStatus: "broken",
		Welcome:      "Proxy live",
		Headline:     headline,
		HeadlineSub:  "test sub",
		Stats: []heroStat{
			{Label: "Routes", Value: "5", Sub: "configured", Color: "amber", IconSVG: template.HTML(iconCalendar)},
			{Label: "Active", Value: "3", Sub: "last 5 min", Color: "green", IconSVG: template.HTML(iconPulse)},
			{Label: "Requests", Value: "142", Sub: "last 5 min", Color: "blue", IconSVG: template.HTML(iconChart)},
			{Label: "Errors", Value: "3", Sub: "last 5 min", Color: "red", IconSVG: template.HTML(iconAlert)},
		},
	}
}

func TestDashboardTemplateRenders(t *testing.T) {
	snap := dashboardSnapshot{
		Model:         "anthropic/claude-haiku-4.5",
		CacheFile:     "/data/mappings.json",
		TunnelStatus:  "broken",
		BucketSeconds: BucketSeconds(),
		BucketCount:   BucketCount(),
		Routes: []routeRow{
			{
				Hostname:    "api.localhost",
				Type:        "process",
				Target:      "/Users/me/dev/api",
				Port:        3000,
				LLMReason:   "node server on :3000",
				TagClass:    "tag-process",
				HasMapping:  true,
				Active:      true,
				WindowReq:   42,
				WindowErr:   1,
				TotalReq:    1234,
				TotalErr:    5,
				LastSeenAgo: "2s ago",
				LastSeenISO: "2026-05-26T12:00:00Z",
				Buckets:     make([]RouteBucket, BucketCount()),
			},
			{
				Hostname:     "idle.localhost",
				Type:         "docker",
				Target:       "web",
				Port:         80,
				HasMapping:   true,
				TagClass:     "tag-docker",
				PortEditable: true,
				Buckets:      make([]RouteBucket, BucketCount()),
			},
		},
		AvailableTargets: []availableTarget{
			{Type: "process", Target: "/tmp/api", Port: 3000, Label: ":3000  /tmp/api"},
		},
		Totals: snapshotTotals{Routes: 2, Active: 1, WindowReq: 42, WindowErr: 1},
	}

	var buf bytes.Buffer
	if err := pages.ExecuteTemplate(&buf, "dashboard", struct {
		pageChrome
		Snapshot dashboardSnapshot
	}{makeChrome("activity", "Here's what's happening on your dev box."), snap}); err != nil {
		t.Fatalf("dashboard render failed: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"tudy",
		"Docker tunnel broken",
		"anthropic/claude-haiku-4.5",
		`id="routes-panel"`,
		`new EventSource('/_events')`,
		"sparklineSVG",
		"stat-card",
		"Here&#39;s what&#39;s happening",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dashboard output missing %q", want)
		}
	}
}

func TestDiscoveryTemplateRenders(t *testing.T) {
	var buf bytes.Buffer
	if err := pages.ExecuteTemplate(&buf, "discovery", struct {
		pageChrome
		Processes  []debugProcessRow
		Containers []debugContainerRow
	}{
		pageChrome:  makeChrome("discovery", "Services running on this machine."),
		Processes:   []debugProcessRow{{Port: 5173, Command: "vite", Workdir: "/dev"}},
		Containers:  []debugContainerRow{{Name: "db", Image: "postgres", Ports: "5432", IP: "172.17.0.2", Workdir: ""}},
	}); err != nil {
		t.Fatalf("discovery render failed: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"Local Processes", "Docker Containers", "vite", "postgres"} {
		if !strings.Contains(out, want) {
			t.Errorf("discovery output missing %q", want)
		}
	}
}

func TestLogsTemplateRenders(t *testing.T) {
	var buf bytes.Buffer
	if err := pages.ExecuteTemplate(&buf, "logs", struct {
		pageChrome
		FilterHost      string
		FilterTimeLabel string
		ClearHostURL    string
		ClearTimeURL    string
		Timeline        *logTimeline
		LogEntries      []debugLogRow
	}{
		pageChrome: makeChrome("logs", "Proxy activity log."),
		LogEntries: []debugLogRow{
			{Time: "2026-05-26T12:00:00Z", Level: "info", TagClass: "tag-info", Message: "hello", Details: "k=v"},
		},
	}); err != nil {
		t.Fatalf("logs render failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Recent Logs") || !strings.Contains(out, "hello") {
		t.Errorf("logs output missing expected content")
	}
}

func TestStatsTracker(t *testing.T) {
	tr := NewStatsTracker()
	tr.Record("api.localhost", 200)
	tr.Record("api.localhost", 200)
	tr.Record("api.localhost", 502)
	tr.Record("admin.localhost", 200)

	snap := tr.Snapshot()
	a, ok := snap["api.localhost"]
	if !ok {
		t.Fatalf("api.localhost not in snapshot")
	}
	if a.TotalReq != 3 || a.TotalErr != 1 {
		t.Errorf("api totals = (%d, %d), want (3, 1)", a.TotalReq, a.TotalErr)
	}
	if a.WindowReq != 3 || a.WindowErr != 1 {
		t.Errorf("api window = (%d, %d), want (3, 1)", a.WindowReq, a.WindowErr)
	}
	if len(a.Buckets) != BucketCount() {
		t.Errorf("buckets len = %d, want %d", len(a.Buckets), BucketCount())
	}

	tr.Forget("api.localhost")
	if _, ok := tr.Snapshot()["api.localhost"]; ok {
		t.Errorf("Forget didn't remove the route")
	}
}
