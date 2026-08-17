"use client";

import * as React from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { toast } from "sonner";
import {
  PlusIcon,
  Trash2Icon,
  ArrowRightIcon,
  StarIcon,
  SearchIcon,
  XIcon,
  WorkflowIcon,
  PanelsTopLeftIcon,
} from "lucide-react";

import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { TablePagination } from "@/components/table-pagination";
import { api } from "@/lib/api";
import { clearWorkflowDraft, draftToWorkflow, loadWorkflowDraft } from "@/lib/workflow-draft";
import { cn } from "@/lib/utils";
import type { Task, TaskStatus, LLMProfile, TaskWorkflow, Company } from "@/lib/types";

// ACTIVE_PROFILE is the sentinel Select value for "use the global active profile".
const ACTIVE_PROFILE = "__active__";

// fmtTokens renders a compact token count (1234 → 1.2k, 2_000_000 → 2M).
function fmtTokens(n: number): string {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(n >= 10_000_000 ? 0 : 1) + "M";
  if (n >= 1000) return (n / 1000).toFixed(n >= 10000 ? 0 : 1) + "k";
  return String(n);
}

// fmtDuration renders a run duration in seconds as a compact human string.
function fmtDuration(sec: number): string {
  if (sec <= 0) return "—";
  const d = Math.floor(sec / 86400);
  const h = Math.floor((sec % 86400) / 3600);
  const m = Math.floor((sec % 3600) / 60);
  const s = sec % 60;
  if (d > 0) return `${d}d ${h}h`;
  if (h > 0) return `${h}h ${m}m`;
  if (m > 0) return `${m}m ${s}s`;
  return `${s}s`;
}

// taskDuration is the task's run duration in seconds: created → now while running,
// created → completion for a finished task, else created → last activity. 0 when it
// never ran (no activity yet).
function taskDuration(task: Task, nowSec: number): number {
  const start = task.created_unix ?? 0;
  if (!start) return 0;
  const end =
    task.status === "running"
      ? nowSec
      : task.completed_unix && task.completed_unix > 0
        ? task.completed_unix
        : task.last_activity_unix ?? 0;
  return end > start ? end - start : 0;
}

