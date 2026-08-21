"use client";

import * as React from "react";

import { BookOpenIcon, PlusIcon, SearchIcon, Trash2Icon } from "lucide-react";
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
import { Textarea } from "@/components/ui/textarea";
import { api } from "@/lib/api";
import type { KnowledgeItem } from "@/lib/types";

function ItemForm({ onSaved }: { onSaved: () => void }) {
  const [open, setOpen] = React.useState(false);
  const [title, setTitle] = React.useState("");
  const [tags, setTags] = React.useState("");
  const [content, setContent] = React.useState("");

  async function save() {
    if (!title.trim()) {
      toast.error("标题必填");
      return;
    }
    try {
      await api.saveKnowledge({ title: title.trim(), content, tags: tags.trim() });
      toast.success("已保存");
      setTitle("");
      setTags("");
      setContent("");
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
          <PlusIcon /> 新增文档
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>新增知识库文档</DialogTitle>
          <DialogDescription>文档可被 agent 的 search_knowledge 工具在任务中检索。</DialogDescription>
        </DialogHeader>
        <div className="grid gap-3 py-2">
          <div className="grid gap-2">
            <Label htmlFor="kb-title">标题</Label>
            <Input
              id="kb-title"
              placeholder="例如：SQL 注入绕过手法"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="kb-tags">标签（逗号分隔，可选）</Label>
            <Input
              id="kb-tags"
              placeholder="sqli, waf, bypass"
              value={tags}
              onChange={(e) => setTags(e.target.value)}
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="kb-content">内容</Label>
            <Textarea id="kb-content" rows={10} value={content} onChange={(e) => setContent(e.target.value)} />
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

export default function KnowledgePage() {
  const [items, setItems] = React.useState<KnowledgeItem[]>([]);
  const [q, setQ] = React.useState("");
  const [selected, setSelected] = React.useState<KnowledgeItem | null>(null);

  const load = React.useCallback(() => {
    api
      .knowledge()
      .then(setItems)
      .catch(() => setItems([]));
  }, []);
  React.useEffect(() => {
    load();
  }, [load]);

  const search = async () => {
    if (!q.trim()) return load();
    const r = await api.knowledgeSearch(q.trim());
    setItems(r);
  };

  const remove = async (k: KnowledgeItem) => {
    try {
      await api.deleteKnowledge(k.id);
      toast.success("已删除");
      if (selected?.id === k.id) setSelected(null);
      load();
    } catch (e) {
      toast.error(`删除失败：${(e as Error).message}`);
    }
  };

  return (
    <div className="flex flex-1 flex-col gap-4">
      <div className="flex items-center justify-between gap-4">
        <div className="flex items-center gap-2">
          <BookOpenIcon className="size-5 text-muted-foreground" />
          <h1 className="font-semibold text-xl tracking-tight">知识库</h1>
          <Badge variant="secondary">{items.length}</Badge>
        </div>
        <ItemForm onSaved={load} />
      </div>
      <div className="relative max-w-md">
        <SearchIcon className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          placeholder="检索：sqli / xss / cloud / evasion…"
          className="pl-8"
          value={q}
          onChange={(e) => setQ(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && search()}
        />
      </div>

      <div className="grid min-h-0 flex-1 grid-cols-1 gap-4 lg:grid-cols-2">
        <Card className="overflow-hidden py-0">
          <CardContent className="max-h-[62vh] overflow-auto p-2">
            {items.length === 0 ? (
              <div className="py-12 text-center text-muted-foreground text-sm">
                暂无文档，点击「新增文档」录入漏洞手法/playbook。
              </div>
            ) : (
              items.map((k) => (
                <div
                  key={k.id}
                  className="flex items-center gap-2 rounded-md border-b px-2 py-2 last:border-0 hover:bg-accent"
                >
                  <button type="button" className="min-w-0 flex-1 text-left" onClick={() => setSelected(k)}>
                    <div className="truncate font-medium text-sm">{k.title}</div>
                    {k.tags && (
                      <div className="mt-0.5 flex flex-wrap gap-1">
                        {k.tags
                          .split(",")
                          .map((t) => t.trim())
                          .filter(Boolean)
                          .map((t) => (
                            <Badge key={t} variant="outline" className="text-[10px]">
                              {t}
                            </Badge>
                          ))}
                      </div>
                    )}
                  </button>
                  <Button size="icon" variant="ghost" aria-label="删除" onClick={() => remove(k)}>
                    <Trash2Icon className="text-destructive" />
                  </Button>
                </div>
              ))
            )}
          </CardContent>
        </Card>
        <Card className="overflow-hidden py-0">
          {selected ? (
            <CardContent className="p-0">
              <div className="flex items-center justify-between gap-2 border-b bg-muted/50 px-3 py-2">
                <span className="truncate font-medium text-sm">{selected.title}</span>
                <Badge variant="outline" className="shrink-0 text-[10px]">
                  {selected.tags || "—"}
                </Badge>
              </div>
              <pre className="max-h-[58vh] overflow-auto whitespace-pre-wrap break-all p-3 font-mono text-xs">
                {selected.content}
              </pre>
            </CardContent>
          ) : (
            <CardContent className="flex items-center justify-center py-16 text-muted-foreground text-sm">
              点击左侧文档查看内容
            </CardContent>
          )}
        </Card>
      </div>
    </div>
  );
}
