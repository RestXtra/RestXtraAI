package agent

import (
	"testing"

	"github.com/Autumn-27/norma/llm"
)

// TestCompactionTokenCounter guards the hot-path policy: the compaction token
// counter runs on every model turn, so it must be local (nil) by default for all
// providers, and only use the remote Anthropic count_tokens endpoint when
// explicitly enabled and correctly configured.
func TestCompactionTokenCounter(t *testing.T) {
	cfgAnthropic := Config{Format: llm.FormatAnthropic, APIKey: "sk-ant-x"}
	cfgOpenAI := Config{Format: llm.FormatOpenAI, APIKey: "sk-x"}

	t.Setenv(CountTokensEnv, "")
	if cfgAnthropic.CompactionTokenCounter() != nil {
		t.Fatalf("default must be local (nil) for Anthropic")
	}
	if cfgOpenAI.CompactionTokenCounter() != nil {
		t.Fatalf("default must be local (nil) for OpenAI")
	}

	t.Setenv(CountTokensEnv, "1")
	cases := []struct {
		name string
		cfg  Config
		want bool
	}{
		{"openai with key", cfgOpenAI, false},
		{"anthropic no key", Config{Format: llm.FormatAnthropic}, false},
		{"anthropic blank key", Config{Format: llm.FormatAnthropic, APIKey: "   "}, false},
		{"anthropic with key", cfgAnthropic, true},
	}
	for _, tc := range cases {
		got := tc.cfg.CompactionTokenCounter() != nil
		if got != tc.want {
			t.Errorf("%s: counter!=nil = %v, want %v", tc.name, got, tc.want)
		}
	}
}
