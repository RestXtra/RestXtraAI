"use client";

import * as React from "react";

import { toast } from "sonner";
import { ChevronDownIcon, ChevronRightIcon, PlayIcon, PlusIcon, Trash2Icon } from "lucide-react";

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
import { Switch } from "@/components/ui/switch";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import { PermissionGate } from "@/components/permission-gate";
import { api } from "@/lib/api";
import type { BatchQueue, BatchTask } from "@/lib/types";
import { useCurrentUser } from "@/hooks/use-current-user";

const STATUS_VARIANT: Record<string, "default" | "secondary" | "success" | "warning" | "destructive"> = {
  pending: "secondary",
  running: "warning",
  completed: "success",
  failed: "destructive",
  cancelled: "secondary",
};

export default function WorkflowsPage() {
  const me = useCurrentUser();
  const canWrite = me.admin || (me.permissions ?? []).includes("batch.write");
  const [queues, setQueues] = React.useState<BatchQueue[]>([]);
  const [tasks, setTasks] = React.useState<Record<number, BatchTask[]>>({});
  const [expanded, setExpanded] = React.useState<number | null>(null);
  const [createOpen, setCreateOpen] = React.useState(false);
  const [form, setForm] = React.useState({ name: "", description: "", cron: "" });
  const [addTask, setAddTask] = React.useState<BatchQueue | null>(null);
  const [taskForm, setTaskForm] = React.useState({ title: "", goal: "" });

  const load = React.useCallback(() => {
    api.batchQueues().then((qs) => setQueues(qs)).catch((e) => toast.error(e.message ?? "加载队列失败"));
  }, []);

  React.useEffect(load, [load]);

  async function loadTasks(queueId: number) {
    try {
      const r = await api.batchTasks(queueId);
      setTasks((m) => ({ ...m, [queueId]: r.tasks }));
    } catch {
      /* noop */
    }
  }

  function toggleExpand(id: number) {
    const next = expanded === id ? null : id;
    setExpanded(next);
    if (next) loadTasks(id);
  }

  async function createQueue() {
    try {
      await api.createBatchQueue(form);
      toast.success("队列已创建");
      setCreateOpen(false);
      setForm({ name: "", description: "", cron: "" });
      load();
    } catch (e) {
      toast.error((e as Error).message ?? "创建失败");
    }
  }

  async function toggleQueue(q: BatchQueue, enabled: boolean) {
    try {
      await api.updateBatchQueue(q.id, { enabled });
      load();
    } catch (e) {
      toast.error((e as Error).message ?? "操作失败");
    }
  }

  async function runQueue(q: BatchQueue) {
    try {
      const r = await api.runBatchQueue(q.id);
      toast.success(`已执行 ${r.ran} 个任务`);
      if (expanded === q.id) loadTasks(q.id);
      load();
    } catch (e) {
      toast.error((e as Error).message ?? "执行失败");
    }
  }

  async function removeQueue(q: BatchQueue) {
    if (!window.confirm(`确认删除队列「${q.name}」及其任务？`)) return;
    try {
      await api.deleteBatchQueue(q.id);
      toast.success("队列已删除");
      load();
    } catch (e) {
      toast.error((e as Error).message ?? "删除失败");
    }
  }

  async function addTaskToQueue() {
    if (!addTask) return;
    try {
      await api.addBatchTask(addTask.id, taskForm.title, {
        action: "task",
        description: taskForm.title,
        goal: taskForm.goal,
      });
      toast.success("任务已加入队列");
      setAddTask(null);
      setTaskForm({ title: "", goal: "" });
      loadTasks(addTask.id);
      load();
    } catch (e) {
      toast.error((e as Error).message ?? "添加失败");
    }
  }

  async function removeTask(t: BatchTask) {
    try {
      await api.deleteBatchTask(t.id);
      if (t.queue_id) loadTasks(t.queue_id);
      load();
    } catch (e) {
      toast.error((e as Error).message ?? "删除失败");
    }
  }

  return (
    <PermissionGate perm="batch.read">
      <div className="space-y-6 p-4 md:p-6">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h1 className="text-2xl font-semibold tracking-tight">工作流 · 批量任务</h1>
            <p className="text-sm text-muted-foreground">批量任务队列：每项任务按 payload 调度（默认 spawn 一个探索任务）。</p>
          </div>
          {canWrite && (
            <Button onClick={() => setCreateOpen(true)}>
              <PlusIcon className="size-4" /> 新建队列
            </Button>
          )}
        </div>

        <div className="space-y-3">
          {queues.length === 0 ? (
            <div className="rounded-lg border py-16 text-center text-sm text-muted-foreground">暂无批量队列</div>
          ) : (
            queues.map((q) => (
              <div key={q.id} className="rounded-lg border">
                <div className="flex items-center gap-2 px-4 py-3">
                  <Button variant="ghost" size="icon-sm" onClick={() => toggleExpand(q.id)} aria-label="展开">
                    {expanded === q.id ? <ChevronDownIcon className="size-4" /> : <ChevronRightIcon className="size-4" />}
                  </Button>
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2 font-medium">
                      {q.name}
                      <Badge variant="outline">{q.task_count} 任务</Badge>
                      <Badge variant={q.enabled ? "success" : "secondary"}>{q.enabled ? "启用" : "停用"}</Badge>
                    </div>
                    {q.description && <div className="truncate text-xs text-muted-foreground">{q.description}</div>}
                  </div>
                  {canWrite && (
                    <div className="flex items-center gap-1">
                      <Switch checked={q.enabled} onCheckedChange={(v) => toggleQueue(q, v)} aria-label="启用" />
                      <Button variant="ghost" size="sm" onClick={() => runQueue(q)}>
                        <PlayIcon className="size-4" /> 执行
                      </Button>
                      <Button variant="ghost" size="sm" onClick={() => { setAddTask(q); setTaskForm({ title: "", goal: "" }); }}>
                        <PlusIcon className="size-4" /> 加任务
                      </Button>
                      <Button variant="ghost" size="sm" onClick={() => removeQueue(q)}>
                        <Trash2Icon className="size-4" />
                      </Button>
                    </div>
                  )}
                </div>

                {expanded === q.id && (
                  <div className="border-t">
                    <Table>
                      <TableHeader>
                        <TableRow>
                          <TableHead className="w-16">ID</TableHead>
                          <TableHead>标题</TableHead>
                          <TableHead className="w-28">状态</TableHead>
                          <TableHead className="w-20">尝试</TableHead>
                          <TableHead className="w-40">完成时间</TableHead>
                          {canWrite && <TableHead className="w-16 text-right">操作</TableHead>}
                        </TableRow>
                      </TableHeader>
                      <TableBody>
                        {(tasks[q.id] ?? []).map((t) => (
                          <TableRow key={t.id}>
                            <TableCell className="text-muted-foreground text-xs">#{t.id}</TableCell>
                            <TableCell>
                              <div className="font-medium">{t.title || "-"}</div>
                              {t.error && <div className="text-xs text-destructive">{t.error}</div>}
                            </TableCell>
                            <TableCell>
                              <Badge variant={STATUS_VARIANT[t.status] ?? "secondary"}>{t.status}</Badge>
                            </TableCell>
                            <TableCell>{t.attempts}</TableCell>
                            <TableCell className="text-muted-foreground text-xs">
                              {t.finished_at ? new Date(t.finished_at).toLocaleTimeString("zh-CN") : "-"}
                            </TableCell>
                            {canWrite && (
                              <TableCell className="text-right">
                                <Button variant="ghost" size="sm" onClick={() => removeTask(t)}>
                                  <Trash2Icon className="size-4" />
                                </Button>
                              </TableCell>
                            )}
                          </TableRow>
                        ))}
                        {(tasks[q.id] ?? []).length === 0 && (
                          <TableRow><TableCell colSpan={6} className="py-6 text-center text-sm text-muted-foreground">暂无任务</TableCell></TableRow>
                        )}
                      </TableBody>
                    </Table>
                  </div>
                )}
              </div>
            ))
          )}
        </div>

        {/* 新建队列 */}
        <Dialog open={createOpen} onOpenChange={setCreateOpen}>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>新建批量队列</DialogTitle>
              <DialogDescription>队列启用后由后台定时排空其任务（每 5 秒最多 4 个）。</DialogDescription>
            </DialogHeader>
            <div className="space-y-4">
              <div className="space-y-1.5">
                <Label>名称 *</Label>
                <Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
              </div>
              <div className="space-y-1.5">
                <Label>说明</Label>
                <Input value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} />
              </div>
              <div className="space-y-1.5">
                <Label>Cron（预留）</Label>
                <Input value={form.cron} onChange={(e) => setForm({ ...form, cron: e.target.value })} placeholder="0 3 * * 1" />
              </div>
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => setCreateOpen(false)}>取消</Button>
              <Button onClick={createQueue} disabled={!form.name.trim()}>创建</Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        {/* 加任务 */}
        <Dialog open={!!addTask} onOpenChange={(o) => { if (!o) setAddTask(null); }}>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>添加任务 · {addTask?.name}</DialogTitle>
              <DialogDescription>任务执行时 spawn 一个探索任务（payload.action=task）。</DialogDescription>
            </DialogHeader>
            <div className="space-y-4">
              <div className="space-y-1.5">
                <Label>任务标题 *</Label>
                <Input value={taskForm.title} onChange={(e) => setTaskForm({ ...taskForm, title: e.target.value })} />
              </div>
              <div className="space-y-1.5">
                <Label>目标 / Goal</Label>
                <Textarea rows={3} value={taskForm.goal} onChange={(e) => setTaskForm({ ...taskForm, goal: e.target.value })} placeholder="如：探测 example.com 的攻击面" />
              </div>
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => setAddTask(null)}>取消</Button>
              <Button onClick={addTaskToQueue} disabled={!taskForm.title.trim()}>添加</Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>
    </PermissionGate>
  );
}
