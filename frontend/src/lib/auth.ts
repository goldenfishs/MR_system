import { reactive } from "vue";
import { api, ApiError } from "./api";
import type { User } from "./types";
export const auth = reactive<{
  user: User | null;
  demoMode: boolean;
  ready: boolean;
  error: string;
}>({ user: null, demoMode: false, ready: false, error: "" });
let pending: Promise<void> | undefined;
export function loadSession(): Promise<void> {
  if (auth.ready) return Promise.resolve();
  pending ??= api
    .get<{ user: User; demoMode: boolean }>("/auth/me")
    .then((data) => {
      auth.user = data.user;
      auth.demoMode = data.demoMode;
    })
    .catch((error) => {
      if (!(error instanceof ApiError && error.status === 401))
        auth.error = error.message;
    })
    .finally(() => {
      auth.ready = true;
      pending = undefined;
    });
  return pending;
}
export async function signOut() {
  await api.post("/auth/logout");
  auth.user = null;
}
export const roleNames = {
  admin: "管理员",
  mentor: "导师",
  interviewer: "面试官",
  student: "学员",
};
