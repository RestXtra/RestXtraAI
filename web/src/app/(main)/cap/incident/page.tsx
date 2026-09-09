"use client";

import * as React from "react";

import { Loader2Icon, PlayIcon, PlusIcon, SirenIcon, Trash2Icon } from "lucide-react";
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
import { Textarea } from "@/components/ui/textarea";
import { api } from "@/lib/api";
import type { Incident } from "@/lib/types";

const SEVERITY_LABEL: Record<string, { text: string; variant: "secondary" | "warning" | "destructive" | "default" }> = {
  low: { text: "低", variant: "secondary" },
  medium: { text: "中", variant: "warning" },
  high: { text: "高", variant: "destructive" },
  critical: { text: "严重", variant: "destructive" },
};

const STATUS_LABEL: Record<string, { text: string; variant: "secondary" | "warning" | "success" | "default" }> = {
  new: { text: "新事件", variant: "secondary" },
  triaging: { text: "排查中", variant: "warning" },
  contained: { text: "已遏制", variant: "success" },
  resolved: { text: "已解决", variant: "success" },
  closed_false_positive: { text: "误报", variant: "secondary" },
};

function SeveBadge({ s }: { s: string }) {
  const l = SEVERITY_LABEL[s] ?? { text: s, variant: "secondary" as const };
  return <Badge variant={l.variant}>{l.text}</Badge>;
}

function StatusBadge({ s }: { s: string }) {
  const l = STATUS_LABEL[s] ?? { text: s, variant: "secondary" as const };
  return <Badge variant={l.variant}>{l.text}</Badge>;
}

