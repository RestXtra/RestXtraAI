import {
  Activity,
  BookMarked,
  Bot,
  Boxes,
  Brain,
  Bug,
  FileText,
  type LucideIcon,
  Network,
  Plug,
  Puzzle,
  Radar,
  ScrollText,
  Server,
  Settings2,
  ShieldAlert,
  ShieldCheck,
  Siren,
  Sparkles,
  SquarePen,
  Target,
  Terminal,
  Users,
  Webhook,
  Wrench,
} from "lucide-react";

export type NavBadge = "new" | "soon";

export interface NavSubItem {
  id: string;
  title: string;
  url: string;
  icon?: LucideIcon;
  badge?: NavBadge;
  disabled?: boolean;
  newTab?: boolean;
  perm?: string; // RBAC 权限点；缺省=登录即可见
}

interface NavItemBase {
  id: string;
  title: string;
  icon?: LucideIcon;
  badge?: NavBadge;
  disabled?: boolean;
  newTab?: boolean;
  perm?: string; // RBAC 权限点；缺省=登录即可见
}

export interface NavMainLinkItem extends NavItemBase {
  url: string;
  subItems?: never;
}

export interface NavMainParentItem extends NavItemBase {
  subItems: NavSubItem[];
}

export type NavMainItem = NavMainLinkItem | NavMainParentItem;

export interface NavGroup {
  id: number;
  label?: string;
  items: NavMainItem[];
}

// 侧栏导航（学 kanna 布局）：一级扁平项 —— 任务 / 漏洞发现 / 资产管理 /
// 沙箱管理（二级：主机/容器/出口范围）/ 插件（二级：WebShell/C2/代理池/空间测绘）。
// 新建会话由 ConversationList 提供（与一级项同级样式）。
export const sidebarItems: NavGroup[] = [
  {
    id: 1,
    items: [
      { id: "new-chat", title: "新建对话", url: "/chat", icon: SquarePen },
      { id: "tasks", title: "任务", url: "/function/tasks", icon: Target },
      { id: "findings", title: "漏洞发现", url: "/function/findings", icon: Bug },
      { id: "assets", title: "资产管理", url: "/function/assets", icon: Network },
      {
        id: "sandbox",
        title: "沙箱管理",
        icon: Boxes,
        subItems: [
          { id: "sandbox-hosts", title: "沙箱主机", url: "/sandbox/hosts", icon: Server, perm: "sandbox.read" },
          {
            id: "sandbox-containers",
            title: "沙箱容器",
            url: "/sandbox/containers",
            icon: Boxes,
            perm: "sandbox.read",
          },
          { id: "sandbox-egress", title: "出口范围", url: "/sandbox/egress", icon: ShieldCheck, perm: "sandbox.read" },
        ],
      },
      {
        id: "plugins",
        title: "插件",
        icon: Puzzle,
        subItems: [
          { id: "connection", title: "连接管理", url: "/cap/connection", icon: Plug, perm: "cap.connection.read" },
          { id: "incident", title: "安全事件", url: "/cap/incident", icon: Siren, perm: "task.read" },
          { id: "c2", title: "C2", url: "/cap/c2", icon: Webhook, perm: "cap.c2.read" },
          { id: "proxy-pool", title: "代理池管理", url: "/cap/proxies", icon: Network, perm: "cap.proxy.read" },
          { id: "spacesearch", title: "空间测绘", url: "/cap/spacesearch", icon: Radar, perm: "cap.spacesearch.read" },
        ],
      },
    ],
  },
];

// 设置分节（学 kanna SettingsPage 左侧分节栏）。iframe 内嵌对应现有页面。
export interface SettingsSection {
  id: string;
  label: string;
  url: string; // iframe 内嵌的现有页面路由
  icon?: LucideIcon;
  perm?: string;
}

export const settingsSections: SettingsSection[] = [
  {
    id: "agents",
    label: "Agent 管理",
    url: "/system/agents",
    icon: Bot,
    perm: "agent.read",
  },
  {
    id: "llm",
    label: "LLM 配置",
    url: "/system/llm",
    icon: Brain,
    perm: "platform.settings.read",
  },
  {
    id: "mcp",
    label: "MCP 管理",
    url: "/system/mcp",
    icon: Plug,
    perm: "agent.read",
  },
  {
    id: "kb",
    label: "知识库",
    url: "/agent/kb",
    icon: FileText,
    perm: "knowledge.read",
  },
  {
    id: "skills",
    label: "Skill",
    url: "/system/skills",
    icon: Sparkles,
    perm: "agent.read",
  },
  {
    id: "tools",
    label: "工具",
    url: "/system/tools",
    icon: Wrench,
    perm: "agent.read",
  },
  {
    id: "security",
    label: "安全边界",
    url: "/system/intercept",
    icon: ShieldAlert,
    perm: "sec.intercept.read",
  },
  {
    id: "audit",
    label: "审计日志",
    url: "/sec/audit",
    icon: ScrollText,
    perm: "sec.audit.read",
  },
  {
    id: "worklog",
    label: "工作日志",
    url: "/function/traffic",
    icon: Activity,
    perm: "worklog.read",
  },
  {
    id: "playbook",
    label: "攻击模式库",
    url: "/cap/playbook",
    icon: BookMarked,
    perm: "playbook.read",
  },
  {
    id: "platform",
    label: "平台管理",
    url: "/platform/users",
    icon: Users,
    perm: "platform.user.read",
  },
  {
    id: "system",
    label: "系统配置",
    url: "/system/settings",
    icon: Settings2,
    perm: "platform.settings.read",
  },
  {
    id: "workspace",
    label: "工作空间",
    url: "/workspace",
    icon: Server,
  },
  {
    id: "sync",
    label: "资产同步",
    url: "/function/sync",
    icon: Activity,
  },
];
