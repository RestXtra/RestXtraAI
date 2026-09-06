export interface CurrentUser {
  id: string;
  name: string;
  username: string;
  email: string;
  avatar: string;
  role: string;
  roles?: string[];
  permissions?: string[];
  admin?: boolean;
  display_name?: string;
}

export const auth = {
  getCurrentUser(): CurrentUser | null {
    if (process.env.NEXT_PUBLIC_MOCK !== "1") return null;
    return {
      id: "1",
      name: "Demo Admin",
      username: "demo-admin",
      display_name: "Demo Admin",
      email: "",
      avatar: "",
      role: "admin",
      roles: ["admin"],
      permissions: [
        "platform.user.read",
        "platform.user.write",
        "platform.user.role",
        "platform.role.read",
        "platform.role.write",
        "platform.settings.read",
        "platform.settings.write",
        "sec.intercept.read",
        "sec.intercept.write",
        "sec.intercept.decide",
        "sec.audit.read",
        "sec.audit.export",
        "sandbox.read",
        "sandbox.write",
        "worklog.read",
        "worklog.write",
        "agent.read",
        "agent.write",
        "benchmark.read",
        "benchmark.run",
        "task.read",
        "task.create",
        "task.run",
        "task.kill",
      ],
      admin: true,
    };
  },
};