function fmtTime(ts: string) {
  return new Date(ts).toLocaleString("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

const emptyForm = {
  title: "",
  severity: "medium",
  source: "",
  alert_info: "",
  assets: "",
  iocs: "",
  notes: "",
};

function IncidentForm({
  open,
  onOpenChange,
  onSaved,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onSaved: () => void;
}) {
  const [f, setF] = React.useState(emptyForm);
  React.useEffect(() => {
    if (open) setF(emptyForm);
  }, [open]);
  const set = (k: string, v: unknown) => setF((p) => ({ ...p, [k]: v }));

  async function save() {
    if (!f.title.trim()) {
      toast.error("标题必填");
      return;
    }
    try {
      await api.createIncident({ ...f, title: f.title.trim() });
      toast.success("事件已创建");
      onOpenChange(false);
      onSaved();
    } catch (e) {
      toast.error(`创建失败：${(e as Error).message}`);
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>创建安全事件</DialogTitle>
          <DialogDescription>
            填写事件简报（已知告警 + 与运维/开发交流的零散信息），随后可触发应急响应。
          </DialogDescription>
        </DialogHeader>
        <div className="grid gap-3 py-2">
          <div className="grid grid-cols-3 gap-2">
            <div className="col-span-2 grid gap-2">
              <Label htmlFor="inc-title">标题</Label>
              <Input
                id="inc-title"
                placeholder="如：ERP 服务器疑似勒索前兆"
                value={f.title}
                onChange={(e) => set("title", e.target.value)}
              />
            </div>
            <div className="grid gap-2">
              <Label>严重级别</Label>
              <Select value={f.severity} onValueChange={(v) => set("severity", v)}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="low">低</SelectItem>
                  <SelectItem value="medium">中</SelectItem>
                  <SelectItem value="high">高</SelectItem>
                  <SelectItem value="critical">严重</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>
          <div className="grid gap-2">
            <Label htmlFor="inc-source">告警来源（可选）</Label>
            <Input
              id="inc-source"
              placeholder="SIEM / 工单 / 手工"
              value={f.source}
              onChange={(e) => set("source", e.target.value)}
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="inc-alert">已知告警信息</Label>
            <Textarea
              id="inc-alert"
              className="min-h-[64px]"
              placeholder="时间 / 类型 / 检测规则 / 关键观察…"
              value={f.alert_info}
              onChange={(e) => set("alert_info", e.target.value)}
            />
          </div>
          <div className="grid grid-cols-2 gap-2">
            <div className="grid gap-2">
              <Label htmlFor="inc-assets">受影响资产（逗号分隔）</Label>
              <Input
                id="inc-assets"
                placeholder="db-01,web-02"
                value={f.assets}
                onChange={(e) => set("assets", e.target.value)}
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="inc-iocs">IOC（逗号分隔）</Label>
              <Input
                id="inc-iocs"
                placeholder="1.2.3.4,evil.com"
                value={f.iocs}
                onChange={(e) => set("iocs", e.target.value)}
              />
            </div>
          </div>
          <div className="grid gap-2">
            <Label htmlFor="inc-notes">与运维/开发交流的零散信息</Label>
            <Textarea
              id="inc-notes"
              className="min-h-[64px]"
              placeholder="现象 / 近期变更 / 可疑账号 / 业务背景…"
              value={f.notes}
              onChange={(e) => set("notes", e.target.value)}
            />
          </div>
        </div>
        <DialogFooter>
          <DialogClose asChild>
            <Button variant="outline">取消</Button>
          </DialogClose>
          <Button onClick={save}>创建</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function IncidentCard({ inc, onChanged }: { inc: Incident; onChanged: () => void }) {
  const [responding, setResponding] = React.useState(false);
  const [deleteOpen, setDeleteOpen] = React.useState(false);

  async function respond() {
    setResponding(true);
    try {
      const r = await api.incidentRespond(inc.id);
      toast.success(`已触发应急响应，任务 #${r.task_id}`);
      onChanged();
    } catch (e) {
      toast.error(`触发失败：${(e as Error).message}`);
    } finally {
      setResponding(false);
    }
  }

  async function changeStatus(next: string) {
    try {
      await api.incidentUpdate(inc.id, { ...inc, status: next });
      toast.success("状态已更新");
      onChanged();
    } catch (e) {
      toast.error(`更新失败：${(e as Error).message}`);
    }
  }

  async function del() {
    try {
      await api.deleteIncident(inc.id);
      toast.success("已删除");
      onChanged();
    } catch (e) {
      toast.error(`删除失败：${(e as Error).message}`);
    }
  }

  return (
    <Card>
      <CardContent className="grid gap-2">
        <div className="flex items-start justify-between gap-2">
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <span className="truncate font-medium">{inc.title}</span>
              <SeveBadge s={inc.severity} />
              <StatusBadge s={inc.status} />
              {inc.source && <Badge variant="outline">{inc.source}</Badge>}
            </div>
            <div className="mt-1 flex flex-wrap gap-x-4 text-muted-foreground text-xs">
              <span>#{inc.id}</span>
              <span>{fmtTime(inc.created_at)}</span>
              {inc.task_id && <span>任务 #{inc.task_id}</span>}
            </div>
          </div>
          <div className="flex shrink-0 gap-1">
            <Button size="icon" variant="ghost" aria-label="删除" onClick={() => setDeleteOpen(true)}>
              <Trash2Icon className="text-destructive" />
            </Button>
          </div>
        </div>

        {inc.alert_info && (
          <p className="text-sm">
            <span className="text-muted-foreground">告警：</span>
            {inc.alert_info}
          </p>
        )}
        {inc.assets && (
          <p className="text-sm">
            <span className="text-muted-foreground">资产：</span>
            {inc.assets}
          </p>
        )}
        {inc.iocs && (
          <p className="text-sm">
            <span className="text-muted-foreground">IOC：</span>
            {inc.iocs}
          </p>
        )}
        {inc.notes && <p className="text-muted-foreground text-sm">备注：{inc.notes}</p>}

        <div className="flex flex-wrap items-center gap-2 border-t pt-2">
          <Button size="sm" disabled={responding || !!inc.task_id} onClick={respond}>
            {responding ? <Loader2Icon className="animate-spin" /> : <PlayIcon className="size-3.5" />} 触发应急响应
          </Button>
          <Select value={inc.status} onValueChange={changeStatus}>
            <SelectTrigger className="h-8 w-32">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="new">新事件</SelectItem>
              <SelectItem value="triaging">排查中</SelectItem>
              <SelectItem value="contained">已遏制</SelectItem>
              <SelectItem value="resolved">已解决</SelectItem>
              <SelectItem value="closed_false_positive">误报</SelectItem>
            </SelectContent>
          </Select>
        </div>
      </CardContent>

      <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除事件</AlertDialogTitle>
            <AlertDialogDescription>将永久删除事件「{inc.title}」，此操作不可撤销。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={(e) => {
                e.preventDefault();
                void del();
              }}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              确认删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Card>
  );
}

export default function IncidentPage() {
  const [items, setItems] = React.useState<Incident[]>([]);
  const [status, setStatus] = React.useState("");
  const [formOpen, setFormOpen] = React.useState(false);

  const load = React.useCallback(() => {
    api
      .incidents(status)
      .then(setItems)
      .catch(() => setItems([]));
  }, [status]);
  React.useEffect(load, [load]);

  return (
    <div className="flex flex-1 flex-col gap-4">
      <div className="flex items-center justify-between gap-4">
        <div className="flex items-center gap-2">
          <SirenIcon className="size-5 text-muted-foreground" />
          <h1 className="font-semibold text-xl tracking-tight">安全事件</h1>
          <Badge variant="secondary">{items.length}</Badge>
        </div>
        <Button size="sm" onClick={() => setFormOpen(true)}>
          <PlusIcon /> 创建事件
        </Button>
      </div>

      <p className="text-muted-foreground text-sm">
        事件简报（告警 + 零散信息）→ 触发应急响应：受管连接排查 → 遏制走人工审批（可验证）→ 全程审计。
      </p>

      <div className="flex items-center gap-2">
        <Select value={status} onValueChange={(v) => setStatus(v)}>
          <SelectTrigger className="w-40">
            <SelectValue placeholder="状态筛选" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="">全部</SelectItem>
            <SelectItem value="new">新事件</SelectItem>
            <SelectItem value="triaging">排查中</SelectItem>
            <SelectItem value="contained">已遏制</SelectItem>
            <SelectItem value="resolved">已解决</SelectItem>
            <SelectItem value="closed_false_positive">误报</SelectItem>
          </SelectContent>
        </Select>
      </div>

      <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-2">
        {items.length === 0 && (
          <Card className="md:col-span-2">
            <CardContent className="flex items-center justify-center py-14 text-muted-foreground text-sm">
              暂无事件，点击「创建事件」或通过外部 webhook 推送告警。
            </CardContent>
          </Card>
        )}
        {items.map((inc) => (
          <IncidentCard key={inc.id} inc={inc} onChanged={load} />
        ))}
      </div>

      <IncidentForm open={formOpen} onOpenChange={setFormOpen} onSaved={load} />
    </div>
  );
}
