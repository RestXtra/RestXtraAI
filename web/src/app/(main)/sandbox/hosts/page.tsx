"use client";

import * as React from "react";

import { Loader2Icon, PlusIcon, ServerIcon, Trash2Icon } from "lucide-react";
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
import { Textarea } from "@/components/ui/textarea";
import { api } from "@/lib/api";
import type { SandboxHost } from "@/lib/types";
import { cn } from "@/lib/utils";

type PingState = { ok: boolean; version?: string; api_version?: string; os?: string; arch?: string; error?: string };

function HostStatusBadge({ status }: { status?: PingState }) {
  if (!status) return null;
  if (status.ok) {
    return (
      <Badge variant="secondary" className="text-emerald-600">
        在线
      </Badge>
    );
  }
  return <Badge variant="destructive">离线</Badge>;
}

function HostStatusText({ busy, status }: { busy: boolean; status?: PingState }) {
  if (busy) return <Loader2Icon className="size-3.5 animate-spin" />;
  if (!status) return <>未检测</>;
  if (status.ok)
    return (
      <>
        Docker {status.version} · {status.os}/{status.arch}
      </>
    );
  return <span className="text-destructive">{status.error}</span>;
}

function HostFormDialog({ onSaved }: { onSaved: () => void }) {
  const [open, setOpen] = React.useState(false);
  const [name, setName] = React.useState("");
  const [addr, setAddr] = React.useState("tcp://127.0.0.1:2375");
  const [description, setDescription] = React.useState("");

  async function save() {
    if (!name.trim() || !addr.trim()) {
      toast.error("名称与 Docker 地址必填");
      return;
    }
    try {
      await api.saveSandboxHost({ name: name.trim(), addr: addr.trim(), description: description.trim() });
      toast.success("沙箱主机已保存");
      setName("");
      setAddr("tcp://127.0.0.1:2375");
      setDescription("");
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
          <PlusIcon /> 添加主机
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>添加沙箱主机</DialogTitle>
          <DialogDescription>注册一个 Docker daemon，沙箱容器将运行在它上面。</DialogDescription>
        </DialogHeader>
        <div className="grid gap-4 py-2">
          <div className="grid gap-2">
            <Label htmlFor="h-name">名称</Label>
            <Input id="h-name" placeholder="例如：本机 Docker" value={name} onChange={(e) => setName(e.target.value)} />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="h-addr">Docker 地址</Label>
            <Input
              id="h-addr"
              className="font-mono"
              placeholder="tcp://127.0.0.1:2375 · http(s)://host:2375 · unix:///var/run/docker.sock"
              value={addr}
              onChange={(e) => setAddr(e.target.value)}
            />
            <p className="text-muted-foreground text-xs">
              未启用 TLS 的远程 daemon 需在 dockerd 加 -H tcp://0.0.0.0:2375。
            </p>
          </div>
          <div className="grid gap-2">
            <Label htmlFor="h-desc">描述（可选）</Label>
            <Textarea id="h-desc" rows={2} value={description} onChange={(e) => setDescription(e.target.value)} />
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

export default function SandboxHostsPage() {
  const [hosts, setHosts] = React.useState<SandboxHost[]>([]);
  const [ping, setPing] = React.useState<Record<string, PingState>>({});
  const [busy, setBusy] = React.useState<Record<string, boolean>>({});

  const load = React.useCallback(() => {
    api
      .sandboxHosts()
      .then(setHosts)
      .catch(() => setHosts([]));
  }, []);
  React.useEffect(() => {
    load();
    const i = setInterval(load, 15000);
    return () => clearInterval(i);
  }, [load]);

  const pingHost = React.useCallback(async (h: SandboxHost) => {
    setBusy((b) => ({ ...b, [h.id]: true }));
    try {
      const r = await api.pingSandboxHost(h.id);
      setPing((p) => ({ ...p, [h.id]: r }));
    } catch (e) {
      setPing((p) => ({ ...p, [h.id]: { ok: false, error: (e as Error).message } }));
    } finally {
      setBusy((b) => ({ ...b, [h.id]: false }));
    }
  }, []);
  // 进入页面自动 ping 一次
  React.useEffect(() => {
    for (const host of hosts) void pingHost(host);
  }, [hosts, pingHost]);

  const remove = async (h: SandboxHost) => {
    try {
      await api.deleteSandboxHost(h.id);
      toast.success(`已删除主机：${h.name}`);
      load();
    } catch (e) {
      toast.error(`删除失败：${(e as Error).message}`);
    }
  };

  return (
    <div className="flex flex-1 flex-col gap-4 md:gap-6">
      <div className="flex items-center justify-between gap-4">
        <div className="flex items-center gap-2">
          <ServerIcon className="size-5 text-muted-foreground" />
          <h1 className="font-semibold text-xl tracking-tight">沙箱主机</h1>
          <Badge variant="secondary">{hosts.length}</Badge>
        </div>
        <HostFormDialog onSaved={load} />
      </div>
      <p className="text-muted-foreground text-sm">
        管理 Docker 沙箱主机。容器页在选定主机上创建/启停受管沙箱容器；出口范围页登记授权访问 scope。
      </p>

      <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
        {hosts.length === 0 && (
          <Card className="md:col-span-2 xl:col-span-3">
            <CardContent className="flex items-center justify-center py-16 text-muted-foreground text-sm">
              暂无沙箱主机，点击右上角「添加主机」注册一个 Docker daemon。
            </CardContent>
          </Card>
        )}
        {hosts.map((h) => {
          const p = ping[h.id];
          return (
            <Card key={h.id}>
              <CardContent className="grid gap-3">
                <div className="flex items-start justify-between gap-2">
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="truncate font-medium">{h.name}</span>
                      <HostStatusBadge status={p} />
                    </div>
                    <code className="mt-1 block truncate font-mono text-muted-foreground text-xs">{h.addr}</code>
                  </div>
                  <Button size="icon" variant="outline" aria-label="删除主机" onClick={() => remove(h)}>
                    <Trash2Icon className="text-destructive" />
                  </Button>
                </div>
                {h.description && <p className="text-muted-foreground text-xs">{h.description}</p>}
                <div className="flex items-center justify-between gap-2 border-t pt-2">
                  <span className="text-muted-foreground text-xs">
                    <HostStatusText busy={Boolean(busy[h.id])} status={p} />
                  </span>
                  <Button size="sm" variant="outline" disabled={busy[h.id]} onClick={() => pingHost(h)}>
                    {busy[h.id] ? <Loader2Icon className="animate-spin" /> : "测试连接"}
                  </Button>
                </div>
              </CardContent>
            </Card>
          );
        })}
      </div>
      <p className={cn("text-muted-foreground text-xs")}>
        主机状态每 15 秒自动刷新；也可手动「测试连接」。容器与出口范围见侧栏「沙箱管理」下另两页。
      </p>
    </div>
  );
}
