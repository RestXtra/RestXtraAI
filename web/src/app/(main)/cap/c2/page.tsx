"use client";

import * as React from "react";

import { PlusIcon, Trash2Icon, WebhookIcon } from "lucide-react";
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
import type { C2Listener, C2Session } from "@/lib/types";

function ListenerForm({ onSaved }: { onSaved: () => void }) {
  const [open, setOpen] = React.useState(false);
  const [name, setName] = React.useState("");
  const [protocol, setProtocol] = React.useState("http");
  const [host, setHost] = React.useState("0.0.0.0");
  const [port, setPort] = React.useState("8080");

  async function save() {
    if (!name.trim()) {
      toast.error("名称必填");
      return;
    }
    try {
      await api.saveC2Listener({ name: name.trim(), protocol, host, port: Number(port) || 0 });
      toast.success("已保存");
      setName("");
      setPort("8080");
      setOpen(false);
      onSaved();
    } catch (e) {
      toast.error(`保存失败：${(e as Error).message}`);
    }
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size="sm" className="ml-auto">
          <PlusIcon /> 新增监听器
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>新增 C2 监听器</DialogTitle>
          <DialogDescription>登记监听器；beacon 通过 POST /api/c2/ingest 上报会话。</DialogDescription>
        </DialogHeader>
        <div className="grid gap-3 py-2">
          <div className="grid gap-2">
            <Label htmlFor="c2-name">名称</Label>
            <Input
              id="c2-name"
              placeholder="例如：内网HTTP-C2"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
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

export default function C2Page() {
  const [listeners, setListeners] = React.useState<C2Listener[]>([]);
  const [sessions, setSessions] = React.useState<C2Session[]>([]);

  // batch selection & delete
  const [checkedListeners, setCheckedListeners] = React.useState<Set<string>>(new Set());
  const [checkedSessions, setCheckedSessions] = React.useState<Set<string>>(new Set());
  const [deleteAll, setDeleteAll] = React.useState(false);
  const [deleteOpen, setDeleteOpen] = React.useState(false);
  const [deleting, setDeleting] = React.useState(false);

  const toggleListener = (id: string) => {
    setCheckedListeners((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const toggleSession = (id: string) => {
    setCheckedSessions((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const confirmBatchDelete = async () => {
    setDeleting(true);
    const l = Array.from(checkedListeners);
    const s = Array.from(checkedSessions);
    try {
      let msg = "";
      if (deleteAll || l.length > 0) {
        const res = await api.deleteC2Listeners(l, deleteAll);
        msg += `已删除 ${res.deleted} 个监听器；`;
      }
      if (deleteAll || s.length > 0) {
        const res = await api.deleteC2Sessions(s, deleteAll);
        msg += `已删除 ${res.deleted} 个会话；`;
      }
      toast.success(msg || "无已选项");
      setCheckedListeners(new Set());
      setCheckedSessions(new Set());
      setDeleteOpen(false);
      load();
    } catch (e) {
      toast.error(`删除失败：${(e as Error).message}`);
      setDeleteOpen(false);
    } finally {
      setDeleting(false);
    }
  };

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
  }, []);
  React.useEffect(() => {
    load();
    const i = setInterval(load, 5000);
    return () => clearInterval(i);
  }, [load]);

  const removeListener = async (l: C2Listener) => {
    try {
      await api.deleteC2Listener(l.id);
      toast.success("已删除");
      load();
    } catch (e) {
      toast.error(`删除失败：${(e as Error).message}`);
    }
  };

  const setStatus = async (s: C2Session, status: string) => {
    try {
      await api.c2SetStatus(s.session_id, status);
      load();
    } catch (e) {
      toast.error(`操作失败：${(e as Error).message}`);
    }
  };

  const fmt = (ts: string) => new Date(ts).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" });

  return (
    <div className="flex flex-1 flex-col gap-4">
      <div className="flex items-center justify-between gap-4">
        <div className="flex items-center gap-2">
          <WebhookIcon className="size-5 text-muted-foreground" />
          <h1 className="font-semibold text-xl tracking-tight">C2</h1>
          <Badge variant="secondary">{sessions.length} 会话</Badge>
          {checkedListeners.size + checkedSessions.size > 0 && (
            <>
              <Button
                variant="destructive"
                size="sm"
                onClick={() => {
                  setDeleteAll(false);
                  setDeleteOpen(true);
                }}
              >
                <Trash2Icon className="size-3.5" /> 删除已选 ({checkedListeners.size + checkedSessions.size})
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
        <ListenerForm onSaved={load} />
      </div>

      <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
        {listeners.length === 0 && (
          <Card className="md:col-span-2 xl:col-span-3">
            <CardContent className="flex items-center justify-center py-10 text-muted-foreground text-sm">
              暂无监听器，点击「新增监听器」。
            </CardContent>
          </Card>
        )}
        {listeners.map((l) => (
          <Card key={l.id}>
            <CardContent className="flex items-center justify-between gap-2">
              <div className="flex items-center gap-2">
                <Checkbox
                  checked={checkedListeners.has(String(l.id))}
                  onCheckedChange={() => toggleListener(String(l.id))}
                  aria-label={`选择 ${l.name}`}
                />
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="truncate font-medium">{l.name}</span>
                    <Badge variant="outline" className="uppercase">
                      {l.protocol}
                    </Badge>
                    {l.enabled ? (
                      <Badge variant="secondary" className="text-emerald-600">
                        运行
                      </Badge>
                    ) : (
                      <Badge variant="outline">停止</Badge>
                    )}
                  </div>
                  <code className="mt-0.5 block font-mono text-muted-foreground text-xs">
                    {l.host}:{l.port}
                  </code>
                </div>
              </div>
              <Button size="icon" variant="outline" aria-label="删除监听器" onClick={() => removeListener(l)}>
                <Trash2Icon className="text-destructive" />
              </Button>
            </CardContent>
          </Card>
        ))}
      </div>

      <div>
        <h2 className="mb-2 font-medium text-muted-foreground text-sm">Beacon 会话</h2>
        <Card className="overflow-hidden py-0">
          <CardContent className="p-0">
            {sessions.length === 0 ? (
              <div className="py-12 text-center text-muted-foreground text-sm">
                暂无会话。beacon 心跳：<code className="font-mono">POST /api/c2/ingest</code>
                {' {"session_id, host"} '}
              </div>
            ) : (
              <table className="w-full text-sm">
                <thead className="bg-muted/50 text-muted-foreground text-xs">
                  <tr className="text-left">
                    <th className="w-8 px-3 py-2">
                      <Checkbox
                        checked={sessions.length > 0 && sessions.every((s) => checkedSessions.has(String(s.id)))}
                        onCheckedChange={() => {
                          setCheckedSessions((prev) => {
                            const allSelected = sessions.length > 0 && sessions.every((s) => prev.has(String(s.id)));
                            const next = new Set(prev);
                            if (allSelected) sessions.forEach((s) => next.delete(String(s.id)));
                            else sessions.forEach((s) => next.add(String(s.id)));
                            return next;
                          });
                        }}
                        aria-label="全选会话"
                      />
                    </th>
                    <th className="px-3 py-2 font-medium">Session</th>
                    <th className="px-3 py-2 font-medium">Host</th>
                    <th className="px-3 py-2 font-medium">状态</th>
                    <th className="px-3 py-2 font-medium">最后心跳</th>
                    <th className="px-3 py-2 text-right font-medium">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {sessions.map((s) => (
                    <tr key={s.id} className="border-t">
                      <td className="w-8 px-3 py-2">
                        <Checkbox
                          checked={checkedSessions.has(String(s.id))}
                          onCheckedChange={() => toggleSession(String(s.id))}
                          aria-label={`选择 ${s.session_id}`}
                        />
                      </td>
                      <td className="px-3 py-2 font-mono text-xs">{s.session_id}</td>
                      <td className="px-3 py-2 font-mono text-xs">{s.host || "—"}</td>
                      <td className="px-3 py-2">
                        {s.status === "active" ? (
                          <Badge variant="secondary" className="text-emerald-600">
                            活跃
                          </Badge>
                        ) : (
                          <Badge variant="outline">{s.status}</Badge>
                        )}
                      </td>
                      <td className="px-3 py-2 text-muted-foreground text-xs">{fmt(s.last_seen)}</td>
                      <td className="px-3 py-2 text-right">
                        {s.status === "active" ? (
                          <Button size="sm" variant="outline" onClick={() => setStatus(s, "lost")}>
                            标记丢失
                          </Button>
                        ) : (
                          <Button size="sm" variant="outline" onClick={() => setStatus(s, "active")}>
                            恢复
                          </Button>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
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
                <>将清空全部 C2 监听器与会话，此操作不可撤销。</>
              ) : (
                <>
                  将删除 <span className="font-semibold tabular-nums">{checkedListeners.size}</span> 个监听器、
                  <span className="font-semibold tabular-nums"> {checkedSessions.size}</span> 个会话，此操作不可撤销。
                </>
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleting}>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={(e) => {
                e.preventDefault();
                confirmBatchDelete();
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
