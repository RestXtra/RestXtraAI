"use client";

import * as React from "react";

import { ChevronRightIcon, FileIcon, FolderIcon, ServerIcon } from "lucide-react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { api } from "@/lib/api";
import type { WorkspaceEntry } from "@/lib/types";

export default function WorkspacePage() {
  const [path, setPath] = React.useState("");
  const [entries, setEntries] = React.useState<WorkspaceEntry[]>([]);
  const [file, setFile] = React.useState<{ path: string; content: string } | null>(null);

  const load = React.useCallback((p: string) => {
    api
      .workspaceList(p)
      .then((r) => setEntries(r.entries ?? []))
      .catch(() => setEntries([]));
  }, []);
  React.useEffect(() => {
    load(path);
  }, [path, load]);

  const crumbs = path.split("/").filter(Boolean);
  const go = (p: string) => {
    setFile(null);
    setPath(p);
  };

  const readFile = async (name: string) => {
    try {
      const p = `${path}/${name}`;
      const r = await api.workspaceRead(p);
      setFile(r);
    } catch (e) {
      toast.error(`读取失败：${(e as Error).message}`);
    }
  };

  return (
    <div className="flex flex-1 flex-col gap-4">
      <div className="flex items-center gap-2">
        <ServerIcon className="size-5 text-muted-foreground" />
        <h1 className="font-semibold text-xl tracking-tight">工作空间</h1>
        <Badge variant="secondary">共享工作目录</Badge>
      </div>
      <p className="text-muted-foreground text-sm">
        Agent 写入中间产物的共享目录（Bash 的 CWD），可浏览查看脚本/payload/抓包等产物。
      </p>

      <div className="flex flex-wrap items-center gap-1 text-sm">
        <Button size="sm" variant="ghost" onClick={() => go("")}>
          /
        </Button>
        {crumbs.map((c, i) => {
          const p = `/${crumbs.slice(0, i + 1).join("/")}`;
          return (
            <React.Fragment key={p}>
              <ChevronRightIcon className="size-3 text-muted-foreground" />
              <Button size="sm" variant="ghost" onClick={() => go(p)}>
                {c}
              </Button>
            </React.Fragment>
          );
        })}
      </div>

      <div className="grid min-h-0 flex-1 grid-cols-1 gap-4 lg:grid-cols-2">
        <Card className="overflow-hidden py-0">
          <CardContent className="max-h-[65vh] overflow-auto p-2">
            {entries.length === 0 ? (
              <div className="py-12 text-center text-muted-foreground text-sm">目录为空</div>
            ) : (
              entries.map((e) =>
                e.dir ? (
                  <button
                    key={e.name}
                    type="button"
                    onClick={() => go(`${path}/${e.name}`)}
                    className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-foreground text-sm hover:bg-accent"
                  >
                    <FolderIcon className="size-4 text-amber-500" />
                    <span className="truncate">{e.name}</span>
                  </button>
                ) : (
                  <button
                    key={e.name}
                    type="button"
                    onClick={() => readFile(e.name)}
                    className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-muted-foreground text-sm hover:bg-accent"
                  >
                    <FileIcon className="size-4 shrink-0" />
                    <span className="min-w-0 flex-1 truncate">{e.name}</span>
                    <span className="shrink-0 text-xs tabular-nums">{e.size}B</span>
                  </button>
                ),
              )
            )}
          </CardContent>
        </Card>
        <Card className="overflow-hidden py-0">
          {file ? (
            <CardContent className="p-0">
              <div className="truncate border-b bg-muted/50 px-3 py-2 font-mono text-xs">{file.path}</div>
              <pre className="max-h-[60vh] overflow-auto whitespace-pre-wrap break-all p-3 font-mono text-xs">
                {file.content}
              </pre>
            </CardContent>
          ) : (
            <CardContent className="flex items-center justify-center py-16 text-muted-foreground text-sm">
              点击左侧文件查看内容
            </CardContent>
          )}
        </Card>
      </div>
    </div>
  );
}
