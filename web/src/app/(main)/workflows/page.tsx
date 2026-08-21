"use client";

import * as React from "react";

import Link from "next/link";

import { ChevronDownIcon, GitBranchIcon, Loader2Icon, PlayIcon, PlusIcon, Trash2Icon } from "lucide-react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { api } from "@/lib/api";
import type { WorkflowGraphMeta, WorkflowRunItem } from "@/lib/types";

function runSummary(r: WorkflowRunItem): string {
  if (r.error) return r.error;
  if (r.result) {
    try {
      const j = JSON.parse(r.result);
      if (j.final) return String(j.final).slice(0, 200);
      if (j.status) return j.status;
    } catch {
      /* ignore */
    }
  }
  return r.status;
}

export default function WorkflowsPage() {
  const [items, setItems] = React.useState<WorkflowGraphMeta[]>([]);
  const [runs, setRuns] = React.useState<Record<string, WorkflowRunItem[]>>({});
  const [expanded, setExpanded] = React.useState<Record<string, boolean>>({});
  const [busy, setBusy] = React.useState<string | null>(null);

  const load = React.useCallback(() => {
    api
      .workflows()
      .then(setItems)
      .catch(() => setItems([]));
  }, []);
  React.useEffect(() => {
    load();
  }, [load]);

  const loadRuns = async (id: string) => {
    try {
      const list = await api.workflowRuns(id);
      setRuns((m) => ({ ...m, [id]: list }));
    } catch {
      setRuns((m) => ({ ...m, [id]: [] }));
    }
  };

  const run = async (w: WorkflowGraphMeta) => {
    setBusy(w.id);
    try {
      const r = await api.workflowRun(w.id);
      toast.success(`已发起运行 #${r.run_id}`);
      await loadRuns(w.id);
    } catch (e) {
      toast.error(`运行失败：${(e as Error).message}`);
    } finally {
      setBusy(null);
    }
  };

  const remove = async (w: WorkflowGraphMeta) => {
    try {
      await api.workflowDelete(w.id);
      toast.success(`已删除工作流：${w.name}`);
      load();
    } catch (e) {
      toast.error(`删除失败：${(e as Error).message}`);
    }
  };

  return (
    <div className="flex flex-1 flex-col gap-4 md:gap-6">
      <div className="flex items-center justify-between gap-4">
        <div className="flex items-center gap-2">
          <GitBranchIcon className="size-5 text-muted-foreground" />
          <h1 className="font-semibold text-xl tracking-tight">工作流</h1>
          <Badge variant="secondary">{items.length}</Badge>
        </div>
        <Button size="sm" asChild>
          <Link href="/workflow-builder">
            <PlusIcon /> 新建工作流
          </Link>
        </Button>
      </div>
      <p className="text-muted-foreground text-sm">
        可视化图引擎工作流（start/tool/agent/condition/hitl/output/end）。在画板中构建并保存后，可在此运行与查看记录；新建任务时也可把画板草稿作为初始探索方向预填。
      </p>

      <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
        {items.length === 0 && (
          <Card className="xl:col-span-2">
            <CardContent className="flex items-center justify-center py-16 text-muted-foreground text-sm">
              暂无工作流，点击右上角「新建工作流」在画板中构建。
            </CardContent>
          </Card>
        )}
        {items.map((w) => {
          const rs = runs[w.id];
          const open = !!expanded[w.id];
          return (
            <Card key={w.id}>
              <CardContent className="grid gap-3">
                <div className="flex items-start justify-between gap-2">
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="truncate font-medium">
                        #{w.id} {w.name}
                      </span>
                      {w.enabled ? (
                        <Badge variant="secondary" className="text-emerald-600">
                          启用
                        </Badge>
                      ) : (
                        <Badge variant="outline">停用</Badge>
                      )}
                    </div>
                    {w.description && <p className="mt-0.5 truncate text-muted-foreground text-xs">{w.description}</p>}
                  </div>
                  <Button size="icon" variant="outline" aria-label="删除工作流" onClick={() => remove(w)}>
                    <Trash2Icon className="text-destructive" />
                  </Button>
                </div>
                <div className="flex flex-wrap items-center gap-1.5 border-t pt-2">
                  <Button size="sm" variant="outline" asChild>
                    <Link href={`/workflow-builder?id=${w.id}`}>打开画板</Link>
                  </Button>
                  <Button size="sm" variant="outline" disabled={busy === w.id} onClick={() => run(w)}>
                    {busy === w.id ? <Loader2Icon className="animate-spin" /> : <PlayIcon />} 运行
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => {
                      const next = !open;
                      setExpanded((m) => ({ ...m, [w.id]: next }));
                      if (next && !rs) void loadRuns(w.id);
                    }}
                  >
                    <ChevronDownIcon className={open ? "rotate-180 transition-transform" : "transition-transform"} />
                    运行记录 {rs ? `(${rs.length})` : ""}
                  </Button>
                </div>
                {open && (
                  <div className="grid gap-1.5 border-t pt-2">
                    {!rs ? (
                      <div className="py-2 text-center text-muted-foreground text-xs">加载中…</div>
                    ) : rs.length === 0 ? (
                      <div className="py-2 text-center text-muted-foreground text-xs">暂无运行记录</div>
                    ) : (
                      rs.map((r) => (
                        <div key={r.id} className="flex items-center gap-2 rounded-md border px-2 py-1 text-xs">
                          <Badge
                            variant={
                              r.status === "completed" ? "secondary" : r.status === "failed" ? "destructive" : "outline"
                            }
                          >
                            {r.status}
                          </Badge>
                          <span className="min-w-0 flex-1 truncate text-muted-foreground">
                            #{r.id} {runSummary(r)}
                          </span>
                        </div>
                      ))
                    )}
                  </div>
                )}
              </CardContent>
            </Card>
          );
        })}
      </div>
    </div>
  );
}
