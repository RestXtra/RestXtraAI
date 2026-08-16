"use client";

import * as React from "react";

import { ChevronLeftIcon, ChevronRightIcon, Loader2Icon, SearchIcon, TerminalIcon, XIcon } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { api } from "@/lib/api";
import type { CommandRecord } from "@/lib/types";
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

// toolInput renders a tool's raw input for display. Bash's {"command":"..."} is
// unwrapped to the bare command; other tools show their pretty-printed JSON args.
function toolInput(raw: string): string {
  try {
    const obj = JSON.parse(raw);
    if (obj && typeof obj.command === "string") return obj.command;
    return JSON.stringify(obj, null, 2);
  } catch {
    /* not JSON */
  }
  return raw;
}

// truncate clips a string to maxLen characters (single-line preview).
function truncate(s: string, maxLen: number): string {
  const first = s.split("\n")[0];
  if (first.length <= maxLen) return first;
  return `${first.slice(0, maxLen)}…`;
}

const PAGE_SIZES = [25, 50, 100];
const CMD_MAX_LEN = 80;

export default function ToolExecPage() {
  const [page, setPage] = React.useState(0);
  const [size, setSize] = React.useState(50);
  const [query, setQuery] = React.useState("");
  const [queryQ, setQueryQ] = React.useState("");
  const [taskFilter, setTaskFilter] = React.useState("");

  const [commands, setCommands] = React.useState<CommandRecord[]>([]);
  const [total, setTotal] = React.useState(0);
  const [loading, setLoading] = React.useState(false);

  // Inline detail panel (Burp-style split, not a dialog)
  const [selected, setSelected] = React.useState<CommandRecord | null>(null);

  // Debounce search input.
  React.useEffect(() => {
    const t = setTimeout(() => setQueryQ(query.trim()), 300);
    return () => clearTimeout(t);
  }, [query]);

  // Reset page on filter change.
  React.useEffect(() => {
    setPage(0);
  }, []);

  // Load data.
  React.useEffect(() => {
    let alive = true;
    setLoading(true);
    api
      .commands({ task: taskFilter || undefined, q: queryQ || undefined, page, size })
      .then((r) => {
        if (!alive) return;
        setCommands(r.commands ?? []);
        setTotal(r.total ?? 0);
      })
      .catch(() => {})
      .finally(() => alive && setLoading(false));
    return () => {
      alive = false;
    };
  }, [page, size, queryQ, taskFilter]);

  const totalPages = Math.max(1, Math.ceil(total / size));
  const rangeStart = total === 0 ? 0 : page * size + 1;
  const rangeEnd = page * size + commands.length;

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-4">
      <div className="flex items-center justify-between gap-4">
        <div className="flex items-center gap-2">
          <TerminalIcon className="size-5 text-muted-foreground" />
          <h1 className="font-semibold text-xl tracking-tight">工具执行</h1>
          <Badge variant="secondary">{total}</Badge>
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <div className="relative max-w-sm flex-1">
          <SearchIcon className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            placeholder="搜索工具 / 参数..."
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            className="h-8 pl-8"
          />
        </div>
        <Input
          placeholder="任务 ID"
          className="h-8 w-28"
          value={taskFilter}
          onChange={(e) => setTaskFilter(e.target.value.replace(/\D/g, ""))}
        />
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
                  <TableHead className="w-[130px]">时间</TableHead>
                  <TableHead className="w-[60px]">任务</TableHead>
                  <TableHead className="w-[90px]">Worker</TableHead>
                  <TableHead className="w-[110px]">工具</TableHead>
                  <TableHead>输入</TableHead>
                  <TableHead className="w-[60px]">状态</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {loading && commands.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={6} className="py-12 text-center">
                      <Loader2Icon className="mx-auto size-5 animate-spin text-muted-foreground" />
                    </TableCell>
                  </TableRow>
                ) : commands.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={6} className="py-12 text-center text-muted-foreground text-sm">
                      暂无工具执行记录
                    </TableCell>
                  </TableRow>
                ) : (
                  commands.map((cmd) => (
                    <TableRow
                      key={cmd.id}
                      className={cn("cursor-pointer", selected?.id === cmd.id && "bg-accent hover:bg-accent")}
                      onClick={() => setSelected(cmd)}
                    >
                      <TableCell className="text-muted-foreground text-xs tabular-nums">
                        {fmtTime(cmd.created_at)}
                      </TableCell>
                      <TableCell className="font-mono text-muted-foreground text-xs">#{cmd.exploration_id}</TableCell>
                      <TableCell>
                        <Badge variant="outline" className="font-mono text-xs">
                          {cmd.worker || "-"}
                        </Badge>
                      </TableCell>
                      <TableCell>
                        <Badge variant="secondary" className="font-mono text-xs">
                          {cmd.tool || "-"}
                        </Badge>
                      </TableCell>
                      <TableCell className="max-w-0">
                        <code className="block truncate font-mono text-xs">
                          {truncate(toolInput(cmd.command), CMD_MAX_LEN)}
                        </code>
                      </TableCell>
                      <TableCell>
                        {cmd.is_error ? (
                          <Badge variant="destructive" className="text-xs">
                            失败
                          </Badge>
                        ) : (
                          <Badge variant="secondary" className="text-emerald-600 text-xs">
                            成功
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
                #{selected.exploration_id}
              </Badge>
              <Badge variant="outline" className="font-mono text-xs">
                {selected.worker || "-"}
              </Badge>
              <Badge variant="secondary" className="font-mono text-xs">
                {selected.tool || "-"}
              </Badge>
              <span className="text-muted-foreground text-xs">{fmtTime(selected.created_at)}</span>
              {selected.is_error ? (
                <Badge variant="destructive" className="text-xs">
                  失败
                </Badge>
              ) : (
                <Badge variant="secondary" className="text-emerald-600 text-xs">
                  成功
                </Badge>
              )}
              <Button variant="ghost" size="icon" className="ml-auto size-7 shrink-0" onClick={() => setSelected(null)}>
                <XIcon />
              </Button>
            </div>
            <div className="grid min-h-0 flex-1 grid-cols-2 divide-x">
              <div className="flex min-h-0 min-w-0 flex-col">
                <div className="border-b px-3 py-1 font-medium text-[11px] text-muted-foreground">输入 Input</div>
                <div className="min-h-0 flex-1 overflow-auto">
                  <pre className="whitespace-pre-wrap break-all p-3 font-mono text-xs">
                    {toolInput(selected.command)}
                  </pre>
                </div>
              </div>
              <div className="flex min-h-0 min-w-0 flex-col">
                <div className="border-b px-3 py-1 font-medium text-[11px] text-muted-foreground">输出 Output</div>
                <div className="min-h-0 flex-1 overflow-auto">
                  <pre
                    className={cn(
                      "whitespace-pre-wrap break-all p-3 font-mono text-xs",
                      selected.is_error && "text-red-600 dark:text-red-400",
                    )}
                  >
                    {selected.output || "（空）"}
                  </pre>
                </div>
              </div>
            </div>
          </Card>
        )}
      </div>
    </div>
  );
}
