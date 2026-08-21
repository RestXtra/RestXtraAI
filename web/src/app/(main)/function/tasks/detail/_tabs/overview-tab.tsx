"use client";

import * as React from "react";

import {
  ActivityIcon,
  AlertTriangleIcon,
  BugIcon,
  ClockIcon,
  ShieldCheckIcon,
  TargetIcon,
  WrenchIcon,
} from "lucide-react";

import { StatusBadge } from "@/components/status-badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import { api } from "@/lib/api";
import type { Finding, Task, TaskNode, TaskRoundCosts } from "@/lib/types";

function StatCard({
  label,
  value,
  sub,
  icon: Icon,
}: {
  label: string;
  value: React.ReactNode;
  sub?: string;
  icon: React.ElementType;
}) {
  return (
    <Card className="gap-1.5">
      <CardHeader className="pb-0">
        <CardDescription className="flex items-center gap-1.5">
          <Icon className="size-3.5" /> {label}
        </CardDescription>
        <CardTitle className="text-2xl tabular-nums">{value}</CardTitle>
      </CardHeader>
      {sub && <CardContent className="text-muted-foreground text-xs">{sub}</CardContent>}
    </Card>
  );
}

export function OverviewTab({ taskId }: { taskId: string }) {
  const [task, setTask] = React.useState<Task | null>(null);
  const [engineMode, setEngineMode] = React.useState<Task["engine_mode"]>("idle");
  const [intents, setIntents] = React.useState<TaskNode[]>([]);
  const [findings, setFindings] = React.useState<Finding[]>([]);
  const [costs, setCosts] = React.useState<TaskRoundCosts | null>(null);

  React.useEffect(() => {
    let cancelled = false;

    let timer: ReturnType<typeof setTimeout> | undefined;
    const load = async () => {
      try {
        const snapshot = await api.taskOverview(taskId);
        if (cancelled) return;
        setTask(snapshot.task);
        setEngineMode(snapshot.engine_mode);
        setIntents(snapshot.intents ?? []);
        setFindings(snapshot.findings ?? []);
        setCosts(snapshot.costs);
      } catch {
        // transient errors are ignored; the next poll will retry
      } finally {
        if (!cancelled) timer = setTimeout(load, 3000);
      }
    };

    void load();
    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
    };
  }, [taskId]);

  const running = intents.filter((i) => i.state === "running");
  const open = intents.filter((i) => i.state === "open");
  const blocked = intents.filter((i) => i.state === "blocked");
  const taskFindings = findings.filter((f) => f.task_id === taskId);
  const goalsPct = task?.goals_total ? Math.round(((task.goals_met ?? 0) / task.goals_total) * 100) : 0;
  const costWorkers = costs?.workers ?? [];

  return (
    <div className="flex flex-col gap-4">
      {/* Heartbeat */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <ActivityIcon className="size-4 text-blue-500" /> 心跳
          </CardTitle>
        </CardHeader>
        <CardContent className="grid grid-cols-2 gap-4 sm:grid-cols-4">
          <div>
            <div className="text-muted-foreground text-xs">引擎态</div>
            <StatusBadge domain="engine" value={engineMode ?? task?.engine_mode ?? "idle"} dot className="mt-1" />
          </div>
          <div>
            <div className="text-muted-foreground text-xs">运行中 Worker</div>
            <div className="mt-1 font-semibold text-lg tabular-nums">{running.length}</div>
          </div>
          <div>
            <div className="text-muted-foreground text-xs">最近活动</div>
            <div className="mt-1 inline-flex items-center gap-1 text-sm">
              <ClockIcon className="size-3.5" />
              {task?.last_activity_unix ? new Date(task.last_activity_unix * 1000).toLocaleTimeString("zh-CN") : "—"}
            </div>
          </div>
          <div>
            <div className="text-muted-foreground text-xs">
              目标 {task?.goals_met ?? 0}/{task?.goals_total ?? 0}
            </div>
            <Progress value={goalsPct} className="mt-2" />
          </div>
          {task?.completed_unix && task.completed_unix > 0 ? (
            <div>
              <div className="text-muted-foreground text-xs">完成时间</div>
              <div className="mt-1 inline-flex items-center gap-1 text-sm">
                <ClockIcon className="size-3.5" />
                {new Date(task.completed_unix * 1000).toLocaleString("zh-CN")}
              </div>
            </div>
          ) : null}
        </CardContent>
      </Card>

      {/* Work set */}
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-sm">
              <TargetIcon className="size-4" /> 进行中意图
            </CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-2">
            {running.slice(0, 6).map((i) => (
              <div key={i.id} className="flex items-center gap-2 text-sm">
                <StatusBadge domain="intent" value={i.state} />
                <span className="min-w-0 flex-1 truncate">{i.payload}</span>
              </div>
            ))}
            {running.length === 0 && <p className="text-muted-foreground text-sm">暂无进行中意图</p>}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-sm">
              <AlertTriangleIcon className="size-4 text-amber-500" /> 需要关注
            </CardTitle>
          </CardHeader>
          <CardContent className="grid grid-cols-2 gap-3 text-sm">
            <div>
              <div className="font-semibold text-2xl text-red-600 tabular-nums">{taskFindings.length}</div>
              <div className="text-muted-foreground text-xs">确认漏洞</div>
            </div>
            <div>
              <div className="font-semibold text-2xl text-blue-600 tabular-nums">{running.length}</div>
              <div className="text-muted-foreground text-xs">执行中</div>
            </div>
            <div>
              <div className="font-semibold text-2xl tabular-nums">{open.length}</div>
              <div className="text-muted-foreground text-xs">frontier 待领</div>
            </div>
            <div>
              <div className="font-semibold text-2xl text-red-600 tabular-nums">{blocked.length}</div>
              <div className="text-muted-foreground text-xs">被拦意图</div>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-sm">
              <BugIcon className="size-4 text-red-500" /> 最近发现
            </CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-2">
            {taskFindings.slice(0, 6).map((f) => (
              <div key={f.id} className="flex items-center gap-2 text-sm">
                <StatusBadge domain="severity" value={f.severity} dot />
                <span className="min-w-0 flex-1 truncate">{f.summary}</span>
              </div>
            ))}
            {taskFindings.length === 0 && <p className="text-muted-foreground text-sm">暂无发现</p>}
          </CardContent>
        </Card>
      </div>

      {/* Stat cards */}
      <div className="grid grid-cols-2 gap-4 lg:grid-cols-3">
        <StatCard label="待领意图" value={open.length} icon={ShieldCheckIcon} sub="frontier 开放" />
        <StatCard label="确认发现" value={taskFindings.length} icon={BugIcon} sub="本任务" />
        <StatCard label="意图总数" value={intents.length} icon={AlertTriangleIcon} sub="本任务全部意图" />
        <StatCard label="Agent 回合" value={costs?.total.rounds ?? 0} icon={ActivityIcon} sub="已完成模型回合" />
        <StatCard
          label="工具调用"
          value={costs?.total.tool_calls ?? 0}
          icon={WrenchIcon}
          sub={costs?.total.tool_errors ? `${costs.total.tool_errors} 次错误` : "无错误调用"}
        />
        <StatCard
          label="Token 成本"
          value={(costs?.total.input_tokens ?? 0) + (costs?.total.output_tokens ?? 0)}
          icon={ShieldCheckIcon}
          sub="输入 + 输出 token；未估算金额"
        />
      </div>

      {costWorkers.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="text-sm">Agent 回合成本</CardTitle>
            <CardDescription>以已完成回合为准，实时用量事件不重复计入。</CardDescription>
          </CardHeader>
          <CardContent className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
            {costWorkers.map((worker) => (
              <div key={worker.worker || "unknown"} className="border p-3">
                <div className="font-medium text-sm">{worker.worker || "未命名 Agent"}</div>
                <div className="mt-1 text-muted-foreground text-xs">
                  {worker.rounds} 回合 · {worker.tool_calls} 工具调用 · {worker.input_tokens + worker.output_tokens}{" "}
                  token
                </div>
              </div>
            ))}
          </CardContent>
        </Card>
      )}
    </div>
  );
}
