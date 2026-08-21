"use client";

import * as React from "react";

import Link from "next/link";
import { useSearchParams } from "next/navigation";

import { ArrowLeftIcon, BrainIcon, Building2Icon, PauseIcon, PlayIcon, PlusIcon } from "lucide-react";
import { toast } from "sonner";

import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Separator } from "@/components/ui/separator";
import { SidebarTrigger } from "@/components/ui/sidebar";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { api } from "@/lib/api";
import type { Company, Task } from "@/lib/types";
import { cn } from "@/lib/utils";

import { AssetsTab } from "./_tabs/assets-tab";
import { CoverageGraphTab } from "./_tabs/coverage-graph-tab";
import { FindingsTab } from "./_tabs/findings-tab";
import { GraphTab } from "./_tabs/graph-tab";
import { InterceptTab } from "./_tabs/intercept-tab";
import { OverviewTab } from "./_tabs/overview-tab";
import { ReportTab } from "./_tabs/report-tab";
import { SessionsTab } from "./_tabs/sessions-tab";

const TABS = [
  { value: "sessions", label: "会话" },
  { value: "overview", label: "总览" },
  { value: "graph", label: "探索链路" },
  { value: "findings", label: "发现" },
  { value: "assets", label: "测试资产" },
  { value: "coverage", label: "资产覆盖图" },
  { value: "intercept", label: "拦截审批" },
  { value: "report", label: "报告" },
];

