"use client";

import * as React from "react";

import { DatabaseIcon, DownloadIcon, KeyIcon, SearchIcon, SparklesIcon } from "lucide-react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { api } from "@/lib/api";
import type { SpaceProvider, SpaceSearchConfigItem, SpaceSearchResult } from "@/lib/types";

const PROVIDERS: { value: SpaceProvider; label: string; hint: string; example: string }[] = [
  { value: "fofa", label: "FOFA", hint: "fofa.info", example: 'domain="example.com" && port="443"' },
  { value: "hunter", label: "Hunter（鹰图）", hint: "hunter.qianxin.com", example: 'domain="example.com"' },
  { value: "quake", label: "Quake（钟馗之眼）", hint: "quake.360.cn", example: 'domain: "example.com"' },
];

function ConfigDialog({ onSaved }: { onSaved?: () => void }) {
  const [open, setOpen] = React.useState(false);
  const [providers, setProviders] = React.useState<SpaceSearchConfigItem[]>([]);
  const [keys, setKeys] = React.useState<Record<string, string>>({ fofa: "", hunter: "", quake: "" });
  const [saving, setSaving] = React.useState(false);

  const load = React.useCallback(() => {
    api
      .spaceSearchConfigs()
      .then((r) => {
        setProviders(r.providers ?? []);
        setKeys((prev) => {
          const next = { ...prev };
          for (const p of r.providers ?? []) {
            next[p.provider] = next[p.provider] ?? "";
          }
          return next;
        });
      })
      .catch(() => setProviders([]));
  }, []);

  const _openDialog = () => {
    load();
    setOpen(true);
  };

  const saveAll = async () => {
    setSaving(true);
    try {
      for (const p of ["fofa", "hunter", "quake"] as SpaceProvider[]) {
        await api.spaceSearchSetConfig(p, keys[p] ?? "");
      }
      toast.success("配置已保存");
      setOpen(false);
      onSaved?.();
    } catch (e) {
      toast.error(`保存失败：${(e as Error).message}`);
    } finally {
      setSaving(false);
    }
  };

  const testOne = async (provider: SpaceProvider) => {
    try {
      const res = await api.spaceSearchTest(provider);
      if (!res.ok) {
        toast.error(res.error || `${PROVIDERS.find((p) => p.value === provider)?.label} 连接失败`);
        return;
      }
      toast.success(`${PROVIDERS.find((p) => p.value === provider)?.label} 连接正常`);
    } catch (e) {
      toast.error(`${(e as Error).message}`);
    }
  };

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size="sm" variant="outline">
          <KeyIcon className="size-4" /> API 配置
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>空间测绘 API 配置</DialogTitle>
          <DialogDescription>填写各引擎的 API Key。Key 仅保存在服务端，界面不回显明文。</DialogDescription>
        </DialogHeader>
        <div className="grid gap-4 py-2">
          {PROVIDERS.map((p) => {
            const cfg = providers.find((c) => c.provider === p.value);
            return (
              <div key={p.value} className="grid gap-1.5">
                <div className="flex items-center justify-between">
                  <Label className="text-sm">{p.label}</Label>
                  {cfg?.key_set ? (
                    <Badge variant="secondary" className="text-emerald-600">
                      已配置 {cfg.key_hint}
                    </Badge>
                  ) : (
                    <Badge variant="outline">未配置</Badge>
                  )}
                </div>
                <div className="flex gap-2">
                  <Input
                    type="password"
                    placeholder={`${p.hint} 的 API Key`}
                    value={keys[p.value] ?? ""}
                    onChange={(e) => setKeys((prev) => ({ ...prev, [p.value]: e.target.value }))}
                  />
                  <Button size="sm" variant="outline" onClick={() => testOne(p.value)}>
                    <SearchIcon className="size-3.5" /> 测试
                  </Button>
                </div>
              </div>
            );
          })}
        </div>
        <DialogFooter>
          <DialogClose asChild>
            <Button variant="outline">取消</Button>
          </DialogClose>
          <Button onClick={saveAll} disabled={saving}>
            {saving ? "保存中…" : "保存"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function QueryPresetDialog({ provider, onPick }: { provider: SpaceProvider; onPick: (q: string) => void }) {
  const [open, setOpen] = React.useState(false);
  const presets = PROVIDERS.find((p) => p.value === provider);
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size="sm" variant="outline">
          <SparklesIcon className="size-4" /> 示例
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>查询示例 · {presets?.label}</DialogTitle>
          <DialogDescription>点击填入查询框后执行。</DialogDescription>
        </DialogHeader>
        <div className="grid gap-2 py-2">
          {presets?.example ? (
            <button
              type="button"
              className="rounded-md border p-3 text-left font-mono text-xs hover:bg-muted"
              onClick={() => {
                onPick(presets.example);
                setOpen(false);
              }}
            >
              {presets.example}
            </button>
          ) : null}
        </div>
      </DialogContent>
    </Dialog>
  );
}

export default function SpaceSearchPage() {
  const [provider, setProvider] = React.useState<SpaceProvider>("fofa");
  const [query, setQuery] = React.useState("");
  const [size, setSize] = React.useState("50");
  const [page, setPage] = React.useState("1");
  const [results, setResults] = React.useState<SpaceSearchResult[]>([]);
  const [total, setTotal] = React.useState(0);
  const [searching, setSearching] = React.useState(false);
  const [importing, setImporting] = React.useState(false);
  const [checked, setChecked] = React.useState<Set<number>>(new Set());

  const toggle = (idx: number) => {
    setChecked((prev) => {
      const next = new Set(prev);
      if (next.has(idx)) next.delete(idx);
      else next.add(idx);
      return next;
    });
  };

  async function doSearch(p: SpaceProvider = provider, q: string = query) {
    if (!q.trim()) {
      toast.error("请输入查询语法");
      return;
    }
    setSearching(true);
    try {
      const resp = await api.spaceSearch({
        provider: p,
        query: q.trim(),
        size: Number(size) || 50,
        page: Number(page) || 1,
      });
      setResults(resp.results ?? []);
      setTotal(resp.total ?? 0);
      setChecked(new Set());
    } catch (e) {
      toast.error(`搜索失败：${(e as Error).message}`);
      setResults([]);
    } finally {
      setSearching(false);
    }
  }

  async function doImport() {
    if (checked.size === 0) {
      toast.error("请先勾选要导入的结果");
      return;
    }
    const selected = results.filter((_, i) => checked.has(i));
    setImporting(true);
    try {
      const resp = await api.spaceSearchImport({
        results: selected,
        provider,
        query,
      });
      toast.success(
        `已导入 ${resp.imported} 条资产（IP ${resp.stats?.ip ?? 0} / 域名 ${resp.stats?.subdomain ?? 0} / 服务 ${resp.stats?.service ?? 0}）`,
      );
      setChecked(new Set());
    } catch (e) {
      toast.error(`导入失败：${(e as Error).message}`);
    } finally {
      setImporting(false);
    }
  }

  function exportCSV() {
    if (results.length === 0) return;
    const header = ["ip", "port", "protocol", "domain", "url", "title", "server", "country", "city"];
    const rows = [header.join(",")];
    for (const r of results) {
      const vals = header.map((h) => String(r[h as keyof SpaceSearchResult] ?? "").replaceAll('"', '""'));
      rows.push(`"${vals.join('","')}"`);
    }
    const blob = new Blob([rows.join("\n")], { type: "text/csv;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `spacesearch-${provider}-${Date.now()}.csv`;
    a.click();
    URL.revokeObjectURL(url);
  }

  const fmtAddr = (r: SpaceSearchResult) => {
    const host = r.domain || r.ip;
    return r.port ? `${host}:${r.port}` : host;
  };

  return (
    <div className="flex flex-1 flex-col gap-4">
      <div className="flex items-center justify-between gap-4">
        <div className="flex items-center gap-2">
          <DatabaseIcon className="size-5 text-muted-foreground" />
          <h1 className="font-semibold text-xl tracking-tight">空间测绘</h1>
          <Badge variant="secondary">FOFA / Hunter / Quake</Badge>
        </div>
        <ConfigDialog />
      </div>

      <Card>
        <CardContent className="grid gap-3 p-4">
          <div className="flex flex-wrap items-end gap-2">
            <div className="grid gap-1.5">
              <Label className="text-xs">数据源</Label>
              <Select
                value={provider}
                onValueChange={(v) => {
                  setProvider(v as SpaceProvider);
                  setResults([]);
                  setChecked(new Set());
                }}
              >
                <SelectTrigger className="w-40">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {PROVIDERS.map((p) => (
                    <SelectItem key={p.value} value={p.value}>
                      {p.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="grid min-w-[280px] flex-1 gap-1.5">
              <Label className="text-xs">查询语法</Label>
              <Input
                placeholder={PROVIDERS.find((p) => p.value === provider)?.example}
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") void doSearch();
                }}
              />
            </div>
            <div className="grid gap-1.5">
              <Label className="text-xs">条数</Label>
              <Input
                type="number"
                value={size}
                onChange={(e) => setSize(e.target.value)}
                className="w-20"
                min={1}
                max={200}
              />
            </div>
            <div className="grid gap-1.5">
              <Label className="text-xs">页</Label>
              <Input type="number" value={page} onChange={(e) => setPage(e.target.value)} className="w-16" min={1} />
            </div>
            <Button size="sm" onClick={() => void doSearch()} disabled={searching}>
              {searching ? <span className="animate-pulse">搜索中…</span> : <SearchIcon className="size-4" />}
              {!searching && "搜索"}
            </Button>
            <QueryPresetDialog provider={provider} onPick={(q) => setQuery(q)} />
          </div>
          <div className="text-muted-foreground text-xs">
            {total > 0
              ? `共匹配 ${total} 条，当前展示 ${results.length} 条。`
              : "输入查询语法后点击「搜索」。结果可勾选后批量导入资产管理。"}
          </div>
        </CardContent>
      </Card>

      <Card className="overflow-hidden py-0">
        <CardContent className="p-0">
          {results.length === 0 ? (
            <div className="flex flex-col items-center justify-center gap-2 py-16 text-muted-foreground text-sm">
              <SearchIcon className="size-8 opacity-40" />
              {searching ? "搜索中…" : "暂无搜索结果"}
            </div>
          ) : (
            <div>
              <div className="flex items-center justify-between border-b bg-muted/50 px-3 py-2">
                <span className="text-muted-foreground text-xs">
                  已选 <span className="font-semibold tabular-nums">{checked.size}</span> 条
                </span>
                <div className="flex items-center gap-2">
                  <Button size="sm" variant="outline" onClick={exportCSV} disabled={results.length === 0}>
                    <DownloadIcon className="size-3.5" /> 导出 CSV
                  </Button>
                  <Button size="sm" onClick={() => void doImport()} disabled={importing || checked.size === 0}>
                    <DatabaseIcon className="size-3.5" /> {importing ? "导入中…" : "导入所选到资产"}
                  </Button>
                </div>
              </div>
              <div className="max-h-[60vh] overflow-auto">
                <table className="w-full text-sm">
                  <thead className="sticky top-0 bg-muted/50 text-muted-foreground text-xs">
                    <tr className="text-left">
                      <th className="w-8 px-3 py-2">
                        <Checkbox
                          checked={results.length > 0 && checked.size === results.length}
                          onCheckedChange={() => {
                            setChecked((prev) =>
                              prev.size === results.length ? new Set() : new Set(results.map((_, i) => i)),
                            );
                          }}
                          aria-label="全选"
                        />
                      </th>
                      <th className="px-3 py-2 font-medium">地址</th>
                      <th className="px-3 py-2 font-medium">协议</th>
                      <th className="px-3 py-2 font-medium">端口</th>
                      <th className="px-3 py-2 font-medium">标题</th>
                      <th className="px-3 py-2 font-medium">服务</th>
                      <th className="px-3 py-2 font-medium">位置</th>
                    </tr>
                  </thead>
                  <tbody>
                    {results.map((r, i) => (
                      // 搜索结果无稳定唯一 id，索引作为 key（配合可勾选序号）可接受
                      // biome-ignore lint/suspicious/noArrayIndexKey: no stable id in space-search results
                      <tr key={`${r.ip}-${r.port}-${r.domain}-${i}`} className="border-t">
                        <td className="w-8 px-3 py-2">
                          <Checkbox
                            checked={checked.has(i)}
                            onCheckedChange={() => toggle(i)}
                            aria-label={`选择 ${fmtAddr(r)}`}
                          />
                        </td>
                        <td className="px-3 py-2">
                          <span className="font-mono text-xs">{fmtAddr(r)}</span>
                          {r.url ? (
                            <span className="ml-2 truncate font-mono text-[10px] text-muted-foreground">{r.url}</span>
                          ) : null}
                        </td>
                        <td className="px-3 py-2">
                          {r.protocol ? (
                            <Badge variant="outline" className="uppercase">
                              {r.protocol}
                            </Badge>
                          ) : (
                            <span className="text-muted-foreground">—</span>
                          )}
                        </td>
                        <td className="px-3 py-2 tabular-nums">{r.port || "—"}</td>
                        <td className="max-w-48 truncate px-3 py-2">{r.title || "—"}</td>
                        <td className="max-w-40 truncate px-3 py-2">{r.server || "—"}</td>
                        <td className="px-3 py-2 text-muted-foreground text-xs">
                          {[r.country, r.city].filter(Boolean).join(" / ") || "—"}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
