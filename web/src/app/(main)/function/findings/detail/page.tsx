"use client";

import * as React from "react";

import Link from "next/link";
import { useSearchParams } from "next/navigation";

import { ArrowLeftIcon, CheckIcon, FileTextIcon, RouteIcon, ShieldAlertIcon } from "lucide-react";
import { toast } from "sonner";

import { Markdown } from "@/components/markdown";
import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { api } from "@/lib/api";
import { statusMeta } from "@/lib/status";
import type { Finding, FindingStatus } from "@/lib/types";

import { FindingLineage } from "./lineage";

const STATUSES: FindingStatus[] = [
  "pending",
  "in_progress",
  "confirmed",
  "resolved",
  "false_positive",
  "ignored",
  "duplicate",
  "risk_accepted",
];

function ReportPreview({ report }: { report?: string }) {
  if (!report) return <p className="text-muted-foreground text-sm">暂无详细报告。</p>;
  return <Markdown text={report} />;
}

function FindingDetailContent() {
  const id = useSearchParams().get("id") ?? "";
  const [finding, setFinding] = React.useState<Finding | null>(null);
  const [loading, setLoading] = React.useState(true);
  const [editing, setEditing] = React.useState(false);
  const [report, setReport] = React.useState("");
  const [saving, setSaving] = React.useState(false);

  React.useEffect(() => {
    if (!id) {
      setLoading(false);
      return;
    }
    api
      .finding(id)
      .then((result) => {
        setFinding(result);
        setReport(result.report ?? "");
      })
      .catch((error) => toast.error((error as Error).message))
      .finally(() => setLoading(false));
  }, [id]);

  const updateStatus = async (status: FindingStatus) => {
    if (!finding || status === finding.status) return;
    const previous = finding;
    setFinding({ ...finding, status });
    try {
      setFinding(await api.updateFinding(id, { status }));
      toast.success(`已标记为${statusMeta("finding", status).label}`);
    } catch (error) {
      setFinding(previous);
      toast.error((error as Error).message);
    }
  };

  const saveReport = async () => {
    setSaving(true);
    try {
      const updated = await api.updateFinding(id, { report });
      setFinding(updated);
      setEditing(false);
      toast.success("详细报告已保存");
    } catch (error) {
      toast.error((error as Error).message);
    } finally {
      setSaving(false);
    }
  };

  if (loading) return <div className="p-8 text-muted-foreground text-sm">加载中...</div>;
  if (!finding) return <div className="p-8 text-muted-foreground text-sm">漏洞不存在或已删除。</div>;

  return (
    <div className="flex flex-1 flex-col gap-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <Button variant="ghost" size="sm" asChild className="mb-2 -ml-2">
            <Link href="/function/findings">
              <ArrowLeftIcon /> 返回发现
            </Link>
          </Button>
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="font-semibold text-xl">{finding.vulnclass || "漏洞详情"}</h1>
            <StatusBadge domain="severity" value={finding.severity} dot />
            <StatusBadge domain="finding" value={finding.status} dot />
          </div>
          <p className="mt-1 text-muted-foreground text-sm">Finding #{finding.id}</p>
        </div>
        <Select value={finding.status} onValueChange={(value) => void updateStatus(value as FindingStatus)}>
          <SelectTrigger className="w-36">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {STATUSES.map((status) => (
              <SelectItem key={status} value={status}>
                {statusMeta("finding", status).label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <Tabs defaultValue="overview">
        <TabsList variant="line">
          <TabsTrigger value="overview">
            <ShieldAlertIcon /> 概览与报告
          </TabsTrigger>
          <TabsTrigger value="lineage">
            <RouteIcon /> 链路图
          </TabsTrigger>
        </TabsList>

        <TabsContent value="overview" className="grid gap-4 pt-3 lg:grid-cols-[minmax(0,1fr)_280px]">
          <div className="flex min-w-0 flex-col gap-4">
            <Card>
              <CardHeader>
                <CardTitle className="text-sm">摘要</CardTitle>
              </CardHeader>
              <CardContent className="whitespace-pre-wrap text-sm leading-relaxed">
                {finding.summary || "无摘要"}
              </CardContent>
            </Card>
            <Card>
              <CardHeader>
                <CardTitle className="text-sm">证据 / PoC</CardTitle>
              </CardHeader>
              <CardContent>
                <pre className="overflow-x-auto whitespace-pre-wrap border bg-muted/40 p-3 font-mono text-xs leading-relaxed">
                  {finding.evidence || "无证据"}
                </pre>
              </CardContent>
            </Card>
            <Card>
              <CardHeader className="flex-row items-center justify-between">
                <CardTitle className="flex items-center gap-2 text-sm">
                  <FileTextIcon className="size-4" /> 详细报告
                </CardTitle>
                <Button
                  size="sm"
                  variant={editing ? "default" : "outline"}
                  onClick={() => setEditing((value) => !value)}
                >
                  {editing ? "预览" : "编辑"}
                </Button>
              </CardHeader>
              <CardContent className="space-y-3">
                {editing ? (
                  <>
                    <Textarea
                      value={report}
                      onChange={(event) => setReport(event.target.value)}
                      rows={18}
                      className="font-mono text-xs"
                    />
                    <Button onClick={() => void saveReport()} disabled={saving}>
                      <CheckIcon /> {saving ? "保存中..." : "保存报告"}
                    </Button>
                  </>
                ) : (
                  <ReportPreview report={finding.report} />
                )}
              </CardContent>
            </Card>
          </div>

          <div className="space-y-3 border-l pl-4 text-sm">
            <div>
              <div className="text-muted-foreground text-xs">发现时间</div>
              <div className="mt-1">{new Date(finding.ts).toLocaleString("zh-CN")}</div>
            </div>
            <div>
              <div className="text-muted-foreground text-xs">所属任务</div>
              <div className="mt-1">
                {finding.task_id ? (
                  <Link className="text-primary hover:underline" href={`/function/tasks/detail?id=${finding.task_id}`}>
                    {finding.task_description || `任务 #${finding.task_id}`}
                  </Link>
                ) : (
                  "原任务已删除"
                )}
              </div>
            </div>
            <div>
              <div className="text-muted-foreground text-xs">关联企业</div>
              <div className="mt-1">
                {finding.company_ids?.length ? finding.company_ids.map((value) => `#${value}`).join(", ") : "未归属"}
              </div>
            </div>
          </div>
        </TabsContent>

        <TabsContent value="lineage" className="pt-3">
          <FindingLineage findingId={finding.id} />
        </TabsContent>
      </Tabs>
    </div>
  );
}

export default function FindingDetailPage() {
  return (
    <React.Suspense fallback={<div className="p-8 text-muted-foreground text-sm">加载中...</div>}>
      <FindingDetailContent />
    </React.Suspense>
  );
}
