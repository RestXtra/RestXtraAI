"use client";

import * as React from "react";

import { PlusIcon, ShieldCheckIcon, Trash2Icon } from "lucide-react";
import { toast } from "sonner";

import { PermissionGate } from "@/components/permission-gate";
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
import { Checkbox } from "@/components/ui/checkbox";
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
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useCurrentUser } from "@/hooks/use-current-user";
import { api } from "@/lib/api";
import type { PermissionPoint, PlatformRole } from "@/lib/types";

// groupKey extracts the catalog group from a permission key ("platform.user.read" → "platform").
function groupKey(key: string): string {
  const i = key.indexOf(".");
  return i > 0 ? key.slice(0, i) : key;
}

const GROUP_LABEL: Record<string, string> = {
  platform: "平台管理",
  sec: "安全边界",
  cap: "能力",
  sandbox: "沙箱",
  worklog: "工作日志",
  agent: "Agent 管理",
  knowledge: "知识库",
  playbook: "攻击模式库",
  batch: "批量任务",
  task: "工作台",
};

export default function PlatformRolesPage() {
  const me = useCurrentUser();
  const [roles, setRoles] = React.useState<PlatformRole[]>([]);
  const [catalog, setCatalog] = React.useState<PermissionPoint[]>([]);
  const [loading, setLoading] = React.useState(true);

  const [createOpen, setCreateOpen] = React.useState(false);
  const [permRole, setPermRole] = React.useState<PlatformRole | null>(null);
  const [permKeys, setPermKeys] = React.useState<string[]>([]);
  const [form, setForm] = React.useState({ name: "", description: "", scope: "own" });

  // batch selection & delete
  const [checked, setChecked] = React.useState<Set<number>>(new Set());
  const [deleteAll, setDeleteAll] = React.useState(false);
  const [deleteOpen, setDeleteOpen] = React.useState(false);
  const [deleting, setDeleting] = React.useState(false);

  const toggleCheck = (id: number) => {
    setChecked((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const toggleCheckAll = (ids: number[]) => {
    setChecked((prev) => {
      const allSelected = ids.length > 0 && ids.every((id) => prev.has(id));
      const next = new Set(prev);
      if (allSelected) ids.forEach((id) => next.delete(id));
      else ids.forEach((id) => next.add(id));
      return next;
    });
  };

  const deleteableIds = React.useMemo(() => roles.filter((r) => !r.is_system).map((r) => r.id), [roles]);

  const confirmBatchDelete = async () => {
    setDeleting(true);
    try {
      const res = await api.deletePlatformRoles(Array.from(checked), deleteAll);
      const msg = res.skipped?.length
        ? `已删除 ${res.deleted.length} 个角色；跳过：${res.skipped.join("、")}`
        : `已删除 ${res.deleted.length} 个角色`;
      toast.success(msg);
      setChecked(new Set());
      setDeleteOpen(false);
      load();
    } catch (e) {
      toast.error((e as Error).message ?? "删除失败");
      setDeleteOpen(false);
    } finally {
      setDeleting(false);
    }
  };

  const load = React.useCallback(() => {
    Promise.all([api.platformRoles(), api.platformPermissions()])
      .then(([r, c]) => {
        setRoles(r);
        setCatalog(c);
      })
      .catch((e) => toast.error(e.message ?? "加载角色失败"))
      .finally(() => setLoading(false));
  }, []);

  React.useEffect(load, [load]);

  // grouped catalog for the permission editor
  const grouped = React.useMemo(() => {
    const m = new Map<string, PermissionPoint[]>();
    for (const p of catalog) {
      const g = groupKey(p.key);
      if (!m.has(g)) m.set(g, []);
      m.get(g)?.push(p);
    }
    return [...m.entries()];
  }, [catalog]);

  async function createRole() {
    try {
      await api.createPlatformRole({ ...form, permissions: [] });
      toast.success("角色已创建");
      setCreateOpen(false);
      setForm({ name: "", description: "", scope: "own" });
      load();
    } catch (e) {
      toast.error((e as Error).message ?? "创建失败");
    }
  }

  async function openPerms(r: PlatformRole) {
    setPermRole(r);
    try {
      const keys = await api.rolePermissions(r.id);
      setPermKeys(keys);
    } catch {
      setPermKeys([]);
    }
  }

  async function savePerms() {
    if (!permRole) return;
    try {
      await api.setRolePermissions(permRole.id, permKeys);
      toast.success("权限已保存");
      setPermRole(null);
      load();
    } catch (e) {
      toast.error((e as Error).message ?? "保存失败");
    }
  }

  async function removeRole(r: PlatformRole) {
    if (!window.confirm(`确认删除角色 ${r.name}？`)) return;
    try {
      await api.deletePlatformRole(r.id);
      toast.success("角色已删除");
      load();
    } catch (e) {
      toast.error((e as Error).message ?? "删除失败");
    }
  }

  const canWrite = me.admin;

  return (
    <PermissionGate perm="platform.role.read">
      <div className="space-y-6 p-4 md:p-6">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h1 className="font-semibold text-2xl tracking-tight">平台角色</h1>
            <p className="text-muted-foreground text-sm">RBAC 角色与权限点绑定；系统角色不可删除。</p>
          </div>
          {canWrite && (
            <div className="flex items-center gap-2">
              {checked.size > 0 && (
                <>
                  <Button
                    variant="destructive"
                    onClick={() => {
                      setDeleteAll(false);
                      setDeleteOpen(true);
                    }}
                  >
                    <Trash2Icon className="size-4" /> 删除已选 ({checked.size})
                  </Button>
                  <Button
                    variant="outline"
                    className="text-destructive hover:text-destructive"
                    onClick={() => {
                      setDeleteAll(true);
                      setDeleteOpen(true);
                    }}
                  >
                    <Trash2Icon className="size-4" /> 删除全部
                  </Button>
                </>
              )}
              <Button onClick={() => setCreateOpen(true)}>
                <PlusIcon className="size-4" /> 新建角色
              </Button>
            </div>
          )}
        </div>

        <div className="rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-8 pr-0">
                  {canWrite && (
                    <Checkbox
                      checked={deleteableIds.length > 0 && deleteableIds.every((id) => checked.has(id))}
                      onCheckedChange={() => toggleCheckAll(deleteableIds)}
                      aria-label="全选"
                    />
                  )}
                </TableHead>
                <TableHead>角色</TableHead>
                <TableHead>说明</TableHead>
                <TableHead>资源范围</TableHead>
                <TableHead>权限数</TableHead>
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
              ) : roles.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={6} className="py-10 text-center text-muted-foreground">
                    暂无角色
                  </TableCell>
                </TableRow>
              ) : (
                roles.map((r) => (
                  <TableRow key={r.id}>
                    <TableCell className="w-8 pr-0">
                      {canWrite && !r.is_system && (
                        <Checkbox
                          checked={checked.has(r.id)}
                          onCheckedChange={() => toggleCheck(r.id)}
                          aria-label={`选择 ${r.name}`}
                        />
                      )}
                    </TableCell>
                    <TableCell className="font-medium">
                      {r.name}
                      {r.is_system && (
                        <Badge variant="secondary" className="ml-2">
                          系统
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell className="max-w-xs truncate text-muted-foreground">{r.description || "-"}</TableCell>
                    <TableCell>
                      <Badge variant="outline">{r.scope}</Badge>
                    </TableCell>
                    <TableCell>{r.perm_count}</TableCell>
                    <TableCell className="text-right">
                      {canWrite ? (
                        <div className="flex justify-end gap-1">
                          <Button variant="ghost" size="sm" onClick={() => openPerms(r)}>
                            <ShieldCheckIcon className="size-4" /> 权限
                          </Button>
                          {!r.is_system && (
                            <Button variant="ghost" size="sm" onClick={() => removeRole(r)}>
                              <Trash2Icon className="size-4" />
                            </Button>
                          )}
                        </div>
                      ) : (
                        <Button variant="ghost" size="sm" onClick={() => openPerms(r)} disabled={false}>
                          查看
                        </Button>
                      )}
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </div>

        {/* 新建角色 */}
        <Dialog open={createOpen} onOpenChange={setCreateOpen}>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>新建角色</DialogTitle>
              <DialogDescription>创建后将进入权限编辑器进行绑定。</DialogDescription>
            </DialogHeader>
            <div className="space-y-4">
              <div className="space-y-1.5">
                <Label htmlFor="rn">角色名</Label>
                <Input id="rn" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="rd">说明</Label>
                <Input
                  id="rd"
                  value={form.description}
                  onChange={(e) => setForm({ ...form, description: e.target.value })}
                />
              </div>
              <div className="space-y-1.5">
                <Label>资源范围</Label>
                <Select value={form.scope} onValueChange={(v) => setForm({ ...form, scope: v })}>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="all">all · 全部资源</SelectItem>
                    <SelectItem value="assigned">assigned · 授权资源</SelectItem>
                    <SelectItem value="own">own · 仅本人资源</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => setCreateOpen(false)}>
                取消
              </Button>
              <Button onClick={createRole} disabled={!form.name.trim()}>
                创建
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        {/* 权限编辑器 */}
        <Dialog
          open={!!permRole}
          onOpenChange={(o) => {
            if (!o) setPermRole(null);
          }}
        >
          <DialogContent className="sm:max-w-xl">
            <DialogHeader>
              <DialogTitle>角色权限 · {permRole?.name}</DialogTitle>
              <DialogDescription>勾选该角色允许的权限点（admin 角色自动拥有全部权限）。</DialogDescription>
            </DialogHeader>
            <div className="max-h-[50vh] space-y-5 overflow-y-auto pr-1">
              {grouped.map(([group, perms]) => (
                <div key={group}>
                  <div className="mb-2 font-medium text-sm">{GROUP_LABEL[group] ?? group}</div>
                  <div className="grid grid-cols-1 gap-1.5">
                    {perms.map((p) => (
                      <label
                        key={p.key}
                        className="flex cursor-pointer items-start gap-2 rounded-md px-2 py-1 hover:bg-muted"
                      >
                        <Checkbox
                          checked={permKeys.includes(p.key)}
                          onCheckedChange={(v) =>
                            setPermKeys((ks) => (v ? [...ks, p.key] : ks.filter((k) => k !== p.key)))
                          }
                        />
                        <span className="text-sm">
                          <code className="text-muted-foreground text-xs">{p.key}</code>
                          <span className="ml-2">{p.description}</span>
                        </span>
                      </label>
                    ))}
                  </div>
                </div>
              ))}
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => setPermRole(null)}>
                取消
              </Button>
              <Button onClick={savePerms}>保存权限</Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        {/* 批量删除角色 */}
        <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>确认删除角色</AlertDialogTitle>
              <AlertDialogDescription>
                {deleteAll ? (
                  <>将删除全部可删除的角色（系统角色自动跳过），此操作不可撤销。</>
                ) : (
                  <>
                    将删除 <span className="font-semibold tabular-nums">{checked.size}</span>{" "}
                    个角色（系统角色自动跳过），此操作不可撤销。
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
    </PermissionGate>
  );
}
