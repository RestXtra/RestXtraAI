"use client";

import * as React from "react";

import { toast } from "sonner";
import { BookMarkedIcon, PlusIcon, SearchIcon, Trash2Icon } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { PermissionGate } from "@/components/permission-gate";
import { api } from "@/lib/api";
import type { AttackPattern, PlaybookResult } from "@/lib/types";
import { useCurrentUser } from "@/hooks/use-current-user";

const VERIFY_LABEL: Record<string, { text: string; variant: "default" | "secondary" | "warning" }> = {
  validated: { text: "已验证", variant: "default" },
  reference: { text: "参考", variant: "secondary" },
  draft: { text: "草稿", variant: "warning" },
};

export default function PlaybookPage() {
  const me = useCurrentUser();
  const canWrite = me.admin || (me.permissions ?? []).includes("playbook.write");
  const [patterns, setPatterns] = React.useState<AttackPattern[]>([]);
  const [results, setResults] = React.useState<PlaybookResult[]>([]);
  const [stats, setStats] = React.useState<{ total: number; counts: Record<string, number> }>({ total: 0, counts: {} });
  const [keywords, setKeywords] = React.useState("");
  const [createOpen, setCreateOpen] = React.useState(false);
  const [form, setForm] = React.useState<{
    title: string; summary: string; attack_technique_id: string; cve_id: string; tags: string;
    verification: AttackPattern["verification"]; execution_steps: string; confidence: number;
  }>({
    title: "", summary: "", attack_technique_id: "", cve_id: "", tags: "",
    verification: "draft", execution_steps: "", confidence: 0,
  });

  const load = React.useCallback(() => {
    api.playbookPatterns().then((r) => setPatterns(r.patterns)).catch(() => {});
    api.playbookStats().then(setStats).catch(() => {});
  }, []);

  React.useEffect(load, [load]);

  async function doSearch() {
    try {
      const r = await api.playbookSearch({ keywords, limit: 20 });
      setResults(r);
    } catch (e) {
      toast.error((e as Error).message ?? "检索失败");
    }
  }

  async function createPattern() {
    try {
      await api.createPlaybookPattern({ ...form });
      toast.success("攻击模式已创建");
      setCreateOpen(false);
      setForm({ title: "", summary: "", attack_technique_id: "", cve_id: "", tags: "", verification: "draft", execution_steps: "", confidence: 0 });
      load();
    } catch (e) {
      toast.error((e as Error).message ?? "创建失败");
    }
  }

  async function removePattern(id: string, title: string) {
    if (!window.confirm(`确认删除攻击模式「${title}」？`)) return;
    try {
      await api.deletePlaybookPattern(id);
      toast.success("已删除");
      load();
    } catch (e) {
      toast.error((e as Error).message ?? "删除失败");
    }
  }

  const shown = results.length > 0 ? results.map((r) => r.pattern) : patterns;

  return (
    <PermissionGate perm="playbook.read">
      <div className="space-y-6 p-4 md:p-6">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h1 className="text-2xl font-semibold tracking-tight">攻击模式库</h1>
            <p className="text-sm text-muted-foreground">跨项目经验库（playbook）：结构化标签 + 文本双路检索。</p>
          </div>
          {canWrite && (
            <Button onClick={() => setCreateOpen(true)}>
              <PlusIcon className="size-4" /> 新增模式
            </Button>
          )}
        </div>

        {/* stats */}
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <Card>
            <CardHeader className="pb-2"><CardTitle className="text-sm text-muted-foreground">模式总数</CardTitle></CardHeader>
            <CardContent><div className="text-2xl font-semibold">{stats.total}</div></CardContent>
          </Card>
          <Card>
            <CardHeader className="pb-2"><CardTitle className="text-sm text-muted-foreground">已验证</CardTitle></CardHeader>
            <CardContent><div className="text-2xl font-semibold text-emerald-500">{stats.counts?.validated ?? 0}</div></CardContent>
          </Card>
          <Card>
            <CardHeader className="pb-2"><CardTitle className="text-sm text-muted-foreground">参考</CardTitle></CardHeader>
            <CardContent><div className="text-2xl font-semibold">{stats.counts?.reference ?? 0}</div></CardContent>
          </Card>
          <Card>
            <CardHeader className="pb-2"><CardTitle className="text-sm text-muted-foreground">草稿</CardTitle></CardHeader>
            <CardContent><div className="text-2xl font-semibold text-amber-500">{stats.counts?.draft ?? 0}</div></CardContent>
          </Card>
        </div>

        {/* search */}
        <div className="flex flex-wrap gap-2">
          <Input
            className="max-w-md"
            placeholder="关键词 / 组件指纹（如 php, laravel, sqli）"
            value={keywords}
            onChange={(e) => setKeywords(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && doSearch()}
          />
          <Button onClick={doSearch}><SearchIcon className="size-4" /> 检索</Button>
          {results.length > 0 && (
            <Button variant="outline" onClick={() => { setResults([]); }}>
              <BookMarkedIcon className="size-4" /> 清空检索
            </Button>
          )}
        </div>

        <div className="rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>模式</TableHead>
                <TableHead className="w-32">ATT&CK</TableHead>
                <TableHead className="w-36">CVE</TableHead>
                <TableHead className="w-28">标签</TableHead>
                <TableHead className="w-24">状态</TableHead>
                <TableHead className="w-20">置信度</TableHead>
                {results.length > 0 && <TableHead className="w-20">得分</TableHead>}
                <TableHead className="w-20 text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {shown.length === 0 ? (
                <TableRow><TableCell colSpan={8} className="py-10 text-center text-muted-foreground">暂无攻击模式</TableCell></TableRow>
              ) : (
                shown.map((p) => {
                  const v = VERIFY_LABEL[p.verification] ?? { text: p.verification, variant: "secondary" as const };
                  const res = results.find((r) => r.pattern.id === p.id);
                  return (
                    <TableRow key={p.id}>
                      <TableCell>
                        <div className="font-medium">{p.title}</div>
                        <div className="max-w-md truncate text-xs text-muted-foreground">{p.summary || "-"}</div>
                      </TableCell>
                      <TableCell><code className="text-xs">{p.attack_technique_id || "-"}</code></TableCell>
                      <TableCell><code className="text-xs">{p.cve_id || "-"}</code></TableCell>
                      <TableCell>
                        <div className="flex flex-wrap gap-1">
                          {p.tags.split(",").filter(Boolean).slice(0, 3).map((t) => (
                            <Badge key={t} variant="outline">{t.trim()}</Badge>
                          ))}
                        </div>
                      </TableCell>
                      <TableCell><Badge variant={v.variant}>{v.text}</Badge></TableCell>
                      <TableCell>{p.confidence}</TableCell>
                      {results.length > 0 && <TableCell className="font-semibold">{res?.score ?? "-"}</TableCell>}
                      <TableCell className="text-right">
                        {canWrite && (
                          <Button variant="ghost" size="sm" onClick={() => removePattern(p.id, p.title)}>
                            <Trash2Icon className="size-4" />
                          </Button>
                        )}
                      </TableCell>
                    </TableRow>
                  );
                })
              )}
            </TableBody>
          </Table>
        </div>

        {/* 新增模式 */}
        <Dialog open={createOpen} onOpenChange={setCreateOpen}>
          <DialogContent className="sm:max-w-xl">
            <DialogHeader>
              <DialogTitle>新增攻击模式</DialogTitle>
              <DialogDescription>记录一种攻击手法 / 漏洞利用模式，供后续检索复用。</DialogDescription>
            </DialogHeader>
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-1.5 sm:col-span-2">
                <Label>标题 *</Label>
                <Input value={form.title} onChange={(e) => setForm({ ...form, title: e.target.value })} placeholder="如：Laravel 调试模式 RCE" />
              </div>
              <div className="space-y-1.5 sm:col-span-2">
                <Label>摘要</Label>
                <Input value={form.summary} onChange={(e) => setForm({ ...form, summary: e.target.value })} />
              </div>
              <div className="space-y-1.5">
                <Label>ATT&CK 编号</Label>
                <Input value={form.attack_technique_id} onChange={(e) => setForm({ ...form, attack_technique_id: e.target.value })} placeholder="T1190" />
              </div>
              <div className="space-y-1.5">
                <Label>CVE</Label>
                <Input value={form.cve_id} onChange={(e) => setForm({ ...form, cve_id: e.target.value })} placeholder="CVE-2024-xxxx" />
              </div>
              <div className="space-y-1.5">
                <Label>标签（逗号分隔）</Label>
                <Input value={form.tags} onChange={(e) => setForm({ ...form, tags: e.target.value })} placeholder="php,laravel,rce" />
              </div>
              <div className="space-y-1.5">
                <Label>验证状态</Label>
                <Select value={form.verification} onValueChange={(v) => setForm({ ...form, verification: v as AttackPattern["verification"] })}>
                  <SelectTrigger><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="draft">draft · 草稿</SelectItem>
                    <SelectItem value="reference">reference · 参考</SelectItem>
                    <SelectItem value="validated">validated · 已验证</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1.5 sm:col-span-2">
                <Label>执行步骤（Markdown）</Label>
                <Textarea rows={4} value={form.execution_steps} onChange={(e) => setForm({ ...form, execution_steps: e.target.value })} />
              </div>
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => setCreateOpen(false)}>取消</Button>
              <Button onClick={createPattern} disabled={!form.title.trim()}>创建</Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>
    </PermissionGate>
  );
}
