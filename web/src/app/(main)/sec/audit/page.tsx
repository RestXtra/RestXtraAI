"use client";

import * as React from "react";

import { RefreshCwIcon, ShieldCheckIcon, Trash2Icon } from "lucide-react";

import { PermissionGate } from "@/components/permission-gate";
import { TablePagination } from "@/components/table-pagination";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { api } from "@/lib/api";
import type { AuditLogEntry } from "@/lib/types";

const RESULT_LABEL: Record<string, { text: string; variant: "success" | "warning" | "destructive" | "secondary" }> = {
  success: { text: "成功", variant: "success" },
  failure: { text: "失败", variant: "destructive" },
  denied: { text: "拒绝", variant: "warning" },
};

function fmtTime(ts: string) {
  return new Date(ts).toLocaleString("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

export default function AuditPage() {
  const [items, setItems] = React.useState<AuditLogEntry[]>([]);
  const [total, setTotal] = React.useState(0);
  const [page, setPage] = React.useState(1);
  const [pageSize, setPageSize] = React.useState(50);
  const [category, setCategory] = React.useState("");
  const [action, _setAction] = React.useState("");
  const [result, setResult] = React.useState("");
  const [actor, setActor] = React.useState("");
  const [loading, setLoading] = React.useState(true);

  const load = React.useCallback(() => {
    setLoading(true);
    api
      .auditLogs({
        category: category || undefined,
        action: action || undefined,
        result: result || undefined,
        actor: actor || undefined,
        limit: pageSize,
        offset: (page - 1) * pageSize,
      })
      .then((r) => {
        setItems(r.items);
        setTotal(r.total);
      })
      .catch(() => {})
      .finally(() => setLoading(false));
  }, [category, action, result, actor, page, pageSize]);

  React.useEffect(load, [load]);

  async function gc() {
    if (!window.confirm("确认清理 90 天前的审计日志？")) return;
    try {
      const r = await api.auditGC(90);
      alert(`已清理 ${r.removed} 条`);
      load();
    } catch {
      /* noop */
    }
  }

  async function verifyChain() {
    try {
      const r = await api.auditVerify();
      const c = r.check;
      if (r.ok) {
        alert(`审计哈希链完整 ✓（共 ${c.total} 条，链无断裂）`);
      } else {
        alert(
          `审计链异常！共 ${c.total} 条，断裂 ${c.broken} 条，未链化 ${c.legacy} 条\n断裂 id：${c.broken_ids.join(", ") || "无"}`,
        );
      }
    } catch (e) {
      alert(`验证失败：${(e as Error).message}`);
    }
  }

  return (
    <PermissionGate perm="sec.audit.read">
      <div className="space-y-6 p-4 md:p-6">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h1 className="font-semibold text-2xl tracking-tight">审计日志</h1>
            <p className="text-muted-foreground text-sm">平台操作全量留痕（登录 / 成员 / 角色 / RBAC 拒绝等）。</p>
          </div>
          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={load}>
              <RefreshCwIcon className="size-4" /> 刷新
            </Button>
            <Button variant="outline" size="sm" onClick={verifyChain}>
              <ShieldCheckIcon className="size-4" /> 验证哈希链
            </Button>
            <Button variant="outline" size="sm" onClick={gc}>
              <Trash2Icon className="size-4" /> 清理 90 天前
            </Button>
          </div>
        </div>

        {/* filters */}
        <div className="flex flex-wrap items-center gap-2">
          <Input
            className="w-40"
            placeholder="操作者"
            value={actor}
            onChange={(e) => {
              setActor(e.target.value);
              setPage(1);
            }}
          />
          <Select
            value={category}
            onValueChange={(v) => {
              setCategory(v);
              setPage(1);
            }}
          >
            <SelectTrigger className="w-36">
              <SelectValue placeholder="分类" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="">全部</SelectItem>
              <SelectItem value="auth">auth</SelectItem>
              <SelectItem value="platform">platform</SelectItem>
              <SelectItem value="rbac">rbac</SelectItem>
            </SelectContent>
          </Select>
          <Select
            value={result}
            onValueChange={(v) => {
              setResult(v);
              setPage(1);
            }}
          >
            <SelectTrigger className="w-28">
              <SelectValue placeholder="结果" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="">全部</SelectItem>
              <SelectItem value="success">成功</SelectItem>
              <SelectItem value="failure">失败</SelectItem>
              <SelectItem value="denied">拒绝</SelectItem>
            </SelectContent>
          </Select>
        </div>

        <div className="rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-40">时间</TableHead>
                <TableHead className="w-28">操作者</TableHead>
                <TableHead className="w-24">分类</TableHead>
                <TableHead className="w-32">动作</TableHead>
                <TableHead className="w-20">结果</TableHead>
                <TableHead>说明</TableHead>
                <TableHead className="w-28">来源 IP</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading ? (
                <TableRow>
                  <TableCell colSpan={7} className="py-10 text-center text-muted-foreground">
                    加载中…
                  </TableCell>
                </TableRow>
              ) : items.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={7} className="py-10 text-center text-muted-foreground">
                    暂无审计记录
                  </TableCell>
                </TableRow>
              ) : (
                items.map((e) => {
                  const rl = RESULT_LABEL[e.result] ?? { text: e.result || "-", variant: "secondary" as const };
                  return (
                    <TableRow key={e.id}>
                      <TableCell className="text-muted-foreground text-xs">{fmtTime(e.created_at)}</TableCell>
                      <TableCell>{e.actor || "-"}</TableCell>
                      <TableCell>{e.category || "-"}</TableCell>
                      <TableCell>
                        <code className="text-xs">{e.action || "-"}</code>
                      </TableCell>
                      <TableCell>
                        <Badge variant={rl.variant}>{rl.text}</Badge>
                      </TableCell>
                      <TableCell className="max-w-md truncate">{e.message || "-"}</TableCell>
                      <TableCell className="text-muted-foreground text-xs">{e.ip || "-"}</TableCell>
                    </TableRow>
                  );
                })
              )}
            </TableBody>
          </Table>
        </div>

        <TablePagination
          page={page}
          pageSize={pageSize}
          total={total}
          onPageChange={setPage}
          onPageSizeChange={setPageSize}
        />
      </div>
    </PermissionGate>
  );
}
