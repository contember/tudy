package llm_resolver

import (
	"testing"
)

func proc(port int, command, args, workdir string) LocalProcess {
	return LocalProcess{Port: port, Command: command, Args: args, Workdir: workdir}
}

func container(name, image string, ports []int, workdir string, labels map[string]string) DockerContainer {
	if labels == nil {
		labels = map[string]string{}
	}
	return DockerContainer{Name: name, Image: image, Ports: ports, Workdir: workdir, Labels: labels}
}

func TestResolveHeuristically_ProcessWorkdirMatch(t *testing.T) {
	processes := []LocalProcess{
		proc(5173, "node", "vite dev", "/Users/dev/myapp"),
		proc(3000, "node", "next dev", "/Users/dev/other"),
	}

	m := ResolveHeuristically("myapp.localhost", processes, nil)
	if m == nil {
		t.Fatal("expected a match")
	}
	if m.Type != "process" || m.Port != 5173 {
		t.Fatalf("unexpected mapping: %+v", m)
	}
	if m.ProcessIdentifier == nil || m.ProcessIdentifier.Workdir != "/Users/dev/myapp" {
		t.Fatalf("expected ProcessIdentifier with workdir, got %+v", m.ProcessIdentifier)
	}
}

func TestResolveHeuristically_CaseInsensitive(t *testing.T) {
	processes := []LocalProcess{
		proc(5173, "node", "", "/Users/dev/MyApp"),
	}
	if m := ResolveHeuristically("myapp.localhost", processes, nil); m == nil {
		t.Fatal("expected case-insensitive workdir match")
	}
}

func TestResolveHeuristically_ContainerNameMatch(t *testing.T) {
	containers := []DockerContainer{
		container("myapp", "nginx", []int{80}, "", nil),
	}
	m := ResolveHeuristically("myapp.localhost", nil, containers)
	if m == nil {
		t.Fatal("expected a match")
	}
	if m.Type != "docker" || m.Target != "myapp" || m.Port != 80 {
		t.Fatalf("unexpected mapping: %+v", m)
	}
}

func TestResolveHeuristically_ComposeProjectService(t *testing.T) {
	containers := []DockerContainer{
		container("myproj-api-1", "node", []int{4000}, "/Users/dev/myproj", map[string]string{
			"com.docker.compose.project": "myproj",
			"com.docker.compose.service": "api",
		}),
		container("myproj-db-1", "postgres", []int{5432}, "/Users/dev/myproj", map[string]string{
			"com.docker.compose.project": "myproj",
			"com.docker.compose.service": "db",
		}),
	}
	m := ResolveHeuristically("api.myproj.localhost", nil, containers)
	if m == nil {
		t.Fatal("expected a match")
	}
	if m.Target != "myproj-api-1" || m.Port != 4000 {
		t.Fatalf("unexpected mapping: %+v", m)
	}
}

func TestResolveHeuristically_MonorepoSubdir(t *testing.T) {
	processes := []LocalProcess{
		proc(4000, "node", "", "/Users/dev/myproj/api"),
		proc(5173, "node", "", "/Users/dev/myproj/web"),
	}
	m := ResolveHeuristically("api.myproj.localhost", processes, nil)
	if m == nil {
		t.Fatal("expected a match")
	}
	if m.Port != 4000 {
		t.Fatalf("unexpected mapping: %+v", m)
	}
}

func TestResolveHeuristically_CommandInProjectDir(t *testing.T) {
	processes := []LocalProcess{
		proc(5173, "node", "vite dev", "/Users/dev/myproj"),
	}
	m := ResolveHeuristically("vite.myproj.localhost", processes, nil)
	if m == nil {
		t.Fatal("expected a match")
	}
	if m.Port != 5173 {
		t.Fatalf("unexpected mapping: %+v", m)
	}
}

func TestResolveHeuristically_AmbiguousReturnsNil(t *testing.T) {
	// Two distinct targets both match "myapp" → ambiguous.
	processes := []LocalProcess{
		proc(5173, "node", "", "/Users/dev/myapp"),
	}
	containers := []DockerContainer{
		container("myapp", "nginx", []int{80}, "", nil),
	}
	if m := ResolveHeuristically("myapp.localhost", processes, containers); m != nil {
		t.Fatalf("expected nil on ambiguity, got %+v", m)
	}
}

