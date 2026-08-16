package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/Autumn-27/norma/llm"
)

func TestDeterministicSummarizer(t *testing.T) {
	msgs := []llm.Message{
		llm.UserText("扫描目标 http://example.com 首页"),
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
			{Type: llm.BlockToolUse, Name: "Bash", Input: []byte(`{"command":"curl -s http://example.com"}`)},
		}},
		{Role: llm.Role("tool"), Content: []llm.ContentBlock{
			llm.ToolResultText("id1", "HTTP 200 OK <title>Example</title>\n大段无关输出...", false),
		}},
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
			llm.TextBlock("首页可达，标题 Example，技术栈待查。"),
		}},
	}
	got, err := DeterministicSummarizer(context.Background(), msgs)
	if err != nil {
		t.Fatalf("summarize err: %v", err)
	}
	for _, want := range []string{"[user]", "[assistant]", "tool(Bash:", "result(", "确定性"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q:\n%s", want, got)
		}
	}
	if len(got) > 2000 {
		t.Errorf("summary too large: %d", len(got))
	}
}
