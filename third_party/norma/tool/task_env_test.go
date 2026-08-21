package tool

import (
	"strings"
	"testing"
)

func TestWithEnvFiltersHostSecrets(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "secret")
	t.Setenv("RESTXTRA_ALLOW_TOOL_SECRET_ENV", "false")
	env := strings.Join(withEnv([]string{"VISIBLE_SETTING=yes", "SESSION_TOKEN=secret"}), "\n")
	if strings.Contains(env, "ANTHROPIC_API_KEY=") || strings.Contains(env, "SESSION_TOKEN=") {
		t.Fatal("child process inherited a secret")
	}
	if !strings.Contains(env, "VISIBLE_SETTING=yes") {
		t.Fatal("non-secret environment was removed")
	}
}
