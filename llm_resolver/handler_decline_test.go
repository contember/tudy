package llm_resolver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

// newDecliningHandler returns a handler whose LLM declines every question,
// plus a counter of how many times the LLM was asked.
func newDecliningHandler(t *testing.T) (*LLMResolver, func() int32) {
	t.Helper()
	m := newTestResolver(t)
	r, calls := newCountingStubResolver(t, `{"type": "none", "reason": "nothing discovered serves this"}`)
	m.resolver = r
	m.llmEnabled = true
	return m, calls.Load
}

func serve(t *testing.T, m *LLMResolver, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	next := caddyhttp.HandlerFunc(func(http.ResponseWriter, *http.Request) error {
		t.Fatalf("request to %s was proxied, want a decline", target)
		return nil
	})
	if err := m.ServeHTTP(rec, req, next); err != nil {
		t.Fatalf("ServeHTTP(%s): %v", target, err)
	}
	return rec
}

// A remembered decline answers plain retries without another LLM call.
func TestRecentDeclineAnswersPlainRetry(t *testing.T) {
	m, calls := newDecliningHandler(t)
	m.declines.Set("blog.example.localhost", &declineError{reason: "earlier"})

	serve(t, m, "http://blog.example.localhost/")
	if got := calls(); got != 0 {
		t.Errorf("LLM called %d times for a recently declined hostname, want 0", got)
	}
}

// ?prompt= is how a user gives the model more context, and right after a
// decline is when they reach for it, so a remembered decline must not
// swallow it.
func TestPromptBypassesRecentDecline(t *testing.T) {
	m, calls := newDecliningHandler(t)
	m.declines.Set("blog.example.localhost", &declineError{reason: "earlier"})

	serve(t, m, "http://blog.example.localhost/?prompt=it+is+the+vite+app")
	if got := calls(); got != 1 {
		t.Errorf("LLM called %d times for a prompted request after a decline, want 1", got)
	}
}

// A decline to a prompted question says nothing about the bare hostname, so
// it is not remembered for the next plain request.
func TestPromptedDeclineIsNotRemembered(t *testing.T) {
	m, _ := newDecliningHandler(t)

	serve(t, m, "http://blog.example.localhost/?prompt=it+is+the+vite+app")
	if got := m.declines.Get("blog.example.localhost"); got != nil {
		t.Errorf("prompted decline was remembered: %v", got)
	}
}

func TestRelatedPromptBypassesRecentDecline(t *testing.T) {
	m, calls := newDecliningHandler(t)
	m.declines.Set("shop.example.localhost:api", &declineError{reason: "earlier"})

	serve(t, m, "http://shop.example.localhost/_proxy/api/?prompt=the+go+backend")
	if got := calls(); got != 1 {
		t.Errorf("LLM called %d times for a prompted /_proxy/ request after a decline, want 1", got)
	}
	if got := m.declines.Get("shop.example.localhost:api"); got == nil {
		t.Error("prompted request cleared the remembered plain decline")
	}
}

// With an explicit prompt, a decline on /_proxy/ is answered as a decline, not
// replaced by a heuristic route that ignores the prompt.
func TestRelatedPromptedDeclineIsReported(t *testing.T) {
	m, _ := newDecliningHandler(t)

	rec := serve(t, m, "http://shop.example.localhost/_proxy/api/?prompt=the+go+backend")
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
	if body := rec.Body.String(); !strings.Contains(body, "No discovered service plausibly serves") {
		t.Errorf("body = %q, want the decline message", body)
	}
	if got := m.cache.Get("shop.example.localhost:api"); got != nil {
		t.Errorf("a mapping was cached for a declined prompted lookup: %+v", got)
	}
}
