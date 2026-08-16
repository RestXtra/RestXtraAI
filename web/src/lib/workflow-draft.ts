// 工作流画板草稿：localStorage 持久化的可视化工作流。
// 两种用途：
//  1) 任务创建：把节点按拓扑序转成后端 TaskWorkflow 的 intent 列表（预填探索方向）。
//  2) 工作流图引擎：把同一份图直接 POST 到 /api/workflows 保存/运行（7 类图节点）。
import type { WorkflowGraphNodeKind } from "@/lib/types";

export type WorkflowStepKind = "step" | "decision";

export interface WorkflowDraftNode {
  id: string;
  summary?: string; // 兼容旧草稿/任务意图描述
  kind: WorkflowGraphNodeKind | WorkflowStepKind; // 图节点类型（start/tool/agent/condition/hitl/output/end/step/decision）
  agent?: string; // agent/step 指定 agent key
  instruction?: string; // agent/hitl/output 节点指令/模板
  tool?: string; // tool 节点工具名
  args?: string; // tool 节点参数 JSON 模板
  expression?: string; // condition 表达式
  outputKey?: string; // 写入 outputs 池
  label?: string;
  position: { x: number; y: number };
}
export interface WorkflowDraft {
  nodes: WorkflowDraftNode[]; // 保存时按拓扑序重排（优先级从高到低）
  edges: { source: string; target: string }[];
  hints: string[];
}

export const WORKFLOW_DRAFT_KEY = "restxtra.workflow.draft";

export function loadWorkflowDraft(): WorkflowDraft | null {
  try {
    const raw = localStorage.getItem(WORKFLOW_DRAFT_KEY);
    if (!raw) return null;
    const d = JSON.parse(raw) as WorkflowDraft;
    if (!d || !Array.isArray(d.nodes) || !Array.isArray(d.hints)) return null;
    return d;
  } catch {
    return null;
  }
}

export function saveWorkflowDraft(d: WorkflowDraft) {
  localStorage.setItem(WORKFLOW_DRAFT_KEY, JSON.stringify(d));
}

export function clearWorkflowDraft() {
  localStorage.removeItem(WORKFLOW_DRAFT_KEY);
}

// 拓扑排序（Kahn）：边 source→target 表示 source 先于 target。
function topoOrder(nodes: WorkflowDraftNode[], edges: { source: string; target: string }[]) {
  const indeg = new Map<string, number>();
  const adj = new Map<string, string[]>();
  for (const n of nodes) {
    indeg.set(n.id, 0);
    adj.set(n.id, []);
  }
  for (const e of edges) {
    if (!indeg.has(e.source) || !indeg.has(e.target)) continue;
    indeg.set(e.target, (indeg.get(e.target) ?? 0) + 1);
    adj.get(e.source)?.push(e.target);
  }
  const queue = nodes.filter((n) => (indeg.get(n.id) ?? 0) === 0).map((n) => n.id);
  const order: string[] = [];
  while (queue.length) {
    const id = queue.shift()!;
    order.push(id);
    for (const t of adj.get(id) ?? []) {
      indeg.set(t, (indeg.get(t) ?? 0) - 1);
      if (indeg.get(t) === 0) queue.push(t);
    }
  }
  // 环内未排到的节点按原顺序追加
  for (const n of nodes) if (!order.includes(n.id)) order.push(n.id);
  const byId = new Map(nodes.map((n) => [n.id, n]));
  return order.map((id) => byId.get(id)!).filter(Boolean);
}

// 从草稿派生后端 TaskWorkflow（任务创建用）：
//   start/end/output/hitl → 跳过；tool → 步骤（摘要=工具名+命令）；agent/step → 步骤；condition/decision → 判断。
export function draftToWorkflow(d: WorkflowDraft): {
  steps: { summary: string; priority: number; agent?: string; kind?: WorkflowStepKind }[];
  hints: string[];
} {
  const sorted = topoOrder(d.nodes, d.edges);
  const steps: { summary: string; priority: number; agent?: string; kind?: WorkflowStepKind }[] = [];
  sorted.forEach((n, i) => {
    const kind = n.kind;
    if (kind === "start" || kind === "end" || kind === "output" || kind === "hitl") return;
    const isDecision = kind === "condition" || kind === "decision";
    let summary = (n.instruction ?? n.summary ?? "").trim();
    if (!summary && kind === "tool") {
      summary = `工具: ${n.tool ?? ""} ${(n.args ?? "").replace(/[{}"]/g, " ").replace(/\s+/g, " ").trim()}`.trim();
    }
    if (!summary) return;
    steps.push({
      summary,
      priority: Math.max(1, 100 - i),
      agent: (n.agent ?? "").trim() || undefined,
      kind: (isDecision ? "decision" : undefined) as WorkflowStepKind | undefined,
    });
  });
  return { steps, hints: d.hints.map((h) => h.trim()).filter(Boolean) };
}

// 从草稿构建后端工作流图定义（workflow.Graph 结构）。
export function draftToGraph(d: WorkflowDraft): {
  nodes: {
    id: string;
    kind: string;
    label?: string;
    instruction?: string;
    tool?: string;
    args?: string;
    expression?: string;
    output_key?: string;
    agent?: string;
  }[];
  edges: { from: string; to: string }[];
} {
  const kindMap: Record<string, string> = {
    step: "agent",
    decision: "condition",
  };
  return {
    nodes: d.nodes.map((n) => {
      const kind = kindMap[n.kind] ?? n.kind;
      const instruction = n.kind === "decision" ? (n.instruction ?? n.summary) : (n.instruction ?? n.summary);
      const node: Record<string, string | undefined> = {
        id: n.id,
        kind,
        label: n.label,
        instruction,
        tool: n.tool,
        args: n.args,
        expression: n.kind === "decision" ? (n.expression ?? "") : n.expression,
        agent: n.agent,
      };
      if (n.outputKey) node.output_key = n.outputKey;
      // 清理空字段
      for (const k of Object.keys(node)) {
        if (node[k] === undefined || node[k] === "") delete node[k];
      }
      return node as {
        id: string;
        kind: string;
        label?: string;
        instruction?: string;
        tool?: string;
        args?: string;
        expression?: string;
        output_key?: string;
        agent?: string;
      };
    }),
    edges: d.edges.map((e) => ({ from: e.source, to: e.target })),
  };
}
