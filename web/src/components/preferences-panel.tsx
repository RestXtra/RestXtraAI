"use client";

import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { type FontKey, fontOptions } from "@/lib/fonts/registry";
import type { ContentLayout, NavbarStyle, SidebarCollapsible, SidebarVariant } from "@/lib/preferences/layout";
import {
  applyContentLayout,
  applyFont,
  applyNavbarStyle,
  applySidebarCollapsible,
  applySidebarVariant,
} from "@/lib/preferences/layout-utils";
import { PREFERENCE_DEFAULTS } from "@/lib/preferences/preferences-config";
import { persistPreference } from "@/lib/preferences/preferences-storage";
import { THEME_PRESET_OPTIONS, type ThemeMode, type ThemePreset } from "@/lib/preferences/theme";
import { applyThemePreset } from "@/lib/preferences/theme-utils";
import { usePreferencesStore } from "@/stores/preferences/preferences-provider";

// 中文版界面偏好设置面板（原 LayoutControls 弹窗内容迁移而来）。
export function PreferencesPanel() {
  const themeMode = usePreferencesStore((s) => s.themeMode);
  const resolvedThemeMode = usePreferencesStore((s) => s.resolvedThemeMode);
  const setThemeMode = usePreferencesStore((s) => s.setThemeMode);
  const themePreset = usePreferencesStore((s) => s.themePreset);
  const setThemePreset = usePreferencesStore((s) => s.setThemePreset);
  const contentLayout = usePreferencesStore((s) => s.contentLayout);
  const setContentLayout = usePreferencesStore((s) => s.setContentLayout);
  const navbarStyle = usePreferencesStore((s) => s.navbarStyle);
  const setNavbarStyle = usePreferencesStore((s) => s.setNavbarStyle);
  const variant = usePreferencesStore((s) => s.sidebarVariant);
  const setSidebarVariant = usePreferencesStore((s) => s.setSidebarVariant);
  const collapsible = usePreferencesStore((s) => s.sidebarCollapsible);
  const setSidebarCollapsible = usePreferencesStore((s) => s.setSidebarCollapsible);
  const font = usePreferencesStore((s) => s.font);
  const setFont = usePreferencesStore((s) => s.setFont);

  const onThemePresetChange = (preset: ThemePreset) => {
    applyThemePreset(preset);
    setThemePreset(preset);
    void persistPreference("theme_preset", preset);
    if (window.parent !== window) window.parent.postMessage({ type: "restxtra:pref", key: "theme_preset", value: preset }, "*");
  };

  const onThemeModeChange = (mode: ThemeMode | "") => {
    if (!mode) return;
    setThemeMode(mode);
    void persistPreference("theme_mode", mode);
    if (window.parent !== window) window.parent.postMessage({ type: "restxtra:pref", key: "theme_mode", value: mode }, "*");
  };

  const onContentLayoutChange = (layout: ContentLayout | "") => {
    if (!layout) return;
    applyContentLayout(layout);
    setContentLayout(layout);
    void persistPreference("content_layout", layout);
  };

  const onNavbarStyleChange = (style: NavbarStyle | "") => {
    if (!style) return;
    applyNavbarStyle(style);
    setNavbarStyle(style);
    void persistPreference("navbar_style", style);
  };

  const onSidebarStyleChange = (value: SidebarVariant | "") => {
    if (!value) return;
    setSidebarVariant(value);
    applySidebarVariant(value);
    void persistPreference("sidebar_variant", value);
    // 若本页在 /settings 的 iframe 中，通知父页面同步侧栏。
    if (window.parent !== window) {
      window.parent.postMessage({ type: "restxtra:pref", key: "sidebar_variant", value }, "*");
    }
  };

  const onSidebarCollapseModeChange = (value: SidebarCollapsible | "") => {
    if (!value) return;
    setSidebarCollapsible(value);
    applySidebarCollapsible(value);
    void persistPreference("sidebar_collapsible", value);
    if (window.parent !== window) {
      window.parent.postMessage({ type: "restxtra:pref", key: "sidebar_collapsible", value }, "*");
    }
  };

  const onFontChange = (value: FontKey | "") => {
    if (!value) return;
    applyFont(value);
    setFont(value);
    void persistPreference("font", value);
  };

  const handleRestore = () => {
    onThemePresetChange(PREFERENCE_DEFAULTS.theme_preset);
    onThemeModeChange(PREFERENCE_DEFAULTS.theme_mode);
    onContentLayoutChange(PREFERENCE_DEFAULTS.content_layout);
    onNavbarStyleChange(PREFERENCE_DEFAULTS.navbar_style);
    onSidebarStyleChange(PREFERENCE_DEFAULTS.sidebar_variant);
    onSidebarCollapseModeChange(PREFERENCE_DEFAULTS.sidebar_collapsible);
    onFontChange(PREFERENCE_DEFAULTS.font);
  };

  return (
    <div className="flex flex-col gap-5">
      <div className="space-y-1.5">
        <h4 className="font-medium text-sm leading-none">界面与布局</h4>
        <p className="text-muted-foreground text-xs">自定义仪表盘的布局与外观偏好。</p>
      </div>
      <div className="space-y-3 **:data-[slot=toggle-group]:w-full **:data-[slot=toggle-group-item]:flex-1 **:data-[slot=toggle-group-item]:text-xs">
        <div className="space-y-1">
          <Label className="font-medium text-xs">主题预设</Label>
          <Select value={themePreset} onValueChange={onThemePresetChange}>
            <SelectTrigger size="sm" className="w-full text-xs">
              <SelectValue placeholder="选择预设" />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                {THEME_PRESET_OPTIONS.map((preset) => (
                  <SelectItem key={preset.value} className="text-xs" value={preset.value}>
                    <span
                      className="size-2.5 rounded-full"
                      style={{
                        backgroundColor:
                          (resolvedThemeMode ?? "light") === "dark" ? preset.primary.dark : preset.primary.light,
                      }}
                    />
                    {preset.label}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        </div>

        <div className="space-y-1">
          <Label className="font-medium text-xs">字体</Label>
          <Select value={font} onValueChange={onFontChange}>
            <SelectTrigger size="sm" className="w-full text-xs">
              <SelectValue placeholder="选择字体" />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                {fontOptions.map((f) => (
                  <SelectItem key={f.key} className="text-xs" value={f.key}>
                    {f.label}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        </div>

        <div className="space-y-1">
          <Label className="font-medium text-xs">主题模式</Label>
          <ToggleGroup
            size="sm"
            spacing={0}
            variant="outline"
            type="single"
            value={themeMode}
            onValueChange={onThemeModeChange}
          >
            <ToggleGroupItem value="light" aria-label="亮色">
              亮色
            </ToggleGroupItem>
            <ToggleGroupItem value="dark" aria-label="暗色">
              暗色
            </ToggleGroupItem>
            <ToggleGroupItem value="system" aria-label="跟随系统">
              跟随系统
            </ToggleGroupItem>
          </ToggleGroup>
        </div>

        <p className="text-muted-foreground text-xs">布局、顶栏和侧栏折叠方式已按 Codex 固定为内嵌布局与图标折叠。</p>

        <Button type="button" size="sm" variant="outline" className="w-full text-xs" onClick={handleRestore}>
          恢复默认
        </Button>
      </div>
    </div>
  );
}
