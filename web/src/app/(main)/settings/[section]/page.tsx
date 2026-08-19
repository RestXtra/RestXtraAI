import { settingsSections } from "@/navigation/sidebar/sidebar-items";

import { SettingsSectionClient } from "./settings-section-client";

// 静态导出需要为动态路由声明全部合法段。
export function generateStaticParams() {
  return settingsSections.map((s) => ({ section: s.id }));
}

export default async function SettingsSectionPage({
  params,
}: {
  params: Promise<{ section: string }>;
}) {
  const { section } = await params;
  return <SettingsSectionClient section={section} />;
}
