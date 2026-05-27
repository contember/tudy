package llm_resolver

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ----- Data shapes used by the templates and SSE -----

// debugLogRow is a flattened log entry for the logs sub-page.
// Request entries (Message=="request") get their fields pulled out into
// dedicated columns; other entries fall back to the generic Message/Details.
type debugLogRow struct {
	Time     string
	Level    string
	TagClass string

	IsRequest   bool
	Method      string
	Host        string
	Path        string
	Status      int
	StatusClass string
	Duration    string

	Message string
	Details string
}

// debugProcessRow is a flattened process entry for the discovery sub-page.
type debugProcessRow struct {
	Port    int
	Command string
	Workdir string
}

// debugContainerRow is a flattened container entry for the discovery sub-page.
type debugContainerRow struct {
	Name    string
	Image   string
	Ports   string
	IP      string
	Workdir string
}


// routeRow combines a mapping entry with its recent activity for the dashboard.
type routeRow struct {
	Hostname     string        `json:"hostname"`
	Type         string        `json:"type"`
	Target       string        `json:"target"`
	Port         int           `json:"port"`
	LLMReason    string        `json:"llmReason"`
	TagClass     string        `json:"tagClass"`
	PortEditable bool          `json:"portEditable"`
	HasMapping   bool          `json:"hasMapping"`
	Active       bool          `json:"active"`
	Buckets      []RouteBucket `json:"buckets"`
	WindowReq    uint64        `json:"windowReq"`
	WindowErr    uint64        `json:"windowErr"`
	TotalReq     uint64        `json:"totalReq"`
	TotalErr     uint64        `json:"totalErr"`
	LastSeenISO  string        `json:"lastSeen,omitempty"`
	LastSeenAgo  string        `json:"lastSeenAgo,omitempty"`
}

// availableTarget is one entry in the "change target" dropdown.
type availableTarget struct {
	Type   string `json:"type"`
	Target string `json:"target"`
	Port   int    `json:"port"`
	Label  string `json:"label"`
}

// heroStat is one tile in the hero banner's stat cluster.
type heroStat struct {
	Label   string
	Value   string
	Sub     string
	Color   string // "amber" | "green" | "blue" | "red"
	IconSVG template.HTML
}

// pageChrome is the data shared by every dashboard page (header, hero, banner).
// Each page-specific struct embeds this so the chrome templates can render
// against `.` regardless of the body data.
type pageChrome struct {
	Page         string // "activity" | "discovery" | "logs"
	Model        string
	CacheFile    string
	TunnelStatus string
	Welcome      string
	Headline     string
	HeadlineSub  string
	Stats        []heroStat
}

// Icon SVGs used inside hero stat cards. Stroke-only so they tint via CSS.
const (
	iconCalendar  = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="5" width="18" height="16" rx="2"/><path d="M3 10h18M8 3v4M16 3v4"/></svg>`
	iconPulse     = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M3 12h4l2-6 4 12 2-6h6"/></svg>`
	iconChart     = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M5 21V10M12 21V4M19 21v-7"/></svg>`
	iconAlert     = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M12 7v6M12 16.5v.5"/></svg>`
	iconCpu       = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><rect x="6" y="6" width="12" height="12" rx="2"/><path d="M9 1v4M15 1v4M9 19v4M15 19v4M1 9h4M1 15h4M19 9h4M19 15h4"/></svg>`
	iconContainer = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M3 8l9-4 9 4-9 4-9-4z"/><path d="M3 8v8l9 4 9-4V8"/><path d="M12 12v8"/></svg>`
	iconNetwork   = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18"/></svg>`
	iconFile      = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M14 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z"/><path d="M14 3v6h6M8 13h8M8 17h5"/></svg>`
	iconInfo      = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M12 11v5M12 8v.5"/></svg>`
	iconWarn      = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3l10 18H2L12 3z"/><path d="M12 10v5M12 18v.5"/></svg>`
)

// snapshotTotals are the aggregate numbers shown in the hero stat cards.
type snapshotTotals struct {
	Routes    int    `json:"routes"`
	Active    int    `json:"active"`
	WindowReq uint64 `json:"windowReq"`
	WindowErr uint64 `json:"windowErr"`
}

