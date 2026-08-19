"use client";

import * as React from "react";

import Link from "next/link";

import { Loader2 } from "lucide-react";

import { useCurrentUser } from "@/hooks/use-current-user";
import { cn } from "@/lib/utils";
import { settingsSections } from "@/navigation/sidebar/sidebar-items";

// SettingsPage（client 部分）：学 kanna 布局 —— 左侧分节栏 + 右侧 iframe 内嵌现有页面。
export function SettingsSectionClient({ section }: { section: string }) {
  const currentUser = useCurrentUser();
  const activeId = section;

  // 权限过滤：admin 放行；permissions 未加载（FALLBACK）时显示全部，避免
  // 切换瞬间"只剩工作空间/资产同步"的闪烁。
  const userLoaded = currentUser.permissions !== undefined || currentUser.admin === true;
  const sections = userLoaded
    ? settingsSections.filter((s) => !s.perm || currentUser.admin || (currentUser.permissions ?? []).includes(s.perm))
    : settingsSections;
  const active = sections.find((s) => s.id === activeId) ?? sections[0];

  // iframe 加载状态：切换分节时短暂显示 loading，避免白屏闪烁。
  const [loading, setLoading] = React.useState(true);
  React.useEffect(() => {
    setLoading(true);
  }, []);

  return (
    <div
      data-content-padding="false"
      className="flex min-h-0 flex-1 flex-col [background:linear-gradient(180deg,#E8F8E9_0%,#FFFFFF_100%)]"
    >
      <div className="flex items-center gap-2 border-b px-4 py-3 lg:px-6">
        <h1 className="font-semibold text-xl tracking-tight">设置</h1>
        {active && <span className="text-muted-foreground text-sm">{active.label}</span>}
      </div>
      <div className="flex min-h-0 flex-1">
        {/* 左侧分节栏（学 kanna SettingsPage registry） */}
        <nav className="w-52 shrink-0 overflow-y-auto border-r bg-muted/30 p-2">
          <div className="flex flex-col gap-0.5">
            {sections.map((s) => {
              const Icon = s.icon;
              const isActive = s.id === active?.id;
              return (
                <Link
                  key={s.id}
                  prefetch={false}
                  href={`/settings/${s.id}`}
                  className={cn(
                    "flex items-center gap-2 rounded-md px-2 py-1.5 text-sm transition-colors",
                    isActive
                      ? "bg-accent font-medium text-accent-foreground"
                      : "text-muted-foreground hover:bg-accent/50 hover:text-foreground",
                  )}
                >
                  {Icon && <Icon className="size-4 shrink-0" />}
                  <span className="truncate">{s.label}</span>
                </Link>
              );
            })}
          </div>
        </nav>

        {/* 右侧内容区：iframe 内嵌现有页面（?embed=1 隐藏内嵌页的侧栏/头部） */}
        <div className="relative min-h-0 min-w-0 flex-1">
          {loading && (
            <div className="absolute inset-0 z-10 flex items-center justify-center bg-white">
              <Loader2 className="size-6 animate-spin text-muted-foreground" />
            </div>
          )}
          {active ? (
            <iframe
              key={active.id}
              src={`${active.url}?embed=1`}
              onLoad={() => setLoading(false)}
              className="h-full w-full border-0 bg-transparent"
              title={active.label}
            />
          ) : (
            <div className="flex h-full items-center justify-center text-muted-foreground text-sm">无可用设置分节</div>
          )}
        </div>
      </div>
    </div>
  );
}
