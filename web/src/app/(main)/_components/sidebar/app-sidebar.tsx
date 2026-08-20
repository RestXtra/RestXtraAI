"use client";

import Link from "next/link";

import { useShallow } from "zustand/react/shallow";

import {
  Sidebar,
  SidebarContent,
  SidebarHeader,
  SidebarMenuButton,
  SidebarTrigger,
  useSidebar,
} from "@/components/ui/sidebar";
import { APP_CONFIG } from "@/config/app-config";
import { hasPerm, useCurrentUser } from "@/hooks/use-current-user";
import { cn } from "@/lib/utils";
import { type NavGroup, sidebarItems } from "@/navigation/sidebar/sidebar-items";
import { usePreferencesStore } from "@/stores/preferences/preferences-provider";

import { ConversationList } from "./conversation-list";
import { NavMain } from "./nav-main";
import { SearchDialog } from "./search-dialog";
import { SidebarFooter } from "./sidebar-footer";
import { CollapsedConversationLauncher } from "./conversation-list";

// visibleNav hides menu items the signed-in user lacks permission for.
function visibleNav(user: ReturnType<typeof useCurrentUser>): NavGroup[] {
  return sidebarItems
    .map((g) => ({
      ...g,
      items: g.items
        // 一级项（含父项）按 perm 过滤
        .filter((it) => !it.perm || hasPerm(user, it.perm))
        // 父项再过滤子项权限，子项全空则去掉父项
        .filter((it) => !it.subItems || it.subItems.some((sub) => !sub.perm || hasPerm(user, sub.perm)))
        .map((it) =>
          it.subItems ? { ...it, subItems: it.subItems.filter((sub) => !sub.perm || hasPerm(user, sub.perm)) } : it,
        ),
    }))
    .filter((g) => g.items.length > 0);
}

export function AppSidebar({ ...props }: React.ComponentProps<typeof Sidebar>) {
  const currentUser = useCurrentUser();
  const { state, isMobile, toggleSidebar } = useSidebar();
  const { sidebarVariant, sidebarCollapsible } = usePreferencesStore(
    useShallow((s) => ({
      sidebarVariant: s.sidebarVariant,
      sidebarCollapsible: s.sidebarCollapsible,
    })),
  );
  // 侧栏样式/折叠方式直接由偏好 store 驱动（实时生效）。
  const variant = sidebarVariant;
  const collapsible = sidebarCollapsible;
  const isCollapsed = state === "collapsed" && !isMobile;
  const nav = visibleNav(currentUser);

  return (
    <Sidebar
      {...props}
      variant={variant}
      collapsible={collapsible}
      className="bg-sidebar"
    >
      <SidebarHeader>
        {/* 头部行：Logo（即仪表盘/展开入口，靠左）+ 搜索 + 折叠按钮 */}
        <div className="flex items-center gap-0.5 px-1.5 py-1">
          <SidebarMenuButton asChild size="lg" className={cn("flex-1", isCollapsed && "flex-none justify-center p-0")}>
            {isCollapsed ? (
              // 折叠态：点击小 logo 展开侧栏（无独立展开按钮）
              <button
                type="button"
                onClick={toggleSidebar}
                title="展开侧栏"
                className="flex w-full items-center justify-center p-0"
              >
                {/* eslint-disable-next-line @next/next/no-img-element */}
                <img src="/logo.png" alt="RestXtra AI" width={24} height={24} className="shrink-0" />
              </button>
            ) : (
              <Link prefetch={false} href="/dashboard" className="flex items-center gap-2" title="仪表盘">
                {/* eslint-disable-next-line @next/next/no-img-element */}
                <img src="/logo.png" alt="RestXtra AI" width={28} height={28} className="shrink-0" />
                <span className="truncate font-semibold">{APP_CONFIG.name}</span>
              </Link>
            )}
          </SidebarMenuButton>

          {!isCollapsed && (
            <div className="flex shrink-0 items-center gap-0.5">
              <SearchDialog />
              <SidebarTrigger />
            </div>
          )}
        </div>
      </SidebarHeader>

      {isCollapsed && collapsible === "icon" ? (
        // 折叠态（icon 模式）：导航项收窄为图标列（含新建对话/任务/沙箱/插件）
        <SidebarContent className="flex flex-col">
          {/* 导航区可滚动，避免撑满后把下方最近对话挤出/裁剪 */}
          <div className="flex min-h-0 w-full flex-1 flex-col overflow-y-auto">
            <NavMain items={nav} />
            <CollapsedConversationLauncher />
          </div>
        </SidebarContent>
      ) : (
        <SidebarContent className="flex flex-col">
          {/* 一级导航：新建对话 / 任务 / 漏洞发现 / 资产管理 / 沙箱管理 / 插件（扁平同级） */}
          <div className="px-1.5 pt-0.5">
            <NavMain items={nav} />
          </div>

          {/* 项目（会话）栏目：灰色加粗标签，与导航间隔稍宽 */}
          <div className="mt-4 mb-1 flex items-center gap-2 px-3">
            <span className="truncate font-semibold text-[11px] text-muted-foreground">项目</span>
          </div>

          {/* 会话列表（小一级） */}
          <div className="flex min-h-0 flex-1 flex-col">
            <ConversationList />
          </div>
        </SidebarContent>
      )}

      {/* 底部：设置 + 用户菜单；折叠态保留设置图标 */}
      <SidebarFooter />
    </Sidebar>
  );
}
