"use client";

import { useEffect, useState } from "react";

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

// useCurrentUser returns the signed-in user, enriched with RBAC roles and
// permissions from GET /api/platform/my (admin flag included). Falls back to the
// JWT-derived identity when the platform API is unavailable (e.g. mock mode).
export function useCurrentUser(): CurrentUser {
  const [user, setUser] = useState<CurrentUser>(FALLBACK);

  useEffect(() => {
    const u = auth.getCurrentUser();
    if (u) setUser(u);
    api
      .platformMy()
      .then((p) => {
        setUser({
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
        });
      })
      .catch(() => {
        /* keep JWT fallback */
      });
  }, []);

  return user;
}

// hasPerm checks whether the current user holds a permission point. Unknown /
// not-yet-loaded profiles default to allow for the built-in admin.
export function hasPerm(u: CurrentUser | null, perm: string): boolean {
  if (!u) return true;
  if (u.admin) return true;
  return (u.permissions ?? []).includes(perm);
}
