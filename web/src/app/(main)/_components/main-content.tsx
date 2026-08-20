"use client";

import type { ReactNode } from "react";

import { usePathname } from "next/navigation";

import { usePageTitle } from "@/hooks/use-page-title";
import { cn } from "@/lib/utils";

// 任务详情页、设置页保持原样：它们自带头部/Tabs 与内边距，不再叠加全局头部和 padding。
function isFullBleed(pathname: string) {
  const p = (() => {
    try {
      return decodeURIComponent(pathname);
    } catch {
      return pathname;
    }
  })();
  return p.startsWith("/function/tasks/") || p.startsWith("/settings");
}

export function MainContent({ children, embed }: { children: ReactNode; embed?: boolean }) {
  const pathname = usePathname();
  const title = usePageTitle();

  // embed 模式（iframe 内嵌到 /settings）：无全局头部；提供统一内边距，
  // 让依赖外层 padding 的管理页（LLM/工具等）不贴顶贴边。
  if (embed) {
    return <div className="h-full w-full overflow-y-auto p-4 md:p-6">{children}</div>;
  }

  if (isFullBleed(pathname)) {
    return <>{children}</>;
  }

  return (
    <>
      <header
        className={cn(
          "flex min-w-0 h-12 shrink-0 items-center gap-2 border-b transition-[width,height] ease-linear group-has-data-[collapsible=icon]/sidebar-wrapper:h-12",
          "codex-shell-header",
          "[html[data-navbar-style=sticky]_&]:sticky [html[data-navbar-style=sticky]_&]:top-0 [html[data-navbar-style=sticky]_&]:z-50 [html[data-navbar-style=sticky]_&]:overflow-hidden [html[data-navbar-style=sticky]_&]:rounded-t-[inherit] [html[data-navbar-style=sticky]_&]:bg-background/50 [html[data-navbar-style=sticky]_&]:backdrop-blur-md",
        )}
      >
        <div className="flex min-w-0 w-full items-center px-4 lg:px-6">
          <h1 className="truncate font-medium text-sm">{title}</h1>
        </div>
      </header>
      <div className="min-h-0 min-w-0 flex-1 overflow-x-hidden p-4 has-data-[content-padding=false]:p-0 md:p-6 md:has-data-[content-padding=false]:p-0">
        {children}
      </div>
    </>
  );
}
