"use client";

import Link from "next/link";

import { Settings2 } from "lucide-react";

import { SidebarMenu, SidebarMenuButton, SidebarMenuItem, useSidebar } from "@/components/ui/sidebar";
import { useCurrentUser } from "@/hooks/use-current-user";

import { AccountSwitcher } from "./account-switcher";

// SidebarFooter：侧栏底部 —— ⚙ 设置入口（/settings）+ 用户菜单（修改密码/退出登录）。
export function SidebarFooter() {
  const currentUser = useCurrentUser();
  const { state, isMobile } = useSidebar();
  const isCollapsed = state === "collapsed" && !isMobile;

  // 折叠态只显示设置图标（用户菜单在折叠态难以承载，保持展开态展示）。
  return (
    <div className="border-t p-2">
      {isCollapsed ? (
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton asChild tooltip="设置">
              <Link prefetch={false} href="/settings">
                <Settings2 />
                <span>设置</span>
              </Link>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      ) : (
        <div className="flex items-center justify-between gap-1">
          <SidebarMenu className="flex-1">
            <SidebarMenuItem>
              <SidebarMenuButton asChild tooltip="设置">
                <Link prefetch={false} href="/settings">
                  <Settings2 />
                  <span>设置</span>
                </Link>
              </SidebarMenuButton>
            </SidebarMenuItem>
          </SidebarMenu>
          <AccountSwitcher users={[currentUser]} />
        </div>
      )}
    </div>
  );
}
