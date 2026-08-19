"use client";

import * as React from "react";

import { useRouter } from "next/navigation";

import { ArchiveIcon, Bot, Check, MessageCircleIcon, PinIcon, Trash2Icon } from "lucide-react";
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
import type { Agent, Conversation } from "@/lib/types";
import { cn } from "@/lib/utils";
import { useChatNavStore } from "@/stores/chat-nav-store";

function ConversationItem({
  conv,
  agent,
  active,
  onSelect,
  onDelete,
  pinned,
  archived,
  onTogglePin,
  onToggleArchive,
}: {
  conv: Conversation;
  agent?: Agent;
  active: boolean;
  onSelect: () => void;
  onDelete: () => void;
  pinned: boolean;
  archived: boolean;
  onTogglePin: () => void;
  onToggleArchive: () => void;
}) {
  return (
    <div
      className={cn(
        "group ml-1 flex min-w-0 items-center gap-1 rounded-lg py-0.5 pr-1 transition-colors",
        active ? "bg-accent text-accent-foreground" : "hover:bg-accent/50",
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
          <span className="min-w-0 truncate">{agent?.name ?? conv.agent_key}</span>
          <span className="shrink-0 opacity-60">
            · {new Date(conv.created_at).toLocaleDateString("zh-CN", { month: "numeric", day: "numeric" })}
          </span>
        </div>
      </button>
      <div className="flex shrink-0 items-center opacity-0 transition-opacity group-hover:opacity-100">
        <Button variant="ghost" size="icon-sm" className="size-7" title={pinned ? "取消置顶" : "置顶"} onClick={onTogglePin}>
          <PinIcon className={cn("size-3.5", pinned && "fill-current text-primary")} />
        </Button>
        <Button variant="ghost" size="icon-sm" className="size-7" title={archived ? "取消归档" : "归档"} onClick={onToggleArchive}>
          {archived ? <Check className="size-3.5" /> : <ArchiveIcon className="size-3.5" />}
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
  const selectedId = useChatNavStore((s) => s.selectedId);
  const _bump = useChatNavStore((s) => s.bump);
  const select = useChatNavStore((s) => s.select);
  const [meta, setMeta] = React.useState<Record<string, { pinned?: boolean; archived?: boolean }>>({});
  React.useEffect(() => {
    try { setMeta(JSON.parse(localStorage.getItem("restxtra.conversation-meta") || "{}")); } catch { /* ignore */ }
  }, []);
  const updateMeta = React.useCallback((id: number, patch: { pinned?: boolean; archived?: boolean }) => {
    setMeta((prev) => {
      const next = { ...prev, [id]: { ...prev[id], ...patch } };
      try { localStorage.setItem("restxtra.conversation-meta", JSON.stringify(next)); } catch { /* ignore */ }
      return next;
    });
  }, []);
  const visible = convs.filter((c) => !meta[c.id]?.archived).sort((a, b) => Number(Boolean(meta[b.id]?.pinned)) - Number(Boolean(meta[a.id]?.pinned)) || +new Date(b.updated_at) - +new Date(a.updated_at));
  const archived = convs.filter((c) => meta[c.id]?.archived).sort((a, b) => +new Date(b.updated_at) - +new Date(a.updated_at));

  const reload = React.useCallback(() => {
    api
      .conversations()
      .then(setConvs)
      .catch(() => setConvs([]));
  }, []);
  React.useEffect(() => {
    reload();
  }, [reload]);
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
          {visible.map((c) => (
            <ConversationItem
              key={c.id}
              conv={c}
              agent={agents.find((a) => a.key === c.agent_key)}
              active={selectedId === c.id}
              onSelect={() => openChat(c.id)}
              onDelete={() => del(c.id)}
              pinned={Boolean(meta[c.id]?.pinned)}
              archived={Boolean(meta[c.id]?.archived)}
              onTogglePin={() => updateMeta(c.id, { pinned: !meta[c.id]?.pinned })}
              onToggleArchive={() => updateMeta(c.id, { archived: !meta[c.id]?.archived })}
            />
          ))}
          {archived.length > 0 && (
            <details className="mt-2 border-t pt-2">
              <summary className="cursor-pointer px-2 py-1 font-medium text-[11px] text-muted-foreground">已归档 ({archived.length})</summary>
              {archived.map((c) => (
                <ConversationItem
                  key={c.id}
                  conv={c}
                  agent={agents.find((a) => a.key === c.agent_key)}
                  active={selectedId === c.id}
                  onSelect={() => openChat(c.id)}
                  onDelete={() => del(c.id)}
                  pinned={false}
                  archived
                  onTogglePin={() => updateMeta(c.id, { pinned: true })}
                  onToggleArchive={() => updateMeta(c.id, { archived: false })}
                />
              ))}
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
  React.useEffect(() => { api.conversations().then(setConvs).catch(() => {}); }, []);
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
          {recent.length === 0 ? <p className="px-2 py-4 text-center text-muted-foreground text-xs">暂无对话</p> : recent.map((c) => (
            <button key={c.id} type="button" className="flex w-full items-center gap-2 rounded-md px-2 py-2 text-left text-sm hover:bg-accent" onClick={() => { setOpen(false); router.push(`/chat?id=${c.id}`); }}>
              <Bot className="size-3.5 shrink-0 text-muted-foreground" /><span className="truncate">{c.title || "新对话"}</span>
            </button>
          ))}
        </PopoverContent>
      </Popover>
    </div>
  );
}
