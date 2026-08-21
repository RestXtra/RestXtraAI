"use client";

import type { ReactNode } from "react";
import * as React from "react";

import { AppSidebar } from "@/app/(main)/_components/sidebar/app-sidebar";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
import { CurrentUserProvider, useCurrentUserState } from "@/hooks/use-current-user";
import { getClientCookie } from "@/lib/cookie.client";
import { applyContentLayout, applyNavbarStyle } from "@/lib/preferences/layout-utils";
import { applyThemeMode, applyThemePreset } from "@/lib/preferences/theme-utils";
import { cn } from "@/lib/utils";
import { usePreferencesStore } from "@/stores/preferences/preferences-provider";

import { MainContent } from "./_components/main-content";

// embed 模式：iframe 内嵌（/xxx?embed=1）时不渲染侧栏与全局头，只显示内容区。
function isEmbed() {
  if (typeof window === "undefined") return false;
  return new URLSearchParams(window.location.search).get("embed") === "1";
}

function AuthenticatedLayout({ children }: Readonly<{ children: ReactNode }>) {
  const { status } = useCurrentUserState();

  // Static export has no server middleware. The shared profile load is the
  // client-side authentication gate and prevents protected UI/API calls flashing.
  React.useEffect(() => {
    if (status === "error") window.location.href = "/login";
  }, [status]);

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
  const setThemeMode = usePreferencesStore((s) => s.setThemeMode);
  const setThemePreset = usePreferencesStore((s) => s.setThemePreset);

  // 从 /settings 的 iframe 接收偏好变更（postMessage），实时同步父页面侧栏。
  React.useEffect(() => {
    function onMessage(e: MessageEvent) {
      const d = e.data as { type?: string; key?: string; value?: string };
      if (d?.type !== "restxtra:pref") return;
      if (d.key === "sidebar_variant" && d.value) setSidebarVariant(d.value as typeof variant);
      if (d.key === "sidebar_collapsible" && d.value) setSidebarCollapsible(d.value as typeof collapsible);
      if (d.key === "theme_mode" && d.value) {
        applyThemeMode(d.value as "light" | "dark" | "system");
        setThemeMode(d.value as "light" | "dark" | "system");
      }
      if (d.key === "theme_preset" && d.value) {
        applyThemePreset(d.value);
        setThemePreset(d.value as Parameters<typeof setThemePreset>[0]);
      }
    }
    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, [setSidebarVariant, setSidebarCollapsible, setThemeMode, setThemePreset]);

  // Sync the html data-* attributes (which the CSS-driven rules for the content
  // wrapper and sticky header read) whenever the store changes, so switching
  // 居中/通栏 and 固定/随页面滚动 applies immediately without a reload.
  React.useEffect(() => {
    applyContentLayout(contentLayout);
  }, [contentLayout]);
  React.useEffect(() => {
    applyNavbarStyle(navbarStyle);
  }, [navbarStyle]);

  if (status !== "ready") return null;

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
          // The shell itself must always occupy the full space beside the
          // sidebar. Page-specific surfaces can still choose their own max
          // width, but the global centered-layout cap must not shrink them.
          "[&>*]:w-full [&>*]:min-w-0",
          "peer-data-[variant=inset]:border",
          "[--dashboard-header-height:--spacing(12)]",
          "main-flex-panel min-w-0 max-w-full flex-1 basis-0 overflow-x-hidden",
        )}
      >
        <MainContent>{children}</MainContent>
      </SidebarInset>
    </SidebarProvider>
  );
}

export default function Layout({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <CurrentUserProvider>
      <AuthenticatedLayout>{children}</AuthenticatedLayout>
    </CurrentUserProvider>
  );
}
