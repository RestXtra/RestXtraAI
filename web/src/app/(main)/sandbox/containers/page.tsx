"use client";

import * as React from "react";
import { BoxesIcon, Loader2Icon, PlayIcon, PlusIcon, RotateCwIcon, SquareIcon, Trash2Icon } from "lucide-react";
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
import { Switch } from "@/components/ui/switch";
import { api } from "@/lib/api";
import type { SandboxContainer, SandboxHost, DockerImage } from "@/lib/types";

function fmtSize(n: number): string {
  if (n >= 1e9) return `${(n / 1e9).toFixed(1)}GB`;
  if (n >= 1e6) return `${(n / 1e6).toFixed(0)}MB`;
  return `${(n / 1e3).toFixed(0)}KB`;
}

function fmtTime(unix: number): string {
  if (!unix) return "—";
  const d = new Date(unix * 1000);
  return d.toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}

function CreateContainerDialog({
  hostId,
  images,
  onCreated,
}: {
  hostId: string;
  images: DockerImage[];
  onCreated: () => void;
}) {
  const [open, setOpen] = React.useState(false);
  const [image, setImage] = React.useState("");
  const [name, setName] = React.useState("");
  const [memory, setMemory] = React.useState("4096");
  const [cpus, setCpus] = React.useState("2");
  const [pids, setPids] = React.useState("512");
  const [network, setNetwork] = React.useState("bridge");
  const [readOnly, setReadOnly] = React.useState(true);
  const [capDropAll, setCapDropAll] = React.useState(true);
  const [managed, setManaged] = React.useState(true);
  const [autoStart, setAutoStart] = React.useState(true);

  const imageOptions = images.flatMap((im) => im.RepoTags ?? []);

  async function create() {
    if (!image.trim()) {
      toast.error("请选择镜像");
      return;
    }
    try {
      await api.createSandboxContainer(hostId, {
        name: name.trim() || undefined,
        image: image.trim(),
        memory_mb: Math.max(0, Number(memory) || 0),
        cpus: Number(cpus) || 0,
        pids_limit: Math.max(0, Number(pids) || 0),
        network_mode: network,
        read_only: readOnly,
        cap_drop_all: capDropAll,
        managed,
        auto_start: autoStart,
      });
      toast.success("沙箱容器已创建");
      setImage("");
      setName("");
      setOpen(false);
      onCreated();
    } catch (e) {
      toast.error(`创建失败：${(e as Error).message}`);
    }
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size="sm">
          <PlusIcon /> 创建容器
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>创建沙箱容器</DialogTitle>
          <DialogDescription>默认只读根 + cap-drop ALL + 资源限额，勾选「受管」会打 sandbox.managed 标签。</DialogDescription>
        </DialogHeader>
        <div className="grid gap-4 py-2">
          <div className="grid gap-2">
            <Label htmlFor="c-image">镜像</Label>
            <Select value={image} onValueChange={setImage}>
              <SelectTrigger id="c-image">
                <SelectValue placeholder="选择镜像" />
              </SelectTrigger>
              <SelectContent>
                {imageOptions.length === 0 && <div className="px-2 py-1 text-xs text-muted-foreground">该主机暂无镜像</div>}
                {imageOptions.map((t) => (
                  <SelectItem key={t} value={t}>
                    {t}
                  </SelectItem>
                ))}
                <SelectItem value={image ? image : "__manual__"} className="hidden" />
              </SelectContent>
            </Select>
            {!imageOptions.includes(image) && image && <p className="text-muted-foreground text-xs">将使用手动输入的镜像：{image}</p>}
          </div>
          <div className="grid gap-2">
            <Label htmlFor="c-name">容器名（可选）</Label>
            <Input id="c-name" placeholder="留空则 Docker 随机命名" value={name} onChange={(e) => setName(e.target.value)} />
          </div>
          <div className="grid grid-cols-3 gap-2">
            <div className="grid gap-1.5">
              <Label className="text-xs">内存(MB)</Label>
              <Input type="number" value={memory} onChange={(e) => setMemory(e.target.value)} />
            </div>
            <div className="grid gap-1.5">
              <Label className="text-xs">CPU</Label>
              <Input type="number" step={0.5} value={cpus} onChange={(e) => setCpus(e.target.value)} />
            </div>
            <div className="grid gap-1.5">
              <Label className="text-xs">Pids 上限</Label>
              <Input type="number" value={pids} onChange={(e) => setPids(e.target.value)} />
            </div>
          </div>
          <div className="grid gap-2">
            <Label>网络模式</Label>
            <Select value={network} onValueChange={setNetwork}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="bridge">bridge（默认）</SelectItem>
                <SelectItem value="host">host（共享宿主机网络）</SelectItem>
                <SelectItem value="none">none（无网络）</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="grid gap-2">
            <div className="flex items-center justify-between">
              <Label className="text-sm">只读根文件系统</Label>
              <Switch checked={readOnly} onCheckedChange={setReadOnly} />
            </div>
            <div className="flex items-center justify-between">
              <Label className="text-sm">cap-drop ALL</Label>
              <Switch checked={capDropAll} onCheckedChange={setCapDropAll} />
            </div>
            <div className="flex items-center justify-between">
              <Label className="text-sm">受管（sandbox.managed 标签）</Label>
              <Switch checked={managed} onCheckedChange={setManaged} />
            </div>
            <div className="flex items-center justify-between">
              <Label className="text-sm">创建后自动启动</Label>
              <Switch checked={autoStart} onCheckedChange={setAutoStart} />
            </div>
          </div>
        </div>
        <DialogFooter>
          <DialogClose asChild>
            <Button variant="outline">取消</Button>
          </DialogClose>
          <Button onClick={create}>创建</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export default function SandboxContainersPage() {
  const [hosts, setHosts] = React.useState<SandboxHost[]>([]);
  const [hostId, setHostId] = React.useState("");
  const [containers, setContainers] = React.useState<SandboxContainer[]>([]);
  const [images, setImages] = React.useState<DockerImage[]>([]);
  const [managedOnly, setManagedOnly] = React.useState(false);
  const [loading, setLoading] = React.useState(false);
  const [acting, setActing] = React.useState<string | null>(null);

  React.useEffect(() => {
    api.sandboxHosts().then(setHosts).catch(() => setHosts([]));
  }, []);
  // 默认选中第一个主机
  React.useEffect(() => {
    if (!hostId && hosts.length) setHostId(hosts[0].id);
  }, [hosts, hostId]);

  const load = React.useCallback(() => {
    if (!hostId) return;
    setLoading(true);
    api
      .sandboxContainers(hostId, { all: true, managed: managedOnly })
      .then((r) => setContainers(r.containers ?? []))
      .catch(() => setContainers([]))
      .finally(() => setLoading(false));
    api.sandboxImages(hostId).then((r) => setImages(r.images ?? [])).catch(() => setImages([]));
  }, [hostId, managedOnly]);

  React.useEffect(() => {
    load();
  }, [load]);

  const act = async (cid: string, action: "start" | "stop" | "restart" | "remove") => {
    setActing(cid);
    try {
      await api.sandboxContainerAction(hostId, cid, action);
      toast.success(`已${action === "start" ? "启动" : action === "stop" ? "停止" : action === "restart" ? "重启" : "删除"}`);
      load();
    } catch (e) {
      toast.error(`${action} 失败：${(e as Error).message}`);
    } finally {
      setActing(null);
    }
  };

  return (
    <div className="flex flex-1 flex-col gap-4 md:gap-6">
      <div className="flex flex-wrap items-center gap-2">
        <BoxesIcon className="size-5 text-muted-foreground" />
        <h1 className="text-xl font-semibold tracking-tight">沙箱容器</h1>
        <Select value={hostId} onValueChange={setHostId}>
          <SelectTrigger className="w-56">
            <SelectValue placeholder="选择沙箱主机" />
          </SelectTrigger>
          <SelectContent>
            {hosts.map((h) => (
              <SelectItem key={h.id} value={h.id}>
                {h.name} · {h.addr}
              </SelectItem>
            ))}
            {hosts.length === 0 && <div className="px-2 py-1 text-xs text-muted-foreground">请先在「沙箱主机」添加主机</div>}
          </SelectContent>
        </Select>
        <label className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <Switch size="sm" checked={managedOnly} onCheckedChange={setManagedOnly} />
          仅看受管容器
        </label>
        {hostId && <CreateContainerDialog hostId={hostId} images={images} onCreated={load} />}
      </div>

      <Card className="overflow-hidden py-0">
        <div className="max-h-[70vh] overflow-auto">
          <table className="w-full text-sm">
            <thead className="bg-muted/50 sticky top-0">
              <tr className="text-left text-muted-foreground text-xs">
                <th className="px-3 py-2 font-medium">名称</th>
                <th className="px-3 py-2 font-medium">镜像</th>
                <th className="px-3 py-2 font-medium">状态</th>
                <th className="px-3 py-2 font-medium">创建时间</th>
                <th className="px-3 py-2 font-medium">受管</th>
                <th className="px-3 py-2 text-right font-medium">操作</th>
              </tr>
            </thead>
            <tbody>
              {loading && containers.length === 0 ? (
                <tr>
                  <td colSpan={6} className="py-12 text-center">
                    <Loader2Icon className="mx-auto size-5 animate-spin text-muted-foreground" />
                  </td>
                </tr>
              ) : containers.length === 0 ? (
                <tr>
                  <td colSpan={6} className="text-muted-foreground py-12 text-center text-sm">
                    该主机暂无容器{managedOnly ? "（受管）" : ""}，点击「创建容器」。
                  </td>
                </tr>
              ) : (
                containers.map((c) => {
                  const name = (c.Names?.[0] ?? c.Id).replace(/^\//, "");
                  const isManaged = !!c.Labels?.["sandbox.managed"];
                  const running = c.State === "running";
                  return (
                    <tr key={c.Id} className="border-t">
                      <td className="max-w-[240px] px-3 py-2">
                        <code className="block truncate font-mono text-xs">{name}</code>
                        <span className="text-muted-foreground text-[11px]">{c.Id.slice(0, 12)}</span>
                      </td>
                      <td className="max-w-[200px] truncate px-3 py-2 font-mono text-xs">{c.Image}</td>
                      <td className="px-3 py-2">
                        {running ? (
                          <Badge variant="secondary" className="text-emerald-600">
                            运行中
                          </Badge>
                        ) : (
                          <Badge variant="outline">{c.State === "exited" ? "已停止" : c.State || "—"}</Badge>
                        )}
                        <div className="text-muted-foreground text-[11px]">{c.Status}</div>
                      </td>
                      <td className="text-muted-foreground px-3 py-2 text-xs">{fmtTime(c.Created)}</td>
                      <td className="px-3 py-2">
                        {isManaged ? <Badge variant="outline">sandbox</Badge> : <span className="text-muted-foreground text-xs">—</span>}
                      </td>
                      <td className="px-3 py-2 text-right">
                        <div className="flex items-center justify-end gap-1">
                          <Button size="icon" variant="ghost" disabled={acting === c.Id || running} onClick={() => act(c.Id, "start")} aria-label="启动">
                            <PlayIcon />
                          </Button>
                          <Button size="icon" variant="ghost" disabled={acting === c.Id || !running} onClick={() => act(c.Id, "stop")} aria-label="停止">
                            <SquareIcon />
                          </Button>
                          <Button size="icon" variant="ghost" disabled={acting === c.Id} onClick={() => act(c.Id, "restart")} aria-label="重启">
                            <RotateCwIcon />
                          </Button>
                          <Button size="icon" variant="ghost" disabled={acting === c.Id} onClick={() => act(c.Id, "remove")} aria-label="删除">
                            <Trash2Icon className="text-destructive" />
                          </Button>
                        </div>
                      </td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>
      </Card>
    </div>
  );
}
