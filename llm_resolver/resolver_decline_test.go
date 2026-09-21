package llm_resolver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

// The resolver used to instruct the model to "still provide your best guess"
// when nothing matched. That guess was cached and then fed back into later
// prompts as an existing mapping, so one wrong answer became precedent for the
// next. A real case: `mojeedu.npi.localhost` was mapped to the unrelated
// `npi-admin-1` container on the reasoning that it "most likely represents a
// content management interface", and every request to that hostname was served
// the admin UI for months. A later unknown hostname then cited that mapping as
// the "established routing pattern" and was mapped to the same container.
//
// These tests pin the two halves of the fix: the model is allowed to decline,
// and a decline never becomes a mapping.

func TestDeclinedRecognisesExplicitNone(t *testing.T) {
	for _, typ := range []string{"none", "None", "  none  ", "unknown", ""} {
		if !declined(&LLMResponse{Type: typ, Target: "npi-admin-1", Port: 1480}) {
			t.Errorf("declined(type=%q) = false, want true", typ)
		}
	}
}

func TestDeclinedRecognisesUnroutableAnswers(t *testing.T) {
	// A mapping with no target or no port cannot be proxied to. Caching one
	// yields a 502 under a hostname that looks configured, which is the same
	// confusing failure the decline path exists to avoid.
	cases := map[string]*LLMResponse{
		"no target": {Type: "docker", Target: "", Port: 1480},
		"zero port": {Type: "docker", Target: "npi-admin-1", Port: 0},
		"negative":  {Type: "process", Target: "localhost", Port: -1},
	}
	for name, resp := range cases {
		if !declined(resp) {
			t.Errorf("declined(%s) = false, want true", name)
		}
	}
}

func TestDeclinedAcceptsARoutableMapping(t *testing.T) {
	ok := &LLMResponse{Type: "docker", Target: "npi-admin-1", Port: 1480}
	if declined(ok) {
		t.Error("declined(routable docker mapping) = true, want false")
	}
	proc := &LLMResponse{Type: "process", Target: "localhost", Port: 3000}
	if declined(proc) {
		t.Error("declined(routable process mapping) = true, want false")
	}
}

// The prompt is the actual control here: without the instruction, the model
// reverts to guessing and the decline path never fires.
func TestSystemPromptForbidsGuessing(t *testing.T) {
	prompt := (&Resolver{}).getSystemPrompt()
	if !strings.Contains(prompt, `"type": "none"`) {
		t.Error("system prompt does not offer the model a way to decline")
	}
	if !strings.Contains(prompt, "Do NOT guess") {
		t.Error("system prompt does not tell the model to stop guessing")
	}
	if strings.Contains(prompt, "still provide your best guess") {
		t.Error("system prompt still instructs the model to guess when nothing matches")
	}
}

func TestRelatedServicePromptForbidsGuessing(t *testing.T) {
	prompt := (&Resolver{}).getRelatedServiceSystemPrompt()
	if !strings.Contains(prompt, `"type": "none"`) {
		t.Error("related-service prompt does not offer the model a way to decline")
	}
	if strings.Contains(prompt, "still provide your best guess") {
		t.Error("related-service prompt still instructs the model to guess")
	}
}

// newStubResolver points a Resolver at a stub LLM API that answers every call
// with the same content, so the decline path can be exercised end to end.
// Testing declined() alone is not enough: the decline has to survive the trip
// through callLLM, which is where an earlier version of this lost it.
func newStubResolver(t *testing.T, content string) *Resolver {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": content}},
			},
		})
	}))
	t.Cleanup(srv.Close)
	return NewResolver("test-key", srv.URL, "test-model", "", zap.NewNop())
}

func TestResolveTargetSurfacesDeclineAsErrNoConfidentMatch(t *testing.T) {
	r := newStubResolver(t, `{"type": "none", "reason": "nothing discovered serves this hostname"}`)

	mapping, err := r.ResolveTarget(context.Background(), "mojeedu.npi.localhost", "", nil, nil, nil)
	if mapping != nil {
		t.Fatalf("ResolveTarget returned a mapping for a declined hostname: %+v", mapping)
	}
	if !errors.Is(err, ErrNoConfidentMatch) {
		t.Fatalf("ResolveTarget error = %v, want ErrNoConfidentMatch", err)
	}
	if got, want := declineReason(err), "nothing discovered serves this hostname"; got != want {
		t.Errorf("declineReason() = %q, want %q", got, want)
	}
}

