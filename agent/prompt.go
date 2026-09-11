package agent

import (
	"bytes"
	"strings"
	"text/template"
)

// PromptOverride, if set, returns the stored system-prompt template for an agent
// key and whether one exists. The server wires it to the PG agent_prompts table.
// When nil or no override exists, agents use their built-in default prompt — so
// behavior is identical until a user edits a prompt in the UI.
var PromptOverride func(agentKey string) (string, bool)

// TaskAgent, if set, returns the specialized agent (key, persona prompt) a task
// runs under, so a delegated sub-task adopts that agent's identity/skills in both
// the planner and the worker system prompts. Empty key = generic role (unchanged).
var TaskAgent func(taskID int64) (key, persona string)

// personaBlock renders the identity block prepended to a role prompt so the
// sub-task "acts as" the specialized agent.
func personaBlock(key, persona string) string {
	if strings.TrimSpace(persona) == "" {
		return ""
	}
	if strings.TrimSpace(key) == "" {
		key = "specialist"
	}
	return "【专家身份｜" + key + "】你以该专用 agent 的身份与打法执行本任务：\n" + persona + "\n\n"
}

// taskAgentFor is a safe lookup helper for the planner/worker.
func taskAgentFor(taskID int64) (string, string) {
	if TaskAgent == nil {
		return "", ""
	}
	return TaskAgent(taskID)
}

// Prompt-variable structs — fields mirror each agent's catalog (docs §5a) so a
// user template referencing a catalog variable renders; referencing anything else
// fails template execution and falls back to the built-in default.
type PlannerVars struct{ Goal, Scope, AssetSummary, Now string }
type WorkerVars struct{ ProxyAddr, WorkerName string }
type MainVars struct{ Goal, AssetSummary, FindingsSummary string }
type GoalsVars struct{ EngagementDescription, Now string }

// GlobalInstruction 追加到每个 agent 的系统提示词末尾，保证统一的回复语言与安全纪律。
const GlobalInstruction = `

## 全局纪律
- **始终使用中文回复**（命令 / 代码 / PoC / 原始报文等原文除外）。
- 对可能产生状态变更或破坏性的操作（HTTP DELETE/PUT/PATCH、删除 / 修改 / 禁用账号、资金 / 审批类接口），**必须先向操作者说明将做什么、为什么，获得确认后再执行**；未经确认不得发起写操作。
- 只依据真实工具输出作答，不臆造结论。`

// renderSystem returns the rendered system-prompt BODY (段 [A]) for agentKey.
// Precedence: the DB-stored template (if any) over the built-in default template
// (def). BOTH are Go templates now — the built-in default is seeded into the DB
// verbatim, so the two paths render identically until a user edits the prompt.
// Rendering always runs (def used to be pre-substituted plain text; it is now a
// {{.Var}} template like the DB one). On any render error we fall back to the
// default template, then to the raw default string — an agent never starts with a
// half-rendered prompt. Callers append the code-owned tail (trafficTool / 中间产物
// 输出规约) AFTER this, so those can't be edited away via the DB body.
func renderSystem(agentKey, def string, vars any) string {
	tmpl := def
	if PromptOverride != nil {
		if t, ok := PromptOverride(agentKey); ok && t != "" {
			tmpl = t
		}
	}
	var body string
	if out, err := renderTmpl(tmpl, vars); err == nil {
		body = out
	} else if out, err := renderTmpl(def, vars); err == nil {
		body = out
	} else {
		body = def
	}
	return body + GlobalInstruction
}

func renderTmpl(tmpl string, vars any) (string, error) {
	t, err := template.New("p").Option("missingkey=error").Parse(tmpl)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	if err := t.Execute(&b, vars); err != nil {
		return "", err
	}
	return b.String(), nil
}
