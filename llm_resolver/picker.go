package llm_resolver

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"go.uber.org/zap"
)

// The service picker is the no-LLM (and LLM-failure) fallback for browser
// requests: instead of a bare 502, the unresolved hostname serves a page
// listing every discovered process and container, and the user routes the
// hostname with one click.
//
// The select endpoint mutates state, so it can't be left open like the
// read-only paths: the mappings API is locked to proxy.localhost, but the
// picker must work on the unresolved hostname itself. We bind each form to
// its hostname with an HMAC token derived from a per-run random secret —
// a page served for foo.localhost can only create a mapping for
// foo.localhost, and other origins can't read the token (no CORS grant).

const pickerSelectPath = "/_tudy/select"

// pickerToken returns the HMAC token authorizing mapping creation for a
// hostname. Tokens are stable for the lifetime of the proxy process.
func (m *LLMResolver) pickerToken(hostname string) string {
	mac := hmac.New(sha256.New, m.pickerSecret)
	mac.Write([]byte(strings.ToLower(hostname)))
	return hex.EncodeToString(mac.Sum(nil))
}

func (m *LLMResolver) pickerTokenValid(hostname, token string) bool {
	expected := m.pickerToken(hostname)
	return hmac.Equal([]byte(expected), []byte(token))
}

// pickerProcessRow is one selectable local process.
type pickerProcessRow struct {
	Port    int
	Command string
	Args    string
	Workdir string
}

// pickerContainerRow is one selectable (container, port) pair.
type pickerContainerRow struct {
	Name    string
	Image   string
	Port    int
	Workdir string
}

// containerPickerPorts lists the ports worth offering for a container:
// Dockerfile-exposed ports, falling back to distinct published container ports.
func containerPickerPorts(c *DockerContainer) []int {
	if len(c.Ports) > 0 {
		return c.Ports
	}
	seen := map[int]bool{}
	var ports []int
	for _, pm := range c.PortMappings {
		if !seen[pm.ContainerPort] {
			seen[pm.ContainerPort] = true
			ports = append(ports, pm.ContainerPort)
		}
	}
	return ports
}