// dashboardSnapshot is the payload for the main dashboard, both for the
// server-side first render and for SSE updates.
type dashboardSnapshot struct {
	Model            string            `json:"model"`
	CacheFile        string            `json:"cacheFile"`
	TunnelStatus     string            `json:"tunnelStatus"`
	BucketSeconds    int               `json:"bucketSeconds"`
	BucketCount      int               `json:"bucketCount"`
	Routes           []routeRow        `json:"routes"`
	AvailableTargets []availableTarget `json:"availableTargets"`
	Totals           snapshotTotals    `json:"totals"`
}

// JSONForTemplate marshals the snapshot for embedding in inline script.
// json.Marshal HTML-escapes <, >, & by default so embedding inside
// <script> can't be used to break out of the tag.
func (s dashboardSnapshot) JSONForTemplate() template.JS {
	b, _ := json.Marshal(s)
	return template.JS(b)
}

// ----- Snapshot building -----

func (m *LLMResolver) buildDashboardSnapshot() dashboardSnapshot {
	mappings := m.cache.GetAll()
	processes, _ := m.processCache.Get()
	containers, _ := DiscoverDockerContainers(m.ComposeProject)

	sortedProcesses := make([]LocalProcess, len(processes))
	copy(sortedProcesses, processes)
	sort.Slice(sortedProcesses, func(i, j int) bool {
		return sortedProcesses[i].Port < sortedProcesses[j].Port
	})
	sortedContainers := make([]DockerContainer, len(containers))
	copy(sortedContainers, containers)
	sort.Slice(sortedContainers, func(i, j int) bool {
		return sortedContainers[i].Name < sortedContainers[j].Name
	})

	availableTargets := make([]availableTarget, 0, len(sortedProcesses)+len(sortedContainers))
	for _, proc := range sortedProcesses {
		label := proc.Command
		if proc.Workdir != "" {
			label = proc.Workdir
		}
		availableTargets = append(availableTargets, availableTarget{
			Type:   "process",
			Target: proc.Workdir,
			Port:   proc.Port,
			Label:  fmt.Sprintf(":%d  %s", proc.Port, label),
		})
	}
	for _, container := range sortedContainers {
		port := 0
		if len(container.Ports) > 0 {
			port = container.Ports[0]
		}
		availableTargets = append(availableTargets, availableTarget{
			Type:   "docker",
			Target: container.Name,
			Port:   port,
			Label:  fmt.Sprintf("%s (%s)", container.Name, container.Image),
		})
	}

	var statsSnap map[string]RouteStatsSnapshot
	if m.stats != nil {
		statsSnap = m.stats.Snapshot()
	}

	// Union of hostnames from mappings and recent activity.
	seen := make(map[string]struct{}, len(mappings)+len(statsSnap))
	for h := range mappings {
		seen[h] = struct{}{}
	}
	for h := range statsSnap {
		seen[h] = struct{}{}
	}
	hostnames := make([]string, 0, len(seen))
	for h := range seen {
		hostnames = append(hostnames, h)
	}
	sort.Strings(hostnames)

	now := time.Now()
	rows := make([]routeRow, 0, len(hostnames))
	for _, hostname := range hostnames {
		row := routeRow{Hostname: hostname}
		if mapping, ok := mappings[hostname]; ok && mapping != nil {
			row.HasMapping = true
			row.Type = mapping.Type
			row.Target = mapping.Target
			row.Port = mapping.Port
			row.LLMReason = mapping.LLMReason
			row.TagClass = "tag-process"
			if mapping.Type == "docker" {
				row.TagClass = "tag-docker"
			}
			row.PortEditable = mapping.Type != "process"
		}
		if snap, ok := statsSnap[hostname]; ok {
			row.Buckets = snap.Buckets
			row.WindowReq = snap.WindowReq
			row.WindowErr = snap.WindowErr
			row.TotalReq = snap.TotalReq
			row.TotalErr = snap.TotalErr
			row.Active = snap.WindowReq > 0
			if !snap.LastSeen.IsZero() {
				row.LastSeenISO = snap.LastSeen.Format(time.RFC3339)
				row.LastSeenAgo = humanizeAgo(now.Sub(snap.LastSeen))
			}
		}
		rows = append(rows, row)
	}

	tunnelStatus := ""
	if m.networkTunnel != nil && m.networkTunnel.IsRunning() && !m.networkTunnel.IsHealthy() {
		tunnelStatus = "broken"
	}

	totals := snapshotTotals{Routes: len(mappings)}
	for _, r := range rows {
		if r.Active {
			totals.Active++
		}
		totals.WindowReq += r.WindowReq
		totals.WindowErr += r.WindowErr
	}

	return dashboardSnapshot{
		Model:            m.Model,
		CacheFile:        m.CacheFile,
		TunnelStatus:     tunnelStatus,
		BucketSeconds:    BucketSeconds(),
		BucketCount:      BucketCount(),
		Routes:           rows,
		AvailableTargets: availableTargets,
		Totals:           totals,
	}
}

