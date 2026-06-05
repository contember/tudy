package llm_resolver

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// Heuristic (no-LLM) hostname resolution.
//
// The LLM is only needed for fuzzy cases; most real hostnames map onto a
// discovered service by exact, deterministic rules:
//
//	myapp.localhost      → process with workdir .../myapp, or container "myapp",
//	                       or compose service "myapp"
//	api.myapp.localhost  → compose service "api" in compose project "myapp",
//	                       or process in .../myapp/api, or process in .../myapp
//	                       whose command matches "api"
//
// All rules are exact string matches (case-insensitive). A mapping is only
// returned when exactly one candidate survives — ambiguity falls through to
// the LLM (when configured) or the interactive picker page.

// heuristicCandidate is one potential target found by the deterministic rules.
type heuristicCandidate struct {
	mapping *RouteMapping
	reason  string
}

// ResolveHeuristically tries to resolve a hostname against discovered
// services using exact matching rules. Returns nil when there is no match or
// when the match is ambiguous.
func ResolveHeuristically(hostname string, processes []LocalProcess, containers []DockerContainer) *RouteMapping {
	name := strings.TrimSuffix(strings.ToLower(hostname), ".localhost")
	if name == "" || name == strings.ToLower(hostname) {
		// Not a *.localhost hostname (or bare "localhost") — nothing to do.
		return nil
	}

	labels := strings.Split(name, ".")
	project := labels[len(labels)-1]
	service := ""
	if len(labels) >= 2 {
		service = labels[0]
	}

	var candidates []heuristicCandidate
	add := func(m *RouteMapping, reason string) {
		candidates = append(candidates, heuristicCandidate{mapping: m, reason: reason})
	}

	for i := range containers {
		c := &containers[i]
		composeProject := strings.ToLower(c.Labels["com.docker.compose.project"])
		composeService := strings.ToLower(c.Labels["com.docker.compose.service"])
		containerName := strings.ToLower(c.Name)

		matched := ""
		switch {
		case service == "" && containerName == project:
			matched = fmt.Sprintf("container name %q matches hostname", c.Name)
		case service == "" && composeService == project:
			matched = fmt.Sprintf("compose service %q matches hostname", composeService)
		case service != "" && composeProject == project && composeService == service:
			matched = fmt.Sprintf("compose service %q in project %q matches hostname", composeService, composeProject)
		case service != "" && containerName == service+"."+project:
			matched = fmt.Sprintf("container name %q matches hostname", c.Name)
		}
		if matched == "" {
			continue
		}

		port, ok := unambiguousContainerPort(c)
		if !ok {
			// Several exposed ports and no way to pick — let the LLM or the
			// user decide rather than guessing.
			continue
		}
		add(&RouteMapping{
			Type:   "docker",
			Target: c.Name,
			Port:   port,
		}, matched)
	}

	for i := range processes {
		p := &processes[i]
		if p.Workdir == "" {
			continue
		}
		base := strings.ToLower(filepath.Base(p.Workdir))
		parent := strings.ToLower(filepath.Base(filepath.Dir(p.Workdir)))

		matched := ""
		switch {
		case service == "" && base == project:
			matched = fmt.Sprintf("process workdir %s matches hostname", p.Workdir)
		case service != "" && base == service && parent == project:
			matched = fmt.Sprintf("process workdir %s matches %s.%s", p.Workdir, service, project)
		case service != "" && base == project && commandMatches(p, service):
			matched = fmt.Sprintf("process %q in workdir %s matches hostname", p.Command, p.Workdir)
		}
		if matched == "" {
			continue
		}

		add(&RouteMapping{
			Type:   "process",
			Target: "localhost",
			Port:   p.Port,
			ProcessIdentifier: &ProcessIdentifier{
				Workdir: p.Workdir,
			},
		}, matched)
	}

	candidates = dedupeCandidates(candidates)
	if len(candidates) != 1 {
		return nil
	}

	m := candidates[0].mapping
	m.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	m.LLMReason = "Heuristic match: " + candidates[0].reason
	return m
}

