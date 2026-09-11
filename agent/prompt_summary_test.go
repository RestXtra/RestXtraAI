package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Autumn-27/norma/llm"
)

// TestPromptSummaryIsBounded guards the agent_events size fix: the assembled
// request must be summarized as counts only, never embedding the (large) system
// prompt or message bodies.
func TestPromptSummaryIsBounded(t *testing.T) {
	big := strings.Repeat("X", 100_000)
	temp := 0.7
	req := &llm.CompletionRequest{
		System:      []string{"sys", big},
		Messages:    []llm.Message{llm.UserText(big)},
		Tools:       []llm.ToolSchema{{Name: "bash"}, {Name: "curl"}},
		MaxTokens:   4096,
		Temperature: &temp,
	}
	summary := promptSummary(req)
	if summary["system_parts"] != 2 || summary["messages"] != 1 || summary["tools"] != 2 {
		t.Fatalf("counts wrong: %+v", summary)
	}
	raw, _ := json.Marshal(summary)
	if len(raw) > 4096 {
		t.Fatalf("summary not bounded: %d bytes", len(raw))
	}
	if strings.Contains(string(raw), "XXXX") {
		t.Fatalf("summary must not embed message/system bodies")
	}
	if _, ok := summary["tool_names"]; !ok {
		t.Fatalf("tool names should be retained: %+v", summary)
	}
}