func TestResolveTargetSurfacesUnroutableAnswerAsDecline(t *testing.T) {
	// A "docker" answer with no port is not a schema complaint worth showing
	// the user: nothing serves the hostname, which is the decline outcome.
	r := newStubResolver(t, `{"type": "docker", "target": "npi-admin-1", "port": 0, "reason": "no port"}`)

	_, err := r.ResolveTarget(context.Background(), "mojeedu.npi.localhost", "", nil, nil, nil)
	if !errors.Is(err, ErrNoConfidentMatch) {
		t.Fatalf("ResolveTarget error = %v, want ErrNoConfidentMatch", err)
	}
}

func TestResolveRelatedServiceSurfacesDeclineAsErrNoConfidentMatch(t *testing.T) {
	r := newStubResolver(t, `{"type": "none", "reason": "no sibling service matches"}`)
	origin := &RouteMapping{Type: "docker", Target: "npi-web-1", Port: 3000}

	mapping, err := r.ResolveRelatedService(context.Background(), "npi.localhost", origin, "api", "", nil, nil, nil)
	if mapping != nil {
		t.Fatalf("ResolveRelatedService returned a mapping for a declined service: %+v", mapping)
	}
	if !errors.Is(err, ErrNoConfidentMatch) {
		t.Fatalf("ResolveRelatedService error = %v, want ErrNoConfidentMatch", err)
	}
}

func TestResolveTargetStillRejectsMalformedAnswers(t *testing.T) {
	// Letting declines past the validator must not let genuine garbage past
	// it: an out-of-range port is a broken answer, not a decline.
	r := newStubResolver(t, `{"type": "docker", "target": "npi-admin-1", "port": 99999}`)

	_, err := r.ResolveTarget(context.Background(), "mojeedu.npi.localhost", "", nil, nil, nil)
	if err == nil {
		t.Fatal("ResolveTarget accepted a port outside 1-65535")
	}
	if errors.Is(err, ErrNoConfidentMatch) {
		t.Errorf("malformed answer reported as a decline: %v", err)
	}
}

func TestResolveTargetAcceptsARoutableAnswer(t *testing.T) {
	r := newStubResolver(t, `{"type": "docker", "target": "npi-admin-1", "port": 1480, "reason": "matches"}`)

	mapping, err := r.ResolveTarget(context.Background(), "admin.npi.localhost", "", nil, nil, nil)
	if err != nil {
		t.Fatalf("ResolveTarget: %v", err)
	}
	if mapping.Target != "npi-admin-1" || mapping.Port != 1480 {
		t.Errorf("mapping = %+v, want npi-admin-1:1480", mapping)
	}
}

// A decline is not cached as a mapping, so without this the same unresolvable
// hostname bills an LLM call on every retry.
func TestDeclineCacheRemembersThenExpires(t *testing.T) {
	c := NewDeclineCache()
	refusal := &declineError{reason: "nothing serves this"}

	c.Set("mojeedu.npi.localhost", refusal)
	if got := c.Get("mojeedu.npi.localhost"); got == nil {
		t.Fatal("decline was not remembered")
	}
	if got := c.Get("other.localhost"); got != nil {
		t.Errorf("Get(other.localhost) = %v, want nil", got)
	}

	c.ttl = -time.Second
	c.Set("mojeedu.npi.localhost", refusal)
	if got := c.Get("mojeedu.npi.localhost"); got != nil {
		t.Errorf("expired decline still returned: %v", got)
	}
}

func TestDeclineCacheDeleteForgets(t *testing.T) {
	c := NewDeclineCache()
	c.Set("mojeedu.npi.localhost", &declineError{})
	c.Delete("mojeedu.npi.localhost")
	if got := c.Get("mojeedu.npi.localhost"); got != nil {
		t.Errorf("Get after Delete = %v, want nil", got)
	}
}
