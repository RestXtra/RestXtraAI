"use client";

import { usePathname } from "next/navigation";

import { settingsSections, sidebarItems } from "@/navigation/sidebar/sidebar-items";

// 从当前路径推断页面标题，供全局 header 展示（学 kanna：顶部 header 显示页面名）。
// 匹配顺序：设置分节 → 侧栏项（含子项）→ 兜底。
export function usePageTitle(): string {
  const pathname = usePathname();
  const p = (() => {
    try {
      return decodeURIComponent(pathname ?? "");
    } catch {
      return pathname ?? "";
    }
  })();

  // 设置分节：/settings/<id>
  const secMatch = p.match(/^\/settings\/([^/]+)/);
  if (secMatch) {
    const sec = settingsSections.find((s) => s.id === secMatch[1]);
    if (sec) return sec.label;
    return "设置";
  }
  if (p === "/settings") return "设置";

  // 设置分节的原始页面路径（/system/llm 等）→ 对应 label
  for (const s of settingsSections) {
    if (p.startsWith(s.url)) return s.label;
  }

  // 侧栏项（含子项）
  for (const group of sidebarItems) {
    for (const item of group.items) {
      if (item.subItems) {
        for (const sub of item.subItems) {
          if (p.startsWith(sub.url)) return sub.title;
        }
      } else if (p.startsWith(item.url)) {
        return item.title;
      }
    }
  }

  // 兜底（未在导航中的页面）
  const fallback: Record<string, string> = {
    "/dashboard": "仪表盘",
    "/chat": "对话",
    "/workspace": "工作空间",
    "/workflows": "工作流",
    "/workflow-builder": "工作流构建器",
  };
  for (const [url, title] of Object.entries(fallback)) {
    if (p.startsWith(url)) return title;
  }

  return "RestXtra AI";
}
