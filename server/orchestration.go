package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
	"github.com/RestXtra/RestXtraAI/agent"
	pgdb "github.com/RestXtra/RestXtraAI/db"
)

// jsonResult marshals v to a JSON tool result.
func jsonResult(v any) (actool.Result, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return actool.Errorf(err.Error()), nil
	}
	return actool.Text(string(b)), nil
}

// 本文件实现 P2「跨任务编排工具集」(docs/跑分编排 §2 P2)。这些是 host 工具——需要
// 访问 Manager(任意任务的 Store)、Engine(暂停)、以及建任务流程,所以住在 server 层。
// 读类工具把「现有 per-task 工具」重定向到目标任务的 store 上跑(建一个临时 ToolSet
// 并 Call 其对应工具),从而复用完全相同的逻辑;控制类(spawn/pause)直接调 Manager/Engine。
// 它们像流量工具一样 seed 进 tools 表、按 agent 绑定(只绑给编排 agent 才可见)。

// hostTools is the runtime host-tool provider fed to ToolAugment: traffic tools
// (gated by capture) + cross-task orchestration tools + user-defined custom tools.
// The second return is the names of custom tools flagged `deferred` (schema
// withheld, routed via SearchExtraTools/ExecuteExtraTool). Per-agent binding still
// decides who actually sees any of them.
//
// convCompanyKey carries the current conversation's company id (if any) into
// agent tools, so tasks spawned from a chat inherit the enterprise link.
//
//nolint:unused // used as the hostTools provider in wireAgentAugment
type convCompanyKey struct{}

// convCompanyID returns the conversation's company id from ctx, or 0.
func convCompanyID(ctx context.Context) int64 {
	if v, ok := ctx.Value(convCompanyKey{}).(*int64); ok && v != nil {
		return *v
	}
	return 0
}

func (s *Server) hostTools() ([]actool.CoreTool, map[string][]string) {
	tools := append(s.m.HostTools(), s.orchestrationTools()...)
	tools = append(tools, s.platformTools()...) // 平台操作工具(建改 skill/工具/MCP，给 Auto 用)
	custom, err := s.customTools()
	if err != nil {
		log.Printf("[custom-tool] 加载失败: %v", err)
		return tools, nil
	}
	tools = append(tools, custom...)
	// deferred custom tools → name -> its bound agent keys. ToolAugment turns a
	// name into a deferred entry only for agents it's actually bound to (so we don't
	// advertise a tool the per-agent binding will drop from the callable set).
	deferred := map[string][]string{}
	rows, _ := s.m.pg.ListCustomTools()
	for _, t := range rows {
		if t.Deferred && t.Enabled {
			deferred[t.Key] = t.Agents
		}
	}
	return tools, deferred
}

// orchestrationTools returns the cross-task tool set. Bound per-agent via the
// tools table (default: no binding — opt-in for orchestration agents).
func (s *Server) orchestrationTools() []actool.CoreTool {
	return []actool.CoreTool{
		s.toolListTasks(),
		s.toolListLLMProfiles(),
		s.toolSpawnTask(),
		s.toolWaitTask(),
		s.toolGetTaskResult(),
		s.toolPauseTask(),
		s.toolGetTaskGraph(),
		s.toolListTaskFindings(),
		s.toolAddHint(),
		s.toolGetWorkerTrace(),
		s.toolListWorkerTraces(),
		s.toolSearchWorkerTraces(),
	}
}

// --- schema helpers ---

func strParam(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

// parseProfileID reads an LLM profile id from a tool arg that may arrive as a JSON
// number (5) or a numeric string ("5"); returns 0 when absent/unparseable.
func parseProfileID(raw json.RawMessage) int64 {
	if len(raw) == 0 {
		return 0
	}
	var n int64
	if json.Unmarshal(raw, &n) == nil {
		return n
	}
	var str string
	if json.Unmarshal(raw, &str) == nil {
		v, _ := strconv.ParseInt(strings.TrimSpace(str), 10, 64)
		return v
	}
	return 0
}

func objSchema(props map[string]any, required ...string) map[string]any {
	m := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		req := make([]any, len(required))
		for i, r := range required {
			req[i] = r
		}
		m["required"] = req
	}
	return m
}

func roTool(name, desc string, schema map[string]any, run func(context.Context, json.RawMessage) (actool.Result, error)) actool.CoreTool {
	return actool.Build(actool.Spec{
		Name: name, Description: desc, Schema: schema,
		ReadOnly:   func(json.RawMessage) bool { return true },
		Concurrent: func(json.RawMessage) bool { return true },
		Permissions: func(context.Context, json.RawMessage, permission.Context) permission.Decision {
			return permission.Allowed()
		},
		Run: func(ctx context.Context, in json.RawMessage, _ *actool.ToolContext) (actool.Result, error) {
			return run(ctx, in)
		},
	})
}

