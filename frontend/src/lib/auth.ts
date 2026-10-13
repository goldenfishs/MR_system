import { reactive, watch } from "vue";
import { api, ApiError, setApiSession } from "./api";
import type { User } from "./types";
export const auth = reactive<{
  user: User | null;
  demoMode: boolean;
  ready: boolean;
  signingOut: boolean;
  error: string;
}>({ user: null, demoMode: false, ready: false, signingOut: false, error: "" });
let pending: Promise<void> | undefined;
let generation = 0;
watch(() => auth.user?.id, () => { generation++; pending = undefined; setApiSession(Boolean(auth.user) && !auth.signingOut); }, { flush: "sync" });
export function loadSession(force = false): Promise<void> {
  if (auth.signingOut) return Promise.resolve();
  if (auth.ready && !force) return Promise.resolve();
  if (pending) return pending;
  const requestGeneration = generation;
  const request = api.get<{ user: User; demoMode: boolean }>("/auth/me")
    .then((data) => {
      if (requestGeneration !== generation) return;
      auth.user = data.user;
      auth.demoMode = data.demoMode;
      auth.error = "";
    })
    .catch((error) => {
      if (requestGeneration !== generation) return;
      auth.user = null;
      if (!(error instanceof ApiError && error.status === 401)) auth.error = error.message;
    })
    .finally(() => {
      auth.ready = true;
      if (pending === request) pending = undefined;
    });
  pending = request;
  return request;
}
export async function signOut() {
  auth.signingOut = true;
  generation++;
  pending = undefined;
  setApiSession(false);
  try {
    await api.post("/auth/logout");
    auth.user = null;
    auth.ready = true;
    auth.error = "";
  } finally {
    auth.signingOut = false;
    setApiSession(Boolean(auth.user));
  }
}
export const roleNames = {
  admin: "管理员",
  group_admin: "组管理员",
  member: "正式队员",
  reserve: "梯队队员",
  freshman: "新生",
};
