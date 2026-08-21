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
    return { id: "1", name: "RestXtra", username: "RestXtra", email: "", avatar: "", role: "admin" };
  },
};
