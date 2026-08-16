"use client";

import * as React from "react";
import { Loader2Icon, PlusIcon, TerminalIcon, Trash2Icon } from "lucide-react";
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
import type { WebshellConn } from "@/lib/types";

function ConnForm({ onSaved }: { onSaved: () => void }) {
  const [open, setOpen] = React.useState(false);
  const [name, setName] = React.useState("");
  const [url, setUrl] = React.useState("");
  const [type, setType] = React.useState("php");
  const [password, setPassword] = React.useState("");
  const [note, setNote] = React.useState("");

  async function save() {
    if (!name.trim() || !url.trim()) {
      toast.error("名称与 URL 必填");
      return;
    }
    try {
      await api.saveWebshell({ name: name.trim(), url: url.trim(), type, password, note });
      toast.success("已保存");
      setName("");
      setUrl("");
      setPassword("");
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
          <PlusIcon /> 添加连接
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>添加 WebShell 连接</DialogTitle>
          <DialogDescription>登记目标上的 webshell，可一键测试连通性。</DialogDescription>
        </DialogHeader>
        <div className="grid gap-3 py-2">
          <div className="grid grid-cols-2 gap-2">
            <div className="grid gap-2">
              <Label htmlFor="ws-name">名称</Label>
              <Input id="ws-name" placeholder="例如：目标-A-php" value={name} onChange={(e) => setName(e.target.value)} />
            </div>
            <div className="grid gap-2">
              <Label>类型</Label>
              <Select value={type} onValueChange={setType}>
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
            <Label htmlFor="ws-url">URL</Label>
            <Input id="ws-url" className="font-mono" placeholder="https://target/shell.php" value={url} onChange={(e) => setUrl(e.target.value)} />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="ws-pwd">连接密码（可选）</Label>
            <Input id="ws-pwd" placeholder="pwd 参数" value={password} onChange={(e) => setPassword(e.target.value)} />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="ws-note">备注（可选）</Label>
            <Input id="ws-note" value={note} onChange={(e) => setNote(e.target.value)} />
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

export default function WebshellPage() {
  const [conns, setConns] = React.useState<WebshellConn[]>([]);
  const [busy, setBusy] = React.useState<Record<string, boolean>>({});
  const [result, setResult] = React.useState<Record<string, { ok: boolean; snippet?: string; error?: string }>>({});

  const load = React.useCallback(() => {
    api.webshells().then(setConns).catch(() => setConns([]));
  }, []);
  React.useEffect(() => {
    load();
  }, [load]);

  const test = async (c: WebshellConn) => {
    setBusy((b) => ({ ...b, [c.id]: true }));
    try {
      const r = await api.webshellTest(c);
      setResult((m) => ({ ...m, [c.id]: r }));
    } catch (e) {
      setResult((m) => ({ ...m, [c.id]: { ok: false, error: (e as Error).message } }));
    } finally {
      setBusy((b) => ({ ...b, [c.id]: false }));
    }
  };

  const remove = async (c: WebshellConn) => {
    try {
      await api.deleteWebshell(c.id);
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
          <h1 className="text-xl font-semibold tracking-tight">WebShell</h1>
          <Badge variant="secondary">{conns.length}</Badge>
        </div>
        <ConnForm onSaved={load} />
      </div>
      <p className="text-muted-foreground text-sm">登记目标上的 webshell 连接，测试连通性。执行命令需人工在目标上配合（连接管理）。</p>

      <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
        {conns.length === 0 && (
          <Card className="md:col-span-2 xl:col-span-3">
            <CardContent className="flex items-center justify-center py-14 text-muted-foreground text-sm">
              暂无 webshell 连接，点击「添加连接」。
            </CardContent>
          </Card>
        )}
        {conns.map((c) => {
          const r = result[c.id];
          return (
            <Card key={c.id}>
              <CardContent className="grid gap-2">
                <div className="flex items-start justify-between gap-2">
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="truncate font-medium">{c.name}</span>
                      <Badge variant="outline" className="uppercase">{c.type}</Badge>
                      {c.enabled && <Switch size="sm" checked disabled />}
                    </div>
                    <code className="mt-0.5 block truncate font-mono text-xs text-muted-foreground">{c.url}</code>
                  </div>
                  <Button size="icon" variant="outline" aria-label="删除" onClick={() => remove(c)}>
                    <Trash2Icon className="text-destructive" />
                  </Button>
                </div>
                <div className="flex items-center justify-between gap-2 border-t pt-2">
                  <span className="min-w-0 flex-1 truncate text-xs">
                    {busy[c.id] ? (
                      <Loader2Icon className="animate-spin" />
                    ) : r ? (
                      r.ok ? (
                        <span className="text-emerald-600">连通 ✓ {r.snippet}</span>
                      ) : (
                        <span className="text-destructive">{r.error || r.snippet || "失败"}</span>
                      )
                    ) : (
                      <span className="text-muted-foreground">未测试</span>
                    )}
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
    </div>
  );
}
