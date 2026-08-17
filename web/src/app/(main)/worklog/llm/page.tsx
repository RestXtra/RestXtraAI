"use client";

import * as React from "react";

import { ChevronLeftIcon, ChevronRightIcon, Loader2Icon, RadioIcon, SearchIcon, Trash2Icon, XIcon } from "lucide-react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Checkbox } from "@/components/ui/checkbox";
import { api } from "@/lib/api";
import type { LLMRecordDetail, LLMRecordItem } from "@/lib/types";
import { cn } from "@/lib/utils";

function fmtTime(ts: string) {
  return new Date(ts).toLocaleString("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

function fmtLatency(ms: number) {
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

function fmtTokens(n: number) {
  if (n >= 1000) return `${(n / 1000).toFixed(1)}k`;
  return String(n);
}

function tryFormatJSON(s: string): string {
  try {
    return JSON.stringify(JSON.parse(s), null, 2);
  } catch {
    return s;
  }
}

const PAGE_SIZES = [25, 50, 100];

export default function LLMRecordsPage() {
  const [page, setPage] = React.useState(0);
  const [size, setSize] = React.useState(50);
  const [session, setSession] = React.useState("");
  const [sessionQ, setSessionQ] = React.useState("");
  const [model, setModel] = React.useState("");

  const [records, setRecords] = React.useState<LLMRecordItem[]>([]);
  const [total, setTotal] = React.useState(0);
  const [loading, setLoading] = React.useState(false);

  // Recording on/off toggle (settings.llm_record; default off). When off the
  // backend records nothing.
  const [recEnabled, setRecEnabled] = React.useState(false);
  const [recBusy, setRecBusy] = React.useState(false);

  // Inline detail panel (Burp-style split, not a dialog)
  const [selected, setSelected] = React.useState<LLMRecordItem | null>(null);
  const [detail, setDetail] = React.useState<LLMRecordDetail | null>(null);
  const [detailLoading, setDetailLoading] = React.useState(false);

  // batch selection & delete
  const [checked, setChecked] = React.useState<Set<number>>(new Set());
  const [deleteIds, setDeleteIds] = React.useState<number[]>([]);
  const [deleteAll, setDeleteAll] = React.useState(false);
  const [deleteOpen, setDeleteOpen] = React.useState(false);
  const [deleting, setDeleting] = React.useState(false);
  const [reloadKey, setReloadKey] = React.useState(0);

  const toggleCheck = (id: number) => {
    setChecked((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id); else next.add(id);
      return next;
    });
  };

  const toggleCheckAll = (ids: number[]) => {
    setChecked((prev) => {
      const allSelected = ids.length > 0 && ids.every((id) => prev.has(id));
      const next = new Set(prev);
      if (allSelected) ids.forEach((id) => next.delete(id));
      else ids.forEach((id) => next.add(id));
      return next;
    });
  };

  const confirmDelete = async () => {
    setDeleting(true);
    try {
      const res = deleteAll
        ? await api.clearLLMRecords()
        : await api.deleteLLMRecords(deleteIds);
      const n = res.removed ?? res.deleted ?? 0;
      toast.success(`已删除 ${n} 条 LLM 记录`);
      setChecked(new Set());
      setDeleteOpen(false);
      setSelected(null);
      setReloadKey((k) => k + 1);
    } catch (e) {
      toast.error("删除失败：" + String((e as Error)?.message ?? e));
      setDeleteOpen(false);
    } finally {
      setDeleting(false);
    }
  };

  // Load recording toggle state on mount.
  React.useEffect(() => {
    let alive = true;
    api
      .settings()
      .then((s) => {
        if (alive) setRecEnabled(!!s.llm_record);
      })
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, []);

  const toggleRecording = async (on: boolean) => {
    setRecBusy(true);
    setRecEnabled(on); // optimistic
    try {
      const s = await api.setSettings({ llm_record: on });
      setRecEnabled(!!s.llm_record);
    } catch {
      setRecEnabled(!on); // revert on failure
    } finally {
      setRecBusy(false);
    }
  };

  // Debounce session filter.
  React.useEffect(() => {
    const t = setTimeout(() => setSessionQ(session.trim()), 300);
    return () => clearTimeout(t);
  }, [session]);

  // Reset page on filter change.
  React.useEffect(() => {
    setPage(0);
  }, []);

  // Load list.
  React.useEffect(() => {
    let alive = true;
    setLoading(true);
    api
      .llmRecords({ model: model || undefined, session: sessionQ || undefined, page, size })
      .then((r) => {
        if (!alive) return;
        setRecords(r.records ?? []);
        setTotal(r.total ?? 0);
      })
      .catch(() => {})
      .finally(() => alive && setLoading(false));
    return () => {
      alive = false;
    };
  }, [page, size, sessionQ, model, reloadKey]);

  // Lazy-load full request/response when a row is selected.
  React.useEffect(() => {
    if (!selected) {
      setDetail(null);
      return;
    }
    let alive = true;
    setDetailLoading(true);
    setDetail(null);
    api
      .llmRecordDetail(selected.id)
      .then((d) => {
        if (alive) setDetail(d);
      })
      .catch(() => {})
      .finally(() => {
        if (alive) setDetailLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [selected]);

  const totalPages = Math.max(1, Math.ceil(total / size));
  const rangeStart = total === 0 ? 0 : page * size + 1;
  const rangeEnd = page * size + records.length;

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-4">
      <div className="flex items-center justify-between gap-4">
        <div className="flex items-center gap-2">
          <RadioIcon className="size-5 text-muted-foreground" />
          <h1 className="font-semibold text-xl tracking-tight">LLM 录制</h1>
          <Badge variant="secondary">{total}</Badge>
          {checked.size > 0 && (
            <>
              <Button
                variant="destructive"
                size="sm"
                onClick={() => { setDeleteAll(false); setDeleteIds(Array.from(checked)); setDeleteOpen(true); }}
              >
                <Trash2Icon className="size-3.5" /> 删除已选 ({checked.size})
              </Button>
              <Button
                variant="outline"
                size="sm"
                className="text-destructive hover:text-destructive"
                onClick={() => { setDeleteAll(true); setDeleteOpen(true); }}
              >
                <Trash2Icon className="size-3.5" /> 删除全部
              </Button>
            </>
          )}
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <div className="relative max-w-sm flex-1">
          <SearchIcon className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            placeholder="搜索 Session ID..."
            value={session}
            onChange={(e) => setSession(e.target.value)}
            className="h-8 pl-8"
          />
        </div>
        <Input placeholder="Model" className="h-8 w-48" value={model} onChange={(e) => setModel(e.target.value)} />
        <Select value={String(size)} onValueChange={(v) => setSize(Number(v))}>
          <SelectTrigger className="h-8 w-28">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {PAGE_SIZES.map((n) => (
              <SelectItem key={n} value={String(n)}>
                {n} / 页
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        {/* Recording on/off — off means no LLM calls are recorded */}
        <div className="flex items-center gap-2 rounded-md border px-2.5 py-1">
          <Switch
            id="llm-rec-toggle"
            size="sm"
            checked={recEnabled}
            onCheckedChange={toggleRecording}
            disabled={recBusy}
          />
          <label
            htmlFor="llm-rec-toggle"
            className={cn(
              "cursor-pointer select-none font-medium text-xs",
              recEnabled ? "text-foreground" : "text-muted-foreground",
            )}
          >
            {recEnabled ? "录制中" : "已关闭"}
          </label>
        </div>

        <div className="ml-auto flex items-center gap-2 text-muted-foreground text-xs">
          <span className="tabular-nums">
            {rangeStart}–{rangeEnd} / {total}
          </span>
          <Button
            variant="outline"
            size="icon"
            className="size-8"
            disabled={page <= 0}
            onClick={() => setPage((p) => Math.max(0, p - 1))}
          >
            <ChevronLeftIcon />
          </Button>
          <span className="tabular-nums">
            {page + 1} / {totalPages}
          </span>
          <Button
            variant="outline"
            size="icon"
            className="size-8"
            disabled={page + 1 >= totalPages}
            onClick={() => setPage((p) => Math.min(totalPages - 1, p + 1))}
          >
            <ChevronRightIcon />
          </Button>
        </div>
      </div>

      <div className="flex h-[calc(100vh-13rem)] min-h-0 flex-col gap-3">
        <Card className="flex min-h-0 flex-1 flex-col overflow-hidden py-0">
          <div className="min-h-0 flex-1 overflow-auto">
            <Table>
              <TableHeader className="sticky top-0 z-10 bg-card">
                <TableRow>
                  <TableHead className="w-8 pr-0">
                    <Checkbox
                      checked={records.length > 0 && records.every((r) => checked.has(r.id))}
                      onCheckedChange={() => toggleCheckAll(records.map((r) => r.id))}
                      aria-label="全选"
                    />
                  </TableHead>
                  <TableHead className="w-[130px]">时间</TableHead>
                  <TableHead className="w-[60px]">任务</TableHead>
                  <TableHead className="w-[90px]">Worker</TableHead>
                  <TableHead className="w-[100px]">Profile</TableHead>
                  <TableHead className="w-[140px]">Model</TableHead>
                  <TableHead className="w-[70px]">延迟</TableHead>
                  <TableHead className="w-[90px]">Tokens</TableHead>
                  <TableHead className="w-[60px]">状态</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {loading && records.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={9} className="py-12 text-center">
                      <Loader2Icon className="mx-auto size-5 animate-spin text-muted-foreground" />
                    </TableCell>
                  </TableRow>
                ) : records.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={9} className="py-12 text-center text-muted-foreground text-sm">
                      暂无 LLM 调用记录{recEnabled ? "（打开录制后新产生的调用才会记录）" : ""}
                    </TableCell>
                  </TableRow>
                ) : (
                  records.map((rec) => (
                    <TableRow
                      key={rec.id}
                      className={cn("cursor-pointer", selected?.id === rec.id && "bg-accent hover:bg-accent")}
                      onClick={() => setSelected(rec)}
                    >
                      <TableCell className="w-8 pr-0" onClick={(ev) => ev.stopPropagation()}>
                        <Checkbox
                          checked={checked.has(rec.id)}
                          onCheckedChange={() => toggleCheck(rec.id)}
                          aria-label="选择"
                        />
                      </TableCell>
                      <TableCell className="text-muted-foreground text-xs tabular-nums">{fmtTime(rec.ts)}</TableCell>
                      <TableCell className="font-mono text-muted-foreground text-xs">
                        {rec.task_id ? `#${rec.task_id}` : "-"}
                      </TableCell>
                      <TableCell>
                        <Badge variant="outline" className="font-mono text-xs">
                          {rec.worker || "-"}
                        </Badge>
                      </TableCell>
                      <TableCell>
                        <span className="text-xs">{rec.profile_name || "-"}</span>
                      </TableCell>
                      <TableCell>
                        <span className="font-mono text-xs">{rec.model || "-"}</span>
                      </TableCell>
                      <TableCell>
                        <span className={cn("text-xs", rec.latency_ms > 30000 && "text-amber-500")}>
                          {fmtLatency(rec.latency_ms)}
                        </span>
                      </TableCell>
                      <TableCell>
                        <span className="text-xs">
                          {fmtTokens(rec.input_tokens)} / {fmtTokens(rec.output_tokens)}
                        </span>
                      </TableCell>
                      <TableCell>
                        {rec.status === "ok" ? (
                          <Badge variant="secondary" className="text-emerald-600 text-xs">
                            OK
                          </Badge>
                        ) : (
                          <Badge variant="destructive" className="text-xs">
                            Error
                          </Badge>
                        )}
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </div>
        </Card>

        {selected && (
          <Card className="flex h-[42%] min-h-0 flex-col overflow-hidden py-0">
            <div className="flex items-center gap-2 border-b px-3 py-2">
              <Badge variant="outline" className="font-mono text-xs">
                #{selected.id}
              </Badge>
              <Badge variant="outline" className="font-mono text-xs">
                {selected.profile_name || "-"}
              </Badge>
              <Badge variant="outline" className="font-mono text-xs">
                {selected.model || "-"}
              </Badge>
              {selected.task_id && (
                <Badge variant="outline" className="font-mono text-xs">
                  任务 #{selected.task_id}
                </Badge>
              )}
              <span className="text-muted-foreground text-xs">{fmtTime(selected.ts)}</span>
              <span className={cn("text-xs", selected.latency_ms > 30000 && "text-amber-500")}>
                {fmtLatency(selected.latency_ms)}
              </span>
              {selected.status === "ok" ? (
                <Badge variant="secondary" className="text-emerald-600 text-xs">
                  OK
                </Badge>
              ) : (
                <Badge variant="destructive" className="text-xs">
                  Error
                </Badge>
              )}
              <Button variant="ghost" size="icon" className="ml-auto size-7 shrink-0" onClick={() => setSelected(null)}>
                <XIcon />
              </Button>
            </div>
            <div className="grid min-h-0 flex-1 grid-cols-2 divide-x">
              <div className="flex min-h-0 min-w-0 flex-col">
                <div className="border-b px-3 py-1 font-medium text-[11px] text-muted-foreground">Request</div>
                <div className="min-h-0 flex-1 overflow-auto">
                  {detailLoading ? (
                    <div className="flex items-center gap-2 p-3 text-muted-foreground text-xs">
                      <Loader2Icon className="size-3.5 animate-spin" />
                      加载…
                    </div>
                  ) : (
                    <pre className="whitespace-pre-wrap break-all p-3 font-mono text-xs">
                      {detail?.request_body ? tryFormatJSON(detail.request_body) : "（空）"}
                    </pre>
                  )}
                </div>
              </div>
              <div className="flex min-h-0 min-w-0 flex-col">
                <div className="border-b px-3 py-1 font-medium text-[11px] text-muted-foreground">Response</div>
                <div className="min-h-0 flex-1 overflow-auto">
                  {detailLoading ? (
                    <div className="flex items-center gap-2 p-3 text-muted-foreground text-xs">
                      <Loader2Icon className="size-3.5 animate-spin" />
                      加载…
                    </div>
                  ) : (
                    <pre
                      className={cn(
                        "whitespace-pre-wrap break-all p-3 font-mono text-xs",
                        selected.status !== "ok" && "text-red-600 dark:text-red-400",
                      )}
                    >
                      {detail?.response_body ? tryFormatJSON(detail.response_body) : "（空）"}
                    </pre>
                  )}
                </div>
              </div>
            </div>
          </Card>
        )}
      </div>

      <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认删除</AlertDialogTitle>
            <AlertDialogDescription>
              {deleteAll ? (
                <>将清空全部 LLM 调用记录（含请求/响应），此操作不可撤销。</>
              ) : (
                <>将永久删除 <span className="font-semibold tabular-nums">{deleteIds.length}</span> 条 LLM 调用记录（含请求/响应），此操作不可撤销。</>
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleting}>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={(e) => { e.preventDefault(); confirmDelete(); }}
              disabled={deleting}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {deleting ? "删除中…" : "确认删除"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
