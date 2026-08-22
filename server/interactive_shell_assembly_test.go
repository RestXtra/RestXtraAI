package server

import (
	"strings"
	"testing"

	actool "github.com/Autumn-27/norma/tool"
)

func TestInjectInteractiveShellFollowsSnapshotFlag(t *testing.T) {
	t.Setenv("AGENT_CORE_DISABLE_INTERACTIVE_SHELL", "")
	base := []actool.CoreTool{actool.NewBash()}

	disabled := injectInteractiveShell(append([]actool.CoreTool{}, base...), false)
	if len(disabled) != 1 || strings.Contains(disabled[0].Description(), "shell_open") {
		t.Fatalf("disabled interactive shell changed tools: %+v", names(disabled))
	}

	enabled := injectInteractiveShell(append([]actool.CoreTool{}, base...), true)
	byName := names(enabled)
	for _, name := range []string{"shell_open", "shell_send", "shell_read", "shell_close", "shell_list"} {
		if byName[name] == nil {
			t.Fatalf("enabled interactive shell tool %s missing: %+v", name, byName)
		}
	}
	if bash := byName["Bash"]; bash == nil || !strings.Contains(bash.Description(), "shell_open") {
		t.Fatal("Bash description does not point to shell_open when interactive shell is enabled")
	}
}
