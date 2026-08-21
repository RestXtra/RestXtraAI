"use client";

import * as React from "react";

import Link from "next/link";

import { ArrowUpRightIcon, ChevronRightIcon, ExternalLinkIcon, ShieldAlertIcon } from "lucide-react";

import { StatusBadge } from "@/components/status-badge";
import { TablePagination } from "@/components/table-pagination";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { api } from "@/lib/api";
import type { Company, Finding, FindingStatus, Severity } from "@/lib/types";
import { cn } from "@/lib/utils";

function fmtTime(ts: string) {
  return new Date(ts).toLocaleString("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

export default function FindingsPage() {
  const [severity, setSeverity] = React.useState<"all" | Severity>("all");
  const [vulnclass, setVulnclass] = React.useState<string>("all");
  const [status, setStatus] = React.useState<"all" | FindingStatus>("all");
  const [companyFilter, setCompanyFilter] = React.useState<number | "all">("all");
  const [sort, setSort] = React.useState<"severity" | "time">("severity");
  const [expanded, setExpanded] = React.useState<string | null>(null);
  const [findings, setFindings] = React.useState<Finding[]>([]);
  const [companies, setCompanies] = React.useState<Company[]>([]);
  const [page, setPage] = React.useState(1);
  const [pageSize, setPageSize] = React.useState(20);
  const [total, setTotal] = React.useState(0);
  const [stats, setStats] = React.useState({
    total: 0,
    pending: 0,
    high: 0,
    medium: 0,
    low: 0,
    tasks: 0,
    vulnclasses: [] as string[],
  });

  React.useEffect(() => {
    let alive = true;
    const load = () => {
      const companyId = companyFilter === "all" ? undefined : companyFilter;
      api
        .findingsPage({ page, pageSize, severity, status, vulnclass, companyId, sort })
        .then((result) => {
          if (alive) {
            setFindings(result.items ?? []);
            setTotal(result.total ?? 0);
          }
        })
        .catch(() => {
          // Keep the last successful page during transient polling failures.
        });
      api
        .findingStats(companyId)
        .then((result) => {
          if (alive) setStats(result);
        })
        .catch(() => {
          // Keep the last successful aggregate during transient polling failures.
        });
    };
    load();
    const t = setInterval(load, 5000);
    return () => {
      alive = false;
      clearInterval(t);
    };
  }, [companyFilter, page, pageSize, severity, status, vulnclass, sort]);

  React.useEffect(() => {
    api
      .companies()
      .then(setCompanies)
      .catch(() => setCompanies([]));
  }, []);

  const companyName = React.useCallback((id: number) => companies.find((c) => c.id === id)?.name ?? "", [companies]);

  const rows = findings;

  // reset to page 1 whenever filters change
  React.useEffect(() => {
    setPage(1);
  }, [companyFilter, severity, status, vulnclass, sort]);

  const statCards: { label: string; value: number; tone?: string }[] = [
    { label: "发现总数", value: stats.total },
    { label: "高危", value: stats.high, tone: "text-red-500" },
    { label: "中危", value: stats.medium, tone: "text-amber-500" },
    { label: "待处理", value: stats.pending, tone: "text-blue-500" },
    { label: "涉及任务", value: stats.tasks },
  ];

  return (
    <div className="flex flex-1 flex-col gap-4 md:gap-6">
      <div>
        <h1 className="font-semibold text-xl tracking-tight">发现</h1>
        <p className="text-muted-foreground text-sm">跨任务漏洞汇总</p>
      </div>
      <div className="flex flex-1 flex-col gap-4 md:gap-6">
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-5">
          {statCards.map((s) => (
            <Card key={s.label} className="gap-1 py-4">
              <CardHeader className="px-4">
                <CardDescription>{s.label}</CardDescription>
                <CardTitle className={cn("text-2xl tabular-nums", s.tone)}>{s.value}</CardTitle>
              </CardHeader>
            </Card>
          ))}
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <div className="flex items-center gap-1 rounded-md border p-0.5">
            {(
              [
                ["all", "全部"],
                ["high", "高危"],
                ["medium", "中危"],
                ["low", "低危"],
              ] as const
            ).map(([val, label]) => (
              <button
                key={val}
                onClick={() => setSeverity(val)}
                className={cn(
                  "rounded px-2.5 py-1 font-medium text-xs transition-colors",
                  severity === val ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:bg-muted",
                )}
              >
                {label}
              </button>
            ))}
          </div>

          <Select value={vulnclass} onValueChange={setVulnclass}>
            <SelectTrigger size="sm" className="w-40">
              <SelectValue placeholder="漏洞类型" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部类型</SelectItem>
              {stats.vulnclasses.map((vc) => (
                <SelectItem key={vc} value={vc}>
                  {vc}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>

          <Select value={status} onValueChange={(value) => setStatus(value as "all" | FindingStatus)}>
            <SelectTrigger size="sm" className="w-36">
              <SelectValue placeholder="处理状态" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部状态</SelectItem>
              <SelectItem value="pending">待处理</SelectItem>
              <SelectItem value="in_progress">处理中</SelectItem>
              <SelectItem value="confirmed">已确认</SelectItem>
              <SelectItem value="resolved">已修复</SelectItem>
              <SelectItem value="false_positive">误报</SelectItem>
              <SelectItem value="ignored">已忽略</SelectItem>
              <SelectItem value="duplicate">重复</SelectItem>
              <SelectItem value="risk_accepted">风险接受</SelectItem>
            </SelectContent>
          </Select>

          <Select
            value={companyFilter === "all" ? "all" : String(companyFilter)}
            onValueChange={(v) => setCompanyFilter(v === "all" ? "all" : Number(v))}
          >
            <SelectTrigger size="sm" className="w-40">
              <SelectValue placeholder="企业" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部企业</SelectItem>
              {companies.map((c) => (
                <SelectItem key={c.id} value={String(c.id)}>
                  {c.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>

          <Select value={sort} onValueChange={(v) => setSort(v as "severity" | "time")}>
            <SelectTrigger size="sm" className="w-36">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="severity">按严重度</SelectItem>
              <SelectItem value="time">按时间</SelectItem>
            </SelectContent>
          </Select>

          <span className="ml-auto text-muted-foreground text-xs tabular-nums">共 {total} 条</span>
        </div>

        <Card className="py-0">
          <CardContent className="px-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-8" />
                  <TableHead className="w-20">严重度</TableHead>
                  <TableHead className="w-24">状态</TableHead>
                  <TableHead className="w-28">漏洞类型</TableHead>
                  <TableHead>摘要</TableHead>
                  <TableHead className="w-32 max-w-[7rem]">企业</TableHead>
                  <TableHead className="w-36 max-w-[9rem]">所属任务</TableHead>
                  <TableHead className="w-32">时间</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((f) => {
                  const open = expanded === f.id;
                  return (
                    <React.Fragment key={f.id}>
                      <TableRow className="cursor-pointer" onClick={() => setExpanded(open ? null : f.id)}>
                        <TableCell>
                          <ChevronRightIcon
                            className={cn("size-4 text-muted-foreground transition-transform", open && "rotate-90")}
                          />
                        </TableCell>
                        <TableCell>
                          <StatusBadge domain="severity" value={f.severity} dot />
                        </TableCell>
                        <TableCell>
                          <StatusBadge domain="finding" value={f.status} dot />
                        </TableCell>
                        <TableCell>
                          <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">{f.vulnclass}</code>
                        </TableCell>
                        <TableCell className="max-w-md">
                          <div className="flex items-center gap-2">
                            <Link
                              href={`/function/findings/detail?id=${f.id}`}
                              onClick={(event) => event.stopPropagation()}
                              className="line-clamp-1 hover:text-primary hover:underline"
                            >
                              {f.summary}
                            </Link>
                            <ExternalLinkIcon className="size-3 shrink-0 text-muted-foreground" />
                          </div>
                        </TableCell>
                        <TableCell className="w-32 max-w-[7rem]">
                          {(f.company_ids ?? []).length === 0 ? (
                            <span className="text-muted-foreground">—</span>
                          ) : (
                            <div className="flex flex-wrap gap-1">
                              {f.company_ids?.map((cid) => (
                                <span key={cid} className="rounded bg-muted px-1.5 py-0.5 text-[10px]">
                                  {companyName(cid) || (cid === 0 ? "未归属" : `#${cid}`)}
                                </span>
                              ))}
                            </div>
                          )}
                        </TableCell>
                        <TableCell className="w-36 max-w-[9rem]">
                          {f.task_id ? (
                            <Link
                              href={`/function/tasks/detail?id=${f.task_id}`}
                              onClick={(e) => e.stopPropagation()}
                              className="inline-flex max-w-full items-center gap-1 text-primary hover:underline"
                              title={f.task_description}
                            >
                              <span className="truncate">{f.task_description}</span>
                              <ArrowUpRightIcon className="size-3 shrink-0" />
                            </Link>
                          ) : (
                            <span className="text-muted-foreground">—</span>
                          )}
                        </TableCell>
                        <TableCell className="text-muted-foreground text-xs tabular-nums">{fmtTime(f.ts)}</TableCell>
                      </TableRow>
                      {open && (
                        <TableRow className="hover:bg-transparent">
                          <TableCell colSpan={8} className="bg-muted/30">
                            <div className="flex flex-col gap-2 px-2 py-1">
                              <div className="flex items-center gap-2 text-muted-foreground text-xs">
                                <ShieldAlertIcon className="size-3.5" />
                                证据 · {f.id}
                                {f.param_id && (
                                  <code className="rounded bg-muted px-1.5 py-0.5 font-mono">{f.param_id}</code>
                                )}
                              </div>
                              <pre className="overflow-x-auto whitespace-pre-wrap rounded-md bg-muted px-3 py-2 font-mono text-xs">
                                {f.evidence}
                              </pre>
                            </div>
                          </TableCell>
                        </TableRow>
                      )}
                    </React.Fragment>
                  );
                })}
                {rows.length === 0 && (
                  <TableRow>
                    <TableCell colSpan={8} className="py-12 text-center text-muted-foreground text-sm">
                      没有匹配的发现。
                    </TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
            <TablePagination
              page={page}
              pageSize={pageSize}
              total={total}
              onPageChange={setPage}
              onPageSizeChange={setPageSize}
            />
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
