import {
  Activity,
  BookMarked,
  Bot,
  Boxes,
  Brain,
  Bug,
  ClipboardList,
  FileText,
  FolderSync,
  GitBranch,
  LayoutDashboard,
  type LucideIcon,
  MessageSquare,
  Network,
  Plug,
  ScrollText,
  Server,
  Settings2,
  ShieldAlert,
  ShieldCheck,
  Sparkles,
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

// 信息架构：按融合蓝图 §3.1 分为 7 个分组。
// badge: "soon" = 路由已存在但功能为铺底占位。
export const sidebarItems: NavGroup[] = [
  {
    id: 1,
    label: "工作台",
    items: [
      { id: "dashboard", title: "仪表盘", url: "/dashboard", icon: LayoutDashboard },
      { id: "chat", title: "对话", url: "/chat", icon: MessageSquare },
      { id: "tasks", title: "任务", url: "/function/tasks", icon: Target },
      { id: "findings", title: "漏洞发现", url: "/function/findings", icon: Bug },
      { id: "assets", title: "资产管理", url: "/function/assets", icon: Network },
      { id: "sync", title: "资产同步", url: "/function/sync", icon: FolderSync },
      { id: "workflows", title: "工作流", url: "/workflows", icon: GitBranch, perm: "batch.read" },
      { id: "workspace", title: "工作空间", url: "/workspace", icon: Server },
    ],
  },
  {
    id: 2,
    label: "沙箱管理",
    items: [
      { id: "sandbox-hosts", title: "沙箱主机", url: "/sandbox/hosts", icon: Server, perm: "sandbox.read" },
      { id: "sandbox-containers", title: "沙箱容器", url: "/sandbox/containers", icon: Boxes, perm: "sandbox.read" },
      { id: "sandbox-egress", title: "出口范围", url: "/sandbox/egress", icon: ShieldCheck, perm: "sandbox.read" },
    ],
  },
  {
    id: 3,
    label: "能力",
    items: [
      { id: "webshell", title: "WebShell", url: "/cap/webshell", icon: Terminal, perm: "cap.webshell.read" },
      { id: "c2", title: "C2", url: "/cap/c2", icon: Webhook, perm: "cap.c2.read" },
      { id: "proxy-pool", title: "代理池管理", url: "/cap/proxies", icon: Network, perm: "cap.proxy.read" },
      { id: "playbook", title: "攻击模式库", url: "/cap/playbook", icon: BookMarked, perm: "playbook.read" },
    ],
  },
  {
    id: 4,
    label: "Agent 管理",
    items: [
      { id: "llm", title: "LLM 配置", url: "/system/llm", icon: Brain, perm: "platform.settings.read" },
      { id: "mcp", title: "MCP 管理", url: "/system/mcp", icon: Plug, perm: "agent.read" },
      { id: "kb", title: "知识库", url: "/agent/kb", icon: FileText, perm: "knowledge.read" },
      { id: "agents", title: "智能体管理", url: "/system/agents", icon: Bot, perm: "agent.read" },
      { id: "skills", title: "Skill", url: "/system/skills", icon: Sparkles, perm: "agent.read" },
      { id: "tools", title: "工具", url: "/system/tools", icon: Wrench, perm: "agent.read" },
    ],
  },
  {
    id: 5,
    label: "安全边界",
    items: [
      { id: "intercept", title: "拦截规则", url: "/system/intercept", icon: ShieldAlert, perm: "sec.intercept.read" },
      {
        id: "approvals",
        title: "审批记录",
        url: "/system/intercept/approvals",
        icon: ClipboardList,
        perm: "sec.intercept.read",
      },
      { id: "audit", title: "审计日志", url: "/sec/audit", icon: ScrollText, perm: "sec.audit.read" },
    ],
  },
  {
    id: 6,
    label: "工作日志",
    items: [
      { id: "traffic", title: "流量记录", url: "/function/traffic", icon: Activity, perm: "worklog.read" },
      { id: "worklog-tools", title: "工具执行", url: "/worklog/tools", icon: Wrench, perm: "worklog.read" },
      { id: "worklog-llm", title: "LLM 录制", url: "/worklog/llm", icon: Brain, perm: "worklog.read" },
      { id: "logs", title: "后端日志", url: "/system/logs", icon: ScrollText, perm: "worklog.read" },
    ],
  },
  {
    id: 7,
    label: "平台管理",
    items: [
      { id: "users", title: "成员管理", url: "/platform/users", icon: Users, perm: "platform.user.read" },
      { id: "roles", title: "平台角色", url: "/platform/roles", icon: ShieldCheck, perm: "platform.role.read" },
      { id: "settings", title: "系统配置", url: "/system/settings", icon: Settings2, perm: "platform.settings.read" },
    ],
  },
];