// fmtDateTime renders a unix-seconds timestamp as a compact local date-time
// (MM-DD HH:mm), or "—" when unset.
function fmtDateTime(unix?: number): string {
  if (!unix || unix <= 0) return "—";
  const d = new Date(unix * 1000);
  const p = (n: number) => String(n).padStart(2, "0");
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

const STATUS_OPTIONS: { value: TaskStatus; label: string }[] = [
  { value: "created", label: "已创建" },
  { value: "running", label: "运行中" },
  { value: "paused", label: "已暂停" },
  { value: "done", label: "已完成" },
  { value: "failed", label: "失败" },
  { value: "timeout", label: "已超时" },
];

export default function TasksPage() {
  const router = useRouter();
  const [tasks, setTasks] = React.useState<Task[]>([]);
  const [description, setDescription] = React.useState("");
  const [goal, setGoal] = React.useState("");
  const [profiles, setProfiles] = React.useState<LLMProfile[]>([]);
  const [llmProfile, setLlmProfile] = React.useState<string>(ACTIVE_PROFILE); // sentinel = active
  const [timeoutMin, setTimeoutMin] = React.useState(""); // 任务级超时(分钟);空/0 = 不限时
  const [wfEnabled, setWfEnabled] = React.useState(false); // 是否编写工作流(初始探索方向 + 提示)
  const [wfSteps, setWfSteps] = React.useState<{ summary: string }[]>([]);
  const [wfHints, setWfHints] = React.useState<string[]>([]);
  const [wfDraft, setWfDraft] = React.useState<TaskWorkflow | null>(null); // 画板草稿（优先级更高）
  const [wfDraftLoaded, setWfDraftLoaded] = React.useState(false);
  const [open, setOpen] = React.useState(false);
  const [query, setQuery] = React.useState("");
  const [statusFilter, setStatusFilter] = React.useState<TaskStatus | "all">("all");
  const [companyFilter, setCompanyFilter] = React.useState<number | "all">("all");
  const [companies, setCompanies] = React.useState<Company[]>([]);
  const [taskCompanies, setTaskCompanies] = React.useState<number[]>([]); // 新建任务的企业多选
  const [page, setPage] = React.useState(1);
  const [pageSize, setPageSize] = React.useState(20);
  const [nowSec, setNowSec] = React.useState(() => Math.floor(Date.now() / 1000));

  const filtered = React.useMemo(() => {
    const q = query.trim().toLowerCase();
    return tasks.filter((t) => {
      if (statusFilter !== "all" && t.status !== statusFilter) return false;
      if (companyFilter !== "all" && !(t.companies ?? []).some((c) => c.id === companyFilter)) return false;
      if (!q) return true;
      return (
        t.description.toLowerCase().includes(q) ||
        t.goal.toLowerCase().includes(q) ||
        t.id.toLowerCase().includes(q)
      );
    });
  }, [tasks, query, statusFilter, companyFilter]);

  // reset to page 1 whenever filters change
  React.useEffect(() => { setPage(1); }, [query, statusFilter, companyFilter]);

  const paginated = React.useMemo(
    () => filtered.slice((page - 1) * pageSize, page * pageSize),
    [filtered, page, pageSize],
  );

  const load = React.useCallback(() => {
    api.tasks()
      .then((r) => {
        setTasks(r.tasks.map((t) => (t.id === r.active ? { ...t, active: true } : t)));
      })
      .catch(() => {});
  }, []);

  React.useEffect(() => {
    load();
    const i = setInterval(load, 3000);
    return () => clearInterval(i);
  }, [load]);

  // load LLM profiles once for the create-task profile picker.
  React.useEffect(() => {
    api.llmProfiles().then(setProfiles).catch(() => setProfiles([]));
  }, []);

  // load companies for the create-task multi-select + list filter.
  React.useEffect(() => {
    api.companies().then(setCompanies).catch(() => setCompanies([]));
  }, []);

  // 从画板草稿(localStorage)载入工作流（步骤带优先级，优先于内联编辑）。
  const reloadDraft = React.useCallback(() => {
    const d = loadWorkflowDraft();
    if (d) {
      const wf = draftToWorkflow(d);
      setWfDraft(wf);
      setWfDraftLoaded(wf.steps.length > 0 || wf.hints.length > 0);
    } else {
      setWfDraft(null);
      setWfDraftLoaded(false);
    }
  }, []);
  // 页面回到前台（从画板返回）时重新载入草稿。
  React.useEffect(() => {
    reloadDraft();
    const onVis = () => {
      if (document.visibilityState === "visible") reloadDraft();
    };
    document.addEventListener("visibilitychange", onVis);
    return () => document.removeEventListener("visibilitychange", onVis);
  }, [reloadDraft]);

  // tick every second so running tasks' 运行时长 counts up live.
  React.useEffect(() => {
    const i = setInterval(() => setNowSec(Math.floor(Date.now() / 1000)), 1000);
    return () => clearInterval(i);
  }, []);

  async function createTask() {
    if (!description.trim() || !goal.trim()) {
      toast.error("请填写描述与目标");
      return;
    }
    // 组装可选工作流：优先用画板草稿，否则用内联编辑（预填初始探索方向 + 提示）。
    let workflow: TaskWorkflow | undefined;
    if (wfEnabled) {
      if (wfDraft && (wfDraft.steps?.length || wfDraft.hints?.length)) {
        workflow = wfDraft;
      } else {
        const steps = wfSteps.map((s) => ({ summary: s.summary.trim() })).filter((s) => s.summary);
        const hints = wfHints.map((h) => h.trim()).filter(Boolean);
        if (steps.length || hints.length) {
          workflow = { steps, hints };
        }
      }
    }
    try {
      const pid = llmProfile === ACTIVE_PROFILE ? undefined : Number(llmProfile);
      const timeoutSec = Math.max(0, Math.floor(Number(timeoutMin) || 0)) * 60;
      await api.createTask(description.trim(), goal.trim(), pid, timeoutSec, workflow, taskCompanies);
      toast.success(workflow ? "任务已创建（含工作流）" : "任务已创建");
      setDescription("");
      setGoal("");
      setLlmProfile(ACTIVE_PROFILE);
      setTimeoutMin("");
      setWfEnabled(false);
      setWfSteps([]);
      setWfHints([]);
      setWfDraft(null);
      setWfDraftLoaded(false);
      setTaskCompanies([]);
      setOpen(false);
      load();
    } catch (e) {
      toast.error("创建失败：" + (e as Error).message);
    }
  }

  async function deleteTask(id: string) {
    try {
      await api.deleteTask(id);
      toast.success("任务已删除（全局资产图保留）");
      load();
    } catch (e) {
      toast.error("删除失败：" + (e as Error).message);
    }
  }

  return (
    <Card>
      <CardContent className="flex flex-col gap-4 px-0 pt-6">
        <div className="flex flex-wrap items-center gap-2 px-4 lg:px-6">
          <div className="relative w-full sm:max-w-xs">
            <SearchIcon className="text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2" />
            <Input
              placeholder="搜索描述 / 目标 / ID"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              className="pl-8"
            />
            {query && (
              <button
                type="button"
                onClick={() => setQuery("")}
                aria-label="清除搜索"
                className="text-muted-foreground hover:text-foreground absolute top-1/2 right-2 -translate-y-1/2"
              >
                <XIcon className="size-4" />
              </button>
            )}
          </div>
          <Select value={statusFilter} onValueChange={(v) => setStatusFilter(v as TaskStatus | "all")}>
            <SelectTrigger className="w-36">
              <SelectValue placeholder="状态" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部状态</SelectItem>
              {STATUS_OPTIONS.map((s) => (
                <SelectItem key={s.value} value={s.value}>
                  {s.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select
            value={companyFilter === "all" ? "all" : String(companyFilter)}
            onValueChange={(v) => setCompanyFilter(v === "all" ? "all" : Number(v))}
          >
            <SelectTrigger className="w-40">
              <SelectValue placeholder="企业" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部企业</SelectItem>
              {companies.map((c) => (
                <SelectItem key={c.id} value={String(c.id)}>
                  {c.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <span className="text-muted-foreground text-xs tabular-nums">
            {filtered.length}/{tasks.length} 条
          </span>
          <Dialog open={open} onOpenChange={setOpen}>
            <DialogTrigger asChild>
              <Button size="sm" className="ml-auto">
                <PlusIcon /> 新建任务
              </Button>
            </DialogTrigger>
            <DialogContent className="sm:max-w-xl">
              <DialogHeader>
                <DialogTitle>新建任务</DialogTitle>
                <DialogDescription>
                  填写测试对象与目标。
                </DialogDescription>
              </DialogHeader>
              <div className="max-h-[70vh] grid gap-4 overflow-y-auto py-2 pr-1">
                <div className="grid gap-2">
                  <Label htmlFor="description">描述</Label>
                  <Textarea
                    id="description"
                    placeholder="测试对象与背景，例如：测试 example.com 这个站点"
                    value={description}
                    onChange={(e) => setDescription(e.target.value)}
                  />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="goal">目标</Label>
                  <Textarea
                    id="goal"
                    placeholder="要达成什么，例如：拿下后台管理权限、获取服务器权限"
                    value={goal}
                    onChange={(e) => setGoal(e.target.value)}
                  />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="llm-profile">LLM 配置</Label>
                  <Select value={llmProfile} onValueChange={setLlmProfile}>
                    <SelectTrigger id="llm-profile">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value={ACTIVE_PROFILE}>使用激活配置（默认）</SelectItem>
                      {profiles.map((p) => (
                        <SelectItem key={p.id} value={String(p.id)}>
                          {p.name}
                          {p.is_default ? "（激活）" : ""} · {p.model}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <p className="text-muted-foreground text-xs">
                    本任务的 planner/worker 使用所选配置运行；对话仍用激活配置。
                  </p>
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="timeout-min">任务超时（分钟，可选）</Label>
                  <Input
                    id="timeout-min"
                    type="number"
                    min={0}
                    className="w-40"
                    placeholder="留空 = 不限时"
                    value={timeoutMin}
                    onChange={(e) => setTimeoutMin(e.target.value)}
                  />
                  <p className="text-muted-foreground text-xs">
                    到点后触发优雅收尾（各 agent 写回 + planner 终局判定），任务进入 timeout 终态。
                  </p>
                </div>

                <div className="grid gap-2">
                  <Label>企业（可选，可多选）</Label>
                  <div className="flex flex-wrap gap-1.5">
                    {companies.map((c) => {
                      const on = taskCompanies.includes(c.id);
                      return (
                        <button
                          key={c.id}
                          type="button"
                          onClick={() =>
                            setTaskCompanies((prev) =>
                              on ? prev.filter((id) => id !== c.id) : [...prev, c.id],
                            )
                          }
                          className={cn(
                            "rounded-md border px-2.5 py-1 text-xs font-medium transition-colors",
                            on
                              ? "border-primary bg-primary text-primary-foreground"
                              : "border-input text-muted-foreground hover:bg-muted",
                          )}
                        >
                          {c.name}
                        </button>
                      );
                    })}
                    {companies.length === 0 && (
                      <p className="text-muted-foreground text-xs">
                        暂无企业。可先在「资产」页新增企业后再关联。
                      </p>
                    )}
                  </div>
                  <p className="text-muted-foreground text-xs">
                    第一个选中的企业作为主企业；创建后仍可在任务详情关联更多企业。
                  </p>
                </div>

                <div className="grid gap-3 rounded-lg border p-3">
                  <div className="flex items-center justify-between gap-4">
                    <div className="grid gap-0.5">
                      <Label className="flex items-center gap-1.5 text-sm">
                        <WorkflowIcon className="size-4 text-muted-foreground" />
                        编写工作流（可选）
                      </Label>
                      <p className="text-muted-foreground text-xs">
                        预填初始探索方向与战略提示，引擎启动后按顺序领取执行；留空则完全由 AI 自主规划。
                      </p>
                    </div>
                    <Switch
                      checked={wfEnabled}
                      onCheckedChange={(v) => {
                        setWfEnabled(v);
                        if (v) reloadDraft();
                      }}
                    />
                  </div>
                  {wfEnabled && (
                    <div className="grid gap-3 pt-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <Button size="sm" variant="outline" onClick={() => router.push("/workflow-builder")}>
                          <PanelsTopLeftIcon /> {wfDraftLoaded ? "重新打开画板" : "打开画板构建"}
                        </Button>
                        {wfDraftLoaded && (
                          <Button
                            size="sm"
                            variant="ghost"
                            onClick={() => {
                              clearWorkflowDraft();
                              reloadDraft();
                            }}
                          >
                            清空画板
                          </Button>
                        )}
                      </div>
                      {wfDraftLoaded && (
                        <p className="text-emerald-600 text-xs">
                          已载入画板工作流：{wfDraft?.steps?.length ?? 0} 步 · {wfDraft?.hints?.length ?? 0} 条提示（创建时优先使用）
                        </p>
                      )}
                      <div className="grid gap-2">
                        <Label className="text-xs text-muted-foreground">探索步骤（按顺序执行）</Label>
                        {wfSteps.length === 0 && (
                          <p className="text-muted-foreground/70 text-xs">还没有步骤，添加第一步开始。</p>
                        )}
                        {wfSteps.map((s, i) => (
                          <div key={i} className="flex items-start gap-2">
                            <span className="text-muted-foreground mt-2.5 w-4 font-mono text-xs">{i + 1}.</span>
                            <Textarea
                              rows={2}
                              className="min-h-0"
                              placeholder="例如：对目标做 Web 指纹识别与端口扫描"
                              value={s.summary}
                              onChange={(e) => {
                                const next = [...wfSteps];
                                next[i] = { summary: e.target.value };
                                setWfSteps(next);
                              }}
                            />
                            <Button
                              size="icon"
                              variant="outline"
                              aria-label="删除步骤"
                              onClick={() => setWfSteps(wfSteps.filter((_, j) => j !== i))}
                            >
                              <Trash2Icon className="text-destructive" />
                            </Button>
                          </div>
                        ))}
                        <Button
                          size="sm"
                          variant="outline"
                          className="justify-self-start"
                          onClick={() => setWfSteps([...wfSteps, { summary: "" }])}
                        >
                          <PlusIcon /> 添加步骤
                        </Button>
                      </div>
                      <Separator />
                      <div className="grid gap-2">
                        <Label className="text-xs text-muted-foreground">战略提示（供 planner 首轮读取）</Label>
                        {wfHints.map((h, i) => (
                          <div key={i} className="flex items-center gap-2">
                            <Input
                              placeholder="例如：优先测试登录接口的弱口令"
                              value={h}
                              onChange={(e) => {
                                const next = [...wfHints];
                                next[i] = e.target.value;
                                setWfHints(next);
                              }}
                            />
                            <Button
                              size="icon"
                              variant="outline"
                              aria-label="删除提示"
                              onClick={() => setWfHints(wfHints.filter((_, j) => j !== i))}
                            >
                              <Trash2Icon className="text-destructive" />
                            </Button>
                          </div>
                        ))}
                        <Button
                          size="sm"
                          variant="outline"
                          className="justify-self-start"
                          onClick={() => setWfHints([...wfHints, ""])}
                        >
                          <PlusIcon /> 添加提示
                        </Button>
                      </div>
                    </div>
                  )}
                </div>
              </div>
              <DialogFooter>
                <DialogClose asChild>
                  <Button variant="outline">取消</Button>
                </DialogClose>
                <Button onClick={createTask}>创建</Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        </div>

        {tasks.length === 0 ? (
          <div className="text-muted-foreground mx-4 flex items-center justify-center rounded-lg border border-dashed py-20 text-sm lg:mx-6">
            暂无任务，点击右上角「新建任务」开始。
          </div>
        ) : filtered.length === 0 ? (
          <div className="text-muted-foreground mx-4 flex items-center justify-center rounded-lg border border-dashed py-20 text-sm lg:mx-6">
            没有匹配的任务。
          </div>
        ) : (
          <Table className="**:data-[slot='table-cell']:px-4 **:data-[slot='table-head']:px-4">
            <TableHeader className="[&_tr]:border-t">
              <TableRow>
                <TableHead className="font-mono">ID</TableHead>
                <TableHead>描述</TableHead>
                <TableHead>企业</TableHead>
                <TableHead>目标</TableHead>
                <TableHead>状态</TableHead>
                <TableHead className="text-center">目标进度</TableHead>
                <TableHead className="text-right">创建时间</TableHead>
                <TableHead className="text-right">运行时长</TableHead>
                <TableHead className="text-right">Token</TableHead>
                <TableHead className="sticky right-0 z-10 bg-card text-right shadow-[-1px_0_0_0_hsl(var(--border))]">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {paginated.map((task) => (
                <TableRow key={task.id} className="group border-border/60">
                  <TableCell>
                    <code className="bg-muted rounded px-1.5 py-0.5 font-mono text-xs">{task.id}</code>
                  </TableCell>
                  <TableCell className="font-medium">
                    <div className="flex max-w-xs items-center gap-2">
                      <span className="truncate" title={task.description}>
                        {task.description}
                      </span>
                      {task.active && (
                        <StarIcon className="size-4 shrink-0 fill-amber-400 text-amber-400" />
                      )}
                    </div>
                  </TableCell>
                  <TableCell>
                    {(task.companies ?? []).length === 0 ? (
                      <span className="text-muted-foreground text-xs">—</span>
                    ) : (
                      <div className="flex max-w-[10rem] flex-wrap gap-1">
                        {(task.companies ?? []).map((c) => (
                          <span
                            key={c.id}
                            className="rounded bg-muted px-1.5 py-0.5 text-[10px] text-foreground/80"
                          >
                            {c.name}
                          </span>
                        ))}
                      </div>
                    )}
                  </TableCell>
                  <TableCell className="text-muted-foreground max-w-xs truncate">{task.goal}</TableCell>
                  <TableCell>
                    <StatusBadge domain="task" value={task.status} dot />
                  </TableCell>
                  <TableCell className="text-muted-foreground text-center text-xs tabular-nums">
                    {typeof task.goals_total === "number" && task.goals_total > 0
                      ? `${task.goals_met}/${task.goals_total}`
                      : <span className="text-muted-foreground">—</span>}
                  </TableCell>
                  <TableCell className="text-muted-foreground text-right text-xs whitespace-nowrap tabular-nums">
                    {fmtDateTime(task.created_unix)}
                  </TableCell>
                  <TableCell className="text-right text-xs whitespace-nowrap tabular-nums">
                    {(() => {
                      const secs = taskDuration(task, nowSec);
                      if (secs <= 0) return <span className="text-muted-foreground">—</span>;
                      return (
                        <span className={task.status === "running" ? "text-foreground" : "text-muted-foreground"}>
                          {fmtDuration(secs)}
                        </span>
                      );
                    })()}
                  </TableCell>
                  <TableCell
                    className="text-right text-xs whitespace-nowrap tabular-nums"
                    title={
                      task.tokens
                        ? `输入 ${task.tokens.input_tokens} · 缓存 ${task.tokens.cache_read_tokens} · 输出 ${task.tokens.output_tokens}`
                        : undefined
                    }
                  >
                    {task.tokens ? (
                      <span className="text-muted-foreground">
                        入 <span className="text-foreground">{fmtTokens(task.tokens.input_tokens)}</span>
                        {" · 缓 "}
                        <span className="text-foreground">{fmtTokens(task.tokens.cache_read_tokens)}</span>
                        {" · 出 "}
                        <span className="text-foreground">{fmtTokens(task.tokens.output_tokens)}</span>
                      </span>
                    ) : (
                      "—"
                    )}
                  </TableCell>
                  <TableCell className="sticky right-0 z-10 bg-card text-right shadow-[-1px_0_0_0_hsl(var(--border))] group-hover:bg-muted/50">
                    <div className="flex items-center justify-end gap-2">
                      <Button asChild size="sm">
                        <Link href={`/function/tasks/detail?id=${task.id}`}>
                          进入 <ArrowRightIcon />
                        </Link>
                      </Button>
                      <Button
                        size="icon"
                        variant="outline"
                        onClick={() => deleteTask(task.id)}
                        aria-label="删除任务"
                      >
                        <Trash2Icon className="text-destructive" />
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
        <TablePagination
          page={page}
          pageSize={pageSize}
          total={filtered.length}
          onPageChange={setPage}
          onPageSizeChange={setPageSize}
        />
      </CardContent>
    </Card>
  );
}
