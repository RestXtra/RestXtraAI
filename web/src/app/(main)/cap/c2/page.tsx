"use client";

import * as React from "react";

import {
  BoxesIcon,
  CopyIcon,
  CrosshairIcon,
  ListChecksIcon,
  PlugZapIcon,
  PlusIcon,
  RadioTowerIcon,
  ScanSearchIcon,
  SendIcon,
  ServerIcon,
  Settings2Icon,
  Trash2Icon,
  WebhookIcon,
  ZapIcon,
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { api } from "@/lib/api";
import type {
  C2AutoTask,
  C2Generated,
  C2Listener,
  C2Plugin,
  C2PostexModule,
  C2Profile,
  C2Session,
  C2Task,
  C2Tunnel,
} from "@/lib/types";

const fmtTime = (ts?: string) => (ts ? new Date(ts).toLocaleString("zh-CN", { hour12: false }) : "—");

function relTime(ts?: string) {
  if (!ts) return "—";
  const diff = Date.now() - new Date(ts).getTime();
  if (diff < 60_000) return "刚刚";
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)} 分钟前`;
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)} 小时前`;
  return `${Math.floor(diff / 86_400_000)} 天前`;
}

function statusBadge(status?: string) {
  switch (status) {
    case "active":
      return (
        <Badge variant="secondary" className="text-emerald-600">
          活跃
        </Badge>
      );
    case "lost":
      return (
        <Badge variant="outline" className="text-amber-600">
          丢失
        </Badge>
      );
    case "closed":
      return <Badge variant="outline">已关闭</Badge>;
    case "running":
      return (
        <Badge variant="secondary" className="text-sky-600">
          运行中
        </Badge>
      );
    case "stopped":
      return <Badge variant="outline">已停止</Badge>;
    case "error":
      return <Badge variant="destructive">错误</Badge>;
    default:
      return <Badge variant="outline">{status ?? "—"}</Badge>;
  }
}

function copy(text: string, label = "已复制") {
  navigator.clipboard?.writeText(text).then(() => toast.success(label));
}

// ================= 客户端管理 =================

