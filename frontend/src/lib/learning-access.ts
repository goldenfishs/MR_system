import { computed, reactive, watch } from "vue";
import { api } from "./api";
import { auth, loadSession } from "./auth";
import { groupOptions, hasPermission } from "./permissions";

export const learningAccess = reactive({ userId: "", allowed: false, directions: [] as string[], appliedDirections: [] as string[], enrolledDirections: [] as string[], error: "" });
export const canAccessLearning = computed(() => Boolean(auth.user && learningAccess.userId === auth.user.id && learningAccess.allowed));
export const learningDirections = computed(() => groupOptions.filter(group =>
  canAccessLearning.value && learningAccess.directions.includes(group.value) || hasPermission("tasks", group.value) || hasPermission("knowledge", group.value),
));
export const canOpenLearning = computed(() => Boolean(auth.user && (canAccessLearning.value || learningDirections.value.length)));
export const canAccessDirection = (direction: string) => canOpenLearning.value && (!direction || learningDirections.value.some(group => group.value === direction));
export const canAccessLearningModule = (permission: "tasks" | "knowledge", direction?: string) =>
  canAccessLearning.value && (!direction || learningAccess.directions.includes(direction)) || hasPermission(permission, direction);
let generation = 0;
let pending: Promise<void> | undefined;
watch(() => auth.user?.id, () => {
  generation++;
  pending = undefined;
  Object.assign(learningAccess, { userId: "", allowed: false, directions: [], appliedDirections: [], enrolledDirections: [], error: "" });
}, { flush: "sync" });

export function refreshLearningAccess(): Promise<void> {
  if (!auth.user || auth.signingOut) return Promise.resolve();
  if (pending) return pending;
  const userId = auth.user.id;
  const requestGeneration = generation;
  pending = loadSession(true).then(async () => {
    if (!auth.user || auth.signingOut || auth.user.id !== userId || requestGeneration !== generation) return;
    const { allowed, directions, appliedDirections, enrolledDirections } = await api.get<{ allowed: boolean; directions: string[]; appliedDirections: string[]; enrolledDirections: string[] }>("/learning-access");
    if (requestGeneration !== generation || auth.signingOut) return;
    Object.assign(learningAccess, { userId, allowed, directions: directions || [], appliedDirections: appliedDirections || [], enrolledDirections: enrolledDirections || [], error: "" });
  }).catch(() => {
    if (requestGeneration !== generation || auth.signingOut) return;
    Object.assign(learningAccess, { userId, allowed: false, directions: [], appliedDirections: [], enrolledDirections: [], error: "暂时无法确认组别权限，请刷新重试。" });
  }).finally(() => {
    if (requestGeneration === generation) pending = undefined;
  });
  return pending;
}
