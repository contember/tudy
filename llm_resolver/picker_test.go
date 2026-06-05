package llm_resolver

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func newTestResolver(t *testing.T) *LLMResolver {
	t.Helper()
	logger := zap.NewNop()
	m := &LLMResolver{
		logger:       logger,
		cache:        NewCache(filepath.Join(t.TempDir(), "mappings.json"), logger),
		resolver:     NewResolver("", "", "test-model", "", logger),
		pickerSecret: []byte("test-secret-test-secret-test-sec"),
	}
	return m
}

func postPickerSelect(t *testing.T, m *LLMResolver, hostname string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "http://"+hostname+pickerSelectPath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	if err := m.handlePickerSelect(w, req, hostname); err != nil {
		t.Fatalf("handlePickerSelect returned error: %v", err)
	}
	return w
}

func TestPickerSelect_CreatesMapping(t *testing.T) {
	m := newTestResolver(t)
	host := "myapp.localhost"

	w := postPickerSelect(t, m, host, url.Values{
		"token":   {m.pickerToken(host)},
		"type":    {"process"},
		"target":  {"localhost"},
		"port":    {"5173"},
		"workdir": {"/Users/dev/myapp"},
	})

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", w.Code, w.Body.String())
	}
	mapping := m.cache.Get(host)
	if mapping == nil {
		t.Fatal("expected mapping to be created")
	}
	if mapping.Type != "process" || mapping.Port != 5173 {
		t.Fatalf("unexpected mapping: %+v", mapping)
	}
	if mapping.ProcessIdentifier == nil || mapping.ProcessIdentifier.Workdir != "/Users/dev/myapp" {
		t.Fatalf("expected ProcessIdentifier, got %+v", mapping.ProcessIdentifier)
	}
}

func TestPickerSelect_RejectsBadToken(t *testing.T) {
	m := newTestResolver(t)
	host := "myapp.localhost"

	w := postPickerSelect(t, m, host, url.Values{
		"token":  {"bogus"},
		"type":   {"docker"},
		"target": {"myapp"},
		"port":   {"80"},
	})

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
	if m.cache.Get(host) != nil {
		t.Fatal("mapping must not be created with a bad token")
	}
}

func TestPickerSelect_TokenBoundToHostname(t *testing.T) {
	m := newTestResolver(t)

	// Token for foo.localhost must not authorize bar.localhost.
	w := postPickerSelect(t, m, "bar.localhost", url.Values{
		"token":  {m.pickerToken("foo.localhost")},
		"type":   {"docker"},
		"target": {"thing"},
		"port":   {"80"},
	})

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestPickerSelect_RejectsNonLocalhost(t *testing.T) {
	m := newTestResolver(t)
	host := "example.com"

	w := postPickerSelect(t, m, host, url.Values{
		"token":  {m.pickerToken(host)},
		"type":   {"docker"},
		"target": {"thing"},
		"port":   {"80"},
	})

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestPickerSelect_ValidatesInput(t *testing.T) {
	m := newTestResolver(t)
	host := "myapp.localhost"

	for name, form := range map[string]url.Values{
		"bad type":  {"token": {m.pickerToken(host)}, "type": {"weird"}, "target": {"x"}, "port": {"80"}},
		"bad port":  {"token": {m.pickerToken(host)}, "type": {"docker"}, "target": {"x"}, "port": {"99999"}},
		"no target": {"token": {m.pickerToken(host)}, "type": {"docker"}, "target": {""}, "port": {"80"}},
	} {
		w := postPickerSelect(t, m, host, form)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d", name, w.Code)
		}
	}
	if m.cache.Get(host) != nil {
		t.Fatal("no mapping should have been created")
	}
}

func TestPickerSelect_RedirectsToNext(t *testing.T) {
	m := newTestResolver(t)
	host := "myapp.localhost"

	w := postPickerSelect(t, m, host, url.Values{
		"token":  {m.pickerToken(host)},
		"type":   {"docker"},
		"target": {"myapp"},
		"port":   {"80"},
		"next":   {"/deep/path?tab=2"},
	})

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/deep/path?tab=2" {
		t.Fatalf("expected redirect to original path, got %q", loc)
	}
}

func TestSanitizeNextURI(t *testing.T) {
	for input, want := range map[string]string{
		"":                      "/",
		"/":                     "/",
		"/deep/path":            "/deep/path",
		"/deep/path?tab=2":      "/deep/path?tab=2",
		"/?force":               "/",
		"/page?force&tab=1":     "/page?tab=1",
		"/page?prompt=use+3000": "/page",
		"//evil.com/x":          "/",
		"/\\evil.com/x":         "/",
		"http://evil.com/":      "/",
		"relative/path":         "/",
		"%%%":                   "/",
	} {
		if got := sanitizeNextURI(input); got != want {
			t.Errorf("sanitizeNextURI(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestWantsHTML(t *testing.T) {
	get := httptest.NewRequest(http.MethodGet, "http://x.localhost/", nil)
	get.Header.Set("Accept", "text/html,application/xhtml+xml")
	if !wantsHTML(get) {
		t.Error("browser GET should want HTML")
	}

	api := httptest.NewRequest(http.MethodGet, "http://x.localhost/", nil)
	api.Header.Set("Accept", "application/json")
	if wantsHTML(api) {
		t.Error("JSON GET should not want HTML")
	}

	post := httptest.NewRequest(http.MethodPost, "http://x.localhost/", nil)
	post.Header.Set("Accept", "text/html")
	if wantsHTML(post) {
		t.Error("POST should not want HTML")
	}
}
