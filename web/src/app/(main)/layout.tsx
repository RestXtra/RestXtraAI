"use client";

import type { ReactNode } from "react";
import * as React from "react";

import { AppSidebar } from "@/app/(main)/_components/sidebar/app-sidebar";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
import { auth } from "@/lib/auth";
import { getClientCookie } from "@/lib/cookie.client";
import { applyContentLayout, applyNavbarStyle } from "@/lib/preferences/layout-utils";
import { cn } from "@/lib/utils";
import { usePreferencesStore } from "@/stores/preferences/preferences-provider";

import { MainContent } from "./_components/main-content";

// embed 模式：iframe 内嵌（/xxx?embed=1）时不渲染侧栏与全局头，只显示内容区。
function isEmbed() {
  if (typeof window === "undefined") return false;
  return new URLSearchParams(window.location.search).get("embed") === "1";
}

export default function Layout({ children }: Readonly<{ children: ReactNode }>) {
  // Client-side auth gate — replaces the Next proxy/middleware that static export
  // disables. No token → bounce to /login; render nothing until confirmed so no
  // protected UI (or its API calls) flashes for a logged-out visitor.
  const [authed, setAuthed] = React.useState(false);
  React.useEffect(() => {
    if (auth.getToken()) {
      setAuthed(true);
    } else {
      window.location.href = "/login";
    }
  }, []);

  const [embed, setEmbed] = React.useState(false);
  React.useEffect(() => {
    setEmbed(isEmbed());
  }, []);

  const defaultOpen = typeof document === "undefined" ? true : getClientCookie("sidebar_state") !== "false";
  // Live from the preferences store so the sidebar reacts immediately to changes
  // made in 系统配置 → 界面与布局 (not a one-shot cookie read).
  const variant = usePreferencesStore((s) => s.sidebarVariant);
  const collapsible = usePreferencesStore((s) => s.sidebarCollapsible);
  const contentLayout = usePreferencesStore((s) => s.contentLayout);
  const navbarStyle = usePreferencesStore((s) => s.navbarStyle);
  const setSidebarVariant = usePreferencesStore((s) => s.setSidebarVariant);
  const setSidebarCollapsible = usePreferencesStore((s) => s.setSidebarCollapsible);

  // 从 /settings 的 iframe 接收偏好变更（postMessage），实时同步父页面侧栏。
  React.useEffect(() => {
    function onMessage(e: MessageEvent) {
      const d = e.data as { type?: string; key?: string; value?: string };
      if (d?.type !== "restxtra:pref") return;
      if (d.key === "sidebar_variant" && d.value) setSidebarVariant(d.value as typeof variant);
      if (d.key === "sidebar_collapsible" && d.value) setSidebarCollapsible(d.value as typeof collapsible);
    }
    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, [setSidebarVariant, setSidebarCollapsible]);

  // Sync the html data-* attributes (which the CSS-driven rules for the content
  // wrapper and sticky header read) whenever the store changes, so switching
  // 居中/通栏 and 固定/随页面滚动 applies immediately without a reload.
  React.useEffect(() => {
    applyContentLayout(contentLayout);
  }, [contentLayout]);
  React.useEffect(() => {
    applyNavbarStyle(navbarStyle);
  }, [navbarStyle]);

  if (!authed) return null;

  // embed 模式：iframe 内嵌，跳过侧栏 + 顶部 header，直接渲染内容。
  if (embed) {
    return (
      <div className="h-screen w-full overflow-y-auto">
        <MainContent embed>{children}</MainContent>
      </div>
    );
  }

  return (
    <SidebarProvider
      defaultOpen={defaultOpen}
      style={
        {
          "--sidebar-width": "calc(var(--spacing) * 68)",
        } as React.CSSProperties
      }
    >
      <AppSidebar variant={variant} collapsible={collapsible} />
      <SidebarInset
        className={cn(
          "[html[data-content-layout=centered]_&>*]:mx-auto",
          "[html[data-content-layout=centered]_&>*]:w-full",
          "[html[data-content-layout=centered]_&>*]:max-w-screen-2xl",
          "peer-data-[variant=inset]:border",
          "[--dashboard-header-height:--spacing(12)]",
          "min-w-0 overflow-x-hidden",
        )}
      >
        <MainContent>{children}</MainContent>
      </SidebarInset>
    </SidebarProvider>
  );
}
