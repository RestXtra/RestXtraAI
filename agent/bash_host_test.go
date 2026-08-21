package agent

import (
	"strings"
	"testing"
)

func TestToolEnvironmentFiltersSecrets(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "host-secret")
	t.Setenv("RESTXTRA_PG_DSN", "postgres://secret")
	t.Setenv("RESTXTRA_ALLOW_TOOL_SECRET_ENV", "false")
	env := ToolEnvironment([]string{"SAFE_VALUE=visible", "BENCHMARK_TOKEN=session-secret"})
	joined := strings.Join(env, "\n")
	for _, forbidden := range []string{"OPENAI_API_KEY=", "RESTXTRA_PG_DSN=", "BENCHMARK_TOKEN="} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("secret environment leaked: %s", forbidden)
		}
	}
	if !strings.Contains(joined, "SAFE_VALUE=visible") {
		t.Fatal("safe session environment was removed")
	}
}

func TestWinToWslPath(t *testing.T) {
	if got := winToWslPath(`F:\work space\task`); got != "/mnt/f/work space/task" {
		t.Fatalf("winToWslPath = %q", got)
	}
}
