package llm_resolver

import (
	"testing"

	"go.uber.org/zap"
)

func TestParseEnabledFlag(t *testing.T) {
	for input, want := range map[string]bool{
		"true": true, "TRUE": true, "on": true, "1": true, "yes": true, " on ": true,
		"false": false, "off": false, "0": false, "no": false, "OFF": false,
	} {
		got, ok := parseEnabledFlag(input)
		if !ok {
			t.Errorf("parseEnabledFlag(%q): expected ok", input)
			continue
		}
		if got != want {
			t.Errorf("parseEnabledFlag(%q) = %v, want %v", input, got, want)
		}
	}

	for _, input := range []string{"", "maybe", "enabled"} {
		if _, ok := parseEnabledFlag(input); ok {
			t.Errorf("parseEnabledFlag(%q): expected !ok", input)
		}
	}
}

func TestLLMAvailable(t *testing.T) {
	logger := zap.NewNop()
	cases := []struct {
		name    string
		apiKey  string
		enabled bool
		want    bool
	}{
		{"key + enabled", "sk-123", true, true},
		{"key + disabled", "sk-123", false, false},
		{"no key + enabled", "", true, false},
		{"no key + disabled", "", false, false},
	}
	for _, tc := range cases {
		m := &LLMResolver{
			resolver:   NewResolver(tc.apiKey, "", "model", "", logger),
			llmEnabled: tc.enabled,
		}
		if got := m.llmAvailable(); got != tc.want {
			t.Errorf("%s: llmAvailable() = %v, want %v", tc.name, got, tc.want)
		}
	}
}