func wrTool(name, desc string, schema map[string]any, run func(context.Context, json.RawMessage) (actool.Result, error)) actool.CoreTool {
	return actool.Build(actool.Spec{
		Name: name, Description: desc, Schema: schema,
		Permissions: func(context.Context, json.RawMessage, permission.Context) permission.Decision {
			return permission.Allowed()
		},
		Run: func(ctx context.Context, in json.RawMessage, _ *actool.ToolContext) (actool.Result, error) {
			return run(ctx, in)
		},
	})
}

// delegateToTask resolves the `task_id` in the input, builds a ToolSet bound to
// that task's store, strips task_id, and calls the chosen per-task tool — so the
// cross-task read reuses the exact in-task logic against another task.
func (s *Server) delegateToTask(ctx context.Context, in json.RawMessage, pick func(*agent.ToolSet) actool.CoreTool) (actool.Result, error) {
	var head struct {
		TaskID string `json:"task_id"`
	}
	_ = json.Unmarshal(in, &head)
	if strings.TrimSpace(head.TaskID) == "" {
		return actool.Errorf("task_id 为必填"), nil
	}
	t, ok := s.m.Task(head.TaskID)
	if !ok {
		return actool.Errorf("task 不存在: " + head.TaskID), nil
	}
	var m map[string]json.RawMessage
	_ = json.Unmarshal(in, &m)
	delete(m, "task_id")
	inner, _ := json.Marshal(m)
	tsx := agent.NewToolSet(t.Store, "orchestrator")
	if s.m.Assets() != nil {
		tsx.SetAssetStore(s.m.Assets(), s.m.Assets().Companies())
	}
	tsx.SetNotify(t.Notify) // hint writes wake this task's planner (no-op for read tools)
	return pick(tsx).Call(ctx, inner, nil)
}

// --- tools ---

func (s *Server) toolListTasks() actool.CoreTool {
	return roTool("list_tasks",
		"列出所有任务(id/描述/目标/状态/运行时长/父任务/LLM 配置)，编排 agent 用它掌握全局、看哪些任务卡太久、各自用哪个 LLM。运行时长：运行中=创建→现在，终态=创建→最后活动(秒)。llm_profile：任务 planner/worker 用的配置名，(激活配置)=跟随全局激活。",
		objSchema(map[string]any{}),
		func(context.Context, json.RawMessage) (actool.Result, error) {
			lastAct, _ := s.m.PG().LastActivityAll()
			// id -> name to resolve each task's pinned LLM profile.
			profName := map[int64]string{}
			if profs, err := s.m.pg.ListProfiles(); err == nil {
				for _, p := range profs {
					profName[p.ID] = p.Name
				}
			}
			out := make([]map[string]any, 0)
			for _, t := range s.m.List() {
				status := s.deriveTaskStatus(t)
				end := lastAct[t.ExpID]
				if live := s.engine.LastActivity(t.ID); live > end {
					end = live
				}
				dur := int64(0)
				if status == "running" {
					dur = time.Now().Unix() - t.CreatedAt
				} else if end > t.CreatedAt {
					dur = end - t.CreatedAt
				}
				row := map[string]any{"id": t.ID, "description": t.Description, "goal": t.Goal, "status": status, "run_seconds": dur}
				if t.ParentRef != "" {
					row["parent_ref"] = t.ParentRef
				}
				if t.LLMProfileID == nil {
					row["llm_profile"] = "(激活配置)"
				} else if n, ok := profName[*t.LLMProfileID]; ok {
					row["llm_profile"] = n
				} else {
					row["llm_profile"] = fmt.Sprintf("#%d(已删除)", *t.LLMProfileID)
				}
				out = append(out, row)
			}
			return jsonResult(out)
		})
}

// toolListLLMProfiles lists the available LLM profiles (name/model/active) so an
// orchestration agent can pick one for spawn_task's llm_profile. Never leaks keys.
func (s *Server) toolListLLMProfiles() actool.CoreTool {
	return roTool("list_llm_profiles",
		"列出可用的 LLM 配置(profile)：id、名称、模型、格式、是否为当前激活配置。用 id 给 spawn_task 的 llm_profile_id 参数指定子任务专属 LLM（如侦察用便宜模型、利用用强模型）。不含 API Key。",
		objSchema(map[string]any{}),
		func(context.Context, json.RawMessage) (actool.Result, error) {
			profs, err := s.m.pg.ListProfiles()
			if err != nil {
				return actool.Errorf(err.Error()), nil
			}
			out := make([]map[string]any, 0, len(profs))
			for _, p := range profs {
				out = append(out, map[string]any{
					"id": p.ID, "name": p.Name, "model": p.Model, "format": p.Format, "is_active": p.IsDefault,
				})
			}
			return jsonResult(map[string]any{"profiles": out})
		})
}

