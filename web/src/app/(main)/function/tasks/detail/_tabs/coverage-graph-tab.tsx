"use client";

import * as React from "react";

import {
  BugIcon,
  CheckCircle2Icon,
  CircleDashedIcon,
  type LucideIcon,
  NetworkIcon,
  ShieldCheckIcon,
} from "lucide-react";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { api } from "@/lib/api";
import type { CoverageGraphNode } from "@/lib/types";
import { cn } from "@/lib/utils";

const statusMeta = {
  discovered: { label: "已发现", dot: "bg-slate-400" },
  verified: { label: "已验证", dot: "bg-emerald-500" },
  vulnerable: { label: "有漏洞", dot: "bg-red-500" },
};

function nodeVisual(node: CoverageGraphNode) {
  if (node.status === "vulnerable") return { Icon: BugIcon, tone: "text-red-500" };
  if (node.tested) return { Icon: CheckCircle2Icon, tone: "text-emerald-500" };
  return { Icon: CircleDashedIcon, tone: "text-muted-foreground" };
}

function AssetNode({ node, onSelect }: { node: CoverageGraphNode; onSelect: (node: CoverageGraphNode) => void }) {
  const { Icon, tone } = nodeVisual(node);
  return (
    <button
      type="button"
      onClick={() => onSelect(node)}
      className={cn(
        "flex min-h-16 w-full items-start gap-2 border bg-card p-3 text-left transition-colors hover:bg-accent/50",
        node.status === "vulnerable" && "border-red-500/50",
        node.status === "verified" && "border-emerald-500/40",
      )}
    >
      <Icon className={cn("mt-0.5 size-4 shrink-0", tone)} />
      <span className="min-w-0 flex-1">
        <span className="block truncate font-medium text-sm">{node.label}</span>
        <span className="mt-1 flex items-center gap-2 text-[11px] text-muted-foreground">
          <code>{node.type}</code>
          {node.findings > 0 && <span>{node.findings} 个漏洞</span>}
        </span>
      </span>
    </button>
  );
}

function AssetBranch({
  node,
  childrenByParent,
  onSelect,
  depth = 0,
}: {
  node: CoverageGraphNode;
  childrenByParent: Map<string, CoverageGraphNode[]>;
  onSelect: (node: CoverageGraphNode) => void;
  depth?: number;
}) {
  const children = childrenByParent.get(node.id) ?? [];
  return (
    <div className="space-y-2">
      <AssetNode node={node} onSelect={onSelect} />
      {children.length > 0 && (
        <div
          className={cn("gap-2 border-muted border-l-2 pl-4", depth === 0 ? "grid md:grid-cols-2" : "flex flex-col")}
        >
          {children.map((child) => (
            <AssetBranch
              key={child.id}
              node={child}
              childrenByParent={childrenByParent}
              onSelect={onSelect}
              depth={depth + 1}
            />
          ))}
        </div>
      )}
    </div>
  );
}

export function CoverageGraphTab({ taskId }: { taskId: string }) {
  const [data, setData] = React.useState<Awaited<ReturnType<typeof api.taskCoverageGraph>> | null>(null);
  const [selected, setSelected] = React.useState<CoverageGraphNode | null>(null);
  const [loaded, setLoaded] = React.useState(false);

  React.useEffect(() => {
    let alive = true;
    const load = () => {
      api
        .taskCoverageGraph(taskId)
        .then((result) => {
          if (alive) setData(result);
        })
        .catch(() => {
          if (alive) setData(null);
        })
        .finally(() => {
          if (alive) setLoaded(true);
        });
    };
    load();
    const timer = setInterval(load, 10000);
    return () => {
      alive = false;
      clearInterval(timer);
    };
  }, [taskId]);

  if (!loaded) return <div className="py-20 text-center text-muted-foreground text-sm">加载覆盖图…</div>;
  if (!data || data.total === 0)
    return <div className="py-20 text-center text-muted-foreground text-sm">该任务暂无资产覆盖数据。</div>;

  const nodeIDs = new Set(data.nodes.map((node) => node.id));
  const roots = data.nodes.filter((node) => !node.parent_id || !nodeIDs.has(node.parent_id));
  const childrenByParent = new Map<string, CoverageGraphNode[]>();
  for (const node of data.nodes) {
    if (!node.parent_id || !nodeIDs.has(node.parent_id)) continue;
    const siblings = childrenByParent.get(node.parent_id) ?? [];
    siblings.push(node);
    childrenByParent.set(node.parent_id, siblings);
  }
  const pct = data.total ? Math.round((data.tested / data.total) * 100) : 0;

  return (
    <div className="flex flex-col gap-4">
      <div className="grid grid-cols-2 gap-3 md:grid-cols-5">
        {(
          [
            ["资产总数", data.total, NetworkIcon, "text-foreground"],
            ["已覆盖", data.tested, CheckCircle2Icon, "text-emerald-500"],
            ["未测试", data.untested, CircleDashedIcon, "text-muted-foreground"],
            ["确认漏洞", data.vulnerable, BugIcon, "text-red-500"],
            ["覆盖率", `${pct}%`, ShieldCheckIcon, "text-blue-500"],
          ] as [string, number | string, LucideIcon, string][]
        ).map(([label, value, Icon, tone]) => (
          <Card key={String(label)} className="py-3">
            <CardContent className="flex items-center gap-2 px-3">
              <Icon className={cn("size-4", tone)} />
              <span>
                <span className="block text-[11px] text-muted-foreground">{label}</span>
                <strong className="text-lg tabular-nums">{value}</strong>
              </span>
            </CardContent>
          </Card>
        ))}
      </div>
      <div className="flex flex-wrap items-center gap-2 text-xs">
        {Object.entries(statusMeta).map(([status, meta]) => (
          <span key={status} className="inline-flex items-center gap-1.5 rounded-md border px-2 py-1">
            <span className={cn("size-1.5 rounded-full", meta.dot)} />
            {meta.label}
          </span>
        ))}
        <span className="text-muted-foreground">点击资产节点查看覆盖状态</span>
      </div>
      <Card>
        <CardHeader>
          <CardTitle className="text-sm">资产覆盖图</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          {roots.map((root) => (
            <AssetBranch key={root.id} node={root} childrenByParent={childrenByParent} onSelect={setSelected} />
          ))}
        </CardContent>
      </Card>
      {selected && (
        <Card className="border-primary/40">
          <CardContent className="flex flex-wrap items-center justify-between gap-3 p-4 text-sm">
            <div>
              <div className="font-medium">{selected.label}</div>
              <div className="mt-1 text-muted-foreground text-xs">
                类型：{selected.type} · 状态：{statusMeta[selected.status].label} · 关联漏洞：{selected.findings}
              </div>
            </div>
            <button
              type="button"
              className="text-muted-foreground text-xs hover:text-foreground"
              onClick={() => setSelected(null)}
            >
              关闭
            </button>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
