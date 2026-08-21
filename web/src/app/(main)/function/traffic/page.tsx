"use client";

import * as React from "react";

import {
  ChevronLeftIcon,
  ChevronRightIcon,
  Loader2Icon,
  RadioTowerIcon,
  SearchIcon,
  Trash2Icon,
  XIcon,
} from "lucide-react";
import { toast } from "sonner";

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
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { api } from "@/lib/api";
import type { TrafficDetail, TrafficExchange, TrafficResp } from "@/lib/types";
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

function fmtBytes(n: number) {
  if (n <= 0) return "0 B";
  const units = ["B", "KB", "MB"];
  const i = Math.min(Math.floor(Math.log(n) / Math.log(1024)), units.length - 1);
  const v = n / 1024 ** i;
  return `${i === 0 ? v : v.toFixed(1)} ${units[i]}`;
}

function statusTone(status: number) {
  if (status >= 500) return "text-red-500";
  if (status >= 400) return "text-amber-500";
  if (status >= 300) return "text-blue-500";
  if (status >= 200) return "text-emerald-500";
  return "text-muted-foreground";
}

// Fixed method set (server filters exact-match); avoids deriving options from a
// single page, which would only ever list the methods on that page.
const METHODS = ["GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"];
const PAGE_SIZES = [25, 50, 100, 200];