func (s *Server) toolSpawnTask() actool.CoreTool {
	return wrTool("spawn_task",
		"新建一个隔离子任务并启动探索引擎，返回结构化 delegation contract。只传 objective、asset_ids、required_evidence、budget、allowed_tools 等最小交接信息，不复制父 Agent 历史。parent_ref 用于父子关联。",
		objSchema(map[string]any{
			"description":       strParam("任务描述(简短标题)"),
			"objective":         strParam("子 Agent 的唯一目标（推荐；goal 作为旧参数仍兼容）"),
			"goal":              strParam("兼容旧调用：任务目标；objective 为空时使用"),
			"parent_ref":        strParam("可选：父任务 id(做父子关联)"),
			"asset_ids":         map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": "子 Agent 可直接引用的目标资产 id；只传引用，不复制资产/历史正文"},
			"required_evidence": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "完成条件要求的证据清单；为空时使用平台安全默认"},
			"allowed_tools":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "可选能力白名单。图谱核心读写工具始终保留；Bash、外部工具、MCP/Skill 仅白名单内可用。空数组保持普通任务工具策略"},
			"agent":             map[string]any{"type": "string", "description": "可选：指定本子任务由哪个专用 agent 执行（agent key，如 asset_intel 信息收集 / web_vuln 漏洞猎人 / exploit 利用专家 / pentest_chain 渗透链 / cloud_attack / evasion / binary_vuln / code_audit）。子任务的 planner/worker 会以该 agent 的身份与打法运行。"},
			"budget": map[string]any{"type": "object", "description": "子 Agent 预算", "properties": map[string]any{
				"max_wall_time_seconds": map[string]any{"type": "integer", "description": "墙钟上限；映射为任务 deadline 并强制执行"},
				"max_input_tokens":      map[string]any{"type": "integer", "description": "输入 token 预算，用于结果核算/熔断决策"},
				"max_output_tokens":     map[string]any{"type": "integer", "description": "输出 token 预算，用于结果核算/熔断决策"},
				"max_tool_calls":        map[string]any{"type": "integer", "description": "工具调用预算，用于结果核算/熔断决策"},
			}},
			"llm_profile_id":  map[string]any{"type": "integer", "description": "可选：指定本子任务 planner/worker 用的 LLM 配置 id(见 list_llm_profiles)；留空则继承父任务、再回退全局激活配置"},
			"timeout_seconds": map[string]any{"type": "integer", "description": "兼容旧调用：任务级超时；budget.max_wall_time_seconds 优先"},
		}, "description"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				Description      string                `json:"description"`
				Objective        string                `json:"objective"`
				Goal             string                `json:"goal"`
				ParentRef        string                `json:"parent_ref"`
				AssetIDs         []int64               `json:"asset_ids"`
				RequiredEvidence []string              `json:"required_evidence"`
				AllowedTools     []string              `json:"allowed_tools"`
				Agent            string                `json:"agent"`
				Budget           pgdb.DelegationBudget `json:"budget"`
				LLMProfileID     json.RawMessage       `json:"llm_profile_id"`
				TimeoutSeconds   int                   `json:"timeout_seconds"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf("参数解析失败: " + err.Error()), nil
			}
			if strings.TrimSpace(a.Description) == "" {
				a.Description = "未命名任务"
			}
			objective := strings.TrimSpace(a.Objective)
			if objective == "" {
				objective = strings.TrimSpace(a.Goal)
			}
			if objective == "" {
				return actool.Errorf("objective（或兼容参数 goal）为必填"), nil
			}
			if a.ParentRef != "" {
				if _, ok := s.m.Task(a.ParentRef); !ok {
					return actool.Errorf("父任务不存在: " + a.ParentRef), nil
				}
			}
			if len(a.AssetIDs) > 0 {
				assets, err := s.m.Assets().GetByIDs(a.AssetIDs)
				if err != nil {
					return actool.Errorf("读取 asset_ids 失败: " + err.Error()), nil
				}
				found := map[int64]bool{}
				for _, asset := range assets {
					found[asset.ID] = true
				}
				for _, id := range a.AssetIDs {
					if id <= 0 || !found[id] {
						return actool.Errorf(fmt.Sprintf("asset 不存在: %d", id)), nil
					}
				}
			}
			a.Agent = strings.TrimSpace(a.Agent)
			if a.Agent != "" {
				if ag, err := s.m.pg.GetAgentByKey(a.Agent); err != nil || ag == nil {
					return actool.Errorf("指定 agent 不存在: " + a.Agent), nil
				}
			}
			if len(a.RequiredEvidence) == 0 {
				a.RequiredEvidence = []string{
					"facts include summary, evidence and confidence",
					"negative results set negative=true and include evidence",
					"findings include reproducible request/response or command evidence",
				}
			}
			timeout := a.Budget.MaxWallTimeSeconds
			if timeout <= 0 {
				timeout = a.TimeoutSeconds
			}
			if timeout < 0 {
				timeout = 0
			}
			a.Budget.MaxWallTimeSeconds = timeout
			// LLM profile resolution: explicit id > inherit parent's pin > active(nil).
			var pin *int64
			if id := parseProfileID(a.LLMProfileID); id > 0 {
				if _, ok := s.loadProfileConfig(id); !ok {
					return actool.Errorf(fmt.Sprintf("LLM 配置 #%d 不存在或未设置 API Key", id)), nil
				}
				pin = &id
			} else if a.ParentRef != "" {
				if pt, ok := s.m.Task(a.ParentRef); ok {
					pin = pt.LLMProfileID
				}
			}
			var companyIDs []int64
			if cid := convCompanyID(ctx); cid > 0 {
				companyIDs = []int64{cid}
			}
			t, err := s.m.CreateTask(a.Description, objective, pin, timeout, 0, companyIDs)
			if err != nil {
				return actool.Errorf(err.Error()), nil
			}
			childID, _ := strconv.ParseInt(t.ID, 10, 64)
			contract := pgdb.TaskDelegation{ChildTaskID: childID, ParentRef: a.ParentRef,
				Objective: objective, AgentKey: a.Agent, AssetIDs: a.AssetIDs, RequiredEvidence: a.RequiredEvidence,
				AllowedTools: a.AllowedTools, Budget: a.Budget}
			if err := s.m.PG().SaveTaskDelegation(contract); err != nil {
				_ = s.m.DeleteTask(t.ID)
				return actool.Errorf("保存 delegation contract 失败: " + err.Error()), nil
			}
			if saved, err := s.m.PG().GetTaskDelegation(childID); err == nil && saved != nil {
				contract = *saved
			}
			t.AllowedTools = append([]string(nil), contract.AllowedTools...)
			t.DelegationBudget = contract.Budget
			if contract.AgentKey != "" {
				t.AgentKey = contract.AgentKey
				if err := s.m.PG().SetTaskAgentKey(childID, contract.AgentKey); err != nil {
					log.Printf("[spawn] 设置子任务 agent_key 失败: %v", err)
				}
			}
			if a.ParentRef != "" {
				t.ParentRef = a.ParentRef
				if err := s.m.PG().SetParentRef(childID, a.ParentRef); err != nil {
					_ = s.m.DeleteTask(t.ID)
					return actool.Errorf("保存父子任务关系失败: " + err.Error()), nil
				}
			}
			origin, err := t.Store.OriginFactID()
			if err != nil {
				_ = s.m.DeleteTask(t.ID)
				return actool.Errorf("读取子任务 origin 失败: " + err.Error()), nil
			}
			if origin > 0 {
				for _, assetID := range contract.AssetIDs {
					if err := t.Store.Anchor(origin, assetID); err != nil {
						_ = s.m.DeleteTask(t.ID)
						return actool.Errorf(fmt.Sprintf("锚定 asset %d 失败: %v", assetID, err)), nil
					}
				}
			}
			s.seed(t, a.Description+" "+objective) // seed 初始资产，喂给事件驱动 loop
			s.createGoals(s.ctx, t, nil)           // 目标分解(LLM;规则兜底)
			s.engine.Run(s.ctx, t)                 // 启动该任务的探索引擎
			return jsonResult(map[string]any{"task_id": t.ID, "delegation": contract})
		})
}

func resultPayload(node *pgdb.Node, fields ...string) map[string]any {
	var payload map[string]any
	_ = json.Unmarshal(node.Payload, &payload)
	out := map[string]any{"id": node.ID}
	for _, field := range fields {
		if value, ok := payload[field]; ok {
			if text, ok := value.(string); ok {
				value = firstLine(text, 1000)
			}
			out[field] = value
		}
	}
	return out
}

// taskDelegationResult returns a bounded, structured child-agent result. Full
// transcripts and artifact bodies stay external and are available only through
// explicit trace/artifact reads.
func (s *Server) taskDelegationResult(t *Task) (map[string]any, error) {
	result := map[string]any{"task_id": t.ID, "status": s.deriveTaskStatus(t)}
	childID, _ := strconv.ParseInt(t.ID, 10, 64)
	delegation, err := s.m.PG().GetTaskDelegation(childID)
	if err != nil {
		return nil, err
	}
	if delegation != nil {
		result["assignment"] = delegation
	} else {
		result["assignment"] = map[string]any{"schema_version": 1, "child_task_id": childID, "objective": t.Goal}
	}

	nodes, err := t.Store.ListByKind(pgdb.KindFact, 200)
	if err != nil {
		return nil, err
	}
	facts := make([]map[string]any, 0, len(nodes))
	negative := make([]map[string]any, 0)
	for _, node := range nodes {
		if node.State != "confirmed" {
			continue
		}
		entry := resultPayload(node, "summary", "confidence", "evidence")
		var payload struct {
			Negative bool `json:"negative"`
		}
		_ = json.Unmarshal(node.Payload, &payload)
		if payload.Negative {
			negative = append(negative, entry)
		} else {
			facts = append(facts, entry)
		}
	}
	result["facts"] = facts
	result["negative_results"] = negative

	findNodes, err := t.Store.ListByKind(pgdb.KindFinding, 100)
	if err != nil {
		return nil, err
	}
	findings := make([]map[string]any, 0, len(findNodes))
	for _, node := range findNodes {
		if node.State == "confirmed" {
			findings = append(findings, resultPayload(node, "summary", "vulnclass", "severity", "evidence", "flag"))
		}
	}
	result["findings"] = findings

	artifacts, err := t.Store.Artifacts("", 0, 100)
	if err != nil {
		return nil, err
	}
	artifactRefs := make([]map[string]any, 0, len(artifacts))
	for _, artifact := range artifacts {
		artifactRefs = append(artifactRefs, map[string]any{
			"id": artifact.ID, "node_id": artifact.NodeID, "path": artifact.StoragePath,
			"content_hash": artifact.ContentHash, "mime_type": artifact.MIMEType,
			"byte_size": artifact.ByteSize, "line_count": artifact.LineCount,
			"summary": firstLine(artifact.Summary, 300), "permission": artifact.PermissionLabel,
		})
	}
	result["artifact_refs"] = artifactRefs

	frontier, err := t.Store.Frontier(20)
	if err != nil {
		return nil, err
	}
	next := make([]map[string]any, 0, len(frontier))
	for _, node := range frontier {
		next = append(next, resultPayload(node, "summary", "asset_ids"))
	}
	result["next_actions"] = next

	usage, err := t.Store.TokenTotal()
	if err != nil {
		return nil, err
	}
	costs, err := t.Store.RoundCostsByWorker()
	if err != nil {
		return nil, err
	}
	total := sumRoundCosts(costs)
	runSeconds := time.Now().Unix() - t.CreatedAt
	if t.CompletedAt > 0 {
		runSeconds = t.CompletedAt - t.CreatedAt
	}
	if runSeconds < 0 {
		runSeconds = 0
	}
	result["usage"] = map[string]any{
		"input_tokens": usage.InputTokens, "output_tokens": usage.OutputTokens,
		"cache_read_tokens": usage.CacheReadTokens, "cache_write_tokens": usage.CacheWriteTokens,
		"tool_calls": total.ToolCalls, "tool_errors": total.ToolErrors, "rounds": total.Rounds,
		"wall_time_seconds": runSeconds,
	}
	if delegation != nil {
		exceeded := make([]string, 0, 4)
		if max := delegation.Budget.MaxWallTimeSeconds; max > 0 && runSeconds >= int64(max) {
			exceeded = append(exceeded, "max_wall_time_seconds")
		}
		if max := delegation.Budget.MaxInputTokens; max > 0 && usage.InputTokens >= max {
			exceeded = append(exceeded, "max_input_tokens")
		}
		if max := delegation.Budget.MaxOutputTokens; max > 0 && usage.OutputTokens >= max {
			exceeded = append(exceeded, "max_output_tokens")
		}
		if max := delegation.Budget.MaxToolCalls; max > 0 && total.ToolCalls >= max {
			exceeded = append(exceeded, "max_tool_calls")
		}
		result["budget_exceeded"] = exceeded
	}
	return result, nil
}

func (s *Server) toolGetTaskResult() actool.CoreTool {
	return roTool("get_task_result",
		"一次读取子 Agent 的结构化结果：assignment、facts、findings、negative_results、artifact_refs、next_actions、usage。不会返回完整 transcript 或大工具输出；需要原文时再按引用读取。",
		objSchema(map[string]any{"task_id": strParam("子任务 id")}, "task_id"),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				TaskID string `json:"task_id"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf("参数解析失败: " + err.Error()), nil
			}
			t, ok := s.m.Task(strings.TrimSpace(a.TaskID))
			if !ok {
				return actool.Errorf("task 不存在: " + a.TaskID), nil
			}
			result, err := s.taskDelegationResult(t)
			if err != nil {
				return actool.Errorf(err.Error()), nil
			}
			return jsonResult(result)
		})
}

