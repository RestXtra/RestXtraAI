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

  const defaultOpen = typeof document === "undefined" ? true : getClientCookie("sidebar_state") !== "false";
  // Live from the preferences store so the sidebar reacts immediately to changes
  // made in 系统配置 → 界面与布局 (not a one-shot cookie read).
  const variant = usePreferencesStore((s) => s.sidebarVariant);
  const collapsible = usePreferencesStore((s) => s.sidebarCollapsible);
  const contentLayout = usePreferencesStore((s) => s.contentLayout);
  const navbarStyle = usePreferencesStore((s) => s.navbarStyle);

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