function ClientsTab({ sessions, onChanged }: { sessions: C2Session[]; onChanged: () => void }) {
  const [selected, setSelected] = React.useState<string>("");
  const [checked, setChecked] = React.useState<Set<string>>(new Set());
  const [command, setCommand] = React.useState("");
  const [tasks, setTasks] = React.useState<C2Task[]>([]);
  const [noteTarget, setNoteTarget] = React.useState<C2Session | null>(null);
  const [noteText, setNoteText] = React.useState("");
  const [analyze, setAnalyze] = React.useState<{
    session: C2Session;
    summary: {
      host: string;
      ip: string;
      remote_ip: string;
      os: string;
      user: string;
      process: string;
      connection: string;
      status: string;
      first_seen: string;
      last_seen: string;
      task_stats: { completed: number; failed: number; pending: number };
    };
    tasks: C2Task[];
  } | null>(null);
  const [analyzeLoading, setAnalyzeLoading] = React.useState(false);

  const openAnalyze = async (s: C2Session) => {
    setAnalyzeLoading(true);
    try {
      const r = await api.c2Analyze(s.session_id);
      setAnalyze(r);
    } catch (e) {
      toast.error(`分析失败：${(e as Error).message}`);
    } finally {
      setAnalyzeLoading(false);
    }
  };

  const loadTasks = React.useCallback(() => {
    if (!selected) {
      setTasks([]);
      return;
    }
    api
      .c2Tasks(selected)
      .then((r) => setTasks(r.tasks ?? []))
      .catch(() => setTasks([]));
  }, [selected]);
  React.useEffect(() => {
    loadTasks();
    const i = setInterval(loadTasks, 3000);
    return () => clearInterval(i);
  }, [loadTasks]);

  const toggle = (id: string) =>
    setChecked((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  const run = async () => {
    if (!selected) return toast.error("请先选择会话");
    if (!command.trim()) return toast.error("命令必填");
    try {
      await api.c2CreateTask(selected, { command: command.trim() });
      toast.success("任务已入队");
      setCommand("");
      loadTasks();
    } catch (e) {
      toast.error(`下发失败：${(e as Error).message}`);
    }
  };

  const setStatus = async (s: C2Session, status: string) => {
    try {
      await api.c2SetStatus(s.session_id, status);
      onChanged();
    } catch (e) {
      toast.error(`操作失败：${(e as Error).message}`);
    }
  };

  return (
    <div className="grid gap-3">
      <Card className="overflow-hidden py-0">
        <CardContent className="p-0">
          {sessions.length === 0 ? (
            <div className="py-12 text-center text-muted-foreground text-sm">
              暂无客户端。启动监听器后，beacon 会通过 POST /api/c2/ingest 自动上报会话。
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full whitespace-nowrap text-sm">
                <thead className="bg-muted/50 text-muted-foreground text-xs">
                  <tr className="text-left">
                    <th className="w-8 px-3 py-2">
                      <Checkbox
                        checked={sessions.length > 0 && sessions.every((s) => checked.has(String(s.id)))}
                        onCheckedChange={() =>
                          setChecked((prev) => {
                            const all = sessions.every((s) => prev.has(String(s.id)));
                            const next = new Set(prev);
                            sessions.forEach((s) => (all ? next.delete(String(s.id)) : next.add(String(s.id))));
                            return next;
                          })
                        }
                        aria-label="全选"
                      />
                    </th>
                    <th className="px-3 py-2 font-medium">ID</th>
                    <th className="px-3 py-2 font-medium">备注</th>
                    <th className="px-3 py-2 font-medium">连接方式</th>
                    <th className="px-3 py-2 font-medium">外网IP</th>
                    <th className="px-3 py-2 font-medium">归属地</th>
                    <th className="px-3 py-2 font-medium">内网IP</th>
                    <th className="px-3 py-2 font-medium">用户</th>
                    <th className="px-3 py-2 font-medium">主机名</th>
                    <th className="px-3 py-2 font-medium">系统类型</th>
                    <th className="px-3 py-2 font-medium">进程名</th>
                    <th className="px-3 py-2 font-medium">状态</th>
                    <th className="px-3 py-2 font-medium">心跳</th>
                    <th className="px-3 py-2 text-right font-medium">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {sessions.map((s) => (
                    <tr key={s.id} className="border-t hover:bg-muted/30">
                      <td className="w-8 px-3 py-2">
                        <Checkbox
                          checked={checked.has(String(s.id))}
                          onCheckedChange={() => toggle(String(s.id))}
                          aria-label={`选择 ${s.session_id}`}
                        />
                      </td>
                      <td className="px-3 py-2 font-mono text-xs">{s.session_id}</td>
                      <td className="px-3 py-2 text-muted-foreground text-xs">{s.note || "—"}</td>
                      <td className="px-3 py-2">
                        {s.connection ? (
                          <Badge variant="outline" className="uppercase">
                            {s.connection}
                          </Badge>
                        ) : (
                          <span className="text-muted-foreground">—</span>
                        )}
                      </td>
                      <td className="px-3 py-2 font-mono text-xs">{s.remote_ip || "—"}</td>
                      <td className="px-3 py-2 text-xs">{s.location || "—"}</td>
                      <td className="px-3 py-2 font-mono text-xs">{s.host || "—"}</td>
                      <td className="px-3 py-2 text-xs">{s.username || "—"}</td>
                      <td className="px-3 py-2 text-xs">{s.hostname || "—"}</td>
                      <td className="px-3 py-2 text-xs">{s.os || s.arch ? `${s.os || "?"}/${s.arch || "?"}` : "—"}</td>
                      <td className="px-3 py-2 text-xs">{s.process_name || "—"}</td>
                      <td className="px-3 py-2">{statusBadge(s.status)}</td>
                      <td className="px-3 py-2 text-muted-foreground text-xs" title={fmtTime(s.last_seen)}>
                        {relTime(s.last_seen)}
                      </td>
                      <td className="px-3 py-2 text-right">
                        <div className="flex items-center justify-end gap-1">
                          <Button
                            size="sm"
                            variant={selected === s.session_id ? "default" : "outline"}
                            onClick={() => setSelected(s.session_id)}
                          >
                            <ListChecksIcon className="size-3.5" /> 任务
                          </Button>
                          <Button size="sm" variant="ghost" onClick={() => openAnalyze(s)}>
                            <ScanSearchIcon className="size-3.5" /> 分析
                          </Button>
                          <Button
                            size="sm"
                            variant="ghost"
                            onClick={() => {
                              setNoteTarget(s);
                              setNoteText(s.note ?? "");
                            }}
                          >
                            备注
                          </Button>
                          {s.status === "active" ? (
                            <Button
                              size="sm"
                              variant="ghost"
                              className="text-amber-600"
                              onClick={() => setStatus(s, "lost")}
                            >
                              标记丢失
                            </Button>
                          ) : (
                            <Button
                              size="sm"
                              variant="ghost"
                              className="text-emerald-600"
                              onClick={() => setStatus(s, "active")}
                            >
                              恢复
                            </Button>
                          )}
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

      <Card>
        <CardContent className="grid gap-3">
          <div className="flex items-center gap-2">
            <Label className="whitespace-nowrap">下发命令</Label>
            <Select value={selected} onValueChange={setSelected}>
              <SelectTrigger className="w-56">
                <SelectValue placeholder="选择会话" />
              </SelectTrigger>
              <SelectContent>
                {sessions.map((s) => (
                  <SelectItem key={s.id} value={s.session_id}>
                    {s.session_id}
                    {s.hostname ? ` (${s.hostname})` : ""}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Input
              placeholder="例如：shell ipconfig"
              value={command}
              onChange={(e) => setCommand(e.target.value)}
              className="flex-1 font-mono"
            />
            <Button onClick={run}>
              <SendIcon className="size-3.5" /> 下发
            </Button>
          </div>
          {selected && (
            <div className="overflow-x-auto rounded border">
              <table className="w-full text-sm">
                <thead className="bg-muted/50 text-muted-foreground text-xs">
                  <tr className="text-left">
                    <th className="px-3 py-2 font-medium">ID</th>
                    <th className="px-3 py-2 font-medium">命令</th>
                    <th className="px-3 py-2 font-medium">状态</th>
                    <th className="px-3 py-2 font-medium">结果</th>
                  </tr>
                </thead>
                <tbody>
                  {tasks.length === 0 ? (
                    <tr>
                      <td colSpan={4} className="px-3 py-6 text-center text-muted-foreground text-sm">
                        暂无任务
                      </td>
                    </tr>
                  ) : (
                    tasks.map((t) => (
                      <tr key={t.id} className="border-t align-top">
                        <td className="px-3 py-2 font-mono text-xs">{t.id}</td>
                        <td className="px-3 py-2 font-mono text-xs">{t.command || "—"}</td>
                        <td className="px-3 py-2">
                          {t.state === "queued" && (
                            <Badge variant="outline" className="text-sky-600">
                              排队
                            </Badge>
                          )}
                          {t.state === "sent" && (
                            <Badge variant="secondary" className="text-amber-600">
                              已发送
                            </Badge>
                          )}
                          {t.state === "completed" && (
                            <Badge variant="secondary" className="text-emerald-600">
                              完成
                            </Badge>
                          )}
                          {t.state === "failed" && <Badge variant="destructive">失败</Badge>}
                        </td>
                        <td className="max-w-96 px-3 py-2">
                          {t.response && Object.keys(t.response).length > 0 ? (
                            <pre className="max-h-24 overflow-auto rounded bg-muted/50 p-1 font-mono text-[11px]">
                              {JSON.stringify(t.response, null, 1)}
                            </pre>
                          ) : (
                            <span className="text-muted-foreground text-xs">—</span>
                          )}
                        </td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>

      <Dialog
        open={noteTarget !== null}
        onOpenChange={(o) => {
          if (!o) setNoteTarget(null);
        }}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>编辑备注</DialogTitle>
            <DialogDescription>会话 {noteTarget?.session_id}</DialogDescription>
          </DialogHeader>
          <div className="grid gap-2 py-2">
            <Label htmlFor="sess-note">备注</Label>
            <Textarea id="sess-note" rows={3} value={noteText} onChange={(e) => setNoteText(e.target.value)} />
          </div>
          <DialogFooter>
            <DialogClose asChild>
              <Button variant="outline">取消</Button>
            </DialogClose>
            <Button
              onClick={async () => {
                if (!noteTarget) return;
                try {
                  await api.c2SetNote(noteTarget.session_id, noteText.trim());
                  toast.success("已保存备注");
                  setNoteTarget(null);
                  onChanged();
                } catch (e) {
                  toast.error(`保存失败：${(e as Error).message}`);
                }
              }}
            >
              保存
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={analyze !== null}
        onOpenChange={(o) => {
          if (!o) setAnalyze(null);
        }}
      >
        <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>会话分析</DialogTitle>
            <DialogDescription>会话 {analyze?.session.session_id} 的富化信息与最近任务。</DialogDescription>
          </DialogHeader>
          {analyzeLoading ? <div className="py-10 text-center text-muted-foreground text-sm">分析中…</div> : null}
          {!analyzeLoading && analyze ? (
            <div className="grid gap-3">
              <div className="grid grid-cols-2 gap-x-4 gap-y-1 rounded border p-3 text-sm">
                <span className="text-muted-foreground">主机名</span>
                <span className="font-mono">{analyze.summary.host || "—"}</span>
                <span className="text-muted-foreground">内网IP</span>
                <span className="font-mono">{analyze.summary.ip || "—"}</span>
                <span className="text-muted-foreground">外网IP</span>
                <span className="font-mono">{analyze.summary.remote_ip || "—"}</span>
                <span className="text-muted-foreground">系统</span>
                <span className="font-mono">{analyze.summary.os || "—"}</span>
                <span className="text-muted-foreground">用户</span>
                <span className="font-mono">{analyze.summary.user || "—"}</span>
                <span className="text-muted-foreground">进程</span>
                <span className="truncate font-mono">{analyze.summary.process || "—"}</span>
                <span className="text-muted-foreground">连接</span>
                <span className="font-mono uppercase">{analyze.summary.connection || "—"}</span>
                <span className="text-muted-foreground">状态</span>
                <span>{statusBadge(analyze.summary.status)}</span>
                <span className="text-muted-foreground">首次上线</span>
                <span className="font-mono text-xs">{fmtTime(analyze.summary.first_seen)}</span>
                <span className="text-muted-foreground">最后心跳</span>
                <span className="font-mono text-xs">{fmtTime(analyze.summary.last_seen)}</span>
              </div>
              <div className="flex items-center gap-2 text-sm">
                <Badge variant="secondary">任务统计</Badge>
                <span className="text-emerald-600">完成 {analyze.summary.task_stats.completed}</span>
                <span className="text-destructive">失败 {analyze.summary.task_stats.failed}</span>
                <span className="text-sky-600">进行中 {analyze.summary.task_stats.pending}</span>
              </div>
              {analyze.tasks.length > 0 && (
                <div className="max-h-48 overflow-auto rounded border">
                  <table className="w-full text-xs">
                    <thead className="bg-muted/50 text-muted-foreground">
                      <tr className="text-left">
                        <th className="px-2 py-1">命令</th>
                        <th className="px-2 py-1">状态</th>
                        <th className="px-2 py-1">结果</th>
                      </tr>
                    </thead>
                    <tbody>
                      {analyze.tasks.map((t) => (
                        <tr key={t.id} className="border-t">
                          <td className="px-2 py-1 font-mono">{t.command || "—"}</td>
                          <td className="px-2 py-1">{t.state}</td>
                          <td className="max-w-48 truncate px-2 py-1 font-mono text-[10px]">
                            {t.response && Object.keys(t.response).length ? String(t.response.output ?? "") : "—"}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </div>
          ) : null}
          <DialogFooter>
            <DialogClose asChild>
              <Button variant="outline">关闭</Button>
            </DialogClose>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

// ================= 监听管理 =================

function ListenerForm({
  profiles,
  onSaved,
  initial,
}: {
  profiles: C2Profile[];
  onSaved: () => void;
  initial?: C2Listener | null;
}) {
  const [open, setOpen] = React.useState(false);
  const [name, setName] = React.useState("");
  const [protocol, setProtocol] = React.useState("http");
  const [host, setHost] = React.useState("0.0.0.0");
  const [port, setPort] = React.useState("8080");
  const [profileID, setProfileID] = React.useState<string>("");

  // disguise / firewall JSON (decoy + basic auth), edited as text
  const [disguise, setDisguise] = React.useState("{}");
  const [firewall, setFirewall] = React.useState("{}");
  const [disguiseErr, setDisguiseErr] = React.useState("");
  const [firewallErr, setFirewallErr] = React.useState("");

  const openFor = (l?: C2Listener | null) => {
    setName(l?.name ?? "");
    setProtocol(l?.protocol ?? "http");
    setHost(l?.host ?? "0.0.0.0");
    setPort(l?.port ? String(l.port) : "8080");
    setProfileID(l?.profile_id ? String(l.profile_id) : "");
    setDisguise(
      JSON.stringify(
        l?.disguise && Object.keys(l.disguise).length
          ? l.disguise
          : { decoy_type: "nginx_404", status_code: 404, server_header: "nginx/1.24.0" },
        null,
        2,
      ),
    );
    setFirewall(
      JSON.stringify(
        l?.firewall && Object.keys(l.firewall).length
          ? l.firewall
          : { basic_auth_enabled: false, basic_auth_user: "", basic_auth_pass: "", acl_rules: [] },
        null,
        2,
      ),
    );
    setDisguiseErr("");
    setFirewallErr("");
    setOpen(true);
  };

  async function save() {
    if (!name.trim()) return toast.error("名称必填");
    let dg: Record<string, unknown>;
    let fw: Record<string, unknown>;
    try {
      dg = JSON.parse(disguise);
    } catch {
      return setDisguiseErr("伪装配置不是合法 JSON");
    }
    try {
      fw = JSON.parse(firewall);
    } catch {
      return setFirewallErr("防火墙配置不是合法 JSON");
    }
    try {
      await api.saveC2Listener({
        id: initial?.id ?? undefined,
        name: name.trim(),
        protocol,
        host,
        port: Number(port) || 0,
        profile_id: profileID ? Number(profileID) : null,
        disguise: dg,
        firewall: fw,
      });
      toast.success("已保存");
      setOpen(false);
      onSaved();
    } catch (e) {
      toast.error(`保存失败：${(e as Error).message}`);
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        setOpen(o);
        if (o) openFor(initial);
      }}
    >
      <DialogTrigger asChild>
        {initial ? (
          <Button size="sm" variant="ghost">
            <Settings2Icon className="size-3.5" /> 编辑
          </Button>
        ) : (
          <Button size="sm" className="ml-auto">
            <PlusIcon /> 新增监听器
          </Button>
        )}
      </DialogTrigger>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{initial ? "编辑 C2 监听器" : "新增 C2 监听器"}</DialogTitle>
          <DialogDescription>保存后需点击「启动」才真正绑定端口并接收 beacon 流量。</DialogDescription>
        </DialogHeader>
        {renderFields()}
        <DialogFooter>
          <DialogClose asChild>
            <Button variant="outline">取消</Button>
          </DialogClose>
          <Button onClick={save}>保存</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );

  function renderFields() {
    return (
      <div className="grid gap-3 py-2">
        <div className="grid gap-2">
          <Label htmlFor="c2-name">名称</Label>
          <Input id="c2-name" placeholder="例如：VPS-HTTP-C2" value={name} onChange={(e) => setName(e.target.value)} />
        </div>
        <div className="grid grid-cols-3 gap-2">
          <div className="grid gap-2">
            <Label>协议</Label>
            <Select value={protocol} onValueChange={setProtocol}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="http">HTTP</SelectItem>
                <SelectItem value="https">HTTPS</SelectItem>
                <SelectItem value="tcp">TCP</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="grid gap-2">
            <Label htmlFor="c2-host">Host</Label>
            <Input id="c2-host" value={host} onChange={(e) => setHost(e.target.value)} />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="c2-port">端口</Label>
            <Input id="c2-port" type="number" value={port} onChange={(e) => setPort(e.target.value)} />
          </div>
        </div>
        {profiles.length > 0 && (
          <div className="grid gap-2">
            <Label>Malleable Profile</Label>
            <Select value={profileID} onValueChange={setProfileID}>
              <SelectTrigger>
                <SelectValue placeholder="不绑定" />
              </SelectTrigger>
              <SelectContent>
                {profiles.map((p) => (
                  <SelectItem key={p.id} value={p.id}>
                    {p.name} ({p.kind})
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        )}
        <div className="grid gap-2">
          <Label htmlFor="c2-disguise">监听器伪装 (JSON)</Label>
          <Textarea
            id="c2-disguise"
            rows={4}
            className="font-mono text-xs"
            value={disguise}
            onChange={(e) => {
              setDisguise(e.target.value);
              setDisguiseErr("");
            }}
            placeholder='{"decoy_type":"nginx_404","status_code":404,"server_header":"nginx/1.24.0"}'
          />
          {disguiseErr ? <p className="text-destructive text-xs">{disguiseErr}</p> : null}
        </div>
        <div className="grid gap-2">
          <Label htmlFor="c2-firewall">防火墙 (JSON)</Label>
          <Textarea
            id="c2-firewall"
            rows={4}
            className="font-mono text-xs"
            value={firewall}
            onChange={(e) => {
              setFirewall(e.target.value);
              setFirewallErr("");
            }}
            placeholder='{"basic_auth_enabled":false,"basic_auth_user":"","basic_auth_pass":""}'
          />
          {firewallErr ? <p className="text-destructive text-xs">{firewallErr}</p> : null}
        </div>
      </div>
    );
  }
}

function ListenersTab({
  listeners,
  profiles,
  autoTasks,
  onChanged,
}: {
  listeners: C2Listener[];
  profiles: C2Profile[];
  autoTasks: C2AutoTask[];
  onChanged: () => void;
}) {
  const [autoOpen, setAutoOpen] = React.useState(false);
  const [autoName, setAutoName] = React.useState("");
  const [autoListener, setAutoListener] = React.useState<string>("");
  const [autoCmds, setAutoCmds] = React.useState('["shell ipconfig"]');
  const [autoPostex, setAutoPostex] = React.useState(false);

  React.useEffect(() => {
    api
      .c2AutoPostex()
      .then((r) => setAutoPostex(r.enabled))
      .catch(() => setAutoPostex(false));
  }, []);

  const toggle = async (l: C2Listener) => {
    try {
      if (l.status === "running") await api.c2ListenerStop(l.id);
      else await api.c2ListenerStart(l.id);
      onChanged();
    } catch (e) {
      toast.error(`操作失败：${(e as Error).message}`);
    }
  };

  const saveAuto = async () => {
    if (!autoName.trim()) return toast.error("名称必填");
    let cmds: string[];
    try {
      cmds = JSON.parse(autoCmds);
    } catch {
      return toast.error("命令组不是合法 JSON 数组");
    }
    try {
      await api.saveC2AutoTask({
        name: autoName.trim(),
        listener_id: autoListener ? Number(autoListener) : null,
        target: "commands",
        commands: cmds,
      });
      toast.success("已保存");
      setAutoOpen(false);
      setAutoName("");
      onChanged();
    } catch (e) {
      toast.error(`保存失败：${(e as Error).message}`);
    }
  };

  return (
    <div className="grid gap-3">
      <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
        {listeners.length === 0 && (
          <Card className="md:col-span-2 xl:col-span-3">
            <CardContent className="py-10 text-center text-muted-foreground text-sm">
              暂无监听器，点击「新增监听器」。
            </CardContent>
          </Card>
        )}
        {listeners.map((l) => (
          <Card key={l.id}>
            <CardContent className="flex items-center justify-between gap-2">
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <span className="truncate font-medium">{l.name}</span>
                  <Badge variant="outline" className="uppercase">
                    {l.protocol}
                  </Badge>
                  {statusBadge(l.status)}
                </div>
                <code className="mt-0.5 block font-mono text-muted-foreground text-xs">
                  {l.host}:{l.port}
                </code>
                {l.error ? <div className="mt-0.5 truncate font-mono text-destructive text-xs">{l.error}</div> : null}
              </div>
              <div className="flex items-center gap-1">
                <Button size="sm" variant={l.status === "running" ? "outline" : "default"} onClick={() => toggle(l)}>
                  {l.status === "running" ? "停止" : "启动"}
                </Button>
                <ListenerForm profiles={profiles} onSaved={onChanged} initial={l} />
                <Button
                  size="icon"
                  variant="ghost"
                  onClick={async () => {
                    await api.deleteC2Listener(l.id);
                    onChanged();
                  }}
                >
                  <Trash2Icon className="size-4 text-destructive" />
                </Button>
              </div>
            </CardContent>
          </Card>
        ))}
        <ListenerForm profiles={profiles} onSaved={onChanged} />
      </div>

      <div className="grid gap-3 lg:grid-cols-2">
        <Card>
          <CardContent className="grid gap-2">
            <h3 className="font-medium text-sm">Malleable Profile 配置</h3>
            <p className="text-muted-foreground text-xs">
              对齐 Oktos default.toml：timing / identity / transform / tls / response。
            </p>
            {profiles.length === 0 ? (
              <div className="py-6 text-center text-muted-foreground text-sm">暂无 Profile</div>
            ) : (
              profiles.map((p) => (
                <div key={p.id} className="flex items-center justify-between gap-2 rounded border px-3 py-2">
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="font-medium text-sm">{p.name}</span>
                      <Badge variant="outline" className="uppercase">
                        {p.kind}
                      </Badge>
                    </div>
                    <pre className="mt-1 max-h-16 overflow-auto font-mono text-[10px] text-muted-foreground">
                      {JSON.stringify(p.config)}
                    </pre>
                  </div>
                  <Button
                    size="icon"
                    variant="ghost"
                    onClick={async () => {
                      await api.deleteC2Profile(p.id);
                      onChanged();
                    }}
                  >
                    <Trash2Icon className="size-4 text-destructive" />
                  </Button>
                </div>
              ))
            )}
          </CardContent>
        </Card>

        <Card>
          <CardContent className="grid gap-2">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <h3 className="font-medium text-sm">自动执行（新会话上车即跑）</h3>
                <div className="flex items-center gap-1.5">
                  <Label htmlFor="auto-postex" className="text-muted-foreground text-xs">
                    AI 自动后渗透
                  </Label>
                  <Switch
                    id="auto-postex"
                    checked={autoPostex}
                    onCheckedChange={async (v) => {
                      try {
                        await api.c2SetAutoPostex(v);
                        setAutoPostex(v);
                        toast.success(v ? "已开启 AI 自动后渗透" : "已关闭 AI 自动后渗透");
                      } catch (e) {
                        toast.error(`操作失败：${(e as Error).message}`);
                      }
                    }}
                  />
                </div>
              </div>
              <Dialog open={autoOpen} onOpenChange={setAutoOpen}>
                <DialogTrigger asChild>
                  <Button size="sm" variant="outline">
                    <PlusIcon /> 新增规则
                  </Button>
                </DialogTrigger>
                <DialogContent className="sm:max-w-md">
                  <DialogHeader>
                    <DialogTitle>新增自动执行规则</DialogTitle>
                    <DialogDescription>规则命中后向该监听器所有新会话自动下发命令组。</DialogDescription>
                  </DialogHeader>
                  <div className="grid gap-3 py-2">
                    <div className="grid gap-2">
                      <Label htmlFor="at-name">名称</Label>
                      <Input id="at-name" value={autoName} onChange={(e) => setAutoName(e.target.value)} />
                    </div>
                    <div className="grid gap-2">
                      <Label>监听器</Label>
                      <Select value={autoListener} onValueChange={setAutoListener}>
                        <SelectTrigger>
                          <SelectValue placeholder="全部" />
                        </SelectTrigger>
                        <SelectContent>
                          {listeners.map((l) => (
                            <SelectItem key={l.id} value={String(l.id)}>
                              {l.name}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    </div>
                    <div className="grid gap-2">
                      <Label htmlFor="at-cmds">命令组 (JSON 数组)</Label>
                      <Textarea
                        id="at-cmds"
                        rows={4}
                        className="font-mono text-xs"
                        value={autoCmds}
                        onChange={(e) => setAutoCmds(e.target.value)}
                      />
                    </div>
                  </div>
                  <DialogFooter>
                    <DialogClose asChild>
                      <Button variant="outline">取消</Button>
                    </DialogClose>
                    <Button onClick={saveAuto}>保存</Button>
                  </DialogFooter>
                </DialogContent>
              </Dialog>
            </div>
            {autoTasks.length === 0 ? (
              <div className="py-6 text-center text-muted-foreground text-sm">暂无规则</div>
            ) : (
              autoTasks.map((a) => (
                <div key={a.id} className="flex items-center justify-between gap-2 rounded border px-3 py-2">
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="font-medium text-sm">{a.name}</span>
                      {a.enabled ? (
                        <Badge variant="secondary" className="text-emerald-600">
                          启用
                        </Badge>
                      ) : (
                        <Badge variant="outline">停用</Badge>
                      )}
                    </div>
                    <div className="mt-1 truncate font-mono text-[10px] text-muted-foreground">
                      {(a.commands ?? []).join(" · ")}
                    </div>
                  </div>
                  <Button
                    size="icon"
                    variant="ghost"
                    onClick={async () => {
                      await api.deleteC2AutoTask(a.id);
                      onChanged();
                    }}
                  >
                    <Trash2Icon className="size-4 text-destructive" />
                  </Button>
                </div>
              ))
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

// ================= 客户端生成 =================

const COMBOS = [
  "darwin_amd64",
  "darwin_arm64",
  "linux_amd64",
  "linux_i386",
  "linux_arm64",
  "linux_arm",
  "windows_amd64",
  "windows_i386",
] as const;

// VShell 风格：顶部格式 tab，每个 tab 有标题/副标题
const FORMAT_TABS: { key: string; title: string; subtitle: string }[] = [
  { key: "stageless", title: "Stageless 反向客户端", subtitle: "单文件可执行，配置内嵌，直接运行上线" },
  { key: "dll", title: "DLL Stageless 客户端", subtitle: "Windows DLL（需 CGO/mingw 交叉编译）" },
  { key: "shellcode", title: "Shellcode 客户端", subtitle: "小体积 shellcode（需外部 Donut/SRDI 工具链）" },
  { key: "config", title: "配置生成", subtitle: "仅生成连接配置，不构建可执行文件" },
];

function GenerateTab({ listeners, onChanged }: { listeners: C2Listener[]; onChanged: () => void }) {
  const [list, setList] = React.useState<C2Generated[]>([]);
  const [name, setName] = React.useState("");
  const [listenerID, setListenerID] = React.useState<string>("");
  const [combo, setCombo] = React.useState<string>("windows_amd64");
  const [format, setFormat] = React.useState<string>("stageless");
  const [host, setHost] = React.useState("");
  const [result, setResult] = React.useState<{
    id: number;
    session_id: string;
    format?: string;
    download_url?: string;
    size?: number;
    built?: boolean;
    message?: string;
    build_command: string;
    run_command: string;
  } | null>(null);

  const load = React.useCallback(() => {
    api
      .c2Generated()
      .then((r) => setList(r.generated ?? []))
      .catch(() => setList([]));
  }, []);
  React.useEffect(load, [load]);

  const generate = async () => {
    if (!name.trim()) return toast.error("名称必填");
    if (!listenerID) return toast.error("请选择监听器");
    const [os, arch] = combo.split("_");
    try {
      const r = await api.c2Generate({
        name: name.trim(),
        listener_id: Number(listenerID),
        os,
        arch,
        format,
        host: host.trim() || undefined,
      });
      setResult(r);
      setName("");
      load();
      onChanged();
    } catch (e) {
      toast.error(`生成失败：${(e as Error).message}`);
    }
  };

  return (
    <div className="grid gap-3">
      <Card>
        <CardContent className="grid gap-4">
          {/* VShell 风格：顶部格式 tab */}
          <div className="flex flex-wrap gap-1 rounded-lg bg-muted/50 p-1">
            {FORMAT_TABS.map((f) => (
              <button
                key={f.key}
                type="button"
                onClick={() => setFormat(f.key)}
                className={`rounded-md px-3 py-1.5 font-mono text-xs transition-colors ${
                  format === f.key ? "bg-background shadow-sm" : "text-muted-foreground hover:text-foreground"
                }`}
              >
                {f.key}
              </button>
            ))}
          </div>

          <div className="grid gap-4 lg:grid-cols-2">
            <div className="grid gap-3">
              {/* 当前 tab 标题 + 副标题 */}
              <div>
                <h3 className="font-semibold">{FORMAT_TABS.find((f) => f.key === format)?.title}</h3>
                <p className="text-muted-foreground text-sm">{FORMAT_TABS.find((f) => f.key === format)?.subtitle}</p>
              </div>

              <div className="grid gap-2">
                <Label>客户端类型</Label>
                <div className="grid grid-cols-2 gap-1.5 sm:grid-cols-4">
                  {COMBOS.map((c) => {
                    const selected = combo === c;
                    return (
                      <button
                        key={c}
                        type="button"
                        onClick={() => setCombo(c)}
                        aria-pressed={selected}
                        className={`flex items-center gap-1.5 rounded border px-2 py-1.5 font-mono text-xs transition-colors ${
                          selected ? "border-primary bg-primary/10" : "hover:bg-muted/50"
                        }`}
                      >
                        <span
                          className={`size-2 shrink-0 rounded-full border ${
                            selected ? "border-primary bg-primary" : "border-muted-foreground/40"
                          }`}
                        />
                        {c}
                      </button>
                    );
                  })}
                </div>
              </div>

              <div className="grid grid-cols-2 gap-2">
                <div className="grid gap-2">
                  <Label>选择监听器</Label>
                  <Select value={listenerID} onValueChange={setListenerID}>
                    <SelectTrigger>
                      <SelectValue placeholder="请选择" />
                    </SelectTrigger>
                    <SelectContent>
                      {listeners.map((l) => (
                        <SelectItem key={l.id} value={String(l.id)}>
                          {l.name} ({l.host}:{l.port})
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="gen-host">团队服务器地址(可选)</Label>
                  <Input
                    id="gen-host"
                    placeholder="留空自动推断"
                    value={host}
                    onChange={(e) => setHost(e.target.value)}
                  />
                </div>
              </div>
              <div className="grid gap-2">
                <Label htmlFor="gen-name">名称</Label>
                <Input
                  id="gen-name"
                  placeholder="例如：内网主机-A"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                />
              </div>
              <Button onClick={generate} className="w-full sm:w-auto">
                <BoxesIcon className="size-3.5" /> 生成下载
              </Button>
            </div>

            <div className="grid gap-2 rounded border bg-muted/30 p-3">
              <h3 className="font-medium text-sm">生成结果</h3>
              {result ? (
                <div className="grid gap-2">
                  <div className="flex items-center gap-2">
                    <Badge variant="secondary">session_id: {result.session_id}</Badge>
                    <Badge variant="outline">{result.format ?? "stageless"}</Badge>
                    {result.built && result.size ? (
                      <Badge variant="outline">{(result.size / 1024).toFixed(1)} KB</Badge>
                    ) : null}
                  </div>
                  {result.built && result.download_url ? (
                    <a
                      href={result.download_url}
                      download
                      className="inline-flex items-center gap-2 rounded-md bg-primary px-3 py-2 font-medium text-primary-foreground text-sm hover:bg-primary/90"
                    >
                      <BoxesIcon className="size-4" /> 下载客户端
                    </a>
                  ) : null}
                  {result.message ? <p className="text-amber-600 text-xs">{result.message}</p> : null}
                  <p className="text-muted-foreground text-xs">
                    运行方式：直接执行下载的客户端，配置已内嵌，无需携带文件。
                  </p>
                  <div className="grid gap-1">
                    <div className="flex items-center gap-2">
                      <code className="flex-1 truncate rounded bg-background px-2 py-1 font-mono text-[11px]">
                        {result.build_command}
                      </code>
                      <Button size="sm" variant="ghost" onClick={() => copy(result.build_command)}>
                        <CopyIcon className="size-3.5" /> 复制
                      </Button>
                    </div>
                    <div className="flex items-center gap-2">
                      <code className="flex-1 truncate rounded bg-background px-2 py-1 font-mono text-[11px]">
                        {result.run_command}
                      </code>
                      <Button size="sm" variant="ghost" onClick={() => copy(result.run_command)}>
                        <CopyIcon className="size-3.5" /> 复制
                      </Button>
                    </div>
                  </div>
                </div>
              ) : (
                <div className="py-8 text-center text-muted-foreground text-sm">
                  选择格式 / 客户端类型 / 监听器后点击「生成下载」
                </div>
              )}
            </div>
          </div>
        </CardContent>
      </Card>

      <Card className="overflow-hidden py-0">
        <CardContent className="p-0">
          <table className="w-full text-sm">
            <thead className="bg-muted/50 text-muted-foreground text-xs">
              <tr className="text-left">
                <th className="px-3 py-2 font-medium">名称</th>
                <th className="px-3 py-2 font-medium">类型</th>
                <th className="px-3 py-2 font-medium">格式</th>
                <th className="px-3 py-2 font-medium">session_id</th>
                <th className="px-3 py-2 font-medium">生成时间</th>
                <th className="px-3 py-2 text-right font-medium">操作</th>
              </tr>
            </thead>
            <tbody>
              {list.length === 0 ? (
                <tr>
                  <td colSpan={6} className="px-3 py-8 text-center text-muted-foreground text-sm">
                    暂无生成记录
                  </td>
                </tr>
              ) : (
                list.map((g) => (
                  <tr key={g.id} className="border-t">
                    <td className="px-3 py-2 font-medium">{g.name}</td>
                    <td className="px-3 py-2 font-mono text-xs">
                      {g.os}/{g.arch}
                    </td>
                    <td className="px-3 py-2">
                      <Badge variant="outline">{g.format ?? "stageless"}</Badge>
                    </td>
                    <td className="px-3 py-2 font-mono text-xs">{String(g.config.session_id ?? "")}</td>
                    <td className="px-3 py-2 text-muted-foreground text-xs">{fmtTime(g.created_at)}</td>
                    <td className="px-3 py-2 text-right">
                      {g.artifact ? (
                        <a href={`/api/c2/generated/${g.id}/download`} className="text-primary text-sm hover:underline">
                          下载
                        </a>
                      ) : null}
                      <Button size="sm" variant="ghost" onClick={() => copy(JSON.stringify(g.config, null, 2))}>
                        复制配置
                      </Button>
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={async () => {
                          await api.deleteC2Generated(g.id);
                          load();
                        }}
                      >
                        删除
                      </Button>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </CardContent>
      </Card>
    </div>
  );
}

// ================= 插件运行 =================

function PluginsTab({ sessions, onChanged }: { sessions: C2Session[]; onChanged: () => void }) {
  const [plugins, setPlugins] = React.useState<C2Plugin[]>([]);
  const [open, setOpen] = React.useState(false);
  const [name, setName] = React.useState("");
  const [desc, setDesc] = React.useState("");
  const [cmds, setCmds] = React.useState('["shell whoami","shell ipconfig"]');
  const [runTarget, setRunTarget] = React.useState<C2Plugin | null>(null);
  const [runSession, setRunSession] = React.useState<string>("");

  const load = React.useCallback(() => {
    api
      .c2Plugins()
      .then((r) => setPlugins(r.plugins ?? []))
      .catch(() => setPlugins([]));
  }, []);
  React.useEffect(load, [load]);

  const save = async () => {
    if (!name.trim()) return toast.error("名称必填");
    let parsed: string[];
    try {
      parsed = JSON.parse(cmds);
    } catch {
      return toast.error("命令组不是合法 JSON 数组");
    }
    try {
      await api.saveC2Plugin({ name: name.trim(), description: desc.trim(), commands: parsed });
      toast.success("已保存");
      setOpen(false);
      setName("");
      load();
    } catch (e) {
      toast.error(`保存失败：${(e as Error).message}`);
    }
  };

  const run = async () => {
    if (!runTarget) return;
    if (!runSession) return toast.error("请选择目标会话");
    try {
      const r = await api.c2RunPlugin(runTarget.id, runSession);
      toast.success(`已入队 ${r.enqueued} 个任务`);
      setRunTarget(null);
    } catch (e) {
      toast.error(`运行失败：${(e as Error).message}`);
    }
  };

  return (
    <div className="grid gap-3">
      <div className="flex items-center justify-between">
        <p className="text-muted-foreground text-sm">插件 = 可复用的命令组模板，一键对选中的客户端下发。</p>
        <Dialog open={open} onOpenChange={setOpen}>
          <DialogTrigger asChild>
            <Button size="sm">
              <PlusIcon /> 新增插件
            </Button>
          </DialogTrigger>
          <DialogContent className="sm:max-w-md">
            <DialogHeader>
              <DialogTitle>新增插件</DialogTitle>
              <DialogDescription>命令组为 JSON 数组，每条会作为任务下发给目标客户端。</DialogDescription>
            </DialogHeader>
            <div className="grid gap-3 py-2">
              <div className="grid gap-2">
                <Label htmlFor="pl-name">名称</Label>
                <Input id="pl-name" value={name} onChange={(e) => setName(e.target.value)} />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="pl-desc">描述</Label>
                <Input id="pl-desc" value={desc} onChange={(e) => setDesc(e.target.value)} />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="pl-cmds">命令组 (JSON 数组)</Label>
                <Textarea
                  id="pl-cmds"
                  rows={6}
                  className="font-mono text-xs"
                  value={cmds}
                  onChange={(e) => setCmds(e.target.value)}
                />
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
      </div>

      {plugins.length === 0 ? (
        <Card>
          <CardContent className="py-10 text-center text-muted-foreground text-sm">暂无插件</CardContent>
        </Card>
      ) : (
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
          {plugins.map((p) => (
            <Card key={p.id}>
              <CardContent className="grid gap-2">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <PlugZapIcon className="size-4 text-muted-foreground" />
                    <span className="font-medium">{p.name}</span>
                  </div>
                  <div className="flex items-center gap-1">
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => {
                        setRunTarget(p);
                        setRunSession("");
                      }}
                    >
                      <SendIcon className="size-3.5" /> 运行
                    </Button>
                    <Button
                      size="icon"
                      variant="ghost"
                      onClick={async () => {
                        await api.deleteC2Plugin(p.id);
                        load();
                      }}
                    >
                      <Trash2Icon className="size-4 text-destructive" />
                    </Button>
                  </div>
                </div>
                {p.description ? <p className="text-muted-foreground text-xs">{p.description}</p> : null}
                <pre className="max-h-24 overflow-auto rounded bg-muted/50 p-2 font-mono text-[11px]">
                  {(p.commands ?? []).join("\n")}
                </pre>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      <Dialog
        open={runTarget !== null}
        onOpenChange={(o) => {
          if (!o) setRunTarget(null);
        }}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>运行插件：{runTarget?.name}</DialogTitle>
            <DialogDescription>选择目标客户端，命令组将作为任务下发。</DialogDescription>
          </DialogHeader>
          <div className="grid gap-2 py-2">
            <Label>目标会话</Label>
            <Select value={runSession} onValueChange={setRunSession}>
              <SelectTrigger>
                <SelectValue placeholder="选择客户端" />
              </SelectTrigger>
              <SelectContent>
                {sessions.map((s) => (
                  <SelectItem key={s.id} value={s.session_id}>
                    {s.session_id}
                    {s.hostname ? ` (${s.hostname})` : ""}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <DialogFooter>
            <DialogClose asChild>
              <Button variant="outline">取消</Button>
            </DialogClose>
            <Button onClick={run}>下发</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

// ================= 隧道管理 =================

function TunnelsTab({ sessions, onChanged }: { sessions: C2Session[]; onChanged: () => void }) {
  const [tunnels, setTunnels] = React.useState<C2Tunnel[]>([]);
  const [open, setOpen] = React.useState(false);
  const [sessionID, setSessionID] = React.useState<string>("");
  const [kind, setKind] = React.useState("socks5");
  const [bindPort, setBindPort] = React.useState("1080");

  const load = React.useCallback(() => {
    api
      .c2Tunnels()
      .then((r) => setTunnels(r.tunnels ?? []))
      .catch(() => setTunnels([]));
  }, []);
  React.useEffect(load, [load]);

  const create = async () => {
    if (!sessionID) return toast.error("请选择会话");
    try {
      await api.createC2Tunnel({ session_id: sessionID, kind, bind_port: Number(bindPort) || 1080 });
      toast.success("隧道已启动");
      setOpen(false);
      load();
      onChanged();
    } catch (e) {
      toast.error(`启动失败：${(e as Error).message}`);
    }
  };

  return (
    <div className="grid gap-3">
      <div className="flex items-center justify-between">
        <p className="text-muted-foreground text-sm">
          SOCKS5 隧道经 beacon 中继到目标内网；本地代理端口即绑定在团队服务器上。
        </p>
        <Dialog open={open} onOpenChange={setOpen}>
          <DialogTrigger asChild>
            <Button size="sm">
              <PlusIcon /> 新建隧道
            </Button>
          </DialogTrigger>
          <DialogContent className="sm:max-w-md">
            <DialogHeader>
              <DialogTitle>新建 SOCKS5 隧道</DialogTitle>
              <DialogDescription>选择一个活跃客户端作为跳板，通过它中继到其内网。</DialogDescription>
            </DialogHeader>
            <div className="grid gap-3 py-2">
              <div className="grid gap-2">
                <Label>跳板会话</Label>
                <Select value={sessionID} onValueChange={setSessionID}>
                  <SelectTrigger>
                    <SelectValue placeholder="选择客户端" />
                  </SelectTrigger>
                  <SelectContent>
                    {sessions.map((s) => (
                      <SelectItem key={s.id} value={s.session_id}>
                        {s.session_id}
                        {s.hostname ? ` (${s.hostname})` : ""}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="grid grid-cols-2 gap-2">
                <div className="grid gap-2">
                  <Label>类型</Label>
                  <Select value={kind} onValueChange={setKind}>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="socks5">SOCKS5</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="tn-port">本地端口</Label>
                  <Input id="tn-port" type="number" value={bindPort} onChange={(e) => setBindPort(e.target.value)} />
                </div>
              </div>
            </div>
            <DialogFooter>
              <DialogClose asChild>
                <Button variant="outline">取消</Button>
              </DialogClose>
              <Button onClick={create}>启动</Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>

      {tunnels.length === 0 ? (
        <Card>
          <CardContent className="py-10 text-center text-muted-foreground text-sm">暂无隧道</CardContent>
        </Card>
      ) : (
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
          {tunnels.map((t) => (
            <Card key={t.id}>
              <CardContent className="flex items-center justify-between gap-2">
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="font-medium">{t.session_id}</span>
                    <Badge variant="outline" className="uppercase">
                      {t.kind}
                    </Badge>
                    {statusBadge(t.state)}
                  </div>
                  <code className="mt-0.5 block font-mono text-muted-foreground text-xs">
                    {t.bind_host}:{t.bind_port} via {t.session_id}
                  </code>
                  {t.error ? <div className="mt-0.5 truncate font-mono text-destructive text-xs">{t.error}</div> : null}
                </div>
                <Button
                  size="icon"
                  variant="ghost"
                  onClick={async () => {
                    await api.deleteC2Tunnel(t.id);
                    load();
                  }}
                >
                  <Trash2Icon className="size-4 text-destructive" />
                </Button>
              </CardContent>
            </Card>
          ))}
        </div>
      )}
    </div>
  );
}

// ================= 后渗透 =================

function PostexTab({ sessions, onChanged }: { sessions: C2Session[]; onChanged: () => void }) {
  const [modules, setModules] = React.useState<C2PostexModule[]>([]);
  const [selected, setSelected] = React.useState<C2PostexModule | null>(null);
  const [target, setTarget] = React.useState<string>("");
  const [args, setArgs] = React.useState("");
  const [result, setResult] = React.useState<{
    id: number;
    module: string;
    session_id: string;
    state: string;
  } | null>(null);
  const [taskResult, setTaskResult] = React.useState<string>("");
  const [running, setRunning] = React.useState(false);

  React.useEffect(() => {
    api
      .c2Postex()
      .then((r) => setModules(r.modules ?? []))
      .catch(() => setModules([]));
  }, []);

  // poll the executed task until it completes
  React.useEffect(() => {
    if (!result) return;
    let cancelled = false;
    const poll = async () => {
      try {
        const r = await api.c2Tasks(result.session_id);
        const t = (r.tasks ?? []).find((x) => x.id === String(result.id));
        if (t && (t.state === "completed" || t.state === "failed")) {
          setTaskResult(t.response ? JSON.stringify(t.response, null, 2) : `state=${t.state}`);
          setRunning(false);
          onChanged();
          return;
        }
      } catch {
        /* retry */
      }
      if (!cancelled) setTimeout(poll, 2000);
    };
    void poll();
    return () => {
      cancelled = true;
    };
  }, [result, onChanged]);

  const run = async () => {
    if (!target) return toast.error("请选择目标会话");
    if (!selected) return toast.error("请选择后渗透模块");
    setRunning(true);
    setTaskResult("");
    try {
      const r = await api.c2PostexRun(target, selected.id, args.trim() || undefined);
      setResult(r);
    } catch (e) {
      toast.error(`执行失败：${(e as Error).message}`);
      setRunning(false);
    }
  };

  return (
    <div className="grid gap-3">
      <div className="flex flex-wrap items-center gap-2">
        <Select value={target} onValueChange={setTarget}>
          <SelectTrigger className="w-64">
            <SelectValue placeholder="选择目标会话" />
          </SelectTrigger>
          <SelectContent>
            {sessions.map((s) => (
              <SelectItem key={s.id} value={s.session_id}>
                {s.session_id}
                {s.hostname ? ` (${s.hostname})` : ""}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {selected && selected.args ? (
          <Input
            className="w-72 font-mono"
            placeholder={`参数：${selected.args}`}
            value={args}
            onChange={(e) => setArgs(e.target.value)}
          />
        ) : null}
        <Button onClick={run} disabled={running || !target || !selected}>
          <CrosshairIcon className="size-3.5" /> {running ? "执行中…" : "执行"}
        </Button>
        {result ? <Badge variant="outline">task_id: {result.id}</Badge> : null}
      </div>

      <div className="grid grid-cols-1 gap-2 md:grid-cols-2 xl:grid-cols-3">
        {modules.map((m) => (
          <button
            key={m.id}
            type="button"
            onClick={() => setSelected(m)}
            className={`flex flex-col items-start gap-1 rounded-lg border p-3 text-left transition-colors ${
              selected?.id === m.id ? "border-primary bg-primary/10" : "hover:bg-muted/50"
            }`}
          >
            <div className="flex items-center gap-2">
              <CrosshairIcon className="size-4 text-muted-foreground" />
              <span className="font-medium">{m.name}</span>
              <Badge variant="outline" className="font-mono">
                {m.id}
              </Badge>
            </div>
            <p className="text-muted-foreground text-xs">{m.desc}</p>
            {m.args ? <p className="font-mono text-[11px] text-muted-foreground">参数: {m.args}</p> : null}
          </button>
        ))}
      </div>

      {taskResult && (
        <Card>
          <CardContent className="grid gap-2">
            <div className="flex items-center gap-2">
              <span className="font-medium text-sm">执行结果（{selected?.name ?? result?.module}）</span>
              <Badge variant="secondary" className="text-emerald-600">
                完成
              </Badge>
            </div>
            <pre className="max-h-96 overflow-auto rounded bg-muted/50 p-3 font-mono text-xs">{taskResult}</pre>
          </CardContent>
        </Card>
      )}
    </div>
  );
}

// ================= 页面 =================

export default function C2Page() {
  const [listeners, setListeners] = React.useState<C2Listener[]>([]);
  const [sessions, setSessions] = React.useState<C2Session[]>([]);
  const [profiles, setProfiles] = React.useState<C2Profile[]>([]);
  const [autoTasks, setAutoTasks] = React.useState<C2AutoTask[]>([]);

  const load = React.useCallback(() => {
    api
      .c2()
      .then((r) => {
        setListeners(r.listeners ?? []);
        setSessions(r.sessions ?? []);
      })
      .catch(() => {
        setListeners([]);
        setSessions([]);
      });
    api
      .c2Profiles()
      .then((r) => setProfiles(r.profiles ?? []))
      .catch(() => setProfiles([]));
    api
      .c2AutoTasks()
      .then((r) => setAutoTasks(r.auto_tasks ?? []))
      .catch(() => setAutoTasks([]));
  }, []);
  React.useEffect(() => {
    load();
    const i = setInterval(load, 5000);
    return () => clearInterval(i);
  }, [load]);

  return (
    <div className="flex flex-1 flex-col gap-4">
      <div className="flex items-center gap-2">
        <WebhookIcon className="size-5 text-muted-foreground" />
        <h1 className="font-semibold text-xl tracking-tight">C2</h1>
        <Badge variant="secondary">{sessions.length} 客户端</Badge>
        <Badge variant="outline">
          {listeners.filter((l) => l.status === "running").length}/{listeners.length} 监听器
        </Badge>
      </div>

      <Tabs defaultValue="clients" className="w-full">
        <TabsList className="flex-wrap">
          <TabsTrigger value="clients">
            <ServerIcon className="size-3.5" /> 客户端管理
          </TabsTrigger>
          <TabsTrigger value="listeners">
            <RadioTowerIcon className="size-3.5" /> 监听管理
          </TabsTrigger>
          <TabsTrigger value="generate">
            <BoxesIcon className="size-3.5" /> 客户端生成
          </TabsTrigger>
          <TabsTrigger value="plugins">
            <ZapIcon className="size-3.5" /> 插件运行
          </TabsTrigger>
          <TabsTrigger value="tunnels">
            <PlugZapIcon className="size-3.5" /> 隧道管理
          </TabsTrigger>
          <TabsTrigger value="postex">
            <CrosshairIcon className="size-3.5" /> 后渗透
          </TabsTrigger>
        </TabsList>

        <TabsContent value="clients" className="mt-3">
          <ClientsTab sessions={sessions} onChanged={load} />
        </TabsContent>

        <TabsContent value="listeners" className="mt-3">
          <ListenersTab listeners={listeners} profiles={profiles} autoTasks={autoTasks} onChanged={load} />
        </TabsContent>

        <TabsContent value="generate" className="mt-3">
          <GenerateTab listeners={listeners} onChanged={load} />
        </TabsContent>

        <TabsContent value="plugins" className="mt-3">
          <PluginsTab sessions={sessions} onChanged={load} />
        </TabsContent>

        <TabsContent value="tunnels" className="mt-3">
          <TunnelsTab sessions={sessions} onChanged={load} />
        </TabsContent>

        <TabsContent value="postex" className="mt-3">
          <PostexTab sessions={sessions} onChanged={load} />
        </TabsContent>
      </Tabs>
    </div>
  );
}
