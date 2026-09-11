package agent

import (
	"testing"

	"github.com/Autumn-27/norma/llm"
)

// TestCompactionTokenCounterOnlyForAnthropicWithKey guards against the previous
// regression where an Anthropic count_tokens counter was wired for every provider
// with an empty key, firing a doomed network request on every model turn.
func TestCompactionTokenCounterOnlyForAnthropicWithKey(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want bool
	}{
		{"openai with key", Config{Format: llm.FormatOpenAI, APIKey: "sk-x"}, false},
		{"openai no key", Config{Format: llm.FormatOpenAI}, false},
		{"anthropic no key", Config{Format: llm.FormatAnthropic}, false},
		{"anthropic blank key", Config{Format: llm.FormatAnthropic, APIKey: "   "}, false},
		{"anthropic with key", Config{Format: llm.FormatAnthropic, APIKey: "sk-ant-x"}, true},
	}
	for _, tc := range cases {
		got := tc.cfg.CompactionTokenCounter() != nil
		if got != tc.want {
			t.Errorf("%s: counter!=nil = %v, want %v", tc.name, got, tc.want)
		}
	}
}
