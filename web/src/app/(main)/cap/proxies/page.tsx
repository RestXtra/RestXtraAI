"use client";

import * as React from "react";

import { BoxesIcon, GlobeIcon, PlayIcon, PlusIcon, RefreshCwIcon, SquareIcon, Trash2Icon, ZapIcon } from "lucide-react";
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
import type { ProxyBridgeRule, ProxyBridgeStatus, ProxyItem, ProxyPoolStats, ProxySourceItem } from "@/lib/types";

const PROTOCOLS = [
  { value: "http", label: "HTTP" },
  { value: "https", label: "HTTPS" },
  { value: "socks5", label: "SOCKS5" },
  { value: "socks5h", label: "SOCKS5H" },
];

function ProxyForm({ editing, onSaved }: { editing: ProxyItem | null; onSaved: () => void }) {
  const [open, setOpen] = React.useState(false);
  const [name, setName] = React.useState("");
  const [protocol, setProtocol] = React.useState("http");
  const [host, setHost] = React.useState("");
  const [port, setPort] = React.useState("8080");
  const [username, setUsername] = React.useState("");
  const [password, setPassword] = React.useState("");
  const [region, setRegion] = React.useState("");
  const [note, setNote] = React.useState("");
  const [enabled, setEnabled] = React.useState(true);
  const [saving, setSaving] = React.useState(false);

  const openFor = (p: ProxyItem | null) => {
    setName(p?.name ?? "");
    setProtocol(p?.protocol ?? "http");
    setHost(p?.host ?? "");
    setPort(p ? String(p.port) : "8080");
    setUsername(p?.username ?? "");
    setPassword("");
    setRegion(p?.region ?? "");
    setNote(p?.note ?? "");
    setEnabled(p?.enabled ?? true);
    setOpen(true);
  };

  async function save() {
    if (!host.trim()) {
      toast.error("Host 必填");
      return;
    }
    const portNum = Number(port);
    if (!portNum || portNum < 1 || portNum > 65535) {
      toast.error("端口非法");
      return;
    }
    setSaving(true);
    try {
      await api.saveProxy({
        id: editing ? Number(editing.id) : 0,
        name: name.trim() || `${host}:${port}`,
        protocol: protocol as ProxyItem["protocol"],
        host: host.trim(),
        port: portNum,
        username,
        password,
        region: region.trim(),
        note: note.trim(),
        enabled,
      });
      toast.success("已保存");
      setOpen(false);
      onSaved();
    } catch (e) {
      toast.error(`保存失败：${(e as Error).message}`);
    } finally {
      setSaving(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button
          size="sm"
          variant={editing ? "outline" : "default"}
          className={editing ? "" : "ml-auto"}
          onClick={() => openFor(editing)}
        >
          <PlusIcon /> {editing ? "编辑" : "新增代理"}
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{editing ? "编辑代理" : "新增代理"}</DialogTitle>
          <DialogDescription>登记一个可用的出站代理（支持 HTTP / HTTPS / SOCKS5）。</DialogDescription>
        </DialogHeader>
        <div className="grid gap-3 py-2">
          <div className="grid gap-2">
            <Label htmlFor="p-name">名称（可选）</Label>
            <Input id="p-name" placeholder="例如：海外-住宅A" value={name} onChange={(e) => setName(e.target.value)} />
          </div>
          <div className="grid grid-cols-4 gap-2">
            <div className="grid gap-2">
              <Label>协议</Label>
              <Select value={protocol} onValueChange={setProtocol}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {PROTOCOLS.map((p) => (
                    <SelectItem key={p.value} value={p.value}>
                      {p.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="col-span-2 grid gap-2">
              <Label htmlFor="p-host">Host</Label>
              <Input
                id="p-host"
                value={host}
                onChange={(e) => setHost(e.target.value)}
                placeholder="127.0.0.1 或 proxy.example.com"
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="p-port">端口</Label>
              <Input id="p-port" type="number" value={port} onChange={(e) => setPort(e.target.value)} />
            </div>
          </div>
          <div className="grid grid-cols-2 gap-2">
            <div className="grid gap-2">
              <Label htmlFor="p-user">用户名（可选）</Label>
              <Input id="p-user" value={username} onChange={(e) => setUsername(e.target.value)} />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="p-pass">密码（可选）</Label>
              <Input
                id="p-pass"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder={editing?.password_set ? "已保存，留空不修改" : ""}
              />
            </div>
          </div>
          <div className="grid grid-cols-2 gap-2">
            <div className="grid gap-2">
              <Label htmlFor="p-region">区域/标签（可选）</Label>
              <Input
                id="p-region"
                placeholder="us / hk / residential"
                value={region}
                onChange={(e) => setRegion(e.target.value)}
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="p-note">备注（可选）</Label>
              <Input id="p-note" value={note} onChange={(e) => setNote(e.target.value)} />
            </div>
          </div>
        </div>
        <DialogFooter>
          <DialogClose asChild>
            <Button variant="outline">取消</Button>
          </DialogClose>
          <Button onClick={save} disabled={saving}>
            {saving ? "保存中…" : "保存"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ImportDialog({ onSaved }: { onSaved: () => void }) {
  const [open, setOpen] = React.useState(false);
  const [text, setText] = React.useState("");
  const [url, setUrl] = React.useState("");
  const [importing, setImporting] = React.useState(false);

  async function doImport() {
    if (!text.trim() && !url.trim()) {
      toast.error("请输入代理文本或订阅 URL");
      return;
    }
    setImporting(true);
    try {
      const res = await api.proxyImport({ text, url });
      const detail =
        res.format === "clash"
          ? `（Clash：${res.imported}/${res.total} 节点${res.skipped ? `，跳过 ${res.skipped} 个不支持类型` : ""}${res.groups?.length ? `，${res.groups.length} 个分组` : ""}${res.rules_count ? `，${res.rules_count} 条规则` : ""}）`
          : `（${res.imported}/${res.total} 条${res.errors?.length ? `，${res.errors.length} 条失败` : ""}）`;
      toast.success(`导入完成 ${detail}`);
      setText("");
      setUrl("");
      setOpen(false);
      onSaved();
    } catch (e) {
      toast.error(`导入失败：${(e as Error).message}`);
    } finally {
      setImporting(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size="sm" variant="outline">
          <BoxesIcon className="size-4" /> 批量导入
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>批量导入代理</DialogTitle>
          <DialogDescription>
            支持格式：<code className="font-mono">ip:port</code>、<code className="font-mono">ip:port:user:pass</code>、
            <code className="font-mono">scheme://host:port</code>、
            <code className="font-mono">scheme://user:pass@host:port</code>， 或 <strong>Clash YAML</strong>（含{" "}
            <code className="font-mono">proxies:</code> 的订阅配置，自动解析 socks5/http 节点）； 也可填远程订阅 URL
            自动抓取。
          </DialogDescription>
        </DialogHeader>
        <div className="grid gap-3 py-2">
          <div className="grid gap-2">
            <Label htmlFor="imp-url">远程订阅 URL（可选）</Label>
            <Input
              id="imp-url"
              placeholder="https://example.com/sub?token=xxx"
              value={url}
              onChange={(e) => setUrl(e.target.value)}
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="imp-text">代理文本（每行一个）</Label>
            <textarea
              id="imp-text"
              className="flex min-h-36 w-full rounded-md border border-input bg-transparent px-3 py-2 text-sm shadow-xs placeholder:text-muted-foreground focus-visible:border-ring focus-visible:outline-hidden focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-50"
              placeholder={"1.2.3.4:8080\n1.2.3.5:8080:user:pass\nsocks5://1.2.3.6:1080"}
              value={text}
              onChange={(e) => setText(e.target.value)}
            />
          </div>
        </div>
        <DialogFooter>
          <DialogClose asChild>
            <Button variant="outline">取消</Button>
          </DialogClose>
          <Button onClick={doImport} disabled={importing}>
            {importing ? "导入中…" : "导入"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function SourceDialog({ onSaved }: { onSaved: () => void }) {
  const [open, setOpen] = React.useState(false);
  const [name, setName] = React.useState("");
  const [url, setUrl] = React.useState("");
  const [saving, setSaving] = React.useState(false);

  async function save() {
    if (!name.trim()) {
      toast.error("名称必填");
      return;
    }
    if (!url.trim()) {
      toast.error("订阅 URL 必填");
      return;
    }
    setSaving(true);
    try {
      await api.saveProxySource({ name: name.trim(), url: url.trim() });
      toast.success("已保存，正在刷新节点…");
      setOpen(false);
      setName("");
      setUrl("");
      onSaved();
    } catch (e) {
      toast.error(`保存失败：${(e as Error).message}`);
    } finally {
      setSaving(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size="sm" variant="outline">
          <PlusIcon /> 新增订阅源
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>新增代理订阅源</DialogTitle>
          <DialogDescription>填一个远程订阅 URL，保存后自动抓取并导入节点。</DialogDescription>
        </DialogHeader>
        <div className="grid gap-3 py-2">
          <div className="grid gap-2">
            <Label htmlFor="src-name">名称</Label>
            <Input
              id="src-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="例如：机场订阅-主"
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="src-url">订阅 URL</Label>
            <Input
              id="src-url"
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              placeholder="https://example.com/sub?token=xxx"
            />
          </div>
        </div>
        <DialogFooter>
          <DialogClose asChild>
            <Button variant="outline">取消</Button>
          </DialogClose>
          <Button onClick={save} disabled={saving}>
            {saving ? "保存中…" : "保存"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function StatCard({ label, value, sub }: { label: string; value: React.ReactNode; sub?: string }) {
  return (
    <Card>
      <CardContent className="space-y-1 p-4">
        <div className="text-muted-foreground text-xs">{label}</div>
        <div className="font-semibold text-2xl tabular-nums">{value}</div>{" "}
        {sub ? <div className="text-muted-foreground text-xs">{sub}</div> : null}
      </CardContent>
    </Card>
  );
}

// BridgePanel 展示并控制本地 mixed HTTP/SOCKS5 代理入口（Clash 风格）。
function BridgePanel({
  status,
  proxies,
  onChanged,
}: {
  status: ProxyBridgeStatus | null;
  proxies: ProxyItem[];
  onChanged: () => void;
}) {
  const [port, setPort] = React.useState("7890");
  const [nodeID, setNodeID] = React.useState("0");
  const [rules, setRules] = React.useState<ProxyBridgeRule[]>([]);
  const [rulesText, setRulesText] = React.useState("");
  const [busy, setBusy] = React.useState(false);

  React.useEffect(() => {
    if (!status) return;
    setPort(String(status.port || 7890));
    setNodeID(String(status.node_id || 0));
    setRules(status.rules ?? []);
    setRulesText(ruleListToText(status.rules ?? []));
  }, [status]);

  const healthy = proxies.filter((p) => p.enabled && p.last_check_ok);

  async function save(enabled: boolean) {
    setBusy(true);
    try {
      const parsedRules = parseRulesText(rulesText);
      await api.proxyBridgeSave({
        port: Number(port) || 7890,
        enabled,
        node_id: Number(nodeID) || 0,
        rules: parsedRules,
      });
      toast.success(enabled ? "代理入口已启动" : "代理入口已停止");
      onChanged();
    } catch (e) {
      toast.error(`操作失败：${(e as Error).message}`);
    } finally {
      setBusy(false);
    }
  }

  const rulePreview = ruleListToText(rules);

  return (
    <Card>
      <CardContent className="grid gap-3 p-4">
        <div className="flex items-center justify-between gap-2">
          <div className="flex items-center gap-2">
            <GlobeIcon className="size-5 text-muted-foreground" />
            <h2 className="font-semibold text-base tracking-tight">本地代理入口</h2>
            {status?.running ? (
              <Badge variant="secondary" className="text-emerald-600">
                运行中
              </Badge>
            ) : (
              <Badge variant="outline">已停止</Badge>
            )}
          </div>
          <div className="flex items-center gap-2">
            <Button
              size="sm"
              variant="outline"
              onClick={() => save(true)}
              disabled={busy || (status?.running ?? false)}
            >
              <PlayIcon className="size-4" /> 启动
            </Button>
            <Button
              size="sm"
              variant="outline"
              className="text-destructive hover:text-destructive"
              onClick={() => save(false)}
              disabled={busy || !(status?.running ?? false)}
            >
              <SquareIcon className="size-4" /> 停止
            </Button>
          </div>
        </div>

        <div className="text-muted-foreground text-xs">
          一个 mixed 端口（HTTP 正向代理 + SOCKS5 自动识别）。DIRECT 规则命中的请求直连，其余走下方选定的代理节点。
          {status?.running ? (
            <span className="text-muted-foreground">
              使用：<code className="font-mono">curl -x http://127.0.0.1:{port}</code> 或{" "}
              <code className="font-mono">--proxy socks5://127.0.0.1:{port}</code>
            </span>
          ) : null}
        </div>

        <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
          <div className="grid gap-1.5">
            <Label className="text-xs">监听端口</Label>
            <Input type="number" value={port} onChange={(e) => setPort(e.target.value)} min={1} max={65535} />
          </div>
          <div className="grid gap-1.5">
            <Label className="text-xs">出口节点</Label>
            <Select value={nodeID} onValueChange={setNodeID}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="0">动态挑健康节点</SelectItem>
                {healthy.map((p) => (
                  <SelectItem key={p.id} value={String(p.id)}>
                    {p.name} ({p.host}:{p.port})
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </div>

        <div className="grid gap-1.5">
          <Label className="text-xs">直连规则（每行一条：CIDR / 域名 / *.域名；其余流量走代理）</Label>
          <textarea
            className="flex min-h-24 w-full rounded-md border border-input bg-transparent px-3 py-2 text-sm shadow-xs placeholder:text-muted-foreground focus-visible:border-ring focus-visible:outline-hidden focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-50"
            placeholder={"10.0.0.0/8\n172.16.0.0/12\n192.168.0.0/16\n*.internal.example"}
            value={rulesText}
            onChange={(e) => setRulesText(e.target.value)}
          />
          <div className="text-muted-foreground text-xs">
            当前生效 {rulePreview.length} 条直连规则
            {healthy.length > 0 ? `，${healthy.length} 个健康节点可选` : "，暂无健康节点（先执行测活）"}。
          </div>
        </div>
      </CardContent>
    </Card>
  );
}

// ruleListToText 把 bridge 规则结构转成多行可编辑文本。
function ruleListToText(rules: ProxyBridgeRule[]): string {
  return rules
    .filter((r) => r.direct)
    .map((r) => {
      if (r.kind === "ip-cidr") return r.value;
      if (r.kind === "domain") return r.value;
      if (r.kind === "domain-suffix") return `*.${r.value}`;
      return r.value;
    })
    .join("\n");
}

// parseRulesText 把多行直连规则文本转成 bridge 规则结构。
function parseRulesText(text: string): ProxyBridgeRule[] {
  const out: ProxyBridgeRule[] = [];
  for (const line of text.split("\n")) {
    const v = line.trim();
    if (!v) continue;
    if (v.includes("/")) {
      out.push({ kind: "ip-cidr", value: v, direct: true });
    } else if (v.startsWith("*.")) {
      out.push({ kind: "domain-suffix", value: v.slice(2), direct: true });
    } else {
      out.push({ kind: "domain", value: v, direct: true });
    }
  }
  return out;
}

export default function ProxyPoolPage() {
  const [proxies, setProxies] = React.useState<ProxyItem[]>([]);
  const [stats, setStats] = React.useState<ProxyPoolStats | null>(null);
  const [sources, setSources] = React.useState<ProxySourceItem[]>([]);
  const [bridge, setBridge] = React.useState<ProxyBridgeStatus | null>(null);
  const [tested, setTested] = React.useState<string>("");
  const [testingAll, setTestingAll] = React.useState(false);

  const [checked, setChecked] = React.useState<Set<string>>(new Set());
  const [deleteAll, setDeleteAll] = React.useState(false);
  const [deleteOpen, setDeleteOpen] = React.useState(false);
  const [deleting, setDeleting] = React.useState(false);

  const toggle = (id: string) => {
    setChecked((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const load = React.useCallback(() => {
    api
      .proxies()
      .then((r) => {
        setProxies(r.proxies ?? []);
        setStats(r.stats ?? null);
      })
      .catch(() => {
        setProxies([]);
        setStats(null);
      });
    api
      .proxySources()
      .then((s) => setSources(s))
      .catch(() => setSources([]));
    api
      .proxyBridgeStatus()
      .then((st) => setBridge(st))
      .catch(() => setBridge(null));
  }, []);
  React.useEffect(() => {
    load();
    const i = setInterval(load, 8000);
    return () => clearInterval(i);
  }, [load]);

  const testOne = async (p: ProxyItem) => {
    setTested(`test:${p.id}`);
    try {
      const res = await api.proxyTest(p.id);
      if (res.ok) {
        toast.success(`${p.name} 连通，延迟 ${res.latency_ms}ms`);
      } else {
        toast.error(`${p.name} 测活失败：${res.error ?? "未知错误"}`);
      }
    } catch (e) {
      toast.error(`测活失败：${(e as Error).message}`);
    } finally {
      setTested("");
      load();
    }
  };

  const testAll = async () => {
    setTestingAll(true);
    try {
      const res = await api.proxyTestAll();
      toast.success(`测活完成：${res.ok}/${res.tested} 条连通`);
    } catch (e) {
      toast.error(`全量测活失败：${(e as Error).message}`);
    } finally {
      setTestingAll(false);
      load();
    }
  };

  // 行内启用/停用开关：只改 enabled，保留其余字段。
  const toggleEnable = async (p: ProxyItem) => {
    try {
      await api.saveProxy({
        id: Number(p.id),
        name: p.name,
        protocol: p.protocol,
        host: p.host,
        port: p.port,
        enabled: !p.enabled,
      });
      toast.success(p.enabled ? `已停用 ${p.name || p.host}` : `已启用 ${p.name || p.host}`);
      load();
    } catch (e) {
      toast.error(`操作失败：${(e as Error).message}`);
    }
  };

  const removeOne = async (p: ProxyItem) => {
    try {
      await api.deleteProxy(p.id);
      toast.success("已删除");
      load();
    } catch (e) {
      toast.error(`删除失败：${(e as Error).message}`);
    }
  };

  const confirmBatchDelete = async () => {
    setDeleting(true);
    try {
      const res = await api.deleteProxies(Array.from(checked), deleteAll);
      toast.success(`已删除 ${res.deleted} 条代理`);
      setChecked(new Set());
      setDeleteOpen(false);
      load();
    } catch (e) {
      toast.error(`删除失败：${(e as Error).message}`);
      setDeleteOpen(false);
    } finally {
      setDeleting(false);
    }
  };

  const refreshSource = async (id: string) => {
    try {
      await api.proxySourceRefresh(id);
      toast.success("已触发刷新");
      setTimeout(load, 1500);
    } catch (e) {
      toast.error(`刷新失败：${(e as Error).message}`);
    }
  };

  const statusBadge = (p: ProxyItem) => {
    if (p.last_check_at === null) return <Badge variant="outline">未测</Badge>;
    if (p.last_check_ok) {
      return (
        <Badge variant="secondary" className="text-emerald-600">
          健康
        </Badge>
      );
    }
    return (
      <Badge variant="outline" className="text-destructive">
        失败 ×{p.fail_count}
      </Badge>
    );
  };

  const sourceLabel = (src: string) => {
    if (src === "subscription") return "订阅";
    if (src === "import") return "导入";
    return "手动";
  };

  const latencyCell = (p: ProxyItem) => {
    if (tested === `test:${p.id}`) return <span className="text-muted-foreground text-xs">测活中…</span>;
    if (p.latency_ms > 0) return <span>{p.latency_ms}ms</span>;
    return <span className="text-muted-foreground">—</span>;
  };

  const fmt = (ts: string | null) =>
    ts ? new Date(ts).toLocaleString("zh-CN", { hour: "2-digit", minute: "2-digit" }) : "—";

  return (
    <div className="flex flex-1 flex-col gap-4">
      <div className="flex items-center justify-between gap-4">
        <div className="flex items-center gap-2">
          <ZapIcon className="size-5 text-muted-foreground" />
          <h1 className="font-semibold text-xl tracking-tight">代理池管理</h1>
          <Badge variant="secondary">{proxies.length} 节点</Badge>
          {checked.size > 0 && (
            <>
              <Button
                variant="destructive"
                size="sm"
                onClick={() => {
                  setDeleteAll(false);
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
        <div className="flex items-center gap-2">
          <Button size="sm" variant="outline" onClick={testAll} disabled={testingAll}>
            <RefreshCwIcon className={`size-4 ${testingAll ? "animate-spin" : ""}`} /> 全量测活
          </Button>
          <ImportDialog onSaved={load} />
          <SourceDialog onSaved={load} />
          <ProxyForm editing={null} onSaved={load} />
        </div>
      </div>

      {stats && (
        <div className="grid grid-cols-2 gap-3 md:grid-cols-4 xl:grid-cols-5">
          <StatCard label="总数" value={stats.total} />
          <StatCard label="启用" value={stats.enabled} />
          <StatCard label="健康" value={stats.healthy} sub="启用且最近测活成功" />
          <StatCard label="未测" value={stats.unchecked} />
          <StatCard label="平均延迟" value={`${stats.avg_latency_ms}ms`} sub="健康节点" />
        </div>
      )}

      <Card className="overflow-hidden py-0">
        <CardContent className="p-0">
          {proxies.length === 0 ? (
            <div className="py-12 text-center text-muted-foreground text-sm">
              暂无代理节点，点击「新增代理」或「批量导入」。
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead className="bg-muted/50 text-muted-foreground text-xs">
                  <tr className="text-left">
                    <th className="w-8 px-3 py-2">
                      <Checkbox
                        checked={proxies.length > 0 && proxies.every((p) => checked.has(String(p.id)))}
                        onCheckedChange={() => {
                          setChecked((prev) => {
                            const allSelected = proxies.length > 0 && proxies.every((p) => prev.has(String(p.id)));
                            const next = new Set(prev);
                            if (allSelected) proxies.forEach((p) => next.delete(String(p.id)));
                            else proxies.forEach((p) => next.add(String(p.id)));
                            return next;
                          });
                        }}
                        aria-label="全选"
                      />
                    </th>
                    <th className="px-3 py-2 font-medium">名称</th>
                    <th className="px-3 py-2 font-medium">协议</th>
                    <th className="px-3 py-2 font-medium">地址</th>
                    <th className="px-3 py-2 font-medium">区域</th>
                    <th className="px-3 py-2 font-medium">状态</th>
                    <th className="px-3 py-2 font-medium">延迟</th>
                    <th className="px-3 py-2 font-medium">来源</th>
                    <th className="px-3 py-2 text-right font-medium">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {proxies.map((p) => (
                    <tr key={p.id} className="border-t">
                      <td className="w-8 px-3 py-2">
                        <Checkbox
                          checked={checked.has(String(p.id))}
                          onCheckedChange={() => toggle(String(p.id))}
                          aria-label={`选择 ${p.name}`}
                        />
                      </td>
                      <td className="px-3 py-2">
                        <div className="flex items-center gap-2">
                          <span className="max-w-36 truncate font-medium">{p.name || p.host}</span>
                          {!p.enabled && <Badge variant="outline">停用</Badge>}
                        </div>
                      </td>
                      <td className="px-3 py-2">
                        <Badge variant="outline" className="uppercase">
                          {p.protocol}
                        </Badge>
                      </td>
                      <td className="px-3 py-2 font-mono text-xs">
                        {p.host}:{p.port}
                        {p.password_set ? (
                          <span className="ml-1 text-muted-foreground" title="已配置认证">
                            🔐
                          </span>
                        ) : null}
                      </td>
                      <td className="px-3 py-2">{p.region || "—"}</td>
                      <td className="px-3 py-2">{statusBadge(p)}</td>
                      <td className="px-3 py-2 tabular-nums">{latencyCell(p)}</td>
                      <td className="px-3 py-2 text-muted-foreground text-xs">{sourceLabel(p.source)}</td>
                      <td className="px-3 py-2 text-right">
                        <div className="flex items-center justify-end gap-1">
                          <Button
                            size="sm"
                            variant={p.enabled ? "outline" : "default"}
                            onClick={() => toggleEnable(p)}
                            title={p.enabled ? "停用该节点" : "启用该节点"}
                          >
                            {p.enabled ? "停用" : "启用"}
                          </Button>
                          <Button
                            size="sm"
                            variant="ghost"
                            onClick={() => testOne(p)}
                            disabled={tested === `test:${p.id}`}
                          >
                            <ZapIcon className="size-3.5" /> 测活
                          </Button>
                          <ProxyForm editing={p} onSaved={load} />
                          <Button size="icon" variant="ghost" aria-label="删除" onClick={() => removeOne(p)}>
                            <Trash2Icon className="size-4 text-destructive" />
                          </Button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>

      <BridgePanel status={bridge} proxies={proxies} onChanged={load} />

      <div>
        <h2 className="mb-2 font-medium text-muted-foreground text-sm">订阅源</h2>
        <Card className="overflow-hidden py-0">
          <CardContent className="p-0">
            {sources.length === 0 ? (
              <div className="py-6 text-center text-muted-foreground text-sm">暂无订阅源。</div>
            ) : (
              <div className="divide-y">
                {sources.map((s) => (
                  <div key={s.id} className="flex items-center justify-between gap-3 px-4 py-2.5">
                    <div className="min-w-0">
                      <div className="flex items-center gap-2">
                        <span className="truncate font-medium">{s.name}</span>
                        {s.enabled ? (
                          <Badge variant="secondary" className="text-emerald-600">
                            启用
                          </Badge>
                        ) : (
                          <Badge variant="outline">停用</Badge>
                        )}
                        {s.last_error ? (
                          <Badge variant="outline" className="text-destructive">
                            最近检查失败
                          </Badge>
                        ) : null}
                      </div>
                      <code className="mt-0.5 block truncate font-mono text-muted-foreground text-xs">
                        {s.url || "—"}
                      </code>
                      {s.last_error ? <div className="mt-0.5 text-destructive text-xs">{s.last_error}</div> : null}
                    </div>
                    <div className="flex shrink-0 items-center gap-1">
                      <span className="text-muted-foreground text-xs">上次检查 {fmt(s.last_checked_at)}</span>
                      <Button size="sm" variant="ghost" onClick={() => refreshSource(s.id)}>
                        <RefreshCwIcon className="size-3.5" /> 刷新
                      </Button>
                      <Button
                        size="icon"
                        variant="ghost"
                        aria-label="删除订阅源"
                        onClick={() =>
                          api
                            .deleteProxySource(s.id)
                            .then(() => {
                              toast.success("已删除");
                              load();
                            })
                            .catch((e) => toast.error((e as Error).message))
                        }
                      >
                        <Trash2Icon className="size-4 text-destructive" />
                      </Button>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认删除</AlertDialogTitle>
            <AlertDialogDescription>
              {deleteAll ? (
                <>将清空全部代理节点，此操作不可撤销。</>
              ) : (
                <>
                  将删除 <span className="font-semibold tabular-nums">{checked.size}</span> 条代理，此操作不可撤销。
                </>
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleting}>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={(e) => {
                e.preventDefault();
                void confirmBatchDelete();
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
