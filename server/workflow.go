package server

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/Autumn-27/norma/agentcore"
	acperm "github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
	"github.com/RestXtra/RestXtraAI/agent"
	"github.com/RestXtra/RestXtraAI/workflow"
)

// 工作流图引擎 API：校验 / 保存 / 运行 / dry-run。tool 节点经 Bash 执行，
// agent 节点跑一次激活 LLM 的指令化单轮，hitl 节点 v1 自动放行（记日志）。

// workflowEngine 构造一个接好回调的执行引擎。
func (s *Server) workflowEngine(dry bool) *workflow.Engine {
	return &workflow.Engine{
		Dry: dry,
		RunTool: func(ctx context.Context, tool, args string) (string, error) {
			var m map[string]any
			if err := json.Unmarshal([]byte(args), &m); err != nil {
				m = map[string]any{"command": args}
			}
			cmd, _ := m["command"].(string)
			if strings.TrimSpace(cmd) == "" {
				return "", &simpleErr{"tool 节点缺少 command（args 需为 {\"command\":\"...\"}）"}
			}
			bashIn, _ := json.Marshal(map[string]any{"command": cmd})
			timeout := 120 * time.Second
			cctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			res, err := agent.HostBash().Call(cctx, bashIn, nil)
			if err != nil {
				return "", err
			}
			return textOf(res), nil
		},
		RunAgent: func(ctx context.Context, agentKey, instruction string) (string, error) {
			cfg, ok := s.loadLLMConfig()
			if !ok {
				return "", errNoLLM
			}
			prov, err := cfg.NewProvider()
			if err != nil {
				return "", err
			}
			cctx, cancel := context.WithTimeout(ctx, 120*time.Second)
			defer cancel()
			out, err := agentcore.Run(cctx, agentcore.Options{
				Provider:       prov,
				SystemPrompt:   []string{"你是工作流 agent 节点。按指令完成一步，只输出结论文本。"},
				PermissionMode: acperm.ModeBypass,
				MaxTurns:       1,
				MaxTokens:      1024,
			}, instruction)
			if err != nil {
				return "", err
			}
			return strings.TrimSpace(out), nil
		},
		Hitl: func(ctx context.Context, n workflow.Node) (bool, string, error) {
			log.Printf("[workflow] hitl 节点 %s：指令=%s（v1 自动放行）", n.ID, firstLine(n.Instruction, 120))
			return true, "auto-approve", nil
		},
	}
}

var errNoLLM = &simpleErr{"LLM 未配置，agent 节点无法运行"}

type simpleErr struct{ msg string }

func (e *simpleErr) Error() string { return e.msg }

func textOf(res actool.Result) string {
	return res.Flatten()
}

// ---------- handlers ----------

func (s *Server) workflowValidate(w http.ResponseWriter, r *http.Request) {
	var g workflow.Graph
	if err := decode(r, &g); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	errs := g.Validate()
	writeJSON(w, 200, map[string]any{"ok": len(errs) == 0, "errors": errs})
}

type workflowSaveReq struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Graph       workflow.Graph `json:"graph"`
	ID          int64          `json:"id,omitempty"`
	Enabled     bool           `json:"enabled"`
}

func (s *Server) workflowSave(w http.ResponseWriter, r *http.Request) {
	var req workflowSaveReq
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if pv := r.PathValue("id"); pv != "" {
		if id, ok := pathInt(r, "id"); ok {
			req.ID = id
		}
	}
	if strings.TrimSpace(req.Name) == "" {
		writeErr(w, 400, "名称必填")
		return
	}
	if errs := req.Graph.Validate(); len(errs) > 0 {
		writeErr(w, 400, "图校验失败: "+strings.Join(errs, "; "))
		return
	}
	gj, _ := json.Marshal(req.Graph)
	var id int64
	var err error
	if req.ID > 0 {
		err = s.m.pg.UpdateWorkflowGraph(req.ID, req.Name, req.Description, string(gj), req.Enabled)
		id = req.ID
	} else {
		id, err = s.m.pg.CreateWorkflowGraph(req.Name, req.Description, string(gj))
	}
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"id": id})
}

func (s *Server) workflowList(w http.ResponseWriter, r *http.Request) {
	graphs, err := s.m.pg.ListWorkflowGraphs()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(graphs))
	for _, g := range graphs {
		out = append(out, map[string]any{
			"id": g.ID, "name": g.Name, "description": g.Description,
			"enabled": g.Enabled, "created_at": g.CreatedAt,
		})
	}
	writeJSON(w, 200, map[string]any{"workflows": out})
}

func (s *Server) workflowGet(w http.ResponseWriter, r *http.Request) {
	id, _ := pathInt(r, "id")
	g, err := s.m.pg.GetWorkflowGraph(id)
	if err != nil {
		writeErr(w, 404, "workflow not found")
		return
	}
	var graph workflow.Graph
	_ = json.Unmarshal([]byte(g.GraphJSON), &graph)
	writeJSON(w, 200, map[string]any{
		"id": g.ID, "name": g.Name, "description": g.Description, "enabled": g.Enabled, "graph": graph,
	})
}

func (s *Server) workflowDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := pathInt(r, "id")
	if err := s.m.pg.DeleteWorkflowGraph(id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": id})
}

func (s *Server) workflowRun(w http.ResponseWriter, r *http.Request) {
	id, _ := pathInt(r, "id")
	g, err := s.m.pg.GetWorkflowGraph(id)
	if err != nil {
		writeErr(w, 404, "workflow not found")
		return
	}
	var graph workflow.Graph
	if err := json.Unmarshal([]byte(g.GraphJSON), &graph); err != nil {
		writeErr(w, 500, "graph 解析失败: "+err.Error())
		return
	}
	var req struct {
		Inputs map[string]string `json:"inputs"`
	}
	_ = decode(r, &req)
	inputsJSON, _ := json.Marshal(req.Inputs)
	runID, err := s.m.pg.CreateWorkflowRun(id, string(inputsJSON))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(s.ctx, 15*time.Minute)
		defer cancel()
		res, runErr := s.workflowEngine(false).Run(ctx, &graph, req.Inputs)
		status, result, errMsg := "completed", "", ""
		if runErr != nil {
			status, errMsg = "failed", runErr.Error()
		} else {
			resultJSON, _ := json.Marshal(res)
			result = string(resultJSON)
			if res.Status != "completed" {
				status = res.Status
			}
		}
		if err := s.m.pg.FinishWorkflowRun(runID, status, result, errMsg); err != nil {
			log.Printf("[workflow] 落库运行 %d 失败: %v", runID, err)
		}
	}()
	writeJSON(w, 200, map[string]any{"run_id": runID})
}

func (s *Server) workflowRunDetail(w http.ResponseWriter, r *http.Request) {
	runID, _ := pathInt(r, "id")
	run, err := s.m.pg.GetWorkflowRun(runID)
	if err != nil {
		writeErr(w, 404, "run not found")
		return
	}
	writeJSON(w, 200, run)
}

func (s *Server) workflowRuns(w http.ResponseWriter, r *http.Request) {
	id, _ := pathInt(r, "id")
	runs, err := s.m.pg.ListWorkflowRuns(id, 20)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"runs": runs})
}

func (s *Server) workflowDryRun(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Graph  workflow.Graph    `json:"graph"`
		Inputs map[string]string `json:"inputs"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	res, err := s.workflowEngine(true).Run(r.Context(), &req.Graph, req.Inputs)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, res)
}