function TaskDetailInner() {
  const searchParams = useSearchParams();
  const id = searchParams.get("id") ?? "";
  const [task, setTask] = React.useState<Task | null>(null);
  const [paused, setPaused] = React.useState(false);
  const [loaded, setLoaded] = React.useState(false);
  const [tab, setTab] = React.useState("sessions");
  const [interceptPendingCount, setInterceptPendingCount] = React.useState(0);

  // 企业关联编辑
  const [companies, setCompanies] = React.useState<Company[]>([]);
  const [pickCompanies, setPickCompanies] = React.useState<number[]>([]);
  const [companyOpen, setCompanyOpen] = React.useState(false);
  const [savingCompanies, setSavingCompanies] = React.useState(false);

  React.useEffect(() => {
    api
      .companies()
      .then(setCompanies)
      .catch(() => setCompanies([]));
  }, []);

  React.useEffect(() => {
    let alive = true;
    const load = () =>
      api
        .interceptTask(id)
        .then((rows) => {
          if (alive) setInterceptPendingCount(rows.filter((r) => r.status === "pending").length);
        })
        .catch(() => {});
    load();
    const t = setInterval(load, 5000);
    return () => {
      alive = false;
      clearInterval(t);
    };
  }, [id]);

  const load = React.useCallback(() => {
    Promise.all([api.tasks(), api.stats(id).catch(() => null)])
      .then(([r, s]) => {
        const base = r.tasks.find((t) => t.id === id) ?? null;
        const at = (s as { active_task?: Partial<Task> & { paused?: boolean } } | null)?.active_task;
        if (base && at) {
          base.in_flight = at.in_flight;
          base.goals_total = at.goals_total;
          base.goals_met = at.goals_met;
          base.engine_mode = at.engine_mode;
          base.paused = at.paused;
        }
        setTask(base);
        setPaused(at?.paused ?? base?.paused ?? false);
      })
      .catch(() => {})
      .finally(() => setLoaded(true));
  }, [id]);
  React.useEffect(() => {
    load();
  }, [load]);

  async function togglePause() {
    const next = !paused;
    try {
      await api.controlTask(id, next ? "pause" : "resume");
      setPaused(next);
      toast.success(next ? "已暂停探索" : "已恢复探索");
    } catch (e) {
      toast.error(`操作失败：${(e as Error).message}`);
    }
  }

  function openCompanyEditor() {
    if (!task) return;
    setPickCompanies((task.companies ?? []).map((c) => c.id));
    setCompanyOpen(true);
  }

  async function saveCompanies() {
    setSavingCompanies(true);
    try {
      await api.setTaskCompanies(id, pickCompanies);
      toast.success("企业关联已更新");
      setCompanyOpen(false);
      load();
    } catch (e) {
      toast.error(`保存失败：${(e as Error).message}`);
    } finally {
      setSavingCompanies(false);
    }
  }

  if (!task) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-3 p-10 text-center">
        <p className="text-muted-foreground">{loaded ? `未找到任务 ${id}` : "加载中…"}</p>
        {loaded && (
          <Button asChild variant="outline">
            <Link href="/function/tasks">
              <ArrowLeftIcon /> 返回任务列表
            </Link>
          </Button>
        )}
      </div>
    );
  }

  const engineMode = paused ? "paused" : (task.engine_mode ?? "idle");

  return (
    <Tabs value={tab} onValueChange={setTab} className="flex flex-1 flex-col gap-0">
      {/* Top fixed area */}
      <header className="sticky top-0 z-10 flex flex-col gap-2 border-b bg-background/95 px-4 py-2.5 backdrop-blur lg:px-6">
        <div className="flex items-center gap-2">
          <SidebarTrigger className="-ml-1" />
          <Button asChild variant="ghost" size="icon" className="size-7">
            <Link href="/function/tasks">
              <ArrowLeftIcon />
            </Link>
          </Button>
          <h1 className="max-w-md truncate font-semibold text-sm" title={task.description}>
            {task.description}
          </h1>
          <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-muted-foreground text-xs">{task.id}</code>
          <Separator orientation="vertical" className="mx-1 h-4" />
          {/* status pills */}
          <span className="inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-xs">
            <BrainIcon className="size-3.5 text-emerald-500" /> LLM 已配置
          </span>
          <StatusBadge domain="engine" value={engineMode} dot />
          <div className="ml-auto">
            <Button size="sm" variant={paused ? "default" : "outline"} onClick={togglePause}>
              {paused ? <PlayIcon /> : <PauseIcon />}
              {paused ? "恢复" : "暂停"}
            </Button>
          </div>
        </div>
        <p className="truncate text-muted-foreground text-xs">{task.goal}</p>
        {/* 企业关联 */}
        <div className="flex items-center gap-1.5">
          <Building2Icon className="size-3.5 text-muted-foreground" />
          {(task.companies ?? []).length === 0 ? (
            <span className="text-muted-foreground text-xs">未关联企业</span>
          ) : (
            <div className="flex flex-wrap gap-1">
              {(task.companies ?? []).map((c) => (
                <span key={c.id} className="rounded bg-muted px-1.5 py-0.5 text-[10px]">
                  {c.name}
                </span>
              ))}
            </div>
          )}
          <Button size="sm" variant="outline" className="h-6 px-2 text-[11px]" onClick={openCompanyEditor}>
            <PlusIcon className="size-3" /> 关联企业
          </Button>
        </div>
        {/* Tabs */}
        <TabsList variant="default">
          {TABS.map((t) => (
            <TabsTrigger key={t.value} value={t.value}>
              {t.label}
              {t.value === "intercept" && interceptPendingCount > 0 && (
                <span className="ml-1.5 inline-flex h-4 min-w-[16px] items-center justify-center rounded-full bg-amber-500 px-1 font-semibold text-[10px] text-white leading-none">
                  {interceptPendingCount > 99 ? "99+" : interceptPendingCount}
                </span>
              )}
            </TabsTrigger>
          ))}
        </TabsList>
      </header>

      {/* Tab content */}
      <div className="flex-1 p-4 lg:p-6">
        <TabsContent value="sessions" className="mt-0">
          <SessionsTab taskId={id} />
        </TabsContent>
        <TabsContent value="overview" className="mt-0">
          <OverviewTab taskId={id} />
        </TabsContent>
        <TabsContent value="graph" className="mt-0">
          <GraphTab taskId={id} />
        </TabsContent>
        <TabsContent value="findings" className="mt-0">
          <FindingsTab taskId={id} />
        </TabsContent>
        <TabsContent value="assets" className="mt-0">
          <AssetsTab taskId={id} />
        </TabsContent>
        <TabsContent value="coverage" className="mt-0">
          <CoverageGraphTab taskId={id} />
        </TabsContent>
        <TabsContent value="intercept" className="mt-0">
          <InterceptTab taskId={id} />
        </TabsContent>
        <TabsContent value="report" className="mt-0">
          <ReportTab taskId={id} />
        </TabsContent>
      </div>

      {/* 企业关联编辑 */}
      <Dialog open={companyOpen} onOpenChange={setCompanyOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>关联企业 · {task.description}</DialogTitle>
            <DialogDescription>可多选；第一个选中的企业作为主企业。改绑会立即生效。</DialogDescription>
          </DialogHeader>
          <div className="flex flex-wrap gap-1.5 py-2">
            {companies.map((c) => {
              const on = pickCompanies.includes(c.id);
              return (
                <button
                  key={c.id}
                  type="button"
                  onClick={() => setPickCompanies((prev) => (on ? prev.filter((x) => x !== c.id) : [...prev, c.id]))}
                  className={cn(
                    "rounded-md border px-2.5 py-1 font-medium text-xs transition-colors",
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
              <p className="text-muted-foreground text-xs">暂无企业。可先在「资产」页新增企业。</p>
            )}
          </div>
          <DialogFooter>
            <DialogClose asChild>
              <Button variant="outline">取消</Button>
            </DialogClose>
            <Button onClick={saveCompanies} disabled={savingCompanies}>
              {savingCompanies ? "保存中…" : "保存"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Tabs>
  );
}

// useSearchParams must sit under a Suspense boundary for static export.
export default function TaskDetailPage() {
  return (
    <React.Suspense fallback={null}>
      <TaskDetailInner />
    </React.Suspense>
  );
}