func humanizeAgo(d time.Duration) string {
	if d < 2*time.Second {
		return "just now"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

// ----- Page handlers -----

// handleProxyLocalhost routes requests addressed to proxy.localhost to the
// appropriate dashboard sub-page or SSE stream.
func (m *LLMResolver) handleProxyLocalhost(w http.ResponseWriter, r *http.Request) error {
	switch r.URL.Path {
	case "/_events":
		return m.handleEvents(w, r)
	case "/discovery":
		return m.handleDiscoveryHTML(w, r)
	case "/logs":
		return m.handleLogsHTML(w, r)
	}
	return m.handleDebug(w, r)
}

// handleDebug serves the main dashboard (HTML) or a JSON summary used by the CLI.
func (m *LLMResolver) handleDebug(w http.ResponseWriter, r *http.Request) error {
	if strings.Contains(r.Header.Get("Accept"), "text/html") {
		return m.handleDashboardHTML(w, r)
	}

	processes, _ := m.processCache.Get()
	containers, _ := DiscoverDockerContainers(m.ComposeProject)

	data := map[string]interface{}{
		"mappings":      m.cache.GetAll(),
		"model":         m.Model,
		"cache_file":    m.CacheFile,
		"processes":     processes,
		"containers":    containers,
		"docker_tunnel": m.networkTunnel != nil && m.networkTunnel.IsRunning(),
	}
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(data)
}

func (m *LLMResolver) handleDashboardHTML(w http.ResponseWriter, r *http.Request) error {
	snap := m.buildDashboardSnapshot()
	chrome := pageChrome{
		Page:         "activity",
		Model:        snap.Model,
		CacheFile:    snap.CacheFile,
		TunnelStatus: snap.TunnelStatus,
		Welcome:      "Proxy live",
		Headline:     "Here's what's happening on your dev box.",
		HeadlineSub:  "Routes and traffic over the last 5 min.",
		Stats: []heroStat{
			{Label: "Routes", Value: fmt.Sprintf("%d", snap.Totals.Routes), Sub: "configured", Color: "amber", IconSVG: template.HTML(iconCalendar)},
			{Label: "Active", Value: fmt.Sprintf("%d", snap.Totals.Active), Sub: "last 5 min", Color: "green", IconSVG: template.HTML(iconPulse)},
			{Label: "Requests", Value: fmt.Sprintf("%d", snap.Totals.WindowReq), Sub: "last 5 min", Color: "blue", IconSVG: template.HTML(iconChart)},
			{Label: "Errors", Value: fmt.Sprintf("%d", snap.Totals.WindowErr), Sub: "last 5 min", Color: "red", IconSVG: template.HTML(iconAlert)},
		},
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return pages.ExecuteTemplate(w, "dashboard", struct {
		pageChrome
		Snapshot dashboardSnapshot
	}{chrome, snap})
}

func (m *LLMResolver) handleDiscoveryHTML(w http.ResponseWriter, r *http.Request) error {
	processes, _ := DiscoverLocalProcesses()
	containers, _ := DiscoverDockerContainers(m.ComposeProject)
	sort.Slice(processes, func(i, j int) bool { return processes[i].Port < processes[j].Port })
	sort.Slice(containers, func(i, j int) bool { return containers[i].Name < containers[j].Name })

	processRows := make([]debugProcessRow, 0, len(processes))
	for _, proc := range processes {
		cmd := proc.Args
		if cmd == "" {
			cmd = proc.Command
		}
		if len(cmd) > 100 {
			cmd = cmd[:100] + "..."
		}
		processRows = append(processRows, debugProcessRow{
			Port:    proc.Port,
			Command: cmd,
			Workdir: proc.Workdir,
		})
	}
	containerRows := make([]debugContainerRow, 0, len(containers))
	for _, c := range containers {
		ports := ""
		for i, p := range c.Ports {
			if i > 0 {
				ports += ", "
			}
			ports += fmt.Sprintf("%d", p)
		}
		containerRows = append(containerRows, debugContainerRow{
			Name:    c.Name,
			Image:   c.Image,
			Ports:   ports,
			IP:      c.IP,
			Workdir: c.Workdir,
		})
	}

	tunnelStatus := ""
	tunnelHealthy := true
	if m.networkTunnel != nil && m.networkTunnel.IsRunning() && !m.networkTunnel.IsHealthy() {
		tunnelStatus = "broken"
		tunnelHealthy = false
	}

	tunnelValue := "off"
	tunnelColor := "blue"
	if m.networkTunnel != nil && m.networkTunnel.IsRunning() {
		if tunnelHealthy {
			tunnelValue = "live"
			tunnelColor = "green"
		} else {
			tunnelValue = "broken"
			tunnelColor = "red"
		}
	}

	chrome := pageChrome{
		Page:         "discovery",
		Model:        m.Model,
		CacheFile:    m.CacheFile,
		TunnelStatus: tunnelStatus,
		Welcome:      "Live discovery",
		Headline:     "Services running on this machine.",
		HeadlineSub:  "Local processes and Docker containers visible to the proxy.",
		Stats: []heroStat{
			{Label: "Processes", Value: fmt.Sprintf("%d", len(processRows)), Sub: "listening", Color: "green", IconSVG: template.HTML(iconCpu)},
			{Label: "Containers", Value: fmt.Sprintf("%d", len(containerRows)), Sub: "running", Color: "blue", IconSVG: template.HTML(iconContainer)},
			{Label: "Total", Value: fmt.Sprintf("%d", len(processRows)+len(containerRows)), Sub: "discoverable", Color: "amber", IconSVG: template.HTML(iconChart)},
			{Label: "Tunnel", Value: tunnelValue, Sub: "docker net", Color: tunnelColor, IconSVG: template.HTML(iconNetwork)},
		},
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return pages.ExecuteTemplate(w, "discovery", struct {
		pageChrome
		Processes  []debugProcessRow
		Containers []debugContainerRow
	}{chrome, processRows, containerRows})
}

func (m *LLMResolver) handleLogsHTML(w http.ResponseWriter, r *http.Request) error {
	filterHost := r.URL.Query().Get("host")
	filterFrom := parseUnixParam(r.URL.Query().Get("from"))
	filterTo := parseUnixParam(r.URL.Query().Get("to"))

	all := m.logBuffer.Entries()

	// First pass: filter by host to get the scope used for both timeline and rows.
	hostScoped := make([]LogEntry, 0, len(all))
	for _, e := range all {
		if filterHost != "" && !logEntryMatchesHost(e, filterHost) {
			continue
		}
		hostScoped = append(hostScoped, e)
	}

	timeline := buildLogTimeline(hostScoped, filterFrom, filterTo)

	// Second pass: also apply time filter and build display rows (newest first).
	rows := make([]debugLogRow, 0, len(hostScoped))
	for i := len(hostScoped) - 1; i >= 0; i-- {
		entry := hostScoped[i]
		if !filterFrom.IsZero() && entry.Time.Before(filterFrom) {
			continue
		}
		if !filterTo.IsZero() && !entry.Time.Before(filterTo) {
			continue
		}
		rows = append(rows, buildLogRow(entry))
	}

	tunnelStatus := ""
	if m.networkTunnel != nil && m.networkTunnel.IsRunning() && !m.networkTunnel.IsHealthy() {
		tunnelStatus = "broken"
	}

	var info, warn, errs int
	for _, e := range rows {
		switch e.Level {
		case "info":
			info++
		case "warn":
			warn++
		case "error":
			errs++
		}
	}

	welcome := "Recent activity"
	headline := "Proxy activity log."
	sub := fmt.Sprintf("Last %d log entries from the resolver and tunnel.", len(rows))
	switch {
	case filterHost != "" && !filterFrom.IsZero():
		welcome = "Filtered logs"
		headline = "Logs for " + filterHost
		sub = fmt.Sprintf("%d entries in the selected time window.", len(rows))
	case filterHost != "":
		welcome = "Filtered logs"
		headline = "Logs for " + filterHost
		sub = fmt.Sprintf("%d entries matching this host.", len(rows))
	case !filterFrom.IsZero():
		welcome = "Filtered logs"
		headline = "Logs in selected window"
		sub = fmt.Sprintf("%d entries in the selected time window.", len(rows))
	}

	chrome := pageChrome{
		Page:         "logs",
		Model:        m.Model,
		CacheFile:    m.CacheFile,
		TunnelStatus: tunnelStatus,
		Welcome:      welcome,
		Headline:     headline,
		HeadlineSub:  sub,
		Stats: []heroStat{
			{Label: "Total", Value: fmt.Sprintf("%d", len(rows)), Sub: "entries", Color: "amber", IconSVG: template.HTML(iconFile)},
			{Label: "Info", Value: fmt.Sprintf("%d", info), Sub: "informational", Color: "green", IconSVG: template.HTML(iconInfo)},
			{Label: "Warnings", Value: fmt.Sprintf("%d", warn), Sub: "soft failures", Color: "amber", IconSVG: template.HTML(iconWarn)},
			{Label: "Errors", Value: fmt.Sprintf("%d", errs), Sub: "hard failures", Color: "red", IconSVG: template.HTML(iconAlert)},
		},
	}

	timeFilterLabel := ""
	if !filterFrom.IsZero() && !filterTo.IsZero() {
		timeFilterLabel = fmt.Sprintf("%s → %s", filterFrom.Local().Format("15:04:05"), filterTo.Local().Format("15:04:05"))
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return pages.ExecuteTemplate(w, "logs", struct {
		pageChrome
		FilterHost      string
		FilterTimeLabel string
		ClearHostURL    string
		ClearTimeURL    string
		Timeline        *logTimeline
		LogEntries      []debugLogRow
	}{
		pageChrome:      chrome,
		FilterHost:      filterHost,
		FilterTimeLabel: timeFilterLabel,
		ClearHostURL:    clearQueryParams(r.URL, "host"),
		ClearTimeURL:    clearQueryParams(r.URL, "from", "to"),
		Timeline:        timeline,
		LogEntries:      rows,
	})
}

// buildLogRow converts a raw log entry into the template's row shape, pulling
// per-request fields out when the entry is a request log.
func buildLogRow(entry LogEntry) debugLogRow {
	tagClass := "tag-info"
	switch entry.Level {
	case "warn":
		tagClass = "tag-warn"
	case "error":
		tagClass = "tag-error"
	case "debug":
		tagClass = "tag-debug"
	}
	row := debugLogRow{
		Time:     entry.Time.Local().Format("15:04:05"),
		Level:    entry.Level,
		TagClass: tagClass,
		Message:  entry.Message,
	}
	if entry.Message == "request" {
		row.IsRequest = true
		row.Method = stringField(entry.Fields, "method")
		row.Host = stringField(entry.Fields, "host")
		row.Path = stringField(entry.Fields, "path")
		row.Status = intField(entry.Fields, "status")
		row.StatusClass = httpStatusClass(row.Status)
		if v, ok := entry.Fields["duration"]; ok {
			switch d := v.(type) {
			case time.Duration:
				row.Duration = formatDuration(d)
			case int64:
				row.Duration = formatDuration(time.Duration(d))
			case float64:
				row.Duration = formatDuration(time.Duration(d))
			case string:
				row.Duration = d
			}
		}
		return row
	}
	keys := make([]string, 0, len(entry.Fields))
	for k := range entry.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	details := ""
	for _, k := range keys {
		if details != "" {
			details += " "
		}
		details += fmt.Sprintf("%s=%v", k, entry.Fields[k])
	}
	row.Details = details
	return row
}

// clearQueryParams returns a "/logs?…" URL with the given query keys removed,
// preserving any others. Used for the per-chip clear links on the logs page.
func clearQueryParams(u *url.URL, keys ...string) string {
	q := u.Query()
	for _, k := range keys {
		q.Del(k)
	}
	if encoded := q.Encode(); encoded != "" {
		return "/logs?" + encoded
	}
	return "/logs"
}

// parseUnixParam parses a Unix-seconds query value; returns zero time on empty/invalid.
func parseUnixParam(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(n, 0)
}

// stringField pulls a string-valued zap field by key, returning "" if missing
// or of a different type.
func stringField(fields map[string]interface{}, key string) string {
	if v, ok := fields[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// intField pulls an int-valued zap field. zap encodes via MapObjectEncoder,
// which stores integers as int64.
func intField(fields map[string]interface{}, key string) int {
	if v, ok := fields[key]; ok {
		switch n := v.(type) {
		case int64:
			return int(n)
		case int:
			return n
		case float64:
			return int(n)
		}
	}
	return 0
}

// httpStatusClass returns the CSS class for a status code's hundreds bucket.
func httpStatusClass(s int) string {
	switch {
	case s >= 200 && s < 300:
		return "status-2xx"
	case s >= 300 && s < 400:
		return "status-3xx"
	case s >= 400 && s < 500:
		return "status-4xx"
	case s >= 500:
		return "status-5xx"
	default:
		return "status-other"
	}
}

// formatDuration renders a Duration in a compact, human-readable form.
func formatDuration(d time.Duration) string {
	if d < time.Microsecond {
		return "0ms"
	}
	if d < time.Millisecond {
		return fmt.Sprintf("%dμs", d.Microseconds())
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// ----- Logs timeline -----

// Timeline geometry in SVG viewBox units. Width is N*stride, height fixed.
const (
	timelineBucketTarget = 60 // approximate; actual count depends on aligned span
	timelineBucketMax    = 240
	timelineBarWidth     = 9
	timelineBarGap       = 1
	timelineViewBoxH     = 60
)

// niceBucketDurations are the snap sizes the timeline picks from so bucket
// boundaries stay aligned to clock ticks (e.g. exact 10-second multiples).
// This keeps the visual position of a bucket stable as new entries arrive.
var niceBucketDurations = []time.Duration{
	1 * time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second,
	15 * time.Second, 30 * time.Second,
	1 * time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute,
	15 * time.Minute, 30 * time.Minute,
	1 * time.Hour, 2 * time.Hour, 3 * time.Hour, 6 * time.Hour,
	12 * time.Hour, 24 * time.Hour,
}

func chooseBucketDur(span time.Duration) time.Duration {
	target := span / time.Duration(timelineBucketTarget)
	for _, d := range niceBucketDurations {
		if d >= target {
			return d
		}
	}
	return 24 * time.Hour
}

type logBucket struct {
	StartUnix int64
	EndUnix   int64
	Total     int
	Errors    int
	BarX      float64
	BarY      float64
	BarH      float64
	ErrY      float64
	ErrH      float64
	Selected  bool
}

type logTimeline struct {
	Buckets     []logBucket
	StartLabel  string
	EndLabel    string
	BucketCount int
	BarWidth    int
	BarStride   float64
	ViewBoxW    float64
	ViewBoxH    int
	HasErrors   bool
	BucketsJSON template.JS
}

// isErrorLogEntry returns true if the log entry represents a failure —
// either an error-level log or a 5xx/unset-status request log.
func isErrorLogEntry(e LogEntry) bool {
	if e.Level == "error" {
		return true
	}
	if e.Message == "request" {
		s := intField(e.Fields, "status")
		return s == 0 || s >= 500
	}
	return false
}

// buildLogTimeline aggregates entries into clock-aligned buckets spanning
// their min/max timestamps. Bucket boundaries land on multiples of the chosen
// "nice" duration (e.g. exact 10-second marks) so positions stay stable when
// new entries arrive. Returns nil if there's nothing useful to plot.
func buildLogTimeline(entries []LogEntry, fFrom, fTo time.Time) *logTimeline {
	if len(entries) < 2 {
		return nil
	}
	minT := entries[0].Time
	maxT := entries[0].Time
	for _, e := range entries {
		if e.Time.Before(minT) {
			minT = e.Time
		}
		if e.Time.After(maxT) {
			maxT = e.Time
		}
	}
	span := maxT.Sub(minT)
	if span < time.Second {
		return nil
	}

	bucketDur := chooseBucketDur(span)
	durSec := int64(bucketDur.Seconds())
	if durSec < 1 {
		durSec = 1
	}
	alignedStart := (minT.Unix() / durSec) * durSec
	alignedEnd := ((maxT.Unix() / durSec) + 1) * durSec
	N := int((alignedEnd - alignedStart) / durSec)
	if N < 1 {
		N = 1
	}
	if N > timelineBucketMax {
		N = timelineBucketMax
		alignedStart = alignedEnd - int64(N)*durSec
	}

	buckets := make([]logBucket, N)
	for i := 0; i < N; i++ {
		startUnix := alignedStart + int64(i)*durSec
		buckets[i].StartUnix = startUnix
		buckets[i].EndUnix = startUnix + durSec
	}

	maxCount := 0
	hasErrors := false
	for _, e := range entries {
		offsetSec := e.Time.Unix() - alignedStart
		if offsetSec < 0 {
			continue
		}
		idx := int(offsetSec / durSec)
		if idx >= N {
			continue
		}
		buckets[idx].Total++
		if isErrorLogEntry(e) {
			buckets[idx].Errors++
			hasErrors = true
		}
		if buckets[idx].Total > maxCount {
			maxCount = buckets[idx].Total
		}
	}

	usableH := float64(timelineViewBoxH - 2)
	stride := float64(timelineBarWidth + timelineBarGap)
	for i := range buckets {
		b := &buckets[i]
		b.BarX = float64(i) * stride
		if maxCount > 0 && b.Total > 0 {
			b.BarH = float64(b.Total) / float64(maxCount) * usableH
			if b.BarH < 1.5 {
				b.BarH = 1.5
			}
			b.BarY = float64(timelineViewBoxH) - b.BarH
		}
		if maxCount > 0 && b.Errors > 0 {
			b.ErrH = float64(b.Errors) / float64(maxCount) * usableH
			if b.ErrH < 1.5 {
				b.ErrH = 1.5
			}
			b.ErrY = float64(timelineViewBoxH) - b.ErrH
		}
		if !fFrom.IsZero() && !fTo.IsZero() {
			bStart := time.Unix(b.StartUnix, 0)
			bEnd := time.Unix(b.EndUnix, 0)
			if bStart.Before(fTo) && bEnd.After(fFrom) {
				b.Selected = true
			}
		}
	}

	// Compact array for the drag-select JS — index-aligned with rendered bars.
	jsBuckets := make([][2]int64, len(buckets))
	for i, b := range buckets {
		jsBuckets[i] = [2]int64{b.StartUnix, b.EndUnix}
	}
	jsJSON, _ := json.Marshal(jsBuckets)

	return &logTimeline{
		Buckets:     buckets,
		StartLabel:  time.Unix(alignedStart, 0).Local().Format("15:04:05"),
		EndLabel:    time.Unix(alignedEnd, 0).Local().Format("15:04:05"),
		BucketCount: N,
		BarWidth:    timelineBarWidth,
		BarStride:   stride,
		ViewBoxW:    float64(N) * stride,
		ViewBoxH:    timelineViewBoxH,
		HasErrors:   hasErrors,
		BucketsJSON: template.JS(jsJSON),
	}
}

// logEntryMatchesHost returns true if the entry's zap fields reference the
// given hostname under any of the keys the resolver uses for it.
func logEntryMatchesHost(entry LogEntry, host string) bool {
	for _, k := range []string{"host", "hostname", "origin", "cacheKey"} {
		if v, ok := entry.Fields[k]; ok {
			if s, ok := v.(string); ok && s == host {
				return true
			}
		}
	}
	return false
}

// handleEvents streams dashboard snapshots over Server-Sent Events.
func (m *LLMResolver) handleEvents(w http.ResponseWriter, r *http.Request) error {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return nil
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	send := func() bool {
		snap := m.buildDashboardSnapshot()
		b, err := json.Marshal(snap)
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	if !send() {
		return nil
	}

	ctx := r.Context()
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if !send() {
				return nil
			}
		}
	}
}