// renderPickerPage serves the interactive service picker on an unresolved
// hostname. resolveErr explains why automatic resolution didn't happen.
// nextURI is the originally requested URI, restored after a pick so deep
// links survive the detour through the picker.
func (m *LLMResolver) renderPickerPage(w http.ResponseWriter, hostname, resolveErr, nextURI string) error {
	processes, err := DiscoverLocalProcesses()
	if err != nil {
		m.logger.Warn("picker: failed to discover processes", zap.Error(err))
	}
	containers, err := DiscoverDockerContainers(m.ComposeProject)
	if err != nil {
		m.logger.Warn("picker: failed to discover containers", zap.Error(err))
	}

	procRows := make([]pickerProcessRow, 0, len(processes))
	for _, p := range processes {
		procRows = append(procRows, pickerProcessRow{
			Port:    p.Port,
			Command: p.Command,
			Args:    p.Args,
			Workdir: p.Workdir,
		})
	}

	var contRows []pickerContainerRow
	for i := range containers {
		c := &containers[i]
		for _, port := range containerPickerPorts(c) {
			contRows = append(contRows, pickerContainerRow{
				Name:    c.Name,
				Image:   c.Image,
				Port:    port,
				Workdir: c.Workdir,
			})
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	// The hostname is genuinely unresolved, so keep error semantics for
	// anything probing this host — browsers render the body regardless.
	w.WriteHeader(http.StatusBadGateway)

	return pickerTemplate.Execute(w, struct {
		Hostname   string
		Error      string
		LLMHint    string
		Token      string
		SelectPath string
		Next       string
		Processes  []pickerProcessRow
		Containers []pickerContainerRow
	}{
		Hostname:   hostname,
		Error:      resolveErr,
		LLMHint:    m.llmHint(),
		Token:      m.pickerToken(hostname),
		SelectPath: pickerSelectPath,
		Next:       sanitizeNextURI(nextURI),
		Processes:  procRows,
		Containers: contRows,
	})
}

// sanitizeNextURI constrains a post-pick redirect target to a same-origin
// relative URI: must start with a single "/" (rejects absolute and
// protocol-relative URLs), and drops the force/prompt params — redirecting
// back to "?force" would immediately re-trigger resolution and land the
// user straight back in the picker.
func sanitizeNextURI(raw string) string {
	if raw == "" || raw[0] != '/' || strings.HasPrefix(raw, "//") || strings.HasPrefix(raw, "/\\") {
		return "/"
	}
	u, err := url.Parse(raw)
	if err != nil || u.IsAbs() || u.Host != "" {
		return "/"
	}
	q := u.Query()
	q.Del("force")
	q.Del("prompt")
	u.RawQuery = q.Encode()
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String()
}

// llmHint explains how to (re-)enable automatic LLM routing, or "" when the
// LLM path is already active.
func (m *LLMResolver) llmHint() string {
	switch {
	case !m.resolver.HasAPIKey():
		return "No LLM is configured — run `tudy setup` to enable automatic routing."
	case !m.llmEnabled:
		return "LLM routing is turned off — run `tudy llm on` to re-enable it."
	default:
		return ""
	}
}

// handlePickerSelect creates a mapping from a picker form submission.
// The token proves the form was served by us for this exact hostname.
func (m *LLMResolver) handlePickerSelect(w http.ResponseWriter, r *http.Request, hostname string) error {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return nil
	}
	if !strings.HasSuffix(hostname, ".localhost") {
		http.Error(w, "Picker is only available on *.localhost hostnames", http.StatusForbidden)
		return nil
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return nil
	}
	if !m.pickerTokenValid(hostname, r.PostFormValue("token")) {
		http.Error(w, "Invalid or expired token — reload the page and try again", http.StatusForbidden)
		return nil
	}

	mappingType := r.PostFormValue("type")
	target := r.PostFormValue("target")
	port, _ := strconv.Atoi(r.PostFormValue("port"))
	workdir := r.PostFormValue("workdir")

	if mappingType != "process" && mappingType != "docker" {
		http.Error(w, "Invalid type", http.StatusBadRequest)
		return nil
	}
	if target == "" || port < 1 || port > 65535 {
		http.Error(w, "Invalid target or port", http.StatusBadRequest)
		return nil
	}

	mapping := &RouteMapping{
		Type:      mappingType,
		Target:    target,
		Port:      port,
		CreatedAt: timeNow(),
		LLMReason: "Selected by user from picker",
	}
	if mappingType == "process" && workdir != "" {
		mapping.ProcessIdentifier = &ProcessIdentifier{Workdir: workdir}
	}

	m.cache.Set(hostname, mapping)
	if err := m.cache.Save(); err != nil {
		http.Error(w, "Failed to save mapping", http.StatusInternalServerError)
		return nil
	}

	m.logger.Info("mapping created from picker",
		zap.String("hostname", hostname),
		zap.String("type", mappingType),
		zap.String("target", target),
		zap.Int("port", port),
	)

	// Send the user back where they were headed. Sanitized again here —
	// the form value round-trips through the client, so it can't be trusted
	// just because we emitted it sanitized.
	http.Redirect(w, r, sanitizeNextURI(r.PostFormValue("next")), http.StatusSeeOther)
	return nil
}

// wantsHTML reports whether the request comes from a browser navigation
// (worth answering with the picker page instead of a plain-text error).
func wantsHTML(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

// pickerSuggestionText is the plain-text variant for non-browser clients
// (curl, fetch): the resolution error plus a short list of discovered
// services and how to fix it.
func (m *LLMResolver) pickerSuggestionText(hostname, resolveErr string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "No route for %s: %s\n\n", hostname, resolveErr)

	processes, _ := DiscoverLocalProcesses()
	containers, _ := DiscoverDockerContainers(m.ComposeProject)

	if len(processes)+len(containers) > 0 {
		b.WriteString("Discovered services:\n")
		for _, p := range processes {
			fmt.Fprintf(&b, "  - process :%d  %s  (%s)\n", p.Port, p.Command, p.Workdir)
		}
		for i := range containers {
			c := &containers[i]
			ports := containerPickerPorts(c)
			strs := make([]string, len(ports))
			for j, p := range ports {
				strs[j] = strconv.Itoa(p)
			}
			fmt.Fprintf(&b, "  - docker  %s  ports: %s  (image: %s)\n", c.Name, strings.Join(strs, ","), c.Image)
		}
		b.WriteString("\n")
	}

	fmt.Fprintf(&b, "Open http://%s/ in a browser to pick a service,\n", hostname)
	b.WriteString("manage routes at http://proxy.localhost/")
	switch {
	case !m.resolver.HasAPIKey():
		b.WriteString(",\nor configure an LLM for automatic routing: tudy setup")
	case !m.llmEnabled:
		b.WriteString(",\nor re-enable LLM routing: tudy llm on")
	}
	b.WriteString("\n")
	return b.String()
}

var pickerTemplate = template.Must(template.New("picker").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Pick a service — {{ .Hostname }}</title>
<style>
  :root { color-scheme: dark; }
  * { box-sizing: border-box; }
  body {
    margin: 0; padding: 40px 20px;
    background: #0c0e12; color: #e6e9ef;
    font: 15px/1.5 ui-sans-serif, -apple-system, "Segoe UI", sans-serif;
  }
  .wrap { max-width: 860px; margin: 0 auto; }
  h1 { font-size: 22px; margin: 0 0 4px; }
  h1 code { color: #7dd3fc; font-size: 20px; }
  .sub { color: #9aa3b2; margin: 0 0 24px; }
  .error {
    background: rgba(244, 63, 94, .08); border: 1px solid rgba(244, 63, 94, .35);
    color: #fda4af; border-radius: 10px; padding: 10px 14px; margin: 0 0 24px;
    font-size: 13px;
  }
  h2 { font-size: 13px; text-transform: uppercase; letter-spacing: .08em; color: #9aa3b2; margin: 28px 0 10px; }
  .card {
    display: flex; align-items: center; gap: 14px;
    background: #14171e; border: 1px solid #232834; border-radius: 12px;
    padding: 12px 16px; margin-bottom: 8px;
  }
  .port {
    font: 600 14px ui-monospace, monospace; color: #7dd3fc;
    background: rgba(125, 211, 252, .08); border-radius: 8px;
    padding: 6px 10px; white-space: nowrap;
  }
  .meta { flex: 1; min-width: 0; }
  .meta .name { font-weight: 600; }
  .meta .detail {
    color: #9aa3b2; font-size: 13px;
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  button {
    background: #2563eb; color: #fff; border: 0; border-radius: 8px;
    padding: 8px 16px; font: 600 13px inherit; cursor: pointer; white-space: nowrap;
  }
  button:hover { background: #1d4ed8; }
  .empty { color: #9aa3b2; font-size: 14px; padding: 8px 2px; }
  .foot { margin-top: 32px; color: #9aa3b2; font-size: 13px; }
  .foot a { color: #7dd3fc; }
</style>
</head>
<body>
<div class="wrap">
  <h1>No route yet for <code>{{ .Hostname }}</code></h1>
  <p class="sub">Pick the service this hostname should proxy to. The mapping is saved and used for all future requests.</p>
  {{ if .Error }}<div class="error">{{ .Error }}</div>{{ end }}

  <h2>Local processes</h2>
  {{ range .Processes }}
  <form class="card" method="POST" action="{{ $.SelectPath }}">
    <input type="hidden" name="token" value="{{ $.Token }}">
    <input type="hidden" name="next" value="{{ $.Next }}">
    <input type="hidden" name="type" value="process">
    <input type="hidden" name="target" value="localhost">
    <input type="hidden" name="port" value="{{ .Port }}">
    <input type="hidden" name="workdir" value="{{ .Workdir }}">
    <span class="port">:{{ .Port }}</span>
    <span class="meta">
      <span class="name">{{ .Command }}</span>
      <div class="detail">{{ if .Args }}{{ .Args }} · {{ end }}{{ .Workdir }}</div>
    </span>
    <button type="submit">Route here</button>
  </form>
  {{ else }}
  <div class="empty">No local processes with open ports found.</div>
  {{ end }}

  <h2>Docker containers</h2>
  {{ range .Containers }}
  <form class="card" method="POST" action="{{ $.SelectPath }}">
    <input type="hidden" name="token" value="{{ $.Token }}">
    <input type="hidden" name="next" value="{{ $.Next }}">
    <input type="hidden" name="type" value="docker">
    <input type="hidden" name="target" value="{{ .Name }}">
    <input type="hidden" name="port" value="{{ .Port }}">
    <span class="port">:{{ .Port }}</span>
    <span class="meta">
      <span class="name">{{ .Name }}</span>
      <div class="detail">{{ .Image }}{{ if .Workdir }} · {{ .Workdir }}{{ end }}</div>
    </span>
    <button type="submit">Route here</button>
  </form>
  {{ else }}
  <div class="empty">No Docker containers found.</div>
  {{ end }}

  <div class="foot">
    Manage all routes on the <a href="http://proxy.localhost/">tudy dashboard</a>.
    {{ if .LLMHint }}{{ .LLMHint }}{{ end }}
  </div>
</div>
</body>
</html>
`))