func (s *Server) toolPauseTask() actool.CoreTool {
	return wrTool("pause_task", "暂停指定任务(停止其 planner/worker 循环)。",
		objSchema(map[string]any{"task_id": strParam("要暂停的任务 id")}, "task_id"),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				TaskID string `json:"task_id"`
			}
			_ = json.Unmarshal(in, &a)
			if _, ok := s.m.Task(a.TaskID); !ok {
				return actool.Errorf("task 不存在: " + a.TaskID), nil
			}
			s.engine.Pause(a.TaskID)
			return actool.Text("task paused: " + a.TaskID), nil
		})
}

// toolWaitTask blocks until ANY of the given tasks reaches a terminal state
// (done/timeout/stopped) or the wait budget elapses, polling periodically. It
// returns the states of ALL watched tasks so the orchestrator can immediately
// bench_close finished containers and spawn replacements — no blind sleep.
func (s *Server) toolWaitTask() actool.CoreTool {
	return wrTool("wait_task",
		"阻塞等待任务进入终态，最多等 timeout_seconds(默认 600)。支持同时等待多个任务；任一结束立即返回所有状态，并为已结束子任务内联结构化 result（facts/findings/negative_results/artifact_refs/next_actions/usage），无需再拉完整图或 transcript。",
		objSchema(map[string]any{
			"task_id":         strParam("要等待的任务 id(单个，来自 spawn_task)；或用 task_ids 等多个"),
			"task_ids":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "可选：要同时等待的多个任务 id(任一结束即返回)"},
			"timeout_seconds": map[string]any{"type": "integer", "description": "可选：最多等多少秒(默认 600，0 也按 600)"},
		}),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				TaskID         string   `json:"task_id"`
				TaskIDs        []string `json:"task_ids"`
				TimeoutSeconds int      `json:"timeout_seconds"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf("参数解析失败: " + err.Error()), nil
			}
			ids := a.TaskIDs
			if strings.TrimSpace(a.TaskID) != "" {
				ids = append([]string{a.TaskID}, ids...)
			}
			if len(ids) == 0 {
				return actool.Errorf("task_id 或 task_ids 至少提供一个"), nil
			}
			seenInput := map[string]bool{}
			deduped := ids[:0]
			for _, id := range ids {
				id = strings.TrimSpace(id)
				if id != "" && !seenInput[id] {
					seenInput[id] = true
					deduped = append(deduped, id)
				}
			}
			ids = deduped
			// 并发感知：把显式指定的任务扩展为它的"兄弟任务"(同 parent_ref 的其它在跑任务)
			// 一起监控 —— 这样即便 agent 只传单个 task_id，也能一次感知同批任务里任意一个
			// 结束，避免逐个 wait_task 串行等待。parent_ref 为空时扩展所有非终态任务。
			expand := func() []string {
				seen := map[string]bool{}
				var out []string
				for _, id := range ids {
					if !seen[id] {
						seen[id] = true
						out = append(out, id)
					}
				}
				var parentRef string
				if t, ok := s.m.Task(ids[0]); ok {
					parentRef = t.ParentRef
				}
				for _, t := range s.m.List() {
					if isTerminalStatus(t.Status) {
						continue
					}
					if parentRef != "" && t.ParentRef != parentRef {
						continue
					}
					if !seen[t.ID] {
						seen[t.ID] = true
						out = append(out, t.ID)
					}
				}
				return out
			}
			if len(ids) == 1 {
				ids = expand()
			}
			wait := a.TimeoutSeconds
			if wait <= 0 {
				wait = 600
			}
			deadline := time.Now().Add(time.Duration(wait) * time.Second)
			for {
				tasks := make([]map[string]any, 0, len(ids))
				anyDone := false
				for _, id := range ids {
					t, ok := s.m.Task(id)
					if !ok {
						tasks = append(tasks, map[string]any{"task_id": id, "status": "not_found"})
						anyDone = true
						continue
					}
					st := s.deriveTaskStatus(t)
					row := map[string]any{"task_id": id, "status": st}
					if st == "done" || st == "timeout" || st == "stopped" || st == "failed" {
						anyDone = true
						if result, err := s.taskDelegationResult(t); err == nil {
							row["result"] = result
						} else {
							row["result_error"] = err.Error()
						}
					}
					tasks = append(tasks, row)
				}
				if anyDone {
					return jsonResult(map[string]any{"reason": "terminal", "tasks": tasks})
				}
				if time.Now().After(deadline) {
					return jsonResult(map[string]any{"reason": "wait_timeout", "waited_seconds": wait, "tasks": tasks})
				}
				select {
				case <-ctx.Done():
					return jsonResult(map[string]any{"reason": "cancelled", "tasks": tasks})
				case <-time.After(5 * time.Second):
				}
			}
		})
}

func (s *Server) toolGetTaskGraph() actool.CoreTool {
	return roTool("get_task_graph", "读指定任务的探索图总览(同 graph_overview：资产计数/frontier/发现/覆盖等)，用 task_id 指定任务。",
		objSchema(map[string]any{"task_id": strParam("任务 id")}, "task_id"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			return s.delegateToTask(ctx, in, (*agent.ToolSet).GraphOverviewTool)
		})
}

func (s *Server) toolListTaskFindings() actool.CoreTool {
	return roTool("list_task_findings", "读指定任务的确认漏洞(含 flag/PoC；每条带 id/task_id/intent_id/vulnclass/severity/摘要/状态)，用 task_id 指定任务。",
		objSchema(map[string]any{"task_id": strParam("任务 id")}, "task_id"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			return s.delegateToTask(ctx, in, (*agent.ToolSet).ListFindingsTool)
		})
}

func (s *Server) toolAddHint() actool.CoreTool {
	return wrTool("add_task_hint", "给指定任务注入战略提示(该任务的 planner 下轮生成意图时会读到)。\n"+
		"★优先批量：多条提示放进 hints 数组一次提交（返回 ids 数组，与 hints 等长同序，失败项 id=0）；单条则省略 hints 直接给顶层 text。",
		objSchema(map[string]any{
			"task_id":   strParam("任务 id"),
			"hints":     map[string]any{"type": "array", "description": "【优先用这个】提示数组，每个元素字段同顶层（text/asset_ids）。", "items": map[string]any{"type": "object"}},
			"text":      strParam("[单条] 提示内容"),
			"asset_ids": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": "锚定的资产 id（可选，0/1/多个；该任务内的资产 id）"},
		}, "task_id"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			return s.delegateToTask(ctx, in, (*agent.ToolSet).AddHintTool)
		})
}

func (s *Server) toolGetWorkerTrace() actool.CoreTool {
	return roTool("get_task_worker_trace",
		"看指定任务里某个 work(意图)的执行过程：get_task_worker_trace(task_id, intent_id) 看步骤摘要；再带 step_ids=[...] 取那几步完整内容(一次≤5)。",
		objSchema(map[string]any{
			"task_id":   strParam("任务 id"),
			"intent_id": map[string]any{"type": "integer", "description": "意图 id(该任务里的 work)"},
			"step_ids":  map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": "可选：要取完整内容的步骤 id(≤5)"},
		}, "task_id", "intent_id"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			return s.delegateToTask(ctx, in, (*agent.ToolSet).GetWorkerTraceTool)
		})
}

func (s *Server) toolListWorkerTraces() actool.CoreTool {
	return roTool("list_task_worker_traces", "列出指定任务里跑过哪些 work(意图) + 各自步数，用于发现哪些 work 值得翻看(再用 get_task_worker_trace)。",
		objSchema(map[string]any{"task_id": strParam("任务 id")}, "task_id"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			return s.delegateToTask(ctx, in, (*agent.ToolSet).ListWorkerTracesTool)
		})
}

func (s *Server) toolSearchWorkerTraces() actool.CoreTool {
	return roTool("search_task_worker_traces", "在指定任务里按关键字搜索所有 work 的执行过程(返回命中步骤摘要 + intent_id)。",
		objSchema(map[string]any{"task_id": strParam("任务 id"), "q": strParam("搜索关键字")}, "task_id", "q"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			return s.delegateToTask(ctx, in, (*agent.ToolSet).SearchWorkerTracesTool)
		})
}

// deriveTaskStatus mirrors listTasks' status derivation for the list_tasks tool.
func (s *Server) deriveTaskStatus(t *Task) string {
	switch {
	case isTerminalStatus(t.Status):
		return t.Status
	case t.Paused || s.engine.IsPaused(t.ID):
		return "paused"
	case s.engine.Ready() && s.engine.Started(t.ID):
		return "running"
	}
	return "created"
}

// orchestrationToolSeeds seeds the cross-task tools into the tools table so they
// are bindable per-agent (default: bound to nobody — opt-in for orchestration
// agents). First-insert only, like the traffic seeds.
func (s *Server) seedOrchestrationTools() {
	// task-op + platform tools default-bind to the built-in Auto agent (它天生用来
	// 操作平台)。SeedTool 首插入生效;老库已 seed 的行由 seedAutoDefaultBindings 补绑。
	autoAgents, _ := json.Marshal([]string{"auto"})
	for _, t := range s.orchestrationTools() {
		schema, _ := json.Marshal(t.InputSchema())
		_ = s.m.PG().SeedTool(t.Name(), t.Description(), schema, autoAgents)
	}
	for _, t := range s.platformTools() {
		schema, _ := json.Marshal(t.InputSchema())
		agents := autoAgents
		// C2 工具绑定到 worker：任务执行时 worker 用它驱动后渗透。
		if t.Name() == "c2_postex" || t.Name() == "c2_task_result" || t.Name() == "c2_session_list" {
			agents, _ = json.Marshal([]string{"auto", "worker", "postex"})
		}
		// 连接管理工具绑定到 worker/postex/responder：agent 可经受管连接执行命令。
		if t.Name() == "conn_list" || t.Name() == "conn_exec" || t.Name() == "conn_contain" {
			agents, _ = json.Marshal([]string{"auto", "worker", "postex", "responder"})
		}
		// 弱口令探测工具绑定到进攻 agent（小范围/需审批）。
		if t.Name() == "weak_password_probe" {
			agents, _ = json.Marshal([]string{"worker", "auto", "web_vuln", "exploit", "pentest_chain", "red_team_lead", "evasion", "pentest"})
		}
		_ = s.m.PG().SeedTool(t.Name(), t.Description(), schema, agents)
	}
	s.refreshBuiltinToolSchemas()
	s.seedAutoDefaultBindings()
	s.seedPlannerDefaultBindings()
	s.seedAutoReportFindingBinding()
	s.seedC2AgentBindings()
	s.seedConnAgentBindings()
	s.seedWeakpassAgentBindings()
	// 注：pentest 的默认工具绑定无需迁移——BuiltinToolSeeds 在全新初始化时就把
	// list_assets/insert_assets/report_finding/list_findings/list_companies 连同
	// pentest 一起 seed 好了（项目尚无旧库，不做迁移）。
}

// refreshBuiltinToolSchemas propagates code schema/description changes on the
// orchestration + platform tools into already-seeded rows ONCE per version flag —
// SeedTool is first-insert-only, so a new param (e.g. spawn_task 的 llm_profile) never
// reaches an old DB otherwise. Preserves each tool's agent binding + enabled flag.
// Bump the flag whenever these tools' schemas/descriptions change in code.
func (s *Server) refreshBuiltinToolSchemas() {
	const flag = "tool_schema_refresh_v7_spawn_agent"
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	tools := append(s.orchestrationTools(), s.platformTools()...)
	for _, t := range tools {
		schema, _ := json.Marshal(t.InputSchema())
		if err := s.m.pg.RefreshToolDefaults(t.Name(), t.Description(), schema); err != nil {
			log.Printf("[tools] refresh %s schema failed: %v", t.Name(), err)
		}
	}
	// 同时把 planner 的 goal_met 描述刷成代码默认：旧库 seed 的描述带“结束本轮规划”的
	// 误导，会让 planner 把 goal_met 当成“结束空轮”的手段、刚开跑就误判整个任务完成。
	// report_finding 同步刷新：新增了结构化报告字段(title/url/impact/endpoints/repro/
	// remediation)，旧库首插的 schema 看不到新参数。
	for _, sd := range agent.BuiltinToolSeeds() {
		if sd.Key != "goal_met" && sd.Key != "report_finding" {
			continue
		}
		schema, _ := json.Marshal(sd.Schema)
		if err := s.m.pg.RefreshToolDefaults(sd.Key, sd.Desc, schema); err != nil {
			log.Printf("[tools] refresh %s desc failed: %v", sd.Key, err)
		}
	}
	_ = s.m.pg.SetSetting(flag, "true")
	log.Printf("[tools] 已刷新 orchestration/platform 工具 schema 到代码默认(一次性)")
}

// seedAutoReportFindingBinding adds "auto" to report_finding's binding ONCE so
// conversation-context agents can call it without requiring an intent_id.
func (s *Server) seedAutoReportFindingBinding() {
	const flag = "auto_report_finding_v1"
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	if err := s.m.pg.AddAgentToToolBinding("auto", []string{"report_finding"}); err != nil {
		log.Printf("[auto] report_finding 默认绑定失败: %v", err)
		return
	}
	_ = s.m.pg.SetSetting(flag, "true")
}

// seedC2AgentBindings adds the C2 post-exploitation tools to the worker, auto and
// postex agent bindings once, so task workers / chat assistants can drive post-ex
// on existing DBs.
func (s *Server) seedC2AgentBindings() {
	const flag = "c2_agent_bindings_v2"
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	c2Keys := []string{"c2_postex", "c2_task_result", "c2_session_list"}
	for _, agentKey := range []string{"worker", "auto", "postex"} {
		if err := s.m.pg.AddAgentToToolBinding(agentKey, c2Keys); err != nil {
			log.Printf("[c2] %s 绑定 c2 工具失败: %v", agentKey, err)
			return
		}
	}
	_ = s.m.pg.SetSetting(flag, "true")
	s.toolCatalog.Invalidate()
}

// seedConnAgentBindings adds the conn_* (connection-management) + responder
// investigation tools to the worker/auto/postex/responder agent bindings ONCE
// for existing DBs, so agents can drive managed connections without a fresh init.
func (s *Server) seedConnAgentBindings() {
	const flag = "conn_agent_bindings_v2"
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	connKeys := []string{"conn_list", "conn_exec", "conn_contain"}
	for _, agentKey := range []string{"worker", "auto", "postex", "responder"} {
		if err := s.m.pg.AddAgentToToolBinding(agentKey, connKeys); err != nil {
			log.Printf("[conn] %s 绑定 conn 工具失败: %v", agentKey, err)
			return
		}
	}
	// responder 额外绑定调查/取证工具。
	_ = s.m.pg.AddAgentToToolBinding("responder", []string{"search_knowledge", "list_assets", "insert_assets", "report_finding"})
	_ = s.m.pg.SetSetting(flag, "true")
	s.toolCatalog.Invalidate()
}

// (guarded by a settings flag), so existing DBs — whose report_finding row was
// seeded as worker-only — also let the planner record findings. Fresh DBs already
// get it via PlannerTools(); this only backfills without overriding a user unbind.
func (s *Server) seedPlannerDefaultBindings() {
	const flag = "planner_report_finding_v1"
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	if err := s.m.pg.AddAgentToToolBinding("planner", []string{"report_finding"}); err != nil {
		log.Printf("[planner] report_finding 默认绑定失败: %v", err)
		return
	}
	_ = s.m.pg.SetSetting(flag, "true")
}

// seedAutoDefaultBindings adds "auto" to the task-op + platform tools' bindings
// ONCE (guarded by a settings flag), so existing DBs whose tool rows were seeded
// before Auto existed still give Auto its default toolset — without re-adding it
// after a user deliberately unbinds.
func (s *Server) seedAutoDefaultBindings() {
	const flag = "auto_default_bindings_v3" // v3: 替换旧资产工具名，加入 insert_assets/add_company_scope
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	keys := make([]string, 0, len(platformToolKeys)+12)
	for _, t := range s.orchestrationTools() {
		keys = append(keys, t.Name())
	}
	keys = append(keys, platformToolKeys...)
	// 资产工具：Auto 操作平台常要看/登记资产、管理公司范围。
	keys = append(keys, "insert_assets", "add_company_scope", "list_assets")
	if err := s.m.pg.AddAgentToToolBinding("auto", keys); err != nil {
		log.Printf("[auto] 默认绑定失败: %v", err)
		return
	}
	_ = s.m.pg.SetSetting(flag, "true")
}