func TestResolveHeuristically_SameTargetTwoRulesDeduped(t *testing.T) {
	// One container matched by both name and compose-service rules → still one candidate.
	containers := []DockerContainer{
		container("myapp", "nginx", []int{80}, "", map[string]string{
			"com.docker.compose.service": "myapp",
		}),
	}
	if m := ResolveHeuristically("myapp.localhost", nil, containers); m == nil {
		t.Fatal("expected deduped single match")
	}
}

func TestResolveHeuristically_MultiPortContainerSkipped(t *testing.T) {
	containers := []DockerContainer{
		container("myapp", "thing", []int{80, 9000}, "", nil),
	}
	if m := ResolveHeuristically("myapp.localhost", nil, containers); m != nil {
		t.Fatalf("expected nil for ambiguous ports, got %+v", m)
	}
}

func TestResolveHeuristically_MultiPortWithSinglePublished(t *testing.T) {
	c := container("myapp", "thing", nil, "", nil)
	c.PortMappings = []PortMapping{{ContainerPort: 8080, HostPort: 18080, HostIP: "127.0.0.1"}}
	m := ResolveHeuristically("myapp.localhost", nil, []DockerContainer{c})
	if m == nil || m.Port != 8080 {
		t.Fatalf("expected published-port match, got %+v", m)
	}
}

func TestResolveHeuristically_NoMatch(t *testing.T) {
	processes := []LocalProcess{
		proc(5173, "node", "", "/Users/dev/other"),
	}
	if m := ResolveHeuristically("myapp.localhost", processes, nil); m != nil {
		t.Fatalf("expected nil, got %+v", m)
	}
}

func TestResolveHeuristically_NonLocalhostHostname(t *testing.T) {
	if m := ResolveHeuristically("example.com", nil, nil); m != nil {
		t.Fatalf("expected nil for non-localhost hostname, got %+v", m)
	}
	if m := ResolveHeuristically("localhost", nil, nil); m != nil {
		t.Fatalf("expected nil for bare localhost, got %+v", m)
	}
}

func TestResolveRelatedHeuristically_SameProjectWins(t *testing.T) {
	origin := &RouteMapping{
		Type:              "process",
		Target:            "localhost",
		Port:              5173,
		ProcessIdentifier: &ProcessIdentifier{Workdir: "/Users/dev/myproj/web"},
	}
	containers := []DockerContainer{
		container("myproj-api-1", "node", []int{4000}, "/Users/dev/myproj", map[string]string{
			"com.docker.compose.service": "api",
		}),
		container("otherproj-api-1", "node", []int{4100}, "/Users/dev/otherproj", map[string]string{
			"com.docker.compose.service": "api",
		}),
	}
	m := ResolveRelatedHeuristically(origin, "api", nil, containers)
	if m == nil {
		t.Fatal("expected a match")
	}
	if m.Target != "myproj-api-1" {
		t.Fatalf("expected same-project container, got %+v", m)
	}
}

func TestResolveRelatedHeuristically_GloballyAmbiguous(t *testing.T) {
	containers := []DockerContainer{
		container("a-api-1", "node", []int{4000}, "/a", map[string]string{"com.docker.compose.service": "api"}),
		container("b-api-1", "node", []int{4100}, "/b", map[string]string{"com.docker.compose.service": "api"}),
	}
	// No origin workdir to disambiguate → nil.
	if m := ResolveRelatedHeuristically(&RouteMapping{Type: "docker", Target: "unknown"}, "api", nil, containers); m != nil {
		t.Fatalf("expected nil on ambiguity, got %+v", m)
	}
}

func TestResolveRelatedHeuristically_UniqueGlobalMatch(t *testing.T) {
	containers := []DockerContainer{
		container("proj-db-1", "postgres", []int{5432}, "/p", map[string]string{"com.docker.compose.service": "db"}),
	}
	m := ResolveRelatedHeuristically(nil, "db", nil, containers)
	if m == nil || m.Target != "proj-db-1" || m.Port != 5432 {
		t.Fatalf("expected unique match, got %+v", m)
	}
}