export default function TrafficPage() {
  const [page, setPage] = React.useState(0);
  const [size, setSize] = React.useState(50);
  const [host, setHost] = React.useState(""); // raw host input
  const [hostQ, setHostQ] = React.useState(""); // debounced → server
  const [query, setQuery] = React.useState(""); // raw free-text input
  const [queryQ, setQueryQ] = React.useState(""); // debounced → server
  const [method, setMethod] = React.useState("all");

  const [traffic, setTraffic] = React.useState<TrafficResp | null>(null);
  const [selected, setSelected] = React.useState<TrafficExchange | null>(null);
  const [detail, setDetail] = React.useState<TrafficDetail | null>(null);
  const [detailLoading, setDetailLoading] = React.useState(false);

  // batch selection & delete
  const [checked, setChecked] = React.useState<Set<string>>(new Set());
  const [deleteIds, setDeleteIds] = React.useState<string[]>([]);
  const [deleteAll, setDeleteAll] = React.useState(false);
  const [deleteOpen, setDeleteOpen] = React.useState(false);
  const [deleting, setDeleting] = React.useState(false);

  const toggleCheck = (id: string) => {
    setChecked((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const toggleCheckAll = (ids: string[]) => {
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
      const res = deleteAll ? await api.clearTraffic() : await api.deleteTraffic(deleteIds);
      toast.success(`已删除 ${res.deleted ?? res.removed ?? 0} 条流量`);
      setChecked(new Set());
      setDeleteOpen(false);
      loadTraffic();
    } catch (e) {
      toast.error(`删除失败：${String((e as Error)?.message ?? e)}`);
      setDeleteOpen(false);
    } finally {
      setDeleting(false);
    }
  };

  // Debounce both filters so we don't refetch on every keystroke.
  React.useEffect(() => {
    const t = setTimeout(() => setHostQ(host.trim()), 300);
    return () => clearTimeout(t);
  }, [host]);
  React.useEffect(() => {
    const t = setTimeout(() => setQueryQ(query.trim()), 300);
    return () => clearTimeout(t);
  }, [query]);

  // Any filter/size change resets to the first page.
  React.useEffect(() => {
    setPage(0);
  }, []);

  // Load the current page. Auto-refresh only on page 0 (newest) so paging back
  // through history isn't yanked out from under the user.
  const loadTraffic = React.useCallback(() => {
    api
      .traffic(page, size, hostQ, method, queryQ)
      .then((r) => setTraffic(r))
      .catch(() => {});
  }, [page, size, hostQ, method, queryQ]);

  React.useEffect(() => {
    let alive = true;
    const load = () =>
      api
        .traffic(page, size, hostQ, method, queryQ)
        .then((r) => {
          if (alive) setTraffic(r);
        })
        .catch(() => {});
    load();
    const t = page === 0 ? setInterval(load, 5000) : null;
    return () => {
      alive = false;
      if (t) clearInterval(t);
    };
  }, [page, size, hostQ, method, queryQ]);

  // Lazy-load the raw request/response for the selected exchange.
  React.useEffect(() => {
    if (!selected) {
      setDetail(null);
      return;
    }
    let alive = true;
    setDetailLoading(true);
    setDetail(null);
    api
      .trafficExchange(selected.id)
      .then((d) => {
        if (alive) setDetail(d);
      })
      .catch(() => {
        if (alive) setDetail({ req: "（无法加载报文）", resp: "" });
      })
      .finally(() => {
        if (alive) setDetailLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [selected]);

  const exchanges = React.useMemo(() => traffic?.exchanges ?? [], [traffic]);
  const total = traffic?.total ?? exchanges.length;
  const pageCount = Math.max(1, Math.ceil(total / size));
  const rangeStart = total === 0 ? 0 : page * size + 1;
  const rangeEnd = page * size + exchanges.length;

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-4">
      <div className="flex items-center justify-between gap-4">
        <div>
          <h1 className="font-semibold text-xl tracking-tight">流量</h1>
          <p className="text-muted-foreground text-sm">全局录制代理 · 所有 HTTP 往来</p>
        </div>
        <div className="flex items-center gap-4 text-sm">
          <span
            className={cn(
              "inline-flex items-center gap-1.5 rounded-md border px-2 py-0.5 font-medium text-xs",
              traffic?.enabled
                ? "border-emerald-500/20 bg-emerald-500/15 text-emerald-600 dark:text-emerald-400"
                : "border-transparent bg-muted text-muted-foreground",
            )}
          >
            <RadioTowerIcon className="size-3.5" />
            {traffic?.enabled ? "录制中" : "已停用"}
          </span>
          {traffic?.proxy && <span className="font-mono text-muted-foreground text-xs">{traffic.proxy}</span>}
          <span className="text-muted-foreground text-xs">
            共 <span className="tabular-nums">{traffic?.count ?? 0}</span> 条
          </span>
          {checked.size > 0 && (
            <>
              <Button
                variant="destructive"
                size="sm"
                onClick={() => {
                  setDeleteAll(false);
                  setDeleteIds(Array.from(checked));
                  setDeleteOpen(true);
                }}
              >
                <Trash2Icon className="size-3.5" /> 删除已选 ({checked.size})
              </Button>
              <Button
                variant="outline"
                size="sm"
                className="text-destructive hover:text-destructive"
                onClick={() => {
                  setDeleteAll(true);
                  setDeleteOpen(true);
                }}
              >
                <Trash2Icon className="size-3.5" /> 删除全部
              </Button>
            </>
          )}
        </div>
      </div>

      {/* Toolbar */}
      <div className="flex flex-wrap items-center gap-2">
        <div className="relative w-48">
          <Input placeholder="host…" value={host} onChange={(e) => setHost(e.target.value)} className="h-8" />
        </div>
        <div className="relative max-w-sm flex-1">
          <SearchIcon className="absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            placeholder="搜索全部（URL / 方法 / 类型 / 状态码…）"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            className="h-8 pl-8"
          />
        </div>
        <Select value={method} onValueChange={setMethod}>
          <SelectTrigger size="sm" className="w-32">
            <SelectValue placeholder="方法" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部方法</SelectItem>
            {METHODS.map((m) => (
              <SelectItem key={m} value={m}>
                {m}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={String(size)} onValueChange={(v) => setSize(Number(v))}>
          <SelectTrigger size="sm" className="w-28">
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
            {page + 1} / {pageCount}
          </span>
          <Button
            variant="outline"
            size="icon"
            className="size-8"
            disabled={page + 1 >= pageCount}
            onClick={() => setPage((p) => Math.min(pageCount - 1, p + 1))}
          >
            <ChevronRightIcon />
          </Button>
        </div>
      </div>

      {/* History table + detail (Burp-style split) */}
      <div className="flex h-[calc(100vh-15rem)] min-h-0 flex-col gap-3">
        <Card className="flex min-h-0 flex-1 flex-col overflow-hidden py-0">
          <div className="min-h-0 flex-1 overflow-auto">
            <Table>
              <TableHeader className="sticky top-0 z-10 bg-card">
                <TableRow>
                  <TableHead className="w-8 pr-0">
                    <Checkbox
                      checked={exchanges.length > 0 && exchanges.every((e) => checked.has(e.id))}
                      onCheckedChange={() => toggleCheckAll(exchanges.map((e) => e.id))}
                      aria-label="全选"
                    />
                  </TableHead>
                  <TableHead className="w-36">时间</TableHead>
                  <TableHead className="w-44">host</TableHead>
                  <TableHead className="w-20">方法</TableHead>
                  <TableHead>URL</TableHead>
                  <TableHead className="w-20">状态码</TableHead>
                  <TableHead className="w-36">content-type</TableHead>
                  <TableHead className="w-24 text-right">响应长度</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {exchanges.map((e) => (
                  <TableRow
                    key={e.id}
                    className={cn("cursor-pointer", selected?.id === e.id && "bg-accent hover:bg-accent")}
                    onClick={() => setSelected(e)}
                  >
                    <TableCell className="w-8 pr-0" onClick={(ev) => ev.stopPropagation()}>
                      <Checkbox
                        checked={checked.has(e.id)}
                        onCheckedChange={() => toggleCheck(e.id)}
                        aria-label="选择"
                      />
                    </TableCell>
                    <TableCell className="text-muted-foreground text-xs tabular-nums">{fmtTime(e.ts)}</TableCell>
                    <TableCell className="font-mono text-xs">{e.host}</TableCell>
                    <TableCell>
                      <Badge variant="outline" className="font-mono text-xs">
                        {e.method}
                      </Badge>
                    </TableCell>
                    <TableCell className="max-w-0">
                      <span className="block truncate font-mono text-xs">{e.url}</span>
                    </TableCell>
                    <TableCell>
                      <span className={cn("font-mono font-semibold text-xs tabular-nums", statusTone(e.status))}>
                        {e.status}
                      </span>
                    </TableCell>
                    <TableCell className="text-muted-foreground text-xs">{e.content_type}</TableCell>
                    <TableCell className="text-right text-xs tabular-nums">{fmtBytes(e.resp_len)}</TableCell>
                  </TableRow>
                ))}
                {exchanges.length === 0 && (
                  <TableRow>
                    <TableCell colSpan={8} className="py-12 text-center text-muted-foreground text-sm">
                      {traffic === null ? "加载中…" : "没有匹配的流量。"}
                    </TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
          </div>
        </Card>

        {/* Raw request/response pane */}
        {selected && (
          <Card className="flex h-[42%] min-h-0 flex-col overflow-hidden py-0">
            <div className="flex items-center gap-2 border-b px-3 py-2">
              <Badge variant="outline" className="font-mono text-xs">
                {selected.method}
              </Badge>
              <span className="truncate font-mono text-xs">
                {selected.host}
                {selected.url}
              </span>
              <span className={cn("ml-auto font-mono font-semibold text-xs tabular-nums", statusTone(selected.status))}>
                {selected.status}
              </span>
              <Button variant="ghost" size="icon" className="size-7 shrink-0" onClick={() => setSelected(null)}>
                <XIcon />
              </Button>
            </div>
            <div className="grid min-h-0 flex-1 grid-cols-2 divide-x">
              {(
                [
                  ["请求 Request", detail?.req],
                  ["响应 Response", detail?.resp],
                ] as const
              ).map(([label, body]) => (
                <div key={label} className="flex min-h-0 min-w-0 flex-col">
                  <div className="border-b px-3 py-1 font-medium text-[11px] text-muted-foreground">{label}</div>
                  <div className="min-h-0 flex-1 overflow-auto">
                    {detailLoading ? (
                      <div className="flex items-center gap-2 p-3 text-muted-foreground text-xs">
                        <Loader2Icon className="size-3.5 animate-spin" />
                        加载报文…
                      </div>
                    ) : (
                      <pre className="whitespace-pre-wrap break-all p-3 font-mono text-xs">{body || "（空）"}</pre>
                    )}
                  </div>
                </div>
              ))}
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
                <>
                  将清空 <span className="font-semibold tabular-nums">{traffic?.count ?? 0}</span>{" "}
                  条全部流量记录（含对应报文），此操作不可撤销。
                </>
              ) : (
                <>
                  将永久删除 <span className="font-semibold tabular-nums">{deleteIds.length}</span>{" "}
                  条流量记录（含对应报文），此操作不可撤销。
                </>
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleting}>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={(e) => {
                e.preventDefault();
                confirmDelete();
              }}
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