// ResolveRelatedHeuristically tries to resolve a related service (second-level
// proxy, e.g. "/_proxy/api/...") without the LLM. It looks for a container or
// process whose compose-service name / workdir basename equals the requested
// service name, preferring candidates that live in the same project directory
// as the origin mapping. Returns nil on no match or ambiguity.
func ResolveRelatedHeuristically(
	originMapping *RouteMapping,
	serviceName string,
	processes []LocalProcess,
	containers []DockerContainer,
) *RouteMapping {
	service := strings.ToLower(serviceName)
	if service == "" {
		return nil
	}

	originWorkdir := originProjectDir(originMapping, containers)

	var candidates []heuristicCandidate
	var sameProject []heuristicCandidate

	for i := range containers {
		c := &containers[i]
		composeService := strings.ToLower(c.Labels["com.docker.compose.service"])
		containerName := strings.ToLower(c.Name)
		if composeService != service && containerName != service {
			continue
		}
		port, ok := unambiguousContainerPort(c)
		if !ok {
			continue
		}
		cand := heuristicCandidate{
			mapping: &RouteMapping{Type: "docker", Target: c.Name, Port: port},
			reason:  fmt.Sprintf("compose service %q matches requested service", service),
		}
		candidates = append(candidates, cand)
		if originWorkdir != "" && c.Workdir != "" && sameProjectDir(originWorkdir, c.Workdir) {
			sameProject = append(sameProject, cand)
		}
	}

	for i := range processes {
		p := &processes[i]
		if p.Workdir == "" || strings.ToLower(filepath.Base(p.Workdir)) != service {
			continue
		}
		cand := heuristicCandidate{
			mapping: &RouteMapping{
				Type:              "process",
				Target:            "localhost",
				Port:              p.Port,
				ProcessIdentifier: &ProcessIdentifier{Workdir: p.Workdir},
			},
			reason: fmt.Sprintf("process workdir %s matches requested service", p.Workdir),
		}
		candidates = append(candidates, cand)
		if originWorkdir != "" && sameProjectDir(originWorkdir, p.Workdir) {
			sameProject = append(sameProject, cand)
		}
	}

	// Same-project candidates win; otherwise require a globally unique match.
	pool := dedupeCandidates(sameProject)
	if len(pool) != 1 {
		pool = dedupeCandidates(candidates)
	}
	if len(pool) != 1 {
		return nil
	}

	m := pool[0].mapping
	m.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	m.LLMReason = "Heuristic match: " + pool[0].reason
	return m
}

// unambiguousContainerPort picks the single sensible port for a container.
// Returns ok=false when the container exposes several ports and there's no
// way to choose deterministically.
func unambiguousContainerPort(c *DockerContainer) (int, bool) {
	if len(c.Ports) == 1 {
		return c.Ports[0], true
	}
	// Fall back to published ports: a single distinct container port wins.
	distinct := map[int]bool{}
	for _, pm := range c.PortMappings {
		distinct[pm.ContainerPort] = true
	}
	if len(distinct) == 1 {
		for port := range distinct {
			return port, true
		}
	}
	return 0, false
}

// commandMatches reports whether a process "looks like" the requested service
// by command name (e.g. service "vite" matches command "vite" or an args
// token like "node_modules/.bin/vite" cleaned to "vite").
func commandMatches(p *LocalProcess, service string) bool {
	if strings.EqualFold(p.Command, service) {
		return true
	}
	for _, tok := range strings.Fields(strings.ToLower(p.Args)) {
		if tok == service {
			return true
		}
	}
	return false
}

// originProjectDir extracts the project directory of the origin mapping:
// the process workdir for process mappings, or the compose working_dir of
// the origin container for docker mappings.
func originProjectDir(origin *RouteMapping, containers []DockerContainer) string {
	if origin == nil {
		return ""
	}
	if origin.ProcessIdentifier != nil && origin.ProcessIdentifier.Workdir != "" {
		return origin.ProcessIdentifier.Workdir
	}
	if origin.Type == "docker" {
		for i := range containers {
			if strings.EqualFold(containers[i].Name, origin.Target) {
				return containers[i].Workdir
			}
		}
	}
	return ""
}

// sameProjectDir reports whether two directories belong to the same project:
// equal, or one is nested under the other (process workdirs are often
// subdirectories of the compose project dir, e.g. ~/dev/proj/web vs ~/dev/proj).
func sameProjectDir(a, b string) bool {
	a = strings.TrimSuffix(strings.ToLower(a), "/")
	b = strings.TrimSuffix(strings.ToLower(b), "/")
	if a == "" || b == "" {
		return false
	}
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

// dedupeCandidates collapses candidates that point at the same target (the
// same container matched by two rules, or the same process port).
func dedupeCandidates(in []heuristicCandidate) []heuristicCandidate {
	seen := map[string]bool{}
	var out []heuristicCandidate
	for _, c := range in {
		key := fmt.Sprintf("%s|%s|%d", c.mapping.Type, strings.ToLower(c.mapping.Target), c.mapping.Port)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, c)
	}
	return out
}
