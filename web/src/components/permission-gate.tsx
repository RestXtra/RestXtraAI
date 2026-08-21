"use client";

import type { ReactNode } from "react";

import { ShieldXIcon } from "lucide-react";

import { hasPerm, useCurrentUserState } from "@/hooks/use-current-user";

// PermissionGate renders children when the signed-in user holds the permission,
// otherwise a 403 panel (admin bypasses). Wraps pages whose backend routes are
// RBAC-gated so unauthorized users see an explicit denial instead of a bare error.
export function PermissionGate({ perm, children }: { perm: string; children: ReactNode }) {
  const { user, status } = useCurrentUserState();
  if (status === "loading") return null;
  if (hasPerm(user, perm)) return <>{children}</>;
  return (
    <div className="flex h-full items-center justify-center py-24">
      <div className="flex max-w-md flex-col items-center gap-3 text-center">
        <ShieldXIcon className="size-12 text-muted-foreground" />
        <h3 className="font-semibold text-lg">403 · 无权限</h3>
        <p className="text-muted-foreground text-sm">当前账户缺少访问该页面的权限，请联系管理员分配相应角色。</p>
      </div>
    </div>
  );
}
