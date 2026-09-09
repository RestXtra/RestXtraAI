"use client";

import * as React from "react";

import { Loader2Icon, PencilIcon, PlusIcon, ShieldAlertIcon, TerminalIcon, Trash2Icon } from "lucide-react";
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
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { api } from "@/lib/api";
import type { ConnAction, Connection } from "@/lib/types";

const KIND_LABELS: Record<string, string> = {
  webshell: "WebShell",
  ssh: "SSH",
  rdp: "RDP",
  telnet: "Telnet",
};

function KindBadge({ kind }: { kind: string }) {
  return (
    <Badge variant="outline" className="uppercase">
      {KIND_LABELS[kind] ?? kind}
    </Badge>
  );
}

// ---- 表单（按 kind 动态渲染）----

const DEFAULT_PORT: Record<string, number> = { ssh: 22, rdp: 3389, telnet: 23, webshell: 0 };

function emptyForm(kind: string) {
  return {
    name: "",
    kind,
    host: "",
    port: DEFAULT_PORT[kind] ?? 0,
    username: "",
    secret: "",
    note: "",
    wsType: "php",
    wsHeaders: "{}",
    privateKey: "",
    domain: "",
  };
}

function ConnForm({
  open,
  onOpenChange,
  kind,
  initial,
  onSaved,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  kind: string;
  initial: Connection | null;
  onSaved: () => void;
}) {
  const [f, setF] = React.useState(emptyForm(kind));
  React.useEffect(() => {
    if (!open) return;
    if (initial) {
      const cfg = (initial.config ?? {}) as Record<string, any>;
      setF({
        name: initial.name,
        kind: initial.kind,
        host: initial.host,
        port: initial.port,
        username: initial.username,
        secret: initial.secret ?? "",
        note: initial.note ?? "",
        wsType: (cfg.type as string) ?? "php",
        wsHeaders: (cfg.headers as string) ?? "{}",
        privateKey: (cfg.private_key as string) ?? "",
        domain: (cfg.domain as string) ?? "",
      });
    } else {
      setF(emptyForm(kind));
    }
  }, [open, initial, kind]);
  const set = (k: string, v: unknown) => setF((p) => ({ ...p, [k]: v }));

  async function save() {
    if (!f.name.trim()) {
      toast.error("名称必填");
      return;
    }
    const config: Record<string, any> = {};
    if (f.kind === "webshell") {
      config.type = f.wsType;
      config.headers = f.wsHeaders || "{}";
      if (!f.host.trim()) {
        toast.error("URL 必填");
        return;
      }
    } else {
      if (!f.host.trim()) {
        toast.error("主机必填");
        return;
      }
      if (f.kind === "ssh" && f.privateKey.trim()) config.private_key = f.privateKey;
      if (f.kind === "rdp" && f.domain.trim()) config.domain = f.domain;
    }
    try {
      await api.saveConnection({
        id: initial?.id,
        name: f.name.trim(),
        kind: f.kind,
        host: f.host.trim(),
        port: f.port,
        username: f.username.trim(),
        secret: f.secret,
        note: f.note,
        config,
      });
      toast.success("已保存");
      onOpenChange(false);
      onSaved();
    } catch (e) {
      toast.error(`保存失败：${(e as Error).message}`);
    }
  }

  const isWs = f.kind === "webshell";
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{initial ? "编辑连接" : `添加 ${KIND_LABELS[f.kind] ?? f.kind} 连接`}</DialogTitle>
          <DialogDescription>
            {initial
              ? "修改连接配置。密码/私钥留空表示不修改已存密钥。"
              : "登记受管连接，供 agent（应急响应 / 后渗透）驱动执行命令。"}
          </DialogDescription>
        </DialogHeader>
        <div className="grid gap-3 py-2">
          <div className="grid grid-cols-2 gap-2">
            <div className="grid gap-2">
              <Label htmlFor="conn-name">名称</Label>
              <Input
                id="conn-name"
                placeholder="例如：核心DB-01"
                value={f.name}
                onChange={(e) => set("name", e.target.value)}
              />
            </div>
            <div className="grid gap-2">
              <Label>类型</Label>
              <Select value={f.kind} onValueChange={(v) => set("kind", v)} disabled={!!initial}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {Object.entries(KIND_LABELS).map(([k, v]) => (
                    <SelectItem key={k} value={k}>
                      {v}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>

          {isWs ? (
            <div className="grid gap-2">
              <div className="grid grid-cols-3 gap-2">
                <div className="col-span-2 grid gap-2">
                  <Label htmlFor="conn-url">URL</Label>
                  <Input
                    id="conn-url"
                    className="font-mono"
                    placeholder="https://target/shell.php"
                    value={f.host}
                    onChange={(e) => set("host", e.target.value)}
                  />
                </div>
                <div className="grid gap-2">
                  <Label>Shell 类型</Label>
                  <Select value={f.wsType} onValueChange={(v) => set("wsType", v)}>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="php">PHP</SelectItem>
                      <SelectItem value="jsp">JSP</SelectItem>
                      <SelectItem value="aspx">ASPX</SelectItem>
                      <SelectItem value="asp">ASP</SelectItem>
                      <SelectItem value="generic">通用(JSON)</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
              </div>
              <div className="grid gap-2">
                <Label htmlFor="conn-ws-headers">自定义头（JSON，可选）</Label>
                <Input
                  id="conn-ws-headers"
                  className="font-mono"
                  placeholder='{"X-Api-Key":"..."}'
                  value={f.wsHeaders}
                  onChange={(e) => set("wsHeaders", e.target.value)}
                />
              </div>
            </div>
          ) : (
            <div className="grid grid-cols-3 gap-2">
              <div className="col-span-2 grid gap-2">
                <Label htmlFor="conn-host">主机 / IP</Label>
                <Input
                  id="conn-host"
                  placeholder={f.kind === "ssh" ? "10.0.0.1" : "192.168.1.10"}
                  value={f.host}
                  onChange={(e) => set("host", e.target.value)}
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="conn-port">端口</Label>
                <Input
                  id="conn-port"
                  type="number"
                  value={f.port || ""}
                  onChange={(e) => set("port", Number(e.target.value) || 0)}
                />
              </div>
            </div>
          )}

          {!isWs && (
            <div className="grid grid-cols-2 gap-2">
              <div className="grid gap-2">
                <Label htmlFor="conn-user">用户名</Label>
                <Input
                  id="conn-user"
                  placeholder={f.kind === "ssh" ? "root" : "administrator"}
                  value={f.username}
                  onChange={(e) => set("username", e.target.value)}
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="conn-pwd">密码</Label>
                <Input
                  id="conn-pwd"
                  type="password"
                  placeholder={initial ? "留空不修改" : "连接密码"}
                  value={f.secret}
                  onChange={(e) => set("secret", e.target.value)}
                />
              </div>
            </div>
          )}
          {f.kind === "ssh" && (
            <div className="grid gap-2">
              <Label htmlFor="conn-key">SSH 私钥（可选，与密码二选一或并存）</Label>
              <Textarea
                id="conn-key"
                className="min-h-[80px] font-mono text-xs"
                placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                value={f.privateKey}
                onChange={(e) => set("privateKey", e.target.value)}
              />
            </div>
          )}
          {f.kind === "rdp" && (
            <div className="grid gap-2">
              <Label htmlFor="conn-domain">域（可选）</Label>
              <Input
                id="conn-domain"
                placeholder="CORP"
                value={f.domain}
                onChange={(e) => set("domain", e.target.value)}
              />
            </div>
          )}
          <div className="grid gap-2">
            <Label htmlFor="conn-note">备注（可选）</Label>
            <Input id="conn-note" value={f.note} onChange={(e) => set("note", e.target.value)} />
          </div>
        </div>
        <DialogFooter>
          <DialogClose asChild>
            <Button variant="outline">取消</Button>
          </DialogClose>
          <Button onClick={save}>保存</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ---- 待审批面板 ----

function ApprovalsPanel({ onDone }: { onDone: () => void }) {
  const [items, setItems] = React.useState<ConnAction[]>([]);
  const [busy, setBusy] = React.useState<Record<string, string>>({});

  const load = React.useCallback(() => {
    api
      .connApprovals()
      .then(setItems)
      .catch(() => setItems([]));
  }, []);
  React.useEffect(() => {
    load();
  }, [load]);

  async function decide(a: ConnAction, action: "approve" | "reject") {
    setBusy((b) => ({ ...b, [a.id]: action }));
    try {
      const r = await api.connApprovalDecide(a.id, action);
      toast.success(
        action === "approve" ? `已批准并执行：${r.state ?? "executed"}${r.verified ? "（已回查验证）" : ""}` : "已拒绝",
      );
      load();
      onDone();
    } catch (e) {
      toast.error(`操作失败：${(e as Error).message}`);
    } finally {
      setBusy((b) => {
        const n = { ...b };
        delete n[a.id];
        return n;
      });
    }
  }

  if (items.length === 0) return null;
  return (
    <Card className="border-amber-500/40">
      <CardContent className="grid gap-2">
        <div className="flex items-center gap-2 text-amber-600">
          <ShieldAlertIcon className="size-4" />
          <span className="font-medium text-sm">待审批危险动作（{items.length}）</span>
        </div>
        {items.map((a) => (
          <div key={a.id} className="grid gap-1.5 border-t pt-2">
            <div className="flex flex-wrap items-center gap-2 text-xs">
              <span className="font-medium">
                #{a.id} {a.conn_name ?? `连接#${a.connection_id}`}
              </span>
              <KindBadge kind={a.conn_kind ?? "?"} />
              <Badge variant="secondary">
                {a.kind}
                {a.action ? ` · ${a.action}` : ""}
              </Badge>
              <span className="text-muted-foreground">发起人：{a.requested_by || "-"}</span>
            </div>
            <code className="block break-all rounded bg-muted px-2 py-1 font-mono text-xs">{a.command}</code>
            {a.rationale && <p className="text-muted-foreground text-xs">理由：{a.rationale}</p>}
            <div className="flex gap-2">
              <Button size="sm" disabled={!!busy[a.id]} onClick={() => decide(a, "approve")}>
                {busy[a.id] === "approve" ? <Loader2Icon className="animate-spin" /> : "批准并执行"}
              </Button>
              <Button size="sm" variant="outline" disabled={!!busy[a.id]} onClick={() => decide(a, "reject")}>
                {busy[a.id] === "reject" ? <Loader2Icon className="animate-spin" /> : "拒绝"}
              </Button>
            </div>
          </div>
        ))}
      </CardContent>
    </Card>
  );
}

// ---- 主页面 ----

type TestResult = { ok: boolean; snippet?: string; error?: string; output?: string };

function TestStatus({
  c,
  busy,
  result,
}: {
  c: Connection;
  busy: Record<string, boolean>;
  result: Record<string, TestResult>;
}) {
  if (busy[c.id]) return <Loader2Icon className="animate-spin" />;
  const r = result[c.id];
  if (!r) return <span className="text-muted-foreground">未测试</span>;
  if (r.ok) return <span className="text-emerald-600">连通 ✓ {r.snippet || ""}</span>;
  return <span className="text-destructive">{r.error || r.snippet || "失败"}</span>;
}

export default function ConnectionPage() {
  const [kind, setKind] = React.useState("all");
  const [conns, setConns] = React.useState<Connection[]>([]);
  const [busy, setBusy] = React.useState<Record<string, boolean>>({});
  const [result, setResult] = React.useState<
    Record<string, { ok: boolean; snippet?: string; error?: string; output?: string }>
  >({});
  const [formOpen, setFormOpen] = React.useState(false);
  const [editing, setEditing] = React.useState<Connection | null>(null);

  const [checked, setChecked] = React.useState<Set<string>>(new Set());
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

  const load = React.useCallback(() => {
    api
      .connections(kind === "all" ? "" : kind)
      .then(setConns)
      .catch(() => setConns([]));
  }, [kind]);
  React.useEffect(() => {
    load();
  }, [load]);

  const confirmBatchDelete = async () => {
    setDeleting(true);
    try {
      const res = await api.deleteConnections(Array.from(checked), deleteAll);
      toast.success(`已删除 ${res.deleted} 条连接`);
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

  const test = async (c: Connection) => {
    setBusy((b) => ({ ...b, [c.id]: true }));
    try {
      const r = await api.connectionTest(c);
      setResult((m) => ({ ...m, [c.id]: r }));
    } catch (e) {
      setResult((m) => ({ ...m, [c.id]: { ok: false, error: (e as Error).message } }));
    } finally {
      setBusy((b) => ({ ...b, [c.id]: false }));
    }
  };

  const remove = async (c: Connection) => {
    try {
      await api.deleteConnection(c.id);
      toast.success("已删除");
      load();
    } catch (e) {
      toast.error(`删除失败：${(e as Error).message}`);
    }
  };

  return (
    <div className="flex flex-1 flex-col gap-4">
      <div className="flex items-center justify-between gap-4">
        <div className="flex items-center gap-2">
          <TerminalIcon className="size-5 text-muted-foreground" />
          <h1 className="font-semibold text-xl tracking-tight">连接管理</h1>
          <Badge variant="secondary">{conns.length}</Badge>
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
        <Button
          size="sm"
          onClick={() => {
            setEditing(null);
            setFormOpen(true);
          }}
        >
          <PlusIcon /> 添加连接
        </Button>
      </div>

      <p className="text-muted-foreground text-sm">
        登记受管连接（SSH / WebShell / RDP / Telnet），供应急响应与后渗透 agent 驱动执行命令；危险动作走人工审批。
      </p>

      <ApprovalsPanel onDone={load} />

      <Tabs value={kind} onValueChange={setKind}>
        <TabsList>
          <TabsTrigger value="all">全部</TabsTrigger>
          {Object.entries(KIND_LABELS).map(([k, v]) => (
            <TabsTrigger key={k} value={k}>
              {v}
            </TabsTrigger>
          ))}
        </TabsList>
      </Tabs>

      <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
        {conns.length === 0 && (
          <Card className="md:col-span-2 xl:col-span-3">
            <CardContent className="flex items-center justify-center py-14 text-muted-foreground text-sm">
              暂无连接，点击「添加连接」。
            </CardContent>
          </Card>
        )}
        {conns.map((c) => {
          return (
            <Card key={c.id}>
              <CardContent className="grid gap-2">
                <div className="flex items-start justify-between gap-2">
                  <div className="flex items-start gap-2">
                    <Checkbox
                      className="mt-0.5"
                      checked={checked.has(String(c.id))}
                      onCheckedChange={() => toggleCheck(String(c.id))}
                      aria-label={`选择 ${c.name}`}
                    />
                    <div className="min-w-0">
                      <div className="flex items-center gap-2">
                        <span className="truncate font-medium">{c.name}</span>
                        <KindBadge kind={c.kind} />
                        {c.enabled && <Switch size="sm" checked disabled />}
                      </div>
                      <code className="mt-0.5 block truncate font-mono text-muted-foreground text-xs">
                        {c.kind === "webshell"
                          ? c.host
                          : `${c.host}${c.port ? `:${c.port}` : ""}${c.username ? ` @${c.username}` : ""}`}
                      </code>
                    </div>
                  </div>
                  <div className="flex shrink-0 gap-1">
                    <Button
                      size="icon"
                      variant="ghost"
                      aria-label="编辑"
                      onClick={() => {
                        setEditing(c);
                        setFormOpen(true);
                      }}
                    >
                      <PencilIcon className="size-4" />
                    </Button>
                    <Button size="icon" variant="ghost" aria-label="删除" onClick={() => remove(c)}>
                      <Trash2Icon className="text-destructive" />
                    </Button>
                  </div>
                </div>
                <div className="flex items-center justify-between gap-2 border-t pt-2">
                  <span className="min-w-0 flex-1 truncate text-xs">
                    <TestStatus c={c} busy={busy} result={result} />
                  </span>
                  <Button size="sm" variant="outline" disabled={busy[c.id]} onClick={() => test(c)}>
                    {busy[c.id] ? <Loader2Icon className="animate-spin" /> : "测试连接"}
                  </Button>
                </div>
              </CardContent>
            </Card>
          );
        })}
      </div>

      <ConnForm
        open={formOpen}
        onOpenChange={setFormOpen}
        kind={editing?.kind ?? "ssh"}
        initial={editing}
        onSaved={load}
      />

      <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认删除连接</AlertDialogTitle>
            <AlertDialogDescription>
              {deleteAll ? (
                <>将清空全部连接记录，此操作不可撤销。</>
              ) : (
                <>
                  将永久删除 <span className="font-semibold tabular-nums">{checked.size}</span>{" "}
                  条连接记录，此操作不可撤销。
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
