"use client";

import { createContext, createElement, useContext, useEffect, useMemo, useState } from "react";

import { api } from "@/lib/api";
import { auth, type CurrentUser } from "@/lib/auth";

const FALLBACK: CurrentUser = {
  id: "1",
  name: "RestXtra AI",
  username: "admin",
  email: "",
  avatar: "",
  role: "operator",
};

type CurrentUserStatus = "loading" | "ready" | "error";

type CurrentUserState = {
  user: CurrentUser;
  status: CurrentUserStatus;
};

const CurrentUserContext = createContext<CurrentUserState | null>(null);

function fallbackUser(): CurrentUser {
  return auth.getCurrentUser() ?? FALLBACK;
}

// CurrentUserProvider centralizes /platform/my for the authenticated app shell.
// Without it, every PermissionGate, sidebar, and RBAC-aware page fetched the same
// profile independently on mount.
export function CurrentUserProvider({ children }: { children: React.ReactNode }) {
  const [state, setState] = useState<CurrentUserState>(() => ({ user: fallbackUser(), status: "loading" }));

  useEffect(() => {
    let active = true;
    api
      .platformMy()
      .then((p) => {
        if (!active) return;
        setState({
          status: "ready",
          user: {
            id: String(p.user.id),
            name: p.user.display_name || p.user.username,
            username: p.user.username,
            email: "",
            avatar: "",
            role: p.admin ? "admin" : (p.roles[0] ?? "viewer"),
            roles: p.roles,
            permissions: p.permissions,
            admin: p.admin,
            display_name: p.user.display_name,
          },
        });
      })
      .catch(() => {
        if (active) setState((current) => ({ ...current, status: "error" }));
      });
    return () => {
      active = false;
    };
  }, []);

  const value = useMemo(() => state, [state]);
  return createElement(CurrentUserContext.Provider, { value }, children);
}

export function useCurrentUserState(): CurrentUserState {
  const state = useContext(CurrentUserContext);
  // Components outside the authenticated shell are intentionally not allowed to
  // trigger an additional profile request. They receive the local mock identity
  // or conservative fallback instead.
  return state ?? { user: fallbackUser(), status: "ready" };
}

// useCurrentUser returns the signed-in user, enriched with RBAC roles and
// permissions from GET /api/platform/my (admin flag included). Falls back to the
// JWT-derived identity when the platform API is unavailable (e.g. mock mode).
export function useCurrentUser(): CurrentUser {
  return useCurrentUserState().user;
}

// hasPerm checks whether the current user holds a permission point. Permission
// loading is handled explicitly by PermissionGate; an unknown identity is never
// treated as authorized.
export function hasPerm(u: CurrentUser | null, perm: string): boolean {
  if (!u) return false;
  if (u.admin) return true;
  return (u.permissions ?? []).includes(perm);
}
