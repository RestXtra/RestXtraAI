"use client";

import * as React from "react";

import { useRouter } from "next/navigation";

import { ArchiveIcon, ArchiveRestoreIcon, Bot, FolderIcon, MessageCircleIcon, PinIcon, Trash2Icon } from "lucide-react";
import { toast } from "sonner";

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { ScrollArea } from "@/components/ui/scroll-area";
import { SidebarMenuButton } from "@/components/ui/sidebar";
import { api } from "@/lib/api";
import type { Agent, Company, Conversation } from "@/lib/types";
import { cn } from "@/lib/utils";
import { useChatNavStore } from "@/stores/chat-nav-store";

function ConversationItem({
  conv,
  active,
  onSelect,
  onDelete,
  pinned,
  archived,
  onTogglePin,
}: {
  conv: Conversation;
  active: boolean;
  onSelect: () => void;
  onDelete: () => void;
  pinned: boolean;
  archived: boolean;
  onTogglePin: () => void;
}) {
  return (
    <div
      className={cn(
        "group ml-1 flex min-w-0 items-center gap-1 rounded-lg py-0.5 pr-1 transition-colors",
        active ? "conversation-active text-sidebar-accent-foreground" : "hover:bg-sidebar-accent/60",
      )}
    >
      <button
        type="button"
        onClick={onSelect}
        className="min-w-0 flex-1 rounded-lg px-2 py-1 text-left"
        title={conv.title || "新对话"}
      >
        <div className="truncate text-[13px]">{conv.title || "新对话"}</div>
        <div className="flex min-w-0 items-center gap-1 text-[10px] text-muted-foreground">
          <Bot className="size-2.5 shrink-0" />
          <span className="shrink-0 opacity-60">
            · {new Date(conv.created_at).toLocaleDateString("zh-CN", { month: "numeric", day: "numeric" })}
          </span>
        </div>
      </button>
      <div className="flex shrink-0 items-center opacity-0 transition-opacity group-hover:opacity-100">
        <Button
          variant="ghost"
          size="icon-sm"
          className="size-7"
          title={pinned ? "取消置顶" : "置顶"}
          onClick={onTogglePin}
        >
          <PinIcon className={cn("size-3.5", pinned && "fill-current text-primary")} />
        </Button>
      </div>
      <AlertDialog>
        <AlertDialogTrigger asChild>
          <Button
            variant="ghost"
            size="icon-sm"
            className="shrink-0 text-muted-foreground opacity-0 transition-opacity hover:text-destructive group-hover:opacity-100"
            aria-label="删除对话"
          >
            <Trash2Icon className="size-3.5" />
          </Button>
        </AlertDialogTrigger>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除对话「{conv.title || "新对话"}」？</AlertDialogTitle>
            <AlertDialogDescription>此操作不可撤销。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={onDelete}>删除</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

// ConversationList：侧栏会话列表（学 kanna ThreadRow 形态）。
// 点击会话 → 跳转 /chat?id=<id>；点击新建 → 跳转 /chat（新建态）。
export function ConversationList() {
  const router = useRouter();
  const [convs, setConvs] = React.useState<Conversation[]>([]);
  const [agents, setAgents] = React.useState<Agent[]>([]);
  const [companies, setCompanies] = React.useState<Company[]>([]);
  const selectedId = useChatNavStore((s) => s.selectedId);
  const _bump = useChatNavStore((s) => s.bump);
  const select = useChatNavStore((s) => s.select);
  const [meta, setMeta] = React.useState<Record<string, { pinned?: boolean; archived?: boolean }>>({});
  React.useEffect(() => {
    try {
      setMeta(JSON.parse(localStorage.getItem("restxtra.conversation-meta") || "{}"));
    } catch {
      /* ignore */
    }
  }, []);
  React.useEffect(() => {
    api
      .companies()
      .then(setCompanies)
      .catch(() => {});
  }, []);

  const updateMeta = React.useCallback((id: number, patch: { pinned?: boolean; archived?: boolean }) => {
    setMeta((prev) => {
      const next = { ...prev, [id]: { ...prev[id], ...patch } };
      try {
        localStorage.setItem("restxtra.conversation-meta", JSON.stringify(next));
      } catch {
        /* ignore */
      }
      return next;
    });
  }, []);
  const archiveGroup = React.useCallback((items: Conversation[]) => {
    setMeta((prev) => {
      const next = { ...prev };
      for (const c of items) next[c.id] = { ...next[c.id], archived: true };
      try {
        localStorage.setItem("restxtra.conversation-meta", JSON.stringify(next));
      } catch {
        /* ignore */
      }
      return next;
    });
  }, []);
  const restoreGroup = React.useCallback((items: Conversation[]) => {
    setMeta((prev) => {
      const next = { ...prev };
      for (const c of items) next[c.id] = { ...next[c.id], archived: false };
      try {
        localStorage.setItem("restxtra.conversation-meta", JSON.stringify(next));
      } catch {
        /* ignore */
      }
      return next;
    });
  }, []);
  const grouped = React.useMemo(() => {
    const map = new Map<string, Conversation[]>();
    for (const c of [...convs]
      .filter((c) => !meta[c.id]?.archived)
      .sort(
        (a, b) =>
          Number(Boolean(meta[b.id]?.pinned)) - Number(Boolean(meta[a.id]?.pinned)) ||
          +new Date(b.updated_at) - +new Date(a.updated_at),
      )) {
      const key = String(c.company_id ?? 0);
      map.set(key, [...(map.get(key) ?? []), c]);
    }
    return [...map.entries()].sort(([a], [b]) => Number(b) - Number(a));
  }, [convs, meta]);
  const archivedConvs = convs
    .filter((c) => meta[c.id]?.archived)
    .sort((a, b) => +new Date(b.updated_at) - +new Date(a.updated_at));
  const archivedGrouped = React.useMemo(() => {
    const map = new Map<string, Conversation[]>();
    for (const c of archivedConvs) {
      const key = String(c.company_id ?? 0);
      map.set(key, [...(map.get(key) ?? []), c]);
    }
    return [...map.entries()].sort(([a], [b]) => Number(b) - Number(a));
  }, [archivedConvs]);

  const reload = React.useCallback(() => {
    api
      .conversations()
      .then(setConvs)
      .catch(() => setConvs([]));
  }, []);
  React.useEffect(() => {
    if (_bump >= 0) reload();
  }, [reload, _bump]);
  React.useEffect(() => {
    api
      .agents()
      .then(setAgents)
      .catch(() => {});
  }, []);

  function openChat(id: number | null) {
    select(id);
    router.push(id == null ? "/chat" : `/chat?id=${id}`);
  }

  async function del(id: number) {
    try {
      await api.deleteConversation(id);
      if (useChatNavStore.getState().selectedId === id) select(null);
      reload();
      useChatNavStore.getState().refresh();
    } catch (e) {
      toast.error(`删除失败：${(e as Error).message}`);
    }
  }

  return (
    <div className="flex min-h-0 flex-col">
      <ScrollArea type="auto" className="[&_[data-slot=scroll-area-viewport]>div]:block! min-h-0 flex-1">
        <div className="flex min-w-0 flex-col gap-0.5 px-2 pb-2">
          {convs.length === 0 && <p className="px-2 py-6 text-center text-muted-foreground text-xs">暂无对话</p>}
          {grouped
            .filter(([key]) => key !== "0")
            .map(([companyKey, items]) => (
              <details key={companyKey} open className="mt-2 first:mt-0">
                <summary className="flex cursor-pointer list-none items-center gap-2 border-b border-sidebar-border/50 px-2 py-1 font-medium text-[11px] text-muted-foreground">
                  <FolderIcon className="size-3.5" />
                  <span className="min-w-0 flex-1 truncate">
                    {companies.find((x) => String(x.id) === companyKey)?.name ?? "企业项目"}
                  </span>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    className="size-6"
                    title="归档整个项目"
                    onClick={(e) => {
                      e.preventDefault();
                      archiveGroup(items);
                    }}
                  >
                    <ArchiveIcon className="size-3.5" />
                  </Button>
                </summary>
                {items.map((c) => (
                  <ConversationItem
                    key={c.id}
                    conv={c}
                    active={selectedId === c.id}
                    onSelect={() => openChat(c.id)}
                    onDelete={() => del(c.id)}
                    pinned={Boolean(meta[c.id]?.pinned)}
                    archived={Boolean(meta[c.id]?.archived)}
                    onTogglePin={() => updateMeta(c.id, { pinned: !meta[c.id]?.pinned })}
                  />
                ))}
              </details>
            ))}
          {grouped.find(([key]) => key === "0")?.[1].length ? (
            <details open className="mt-2">
              <summary className="flex cursor-pointer list-none items-center gap-2 border-b border-sidebar-border/50 px-2 py-1 font-medium text-[11px] text-muted-foreground">
                <FolderIcon className="size-3.5" />
                <span className="min-w-0 flex-1 truncate">未关联企业</span>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  className="size-6"
                  title="归档整个项目"
                  onClick={(e) => {
                    e.preventDefault();
                    archiveGroup(grouped.find(([key]) => key === "0")?.[1] ?? []);
                  }}
                >
                  <ArchiveIcon className="size-3.5" />
                </Button>
              </summary>
              {grouped
                .find(([key]) => key === "0")?.[1]
                .map((c) => (
                  <ConversationItem
                    key={c.id}
                    conv={c}
                    active={selectedId === c.id}
                    onSelect={() => openChat(c.id)}
                    onDelete={() => del(c.id)}
                    pinned={Boolean(meta[c.id]?.pinned)}
                    archived={false}
                    onTogglePin={() => updateMeta(c.id, { pinned: !meta[c.id]?.pinned })}
                  />
                ))}
            </details>
          ) : null}
          {archivedConvs.length > 0 && (
            <details className="mt-3 border-t border-sidebar-border/60 pt-2">
              <summary className="cursor-pointer px-2 py-1 font-medium text-[11px] text-muted-foreground">
                已归档 ({archivedConvs.length})
              </summary>
              <div className="mt-1 flex flex-col gap-1">
                {archivedGrouped.map(([companyKey, items]) => (
                  <details key={companyKey} className="rounded-md" open>
                    <summary className="flex cursor-pointer list-none items-center gap-2 border-b border-sidebar-border/40 px-2 py-1 font-medium text-[11px] text-muted-foreground">
                      <FolderIcon className="size-3.5" />
                      <span className="min-w-0 flex-1 truncate">
                        {companyKey === "0"
                          ? "未关联企业"
                          : (companies.find((x) => String(x.id) === companyKey)?.name ?? "企业项目")}
                      </span>
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        className="size-6"
                        title="恢复整个项目"
                        onClick={(e) => {
                          e.preventDefault();
                          restoreGroup(items);
                        }}
                      >
                        <ArchiveRestoreIcon className="size-3.5" />
                      </Button>
                    </summary>
                    {items.map((c) => (
                      <ConversationItem
                        key={c.id}
                        conv={c}
                        active={selectedId === c.id}
                        onSelect={() => openChat(c.id)}
                        onDelete={() => del(c.id)}
                        pinned={Boolean(meta[c.id]?.pinned)}
                        archived
                        onTogglePin={() => updateMeta(c.id, { pinned: !meta[c.id]?.pinned })}
                      />
                    ))}
                  </details>
                ))}
              </div>
            </details>
          )}
        </div>
      </ScrollArea>
    </div>
  );
}

export function CollapsedConversationLauncher() {
  const router = useRouter();
  const [open, setOpen] = React.useState(false);
  const [convs, setConvs] = React.useState<Conversation[]>([]);
  React.useEffect(() => {
    api
      .conversations()
      .then(setConvs)
      .catch(() => {});
  }, []);
  const recent = [...convs].sort((a, b) => +new Date(b.updated_at) - +new Date(a.updated_at)).slice(0, 8);
  return (
    // px-2 与 NavMain 的 SidebarGroup(p-2) 对齐基准一致，保证折叠态图标同列
    <div className="flex flex-col px-2 py-1">
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <SidebarMenuButton tooltip="最近对话" isActive={open} className="text-[13px]">
            <MessageCircleIcon />
            <span className="truncate">最近对话</span>
          </SidebarMenuButton>
        </PopoverTrigger>
        <PopoverContent side="right" align="start" className="w-72 p-2">
          <p className="px-2 py-1.5 font-medium text-muted-foreground text-xs">最近对话</p>
          {recent.length === 0 ? (
            <p className="px-2 py-4 text-center text-muted-foreground text-xs">暂无对话</p>
          ) : (
            recent.map((c) => (
              <button
                key={c.id}
                type="button"
                className="flex w-full items-center gap-2 rounded-md px-2 py-2 text-left text-sm hover:bg-accent"
                onClick={() => {
                  setOpen(false);
                  router.push(`/chat?id=${c.id}`);
                }}
              >
                <Bot className="size-3.5 shrink-0 text-muted-foreground" />
                <span className="truncate">{c.title || "新对话"}</span>
              </button>
            ))
          )}
        </PopoverContent>
      </Popover>
    </div>
  );
}
