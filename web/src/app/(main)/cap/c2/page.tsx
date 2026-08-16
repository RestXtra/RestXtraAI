"use client";

import * as React from "react";
import { PlusIcon, Trash2Icon, WebhookIcon } from "lucide-react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
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
            <Input id="c2-name" placeholder="例如：内网HTTP-C2" value={name} onChange={(e) => setName(e.target.value)} />
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

  const load = React.useCallback(() => {
    api.c2().then((r) => {
      setListeners(r.listeners ?? []);
      setSessions(r.sessions ?? []);
    }).catch(() => {
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
          <h1 className="text-xl font-semibold tracking-tight">C2</h1>
          <Badge variant="secondary">{sessions.length} 会话</Badge>
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
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <span className="truncate font-medium">{l.name}</span>
                  <Badge variant="outline" className="uppercase">{l.protocol}</Badge>
                  {l.enabled ? <Badge variant="secondary" className="text-emerald-600">运行</Badge> : <Badge variant="outline">停止</Badge>}
                </div>
                <code className="text-muted-foreground mt-0.5 block font-mono text-xs">{l.host}:{l.port}</code>
              </div>
              <Button size="icon" variant="outline" aria-label="删除监听器" onClick={() => removeListener(l)}>
                <Trash2Icon className="text-destructive" />
              </Button>
            </CardContent>
          </Card>
        ))}
      </div>

      <div>
        <h2 className="text-muted-foreground mb-2 text-sm font-medium">Beacon 会话</h2>
        <Card className="overflow-hidden py-0">
          <CardContent className="p-0">
            {sessions.length === 0 ? (
              <div className="text-muted-foreground py-12 text-center text-sm">
                暂无会话。beacon 心跳：<code className="font-mono">POST /api/c2/ingest</code>{' {"session_id, host"} '}
              </div>
            ) : (
              <table className="w-full text-sm">
                <thead className="bg-muted/50 text-muted-foreground text-xs">
                  <tr className="text-left">
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
                      <td className="px-3 py-2 font-mono text-xs">{s.session_id}</td>
                      <td className="px-3 py-2 font-mono text-xs">{s.host || "—"}</td>
                      <td className="px-3 py-2">
                        {s.status === "active" ? (
                          <Badge variant="secondary" className="text-emerald-600">活跃</Badge>
                        ) : (
                          <Badge variant="outline">{s.status}</Badge>
                        )}
                      </td>
                      <td className="text-muted-foreground px-3 py-2 text-xs">{fmt(s.last_seen)}</td>
                      <td className="px-3 py-2 text-right">
                        {s.status === "active" ? (
                          <Button size="sm" variant="outline" onClick={() => setStatus(s, "lost")}>标记丢失</Button>
                        ) : (
                          <Button size="sm" variant="outline" onClick={() => setStatus(s, "active")}>恢复</Button>
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
    </div>
  );
}
