"use client";

import * as React from "react";
import { Loader2Icon, PlusIcon, ShieldCheckIcon, Trash2Icon } from "lucide-react";
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
import type { SandboxEgress } from "@/lib/types";

function EgressFormDialog({ onSaved }: { onSaved: () => void }) {
  const [open, setOpen] = React.useState(false);
  const [kind, setKind] = React.useState<"cidr" | "domain">("cidr");
  const [value, setValue] = React.useState("");
  const [action, setAction] = React.useState<"allow" | "deny">("allow");
  const [note, setNote] = React.useState("");

  async function save() {
    if (!value.trim()) {
      toast.error("请输入 CIDR 或域名");
      return;
    }
    try {
      await api.saveSandboxEgress({ kind, value: value.trim(), action, note: note.trim(), enabled: true });
      toast.success("出口规则已保存");
      setValue("");
      setNote("");
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
          <PlusIcon /> 添加规则
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>添加出口范围规则</DialogTitle>
          <DialogDescription>
            登记沙箱容器允许/拒绝访问的 CIDR 或域名（出口授权 scope）。
          </DialogDescription>
        </DialogHeader>
        <div className="grid gap-4 py-2">
          <div className="grid grid-cols-2 gap-2">
            <div className="grid gap-2">
              <Label>类型</Label>
              <Select value={kind} onValueChange={(v) => setKind(v as "cidr" | "domain")}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="cidr">CIDR</SelectItem>
                  <SelectItem value="domain">域名</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="grid gap-2">
              <Label>动作</Label>
              <Select value={action} onValueChange={(v) => setAction(v as "allow" | "deny")}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="allow">允许 allow</SelectItem>
                  <SelectItem value="deny">拒绝 deny</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>
          <div className="grid gap-2">
            <Label htmlFor="e-value">{kind === "cidr" ? "CIDR" : "域名"}</Label>
            <Input
              id="e-value"
              className="font-mono"
              placeholder={kind === "cidr" ? "例如 10.0.0.0/24 或 0.0.0.0/0" : "例如 *.example.com 或 api.example.com"}
              value={value}
              onChange={(e) => setValue(e.target.value)}
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="e-note">备注（可选）</Label>
            <Input id="e-note" placeholder="例如：内网靶场网段" value={note} onChange={(e) => setNote(e.target.value)} />
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

export default function SandboxEgressPage() {
  const [rules, setRules] = React.useState<SandboxEgress[]>([]);
  const [loading, setLoading] = React.useState(false);

  const load = React.useCallback(() => {
    setLoading(true);
    api.sandboxEgress().then(setRules).catch(() => setRules([])).finally(() => setLoading(false));
  }, []);
  React.useEffect(() => {
    load();
  }, [load]);

  const toggle = async (r: SandboxEgress, enabled: boolean) => {
    try {
      await api.saveSandboxEgress({ id: Number(r.id), kind: r.kind, value: r.value, action: r.action, note: r.note, enabled });
      load();
    } catch (e) {
      toast.error(`更新失败：${(e as Error).message}`);
    }
  };

  const remove = async (r: SandboxEgress) => {
    try {
      await api.deleteSandboxEgress(r.id);
      toast.success("规则已删除");
      load();
    } catch (e) {
      toast.error(`删除失败：${(e as Error).message}`);
    }
  };

  return (
    <div className="flex flex-1 flex-col gap-4 md:gap-6">
      <div className="flex items-center justify-between gap-4">
        <div className="flex items-center gap-2">
          <ShieldCheckIcon className="size-5 text-muted-foreground" />
          <h1 className="text-xl font-semibold tracking-tight">出口范围</h1>
          <Badge variant="secondary">{rules.length}</Badge>
        </div>
        <EgressFormDialog onSaved={load} />
      </div>
      <p className="text-muted-foreground text-sm">
        授权 scope：沙箱容器可访问的 CIDR / 域名白名单（deny 优先）。用于界定沙箱的合法出口目标。
      </p>

      <Card className="overflow-hidden py-0">
        <CardContent className="p-0">
          {loading ? (
            <div className="py-12 text-center">
              <Loader2Icon className="mx-auto size-5 animate-spin text-muted-foreground" />
            </div>
          ) : rules.length === 0 ? (
            <div className="text-muted-foreground py-12 text-center text-sm">暂无出口规则，点击「添加规则」。</div>
          ) : (
            <table className="w-full text-sm">
              <thead className="bg-muted/50 text-muted-foreground text-xs">
                <tr className="text-left">
                  <th className="px-3 py-2 font-medium">类型</th>
                  <th className="px-3 py-2 font-medium">值</th>
                  <th className="px-3 py-2 font-medium">动作</th>
                  <th className="px-3 py-2 font-medium">备注</th>
                  <th className="px-3 py-2 font-medium">启用</th>
                  <th className="px-3 py-2 text-right font-medium">操作</th>
                </tr>
              </thead>
              <tbody>
                {rules.map((r) => (
                  <tr key={r.id} className="border-t">
                    <td className="px-3 py-2">
                      <Badge variant="outline">{r.kind === "cidr" ? "CIDR" : "域名"}</Badge>
                    </td>
                    <td className="px-3 py-2 font-mono text-xs">{r.value}</td>
                    <td className="px-3 py-2">
                      {r.action === "allow" ? (
                        <Badge variant="secondary" className="text-emerald-600">
                          允许
                        </Badge>
                      ) : (
                        <Badge variant="destructive">拒绝</Badge>
                      )}
                    </td>
                    <td className="text-muted-foreground px-3 py-2 text-xs">{r.note || "—"}</td>
                    <td className="px-3 py-2">
                      <Switch size="sm" checked={r.enabled} onCheckedChange={(v) => toggle(r, v)} />
                    </td>
                    <td className="px-3 py-2 text-right">
                      <Button size="icon" variant="ghost" aria-label="删除规则" onClick={() => remove(r)}>
                        <Trash2Icon className="text-destructive" />
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
