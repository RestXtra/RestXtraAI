"use client";

import * as React from "react";

import {
  addEdge,
  Background,
  BackgroundVariant,
  type Connection,
  Controls,
  Handle,
  MarkerType,
  MiniMap,
  type NodeChange,
  type NodeProps,
  Panel,
  Position,
  ReactFlow,
  ReactFlowProvider,
  type Edge as RFEdge,
  type Node as RFNode,
  useEdgesState,
  useNodesState,
  useReactFlow,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";

import {
  ArrowLeftIcon,
  GitBranchIcon,
  LightbulbIcon,
  PlayIcon,
  PlusIcon,
  SaveIcon,
  Trash2Icon,
  WorkflowIcon,
  XIcon,
} from "lucide-react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { api } from "@/lib/api";
import type { Agent, WorkflowGraphNodeKind, WorkflowRunItem } from "@/lib/types";
import { cn } from "@/lib/utils";
import {
  draftToGraph,
  draftToWorkflow,
  loadWorkflowDraft,
  saveWorkflowDraft,
  type WorkflowDraft,
  type WorkflowDraftNode,
} from "@/lib/workflow-draft";

type NodeData = {
  kind: WorkflowGraphNodeKind;
  label: string;
  instruction: string;
  tool: string;
  args: string;
  expression: string;
  agent: string;
  outputKey: string;
};

const KIND_META: Record<WorkflowGraphNodeKind, { label: string; color: string }> = {
  start: { label: "开始", color: "border-emerald-500/70 bg-emerald-500/5" },
  tool: { label: "工具", color: "border-blue-500/70 bg-blue-500/5" },
  agent: { label: "Agent", color: "border-violet-500/70 bg-violet-500/5" },
  condition: { label: "条件", color: "border-amber-500/70 bg-amber-500/5" },
  hitl: { label: "审批", color: "border-orange-500/70 bg-orange-500/5" },
  output: { label: "输出", color: "border-sky-500/70 bg-sky-500/5" },
  end: { label: "结束", color: "border-red-500/70 bg-red-500/5" },
};

const SIDES = [Position.Top, Position.Bottom, Position.Left, Position.Right];

function FourSideHandles({ kind }: { kind: WorkflowGraphNodeKind }) {
  const color = "border-primary bg-primary/40";
  return (
    <>
      {SIDES.map((s) => (
        <React.Fragment key={s}>
          <Handle type="target" position={s} id={`t-${s}`} className={cn("size-2.5", color)} />
          <Handle type="source" position={s} id={`s-${s}`} className={cn("size-2.5", color)} />
        </React.Fragment>
      ))}
    </>
  );
}

function WorkflowNode({ id, data, selected }: NodeProps<RFNode<NodeData>>) {
  const rf = useReactFlow();
  const meta = KIND_META[data.kind] ?? KIND_META.agent;
  const upd = (patch: Partial<NodeData>) => rf.updateNodeData(id, patch);
  const [agents, setAgents] = React.useState<Agent[]>([]);
  React.useEffect(() => {
    if (data.kind !== "agent") return;
    api
      .agents()
      .then((a) => setAgents(a ?? []))
      .catch(() => setAgents([]));
  }, [data.kind]);

  return (
    <div
      className={cn("w-60 rounded-xl border bg-card shadow-sm", selected ? "ring-2 ring-primary/30" : "", meta.color)}
    >
      <FourSideHandles kind={data.kind} />
      <div className="flex items-center justify-between gap-2 border-border/50 border-b px-2.5 py-1.5">
        <span className="flex items-center gap-1 font-medium text-[11px] text-muted-foreground">
          {data.kind === "condition" ? <GitBranchIcon className="size-3" /> : <WorkflowIcon className="size-3" />}
          {meta.label}
        </span>
        <span className="nodrag flex items-center gap-0.5">
          <Select value={data.kind} onValueChange={(v) => upd({ kind: v as WorkflowGraphNodeKind })}>
            <SelectTrigger className="h-6 w-[86px] border-0 px-1.5 text-[11px] shadow-none focus-visible:ring-0">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {Object.entries(KIND_META).map(([k, m]) => (
                <SelectItem key={k} value={k}>
                  {m.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <button
            type="button"
            onClick={() => rf.deleteElements({ nodes: [{ id }] })}
            aria-label="删除节点"
            className="rounded p-0.5 text-muted-foreground hover:text-destructive"
          >
            <XIcon className="size-3" />
          </button>
        </span>
      </div>
      <div className="grid gap-1.5 p-2">
        <input
          className="nodrag w-full rounded border-0 bg-transparent px-1 font-medium text-xs outline-none focus-visible:ring-0"
          placeholder="标签（可选）"
          value={data.label}
          onChange={(e) => upd({ label: e.target.value })}
        />
        {(data.kind === "tool" ||
          data.kind === "agent" ||
          data.kind === "condition" ||
          data.kind === "hitl" ||
          data.kind === "output") && (
          <Textarea
            rows={3}
            className="nodrag nowheel min-h-0 resize-none border-0 bg-transparent p-1 text-xs focus-visible:ring-0"
            placeholder={
              data.kind === "tool"
                ? '参数 JSON 模板：{"command":"nmap {{inputs.target}}"}'
                : data.kind === "condition"
                  ? '条件表达式：{{outputs.x}} contains "ok"'
                  : data.kind === "output"
                    ? "输出模板：{{outputs.x}}"
                    : "指令 / 审批说明…"
            }
            value={data.kind === "tool" ? data.args : data.kind === "condition" ? data.expression : data.instruction}
            onChange={(e) => {
              const v = e.target.value;
              if (data.kind === "tool") upd({ args: v });
              else if (data.kind === "condition") upd({ expression: v });
              else upd({ instruction: v });
            }}
          />
        )}
        {data.kind === "tool" && (
          <input
            className="nodrag w-full rounded border-0 bg-transparent px-1 font-mono text-xs outline-none"
            placeholder="工具名（如 Bash）"
            value={data.tool}
            onChange={(e) => upd({ tool: e.target.value })}
          />
        )}
        {data.kind === "agent" && (
          <span className="nodrag grid gap-1">
            <Select value={data.agent} onValueChange={(v) => upd({ agent: v })}>
              <SelectTrigger className="h-7 w-full border-0 px-1.5 text-xs shadow-none focus-visible:ring-0">
                <SelectValue placeholder="选择智能体（默认激活配置）" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="">默认（激活配置）</SelectItem>
                {agents.map((a) => (
                  <SelectItem key={a.key} value={a.key}>
                    {a.name}（{a.key}）
                  </SelectItem>
                ))}
                {agents.length === 0 && (
                  <div className="px-2 py-1 text-muted-foreground text-xs">智能体管理中暂无自定义 Agent</div>
                )}
              </SelectContent>
            </Select>
          </span>
        )}
        {(data.kind === "tool" || data.kind === "agent") && (
          <input
            className="nodrag w-full rounded border-0 bg-transparent px-1 font-mono text-xs outline-none"
            placeholder="output_key（可选，写入 outputs 池）"
            value={data.outputKey}
            onChange={(e) => upd({ outputKey: e.target.value })}
          />
        )}
      </div>
    </div>
  );
}

const nodeTypes = { wf: WorkflowNode };

function defaultNode(kind: WorkflowGraphNodeKind, _x: number, _y: number): NodeData {
  return { kind, label: "", instruction: "", tool: "Bash", args: "", expression: "", agent: "", outputKey: "" };
}

// 蛇形网格布局（拓扑序 → 3 列、间距 320×240），让图一眼展开。
function topoLayout(nds: RFNode<NodeData>[], eds: RFEdge[]): RFNode<NodeData>[] {
  const indeg = new Map<string, number>();
  const adj = new Map<string, string[]>();
  for (const n of nds) {
    indeg.set(n.id, 0);
    adj.set(n.id, []);
  }
  for (const e of eds) {
    if (!indeg.has(e.source) || !indeg.has(e.target)) continue;
    indeg.set(e.target, (indeg.get(e.target) ?? 0) + 1);
    adj.get(e.source)?.push(e.target);
  }
  const queue = nds.filter((n) => (indeg.get(n.id) ?? 0) === 0).map((n) => n.id);
  const order: string[] = [];
  while (queue.length) {
    const id = queue.shift()!;
    order.push(id);
    for (const t of adj.get(id) ?? []) {
      indeg.set(t, (indeg.get(t) ?? 0) - 1);
      if (indeg.get(t) === 0) queue.push(t);
    }
  }
  for (const n of nds) if (!order.includes(n.id)) order.push(n.id);
  const cols = 3;
  const positions = new Map(
    order.map((id, i) => [id, { x: 80 + (i % cols) * 320, y: 60 + Math.floor(i / cols) * 240 }]),
  );
  return nds.map((n) => ({ ...n, position: positions.get(n.id) ?? n.position }));
}

// cramped：存在两节点间距过近（≈重叠），触发自动展开。
function cramped(nds: RFNode<NodeData>[]): boolean {
  for (let i = 0; i < nds.length; i++) {
    for (let j = i + 1; j < nds.length; j++) {
      const dx = nds[i].position.x - nds[j].position.x;
      const dy = nds[i].position.y - nds[j].position.y;
      if (dx * dx + dy * dy < 180 * 180) return true;
    }
  }
  return false;
}

function BuilderCanvas() {
  const [nodes, setNodes, onNodesChange] = useNodesState<RFNode<NodeData>>([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState<RFEdge>([]);
  const [hints, setHints] = React.useState<string[]>([]);
  const [loaded, setLoaded] = React.useState(false);
  const [wfName, setWfName] = React.useState("");
  const [saved, setSaved] = React.useState<{ id: string; name: string } | null>(null);
  const [runs, setRuns] = React.useState<WorkflowRunItem[]>([]);
  const nextId = React.useRef(1);

  const toNodeData = (n: WorkflowDraftNode): NodeData => {
    const kind = (
      ["start", "tool", "agent", "condition", "hitl", "output", "end"].includes(n.kind)
        ? n.kind
        : n.kind === "decision"
          ? "condition"
          : "agent"
    ) as WorkflowGraphNodeKind;
    return {
      kind,
      label: n.label ?? "",
      instruction: n.instruction ?? n.summary ?? "",
      tool: n.tool ?? "Bash",
      args: n.args ?? "",
      expression: n.expression ?? "",
      agent: n.agent ?? "",
      outputKey: n.outputKey ?? "",
    };
  };

  React.useEffect(() => {
    const loadFromBackend = async (id: string): Promise<boolean> => {
      try {
        const w = await api.workflowGet(id);
        setWfName(w.name);
        const nodes = (w.graph.nodes ?? []).map((n) => ({
          id: n.id,
          type: "wf" as const,
          position: n.position ?? { x: 60, y: 60 },
          data: {
            kind: n.kind,
            label: n.label ?? "",
            instruction: n.instruction ?? "",
            tool: n.tool ?? "Bash",
            args: n.args ?? "",
            expression: n.expression ?? "",
            agent: n.agent ?? "",
            outputKey: n.output_key ?? "",
          },
        }));
        const eds = (w.graph.edges ?? []).map((e) => ({ id: `e-${e.from}-${e.to}`, source: e.from, target: e.to }));
        setNodes(cramped(nodes) ? topoLayout(nodes, eds) : nodes);
        setEdges(eds);
        setHints([]);
        setSaved({ id: String(w.id), name: w.name });
        nextId.current = nodes.length + 1;
        return true;
      } catch {
        return false;
      }
    };
    (async () => {
      const qid = typeof window !== "undefined" ? new URLSearchParams(window.location.search).get("id") : null;
      if (qid && (await loadFromBackend(qid))) {
        setLoaded(true);
        return;
      }
      // 默认空画布；仅当有已保存草稿时才载入（进入时不给预置工作流）。
      const d = loadWorkflowDraft();
      if (d?.nodes.length) {
        const nds = d.nodes.map((n) => ({
          id: n.id,
          type: "wf",
          position: n.position,
          data: toNodeData(n),
        }));
        const eds = d.edges.map((e) => ({ id: `e-${e.source}-${e.target}`, source: e.source, target: e.target }));
        // 载入的节点若挤在一起（旧草稿/位置丢失），自动展开。
        setNodes(
          cramped(nds as RFNode<NodeData>[]) ? topoLayout(nds as RFNode<NodeData>[], eds) : (nds as RFNode<NodeData>[]),
        );
        setEdges(eds);
        setHints(d.hints);
        nextId.current = d.nodes.length + 1;
      } else {
        setNodes([]);
        setEdges([]);
        setHints([]);
        nextId.current = 1;
      }
      setLoaded(true);
    })();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [toNodeData, setNodes, setEdges]);

  const onConnect = React.useCallback(
    (c: Connection) => {
      if (!c.source || !c.target || c.source === c.target) return;
      setEdges((eds) =>
        addEdge(
          {
            id: `e-${c.source}-${c.sourceHandle ?? "x"}-${c.target}-${c.targetHandle ?? "x"}-${Date.now()}`,
            source: c.source!,
            target: c.target!,
            sourceHandle: c.sourceHandle ?? undefined,
            targetHandle: c.targetHandle ?? undefined,
          },
          eds,
        ),
      );
    },
    [setEdges],
  );

  const addNode = React.useCallback(
    (kind: WorkflowGraphNodeKind) => {
      const id = `wf-${nextId.current++}`;
      // 新节点放在画布视口中心附近，避免与现有节点堆叠。
      const center = { x: 140, y: 80 + nodes.length * 120 };
      setNodes((nds) => [...nds, { id, type: "wf", position: center, data: defaultNode(kind, 0, 0) }]);
    },
    [nodes, setNodes],
  );

  // 自动布局：按拓扑序排成蛇形网格，间距宽松，提升可读性。
  const autoLayout = React.useCallback(() => {
    setNodes((nds) => topoLayout(nds, edges));
  }, [edges, setNodes]);

  const onNodesChangeCb = React.useCallback(
    (changes: NodeChange<RFNode<NodeData>>[]) => onNodesChange(changes),
    [onNodesChange],
  );

  const toDraft = React.useCallback((): WorkflowDraft => {
    return {
      nodes: nodes.map((n) => ({
        id: n.id,
        kind: n.data.kind,
        label: n.data.label,
        instruction: n.data.instruction,
        tool: n.data.tool,
        args: n.data.args,
        expression: n.data.expression,
        agent: n.data.agent,
        outputKey: n.data.outputKey,
        position: n.position,
      })),
      edges: edges.map((e) => ({ source: e.source, target: e.target })),
      hints: hints.map((h) => h.trim()).filter(Boolean),
    };
  }, [nodes, edges, hints]);

  const saveDraft = () => {
    saveWorkflowDraft(toDraft());
    const wf = draftToWorkflow(toDraft());
    toast.success(
      wf.steps.length || wf.hints.length
        ? `草稿已保存：${wf.steps.length} 步 · ${wf.hints.length} 条提示`
        : "草稿已保存（空）",
    );
  };

  const saveBackend = async () => {
    const name = wfName.trim() || "未命名工作流";
    try {
      const r = await api.workflowSave({ name, graph: draftToGraph(toDraft()) as never });
      setSaved({ id: String(r.id), name });
      toast.success(`已保存到后端工作流：${name} (#${r.id})`);
      void loadRuns(String(r.id));
    } catch (e) {
      toast.error(`保存失败：${(e as Error).message}`);
    }
  };

  const loadRuns = async (id: string) => {
    try {
      setRuns(await api.workflowRuns(id));
    } catch {
      setRuns([]);
    }
  };

  const dryRun = async () => {
    try {
      const res = await api.workflowDryRun(draftToGraph(toDraft()) as never);
      toast.success(`试运行：${res.status}${res.final ? ` · 最终输出：${res.final.slice(0, 120)}` : ""}`);
    } catch (e) {
      toast.error(`试运行失败：${(e as Error).message}`);
    }
  };

  const runBackend = async () => {
    if (!saved) {
      toast.error("请先「保存到后端」再运行");
      return;
    }
    try {
      const r = await api.workflowRun(saved.id);
      toast.success(`已发起运行 #${r.run_id}`);
      void loadRuns(saved.id);
    } catch (e) {
      toast.error(`运行失败：${(e as Error).message}`);
    }
  };

  const runOf = (r: WorkflowRunItem) => {
    let body = r.error || r.status;
    if (r.result) {
      try {
        const j = JSON.parse(r.result);
        if (j.final) body = `final: ${j.final}`;
        else if (j.status) body = j.status;
      } catch {
        /* ignore */
      }
    }
    return body;
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2">
        <Button
          size="sm"
          variant="outline"
          onClick={() => {
            // 返回前自动落盘草稿，回到新建任务弹窗即可带上工作流。
            saveWorkflowDraft(toDraft());
            window.history.back();
          }}
        >
          <ArrowLeftIcon /> 返回
        </Button>
        <div className="flex items-center gap-2">
          <WorkflowIcon className="size-5 text-muted-foreground" />
          <h1 className="font-semibold text-lg tracking-tight">工作流画板</h1>
        </div>
        <div className="ml-auto flex flex-wrap items-center gap-1.5">
          <Button size="sm" variant="outline" onClick={() => addNode("start")}>
            +开始
          </Button>
          <Button size="sm" variant="outline" onClick={() => addNode("tool")}>
            +工具
          </Button>
          <Button size="sm" variant="outline" onClick={() => addNode("agent")}>
            +Agent
          </Button>
          <Button size="sm" variant="outline" className="text-amber-600" onClick={() => addNode("condition")}>
            +条件
          </Button>
          <Button size="sm" variant="outline" className="text-orange-600" onClick={() => addNode("hitl")}>
            +审批
          </Button>
          <Button size="sm" variant="outline" onClick={autoLayout}>
            <WorkflowIcon className="size-3.5" /> 自动布局
          </Button>
          <Button size="sm" variant="outline" onClick={() => addNode("output")}>
            +输出
          </Button>
          <Button size="sm" variant="outline" onClick={() => addNode("end")}>
            +结束
          </Button>
        </div>
      </div>
      <p className="text-muted-foreground text-xs">
        节点四边可连线（A→B 表示 A 先执行）。工具/条件/Agent/审批/输出节点区内编辑； 支持
        {"{{inputs.x}} / {{outputs.x}} / {{previous.output}}"}模板变量。
      </p>

      <div className="grid min-h-0 flex-1 grid-cols-1 gap-3 lg:grid-cols-[1fr_260px]">
        <Card className="relative min-h-0 overflow-hidden py-0">
          {loaded && nodes.length === 0 && (
            <div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center">
              <div className="flex flex-col items-center gap-1 text-muted-foreground text-sm">
                <WorkflowIcon className="size-8 opacity-40" />
                <span>空画布 —— 点击上方「+开始 / +工具 / +Agent…」添加节点，拖动连线编排流程</span>
              </div>
            </div>
          )}
          {loaded && (
            <ReactFlow
              nodes={nodes}
              edges={edges}
              onNodesChange={onNodesChangeCb}
              onEdgesChange={onEdgesChange}
              onConnect={onConnect}
              nodeTypes={nodeTypes}
              defaultEdgeOptions={{ markerEnd: { type: MarkerType.ArrowClosed, width: 16, height: 16 } }}
              fitView
              fitViewOptions={{ padding: 0.3 }}
              deleteKeyCode={["Backspace", "Delete"]}
              minZoom={0.2}
            >
              <Background variant={BackgroundVariant.Dots} gap={20} />
              <MiniMap pannable zoomable />
              <Controls />
              <Panel position="top-left" className="text-muted-foreground text-xs">
                四边连线 · 滚轮缩放 · 拖动节点改顺序
              </Panel>
            </ReactFlow>
          )}
        </Card>

        <Card className="flex max-h-full min-h-0 flex-col overflow-hidden py-0">
          <CardContent className="grid min-h-0 flex-1 gap-3 overflow-auto p-3">
            <div className="grid gap-2">
              <Label className="text-sm">后端工作流</Label>
              <div className="flex items-center gap-1.5">
                <Input
                  placeholder="工作流名称"
                  value={wfName}
                  onChange={(e) => setWfName(e.target.value)}
                  className="h-8"
                />
                <Button size="sm" onClick={saveBackend}>
                  <SaveIcon /> 保存
                </Button>
              </div>
              <div className="flex gap-1.5">
                <Button size="sm" variant="outline" onClick={dryRun}>
                  <PlayIcon /> 试运行
                </Button>
                <Button size="sm" variant="outline" onClick={runBackend}>
                  运行
                </Button>
                <Button size="sm" variant="ghost" onClick={saveDraft}>
                  存草稿
                </Button>
              </div>
              {saved && (
                <p className="text-muted-foreground text-xs">
                  已保存：# {saved.id} · {saved.name}
                </p>
              )}
            </div>

            {runs.length > 0 && (
              <div className="grid gap-1.5">
                <Label className="text-muted-foreground text-xs">运行记录</Label>
                {runs.map((r) => (
                  <div key={r.id} className="flex items-center gap-2 rounded-md border px-2 py-1 text-xs">
                    <Badge
                      variant={
                        r.status === "completed" ? "secondary" : r.status === "failed" ? "destructive" : "outline"
                      }
                    >
                      {r.status}
                    </Badge>
                    <span className="min-w-0 flex-1 truncate text-muted-foreground">{runOf(r)}</span>
                  </div>
                ))}
              </div>
            )}

            <div className="grid gap-2 border-t pt-2">
              <Label className="flex items-center gap-1.5 text-sm">
                <LightbulbIcon className="size-4 text-amber-500" /> 战略提示
              </Label>
              <p className="text-muted-foreground text-xs">供任务 planner 首轮读取（任务创建用）。</p>
              {hints.map((h, i) => (
                <div key={i} className="flex items-center gap-1.5">
                  <Input
                    placeholder="例如：优先测试登录弱口令"
                    value={h}
                    onChange={(e) => setHints(hints.map((x, j) => (j === i ? e.target.value : x)))}
                  />
                  <Button
                    size="icon"
                    variant="outline"
                    aria-label="删除提示"
                    onClick={() => setHints(hints.filter((_, j) => j !== i))}
                  >
                    <Trash2Icon className="text-destructive" />
                  </Button>
                </div>
              ))}
              <Button size="sm" variant="outline" onClick={() => setHints([...hints, ""])}>
                <PlusIcon /> 添加提示
              </Button>
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

export default function WorkflowBuilderPage() {
  return (
    <div className="flex h-full min-h-0 flex-1 flex-col gap-4">
      <ReactFlowProvider>
        <BuilderCanvas />
      </ReactFlowProvider>
    </div>
  );
}
