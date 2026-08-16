package workflow

import (
	"context"
	"testing"
)

func TestValidate(t *testing.T) {
	// 合法 DAG：start → tool → condition → (output|end)
	g := &Graph{
		Nodes: []Node{
			{ID: "s", Kind: KindStart},
			{ID: "t", Kind: KindTool, Tool: "Bash", Args: `{"command":"echo hi"}`},
			{ID: "c", Kind: KindCondition, Expression: `{{previous.output}} contains "hi"`},
			{ID: "o", Kind: KindOutput, Instruction: "done {{outputs.result}}"},
		},
		Edges: []Edge{{From: "s", To: "t"}, {From: "t", To: "c"}, {From: "c", To: "o"}},
	}
	if errs := g.Validate(); len(errs) > 0 {
		t.Fatalf("valid graph rejected: %v", errs)
	}
	// 环
	cyc := &Graph{
		Nodes: []Node{{ID: "a", Kind: KindStart}, {ID: "b", Kind: KindEnd}, {ID: "c", Kind: KindTool, Tool: "Bash"}},
		Edges: []Edge{{From: "a", To: "c"}, {From: "c", To: "b"}, {From: "b", To: "c"}},
	}
	if errs := cyc.Validate(); len(errs) == 0 {
		t.Fatal("cycle not detected")
	}
}

func TestEvalCondition(t *testing.T) {
	vars := map[string]string{"outputs.n": "42", "previous.output": "success: ok", "outputs.a": ""}
	cases := []struct{ expr, want string }{
		{`{{outputs.n}} >= 40`, "true"},
		{`{{outputs.n}} != "0"`, "true"},
		{`{{previous.output}} contains "ok"`, "true"},
		{`{{outputs.a}} == ""`, "true"},
		{`{{outputs.n}} < 10`, "false"},
		{`{{outputs.n}} >= 40 && {{outputs.a}} == ""`, "true"},
		{`{{outputs.n}} >= 40 || {{outputs.n}} < 10`, "true"},
	}
	for _, c := range cases {
		got, err := EvalCondition(c.expr, vars)
		if err != nil {
			t.Fatalf("%q: %v", c.expr, err)
		}
		if (c.want == "true") != got {
			t.Errorf("%q: got %v, want %s", c.expr, got, c.want)
		}
	}
}

func TestEngineRun(t *testing.T) {
	eng := &Engine{
		RunTool: func(ctx context.Context, tool, args string) (string, error) { return "hi from tool", nil },
	}
	g := &Graph{
		Nodes: []Node{
			{ID: "s", Kind: KindStart},
			{ID: "t", Kind: KindTool, Tool: "Bash", Args: `{"command":"x"}`, OutputKey: "result"},
			{ID: "c", Kind: KindCondition, Expression: `{{outputs.result}} contains "hi"`},
			{ID: "o", Kind: KindOutput, Instruction: "final: {{outputs.result}}"},
		},
		Edges: []Edge{{From: "s", To: "t"}, {From: "t", To: "c"}, {From: "c", To: "o"}},
	}
	res, err := eng.Run(context.Background(), g, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != "completed" {
		t.Fatalf("status=%s", res.Status)
	}
	if res.Final != "final: hi from tool" {
		t.Fatalf("final=%q", res.Final)
	}
}
