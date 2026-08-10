"use client";

import * as React from "react";

import { toast } from "sonner";
import { PlusIcon, KeyRoundIcon, ShieldIcon, Trash2Icon } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { PermissionGate } from "@/components/permission-gate";
import { api } from "@/lib/api";
import type { PlatformRole, PlatformUser } from "@/lib/types";
import { useCurrentUser } from "@/hooks/use-current-user";

function fmtTime(ts: string) {
  if (!ts) return "-";
  return new Date(ts).toLocaleString("zh-CN", { year: "numeric", month: "2-digit", day: "2-digit" });
}

export default function PlatformUsersPage() {
  const me = useCurrentUser();
  const [users, setUsers] = React.useState<PlatformUser[]>([]);
  const [roles, setRoles] = React.useState<PlatformRole[]>([]);
  const [loading, setLoading] = React.useState(true);

  const [createOpen, setCreateOpen] = React.useState(false);
  const [editOpen, setEditOpen] = React.useState(false);
  const [editUser, setEditUser] = React.useState<PlatformUser | null>(null);
  const [resetUser, setResetUser] = React.useState<PlatformUser | null>(null);

  const [form, setForm] = React.useState({ username: "", display_name: "", password: "", roles: [] as string[] });
  const [resetPwd, setResetPwd] = React.useState("");

  const load = React.useCallback(() => {
    Promise.all([api.platformUsers(), api.platformRoles()])
      .then(([u, r]) => {
        setUsers(u);
        setRoles(r);
      })
      .catch((e) => toast.error(e.message ?? "加载成员失败"))
      .finally(() => setLoading(false));
  }, []);

  React.useEffect(load, [load]);

  const roleName = (name: string) => roles.find((r) => r.name === name)?.name ?? name;
  const isSystem = (name: string) => roles.find((r) => r.name === name)?.is_system;

  async function createUser() {
    try {
      await api.createPlatformUser({ ...form, roles: form.roles });
      toast.success("成员已创建");
      setCreateOpen(false);
      setForm({ username: "", display_name: "", password: "", roles: [] });
      load();
    } catch (e) {
      toast.error((e as Error).message ?? "创建失败");
    }
  }

  async function toggleEnabled(u: PlatformUser, enabled: boolean) {
    try {
      await api.updatePlatformUser(u.id, { enabled });
      toast.success(enabled ? "已启用" : "已禁用");
      load();
    } catch (e) {
      toast.error((e as Error).message ?? "操作失败");
    }
  }

  async function saveRoles() {
    if (!editUser) return;
    try {
      await api.setUserRoles(editUser.id, form.roles);
      toast.success("角色已更新");
      setEditOpen(false);
      load();
    } catch (e) {
      toast.error((e as Error).message ?? "保存失败");
    }
  }

  async function resetPassword() {
    if (!resetUser) return;
    try {
      await api.resetUserPassword(resetUser.id, resetPwd);
      toast.success("密码已重置");
      setResetUser(null);
      setResetPwd("");
    } catch (e) {
      toast.error((e as Error).message ?? "重置失败");
    }
  }

  async function removeUser(u: PlatformUser) {
    if (!window.confirm(`确认删除成员 ${u.username}？`)) return;
    try {
      await api.deletePlatformUser(u.id);
      toast.success("成员已删除");
      load();
    } catch (e) {
      toast.error((e as Error).message ?? "删除失败");
    }
  }

  return (
    <PermissionGate perm="platform.user.read">
      <div className="space-y-6 p-4 md:p-6">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h1 className="text-2xl font-semibold tracking-tight">成员管理</h1>
            <p className="text-sm text-muted-foreground">平台登录账户与角色分配（RBAC）。</p>
          </div>
          {me.admin && (
            <Button onClick={() => setCreateOpen(true)}>
              <PlusIcon className="size-4" /> 新建成员
            </Button>
          )}
        </div>

        <div className="rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>用户名</TableHead>
                <TableHead>显示名</TableHead>
                <TableHead>角色</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>创建时间</TableHead>
                <TableHead className="text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading ? (
                <TableRow>
                  <TableCell colSpan={6} className="py-10 text-center text-muted-foreground">
                    加载中…
                  </TableCell>
                </TableRow>
              ) : users.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={6} className="py-10 text-center text-muted-foreground">
                    暂无成员
                  </TableCell>
                </TableRow>
              ) : (
                users.map((u) => (
                  <TableRow key={u.id}>
                    <TableCell className="font-medium">
                      {u.username}
                      {u.is_builtin && <Badge variant="secondary" className="ml-2">内置</Badge>}
                    </TableCell>
                    <TableCell>{u.display_name || "-"}</TableCell>
                    <TableCell>
                      <div className="flex flex-wrap gap-1">
                        {u.roles.length === 0 && <span className="text-muted-foreground text-sm">-</span>}
                        {u.roles.map((r) => (
                          <Badge key={r} variant={isSystem(r) ? "default" : "outline"}>
                            {roleName(r)}
                          </Badge>
                        ))}
                      </div>
                    </TableCell>
                    <TableCell>
                      {me.admin ? (
                        <Switch
                          checked={u.enabled}
                          disabled={u.is_builtin}
                          onCheckedChange={(v) => toggleEnabled(u, v)}
                          aria-label={`${u.username} 启用状态`}
                        />
                      ) : (
                        <Badge variant={u.enabled ? "success" : "secondary"}>{u.enabled ? "启用" : "禁用"}</Badge>
                      )}
                    </TableCell>
                    <TableCell className="text-muted-foreground">{fmtTime(u.created_at)}</TableCell>
                    <TableCell className="text-right">
                      {me.admin && (
                        <div className="flex justify-end gap-1">
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => {
                              setEditUser(u);
                              setForm({ username: u.username, display_name: u.display_name, password: "", roles: u.roles });
                              setEditOpen(true);
                            }}
                          >
                            <ShieldIcon className="size-4" /> 角色
                          </Button>
                          <Button variant="ghost" size="sm" onClick={() => setResetUser(u)}>
                            <KeyRoundIcon className="size-4" /> 重置密码
                          </Button>
                          {!u.is_builtin && me.username !== u.username && (
                            <Button variant="ghost" size="sm" onClick={() => removeUser(u)}>
                              <Trash2Icon className="size-4" />
                            </Button>
                          )}
                        </div>
                      )}
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </div>

        {/* 新建成员 */}
        <Dialog open={createOpen} onOpenChange={setCreateOpen}>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>新建成员</DialogTitle>
              <DialogDescription>创建平台登录账户并分配角色。</DialogDescription>
            </DialogHeader>
            <div className="space-y-4">
              <div className="space-y-1.5">
                <Label htmlFor="nu">用户名</Label>
                <Input id="nu" value={form.username} onChange={(e) => setForm({ ...form, username: e.target.value })} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="nd">显示名（可选）</Label>
                <Input id="nd" value={form.display_name} onChange={(e) => setForm({ ...form, display_name: e.target.value })} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="np">初始密码（至少 8 位）</Label>
                <Input id="np" type="password" value={form.password} onChange={(e) => setForm({ ...form, password: e.target.value })} />
              </div>
              <div className="space-y-1.5">
                <Label>角色</Label>
                <Select
                  value={form.roles[0] ?? ""}
                  onValueChange={(v) => setForm({ ...form, roles: v ? [v] : [] })}
                >
                  <SelectTrigger>
                    <SelectValue placeholder="选择角色" />
                  </SelectTrigger>
                  <SelectContent>
                    {roles.map((r) => (
                      <SelectItem key={r.id} value={r.name}>{r.name}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => setCreateOpen(false)}>取消</Button>
              <Button onClick={createUser} disabled={!form.username || form.password.length < 8}>
                创建
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        {/* 编辑角色 */}
        <Dialog open={editOpen} onOpenChange={setEditOpen}>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>成员角色 · {editUser?.username}</DialogTitle>
              <DialogDescription>替换该成员的角色绑定。</DialogDescription>
            </DialogHeader>
            <div className="space-y-1.5">
              <Label>角色</Label>
              <Select
                value={form.roles[0] ?? ""}
                onValueChange={(v) => setForm({ ...form, roles: v ? [v] : [] })}
              >
                <SelectTrigger>
                  <SelectValue placeholder="选择角色" />
                </SelectTrigger>
                <SelectContent>
                  {roles.map((r) => (
                    <SelectItem key={r.id} value={r.name}>{r.name}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => setEditOpen(false)}>取消</Button>
              <Button onClick={saveRoles}>保存</Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        {/* 重置密码 */}
        <Dialog open={!!resetUser} onOpenChange={(o) => { if (!o) setResetUser(null); }}>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>重置密码 · {resetUser?.username}</DialogTitle>
              <DialogDescription>输入新密码（至少 8 位）。</DialogDescription>
            </DialogHeader>
            <div className="space-y-1.5">
              <Label htmlFor="rp">新密码</Label>
              <Input id="rp" type="password" value={resetPwd} onChange={(e) => setResetPwd(e.target.value)} />
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => setResetUser(null)}>取消</Button>
              <Button onClick={resetPassword} disabled={resetPwd.length < 8}>重置</Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>
    </PermissionGate>
  );
}
