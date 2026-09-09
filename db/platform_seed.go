package db

// Platform permission catalog + system-role seeding. The catalog is registered
// idempotently on every startup; system roles (admin/operator/auditor/viewer)
// are created if absent and their permission bindings refreshed so catalog
// additions propagate — but only for SYSTEM roles, never for user-created ones.

// permissionDef is one catalog entry.
type permissionDef struct{ key, desc string }

// platformPermissions is the full permission catalog (see fusion blueprint §7.4).
var platformPermissions = []permissionDef{
	// 平台管理
	{"platform.user.read", "查看成员列表"},
	{"platform.user.write", "创建 / 编辑成员"},
	{"platform.user.role", "给成员分配平台角色"},
	{"platform.role.read", "查看平台角色"},
	{"platform.role.write", "创建 / 编辑平台角色"},
	{"platform.settings.read", "查看系统设置"},
	{"platform.settings.write", "修改系统设置"},
	// 安全边界
	{"sec.intercept.read", "查看拦截规则与审批记录"},
	{"sec.intercept.write", "编辑拦截规则"},
	{"sec.intercept.decide", "审批拦截请求"},
	{"sec.audit.read", "查看审计日志"},
	{"sec.audit.export", "导出审计日志"},
	// 能力
	{"cap.webshell.read", "使用 WebShell"},
	{"cap.webshell.write", "管理 WebShell 会话"},
	{"cap.connection.read", "查看连接管理（SSH / RDP / Telnet / WebShell）"},
	{"cap.connection.write", "管理连接（新增 / 编辑 / 测试 / 删除）"},
	{"cap.c2.read", "查看 C2 会话"},
	{"cap.c2.write", "管理 C2 监听器与任务"},
	{"cap.report.read", "生成 / 查看报告"},
	{"cap.proxy.read", "查看代理池"},
	{"cap.proxy.write", "管理代理池（导入 / 测活 / 删除）"},
	{"cap.spacesearch.read", "使用空间测绘搜索（FOFA / Hunter / Quake）"},
	{"cap.spacesearch.write", "配置空间测绘凭证并导入资产"},
	// 沙箱
	{"sandbox.read", "查看沙箱主机 / 容器 / 出口范围"},
	{"sandbox.write", "管理沙箱与出口范围"},
	// 工作日志
	{"worklog.read", "查看流量 / 工具执行 / LLM 录制"},
	{"worklog.write", "清理流量 / 工具执行 / LLM 录制"},
	// Agent 管理
	{"agent.read", "查看 Agent / MCP / Skill / 工具"},
	{"agent.write", "管理 Agent / MCP / Skill / 工具"},
	{"knowledge.read", "查看知识库"},
	{"knowledge.write", "管理知识库"},
	{"playbook.read", "查看攻击模式库"},
	{"playbook.write", "管理攻击模式库"},
	{"batch.read", "查看批量任务"},
	{"batch.write", "管理批量任务"},
	{"workflow.read", "查看工作流与运行记录"},
	{"workflow.write", "创建 / 修改 / 执行工作流"},
	{"workspace.read", "查看 Agent 工作空间文件"},
	{"workspace.write", "删除 / 清理 Agent 工作空间文件"},
	{"benchmark.read", "查看基准测试配置与题目"},
	{"benchmark.run", "配置并执行基准测试"},
	// 工作台
	{"task.read", "查看任务 / 发现 / 资产"},
	{"task.create", "创建任务"},
	{"task.run", "运行 / 暂停任务"},
	{"task.kill", "终止任务 / 干预执行"},
}

// systemRoleDefs maps a system role to its permission keys. admin is implicit
// (bypasses the catalog in ResolveAccess) and listed with an empty set.
var systemRoleDefs = map[string][]string{
	RoleAdmin: {},
	RoleOperator: {
		"platform.user.read", "platform.user.write", "platform.user.role",
		"platform.role.read", "platform.role.write",
		"platform.settings.read", "platform.settings.write",
		"sec.intercept.read", "sec.intercept.write", "sec.intercept.decide",
		"cap.webshell.read", "cap.webshell.write",
		"cap.connection.read", "cap.connection.write",
		"cap.c2.read", "cap.c2.write",
		"cap.report.read",
		"cap.proxy.read", "cap.proxy.write",
		"cap.spacesearch.read", "cap.spacesearch.write",
		"sandbox.read", "sandbox.write",
		"worklog.read", "worklog.write",
		"agent.read", "agent.write",
		"knowledge.read", "knowledge.write",
		"playbook.read", "playbook.write",
		"batch.read", "batch.write",
		"workflow.read", "workflow.write", "workspace.read", "workspace.write",
		"benchmark.read", "benchmark.run",
		"task.read", "task.create", "task.run", "task.kill",
	},
	RoleAuditor: {
		"sec.audit.read", "sec.audit.export",
		"worklog.read", "workflow.read", "benchmark.read",
		"task.read", "cap.report.read", "platform.settings.read", "agent.read",
	},
	RoleViewer: {
		"task.read", "agent.read", "sec.intercept.read",
		"worklog.read", "cap.report.read", "platform.settings.read", "playbook.read",
		"workflow.read", "benchmark.read",
	},
}

// seedPlatform registers the permission catalog + system roles (idempotent).
// System-role permission bindings are refreshed so newly-added catalog entries
// propagate, but only for system roles — user-created roles are never touched.
func (d *DB) seedPlatform() error {
	for _, p := range platformPermissions {
		if err := d.EnsurePermission(p.key, p.desc); err != nil {
			return err
		}
	}
	for name, perms := range systemRoleDefs {
		role, err := d.GetRoleByName(name)
		if err != nil {
			return err
		}
		if role == nil {
			id, err := d.CreateRole(name, systemRoleDesc(name), "all")
			if err != nil {
				return err
			}
			if err := d.markSystemRole(id); err != nil {
				return err
			}
			if name == RoleAdmin {
				continue // admin bypasses catalog; no bindings needed
			}
			if err := d.SetRolePermissions(id, perms); err != nil {
				return err
			}
			continue
		}
		if name == RoleAdmin {
			// ensure the built-in admin is always flagged system
			if !role.IsSystem {
				if err := d.markSystemRole(role.ID); err != nil {
					return err
				}
			}
			continue
		}
		if !role.IsSystem {
			if err := d.markSystemRole(role.ID); err != nil {
				return err
			}
		}
		// refresh bindings for system roles
		if err := d.SetRolePermissions(role.ID, perms); err != nil {
			return err
		}
	}
	return nil
}

// markSystemRole flags a role as a protected system role (cannot be deleted).
func (d *DB) markSystemRole(id int64) error {
	_, err := d.Exec(`UPDATE roles SET is_system=true WHERE id=$1`, id)
	return err
}

func systemRoleDesc(name string) string {
	switch name {
	case RoleAdmin:
		return "系统内置：全量权限（管理员）"
	case RoleOperator:
		return "系统内置：任务 / Agent / 安全边界 / 平台设置操作员"
	case RoleAuditor:
		return "系统内置：审计与只读审查"
	case RoleViewer:
		return "系统内置：只读查看"
	}
	return ""
}
