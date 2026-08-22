package agent

import (
	"context"
	"testing"

	actool "github.com/Autumn-27/norma/tool"
)

func TestAugmentToolsPassesAssemblyInfoToResolver(t *testing.T) {
	oldAugment, oldResolve := ToolAugment, ToolResolve
	t.Cleanup(func() {
		ToolAugment, ToolResolve = oldAugment, oldResolve
	})

	ToolAugment = func(context.Context, string) ([]actool.CoreTool, DeferredInfo, func()) {
		return nil, DeferredInfo{InteractiveShell: true}, nil
	}
	seen := false
	ToolResolve = func(_ context.Context, _ string, tools []actool.CoreTool, info DeferredInfo) []actool.CoreTool {
		seen = info.InteractiveShell
		return tools
	}

	_, _, cleanup := AugmentTools(context.Background(), "worker", nil)
	cleanup()
	if !seen {
		t.Fatal("tool resolver did not receive agent runtime assembly info")
	}
}
