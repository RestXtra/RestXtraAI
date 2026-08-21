"use client";

import * as React from "react";

import { FileDownIcon, Loader2Icon } from "lucide-react";

import { Markdown } from "@/components/markdown";
import { PermissionGate } from "@/components/permission-gate";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { api } from "@/lib/api";
import type { Task } from "@/lib/types";

// 报告编写：对选定任务生成确定性渗透测试报告（Markdown）。
export default function ReportPage() {
  const [tasks, setTasks] = React.useState<Task[]>([]);
  const [taskId, setTaskId] = React.useState("");
  const [report, setReport] = React.useState("");
  const [loading, setLoading] = React.useState(false);

  React.useEffect(() => {
    api
      .tasks()
      .then(({ tasks }) => setTasks(tasks))
      .catch(() => {});
  }, []);

  async function generate() {
    if (!taskId) return;
    setLoading(true);
    try {
      const md = await api.report(taskId);
      setReport(md);
    } catch (e) {
      setReport(`生成报告失败：${(e as Error).message}`);
    } finally {
      setLoading(false);
    }
  }

  function download() {
    const blob = new Blob([report], { type: "text/markdown;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `report-${taskId || "task"}.md`;
    a.click();
    URL.revokeObjectURL(url);
  }

  return (
    <PermissionGate perm="cap.report.read">
      <div className="space-y-6 p-4 md:p-6">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h1 className="font-semibold text-2xl tracking-tight">报告编写</h1>
            <p className="text-muted-foreground text-sm">基于任务确认发现的确定性报告（证据门控，Markdown）。</p>
          </div>
          <div className="flex items-center gap-2">
            <Select value={taskId} onValueChange={setTaskId}>
              <SelectTrigger className="w-56">
                <SelectValue placeholder="选择任务" />
              </SelectTrigger>
              <SelectContent>
                {tasks.map((t) => (
                  <SelectItem key={t.id} value={t.id}>
                    {t.description || t.id}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Button onClick={generate} disabled={!taskId || loading}>
              {loading ? <Loader2Icon className="size-4 animate-spin" /> : <FileDownIcon className="size-4" />}
              生成报告
            </Button>
            {report && (
              <Button variant="outline" onClick={download}>
                下载 .md
              </Button>
            )}
          </div>
        </div>

        <div className="rounded-lg border p-4">
          {report ? (
            <Markdown text={report} />
          ) : (
            <p className="py-12 text-center text-muted-foreground text-sm">
              选择任务后点击「生成报告」。报告汇总该任务的确认漏洞、资产分布与目标描述。
            </p>
          )}
        </div>
      </div>
    </PermissionGate>
  );
}
