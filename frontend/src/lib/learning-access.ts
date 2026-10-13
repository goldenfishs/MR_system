import { computed, reactive, watch } from "vue";
import { api } from "./api";
import { auth } from "./auth";

export const learningAccess = reactive({ userId: "", allowed: false, error: "" });
export const canAccessLearning = computed(() => {
  const user = auth.user;
  return Boolean(user && (["admin", "mentor", "interviewer"].includes(user.role) ||
    (user.role === "student" && learningAccess.userId === user.id && learningAccess.allowed)));
});

let generation = 0;
let pending: Promise<void> | undefined;
watch(() => auth.user, () => {
  generation++;
  pending = undefined;
  Object.assign(learningAccess, { userId: "", allowed: false, error: "" });
}, { flush: "sync" });

export function refreshLearningAccess(): Promise<void> {
  const user = auth.user;
  if (!user || user.role !== "student") return Promise.resolve();
  if (pending) return pending;
  const requestGeneration = generation;
  pending = api.get<{ allowed: boolean }>("/learning-access")
    .then(({ allowed }) => {
      if (requestGeneration !== generation) return;
      Object.assign(learningAccess, { userId: user.id, allowed, error: "" });
    })
    .catch(() => {
      if (requestGeneration !== generation) return;
      Object.assign(learningAccess, {
        userId: user.id,
        allowed: false,
        error: "暂时无法确认入队状态，请刷新重试。",
      });
    })
    .finally(() => {
      if (requestGeneration === generation) pending = undefined;
    });
  return pending;
}
