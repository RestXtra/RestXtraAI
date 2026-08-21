package server

import (
	"testing"

	"github.com/RestXtra/RestXtraAI/db"
)

func TestSumRoundCosts(t *testing.T) {
	rows := []db.AgentRoundCost{
		{Worker: "planner", Rounds: 2, ToolCalls: 1, InputTokens: 100, OutputTokens: 20, CacheReadTokens: 30},
		{Worker: "worker", Rounds: 3, ToolCalls: 4, ToolErrors: 1, InputTokens: 200, OutputTokens: 40, CacheWriteTokens: 10},
	}
	total := sumRoundCosts(rows)
	if total.Rounds != 5 || total.ToolCalls != 5 || total.ToolErrors != 1 ||
		total.InputTokens != 300 || total.OutputTokens != 60 || total.CacheReadTokens != 30 || total.CacheWriteTokens != 10 {
		t.Fatalf("unexpected total: %+v", total)
	}
}
