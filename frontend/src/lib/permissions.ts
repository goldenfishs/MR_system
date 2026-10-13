import { auth } from "./auth";
import type { Direction, Permission, User } from "./types";

export const groupOptions: { value: Direction; title: string; icon: string }[] = [
  { value: "机械", title: "机械组", icon: "mdi-cog-outline" },
  { value: "电控", title: "控制组", icon: "mdi-chip" },
  { value: "硬件", title: "硬件组", icon: "mdi-memory" },
  { value: "视觉算法", title: "算法组", icon: "mdi-eye-outline" },
  { value: "运营", title: "运营组", icon: "mdi-bullhorn-outline" },
];
export const permissionOptions: { value: Permission; title: string }[] = [
  { value: "recruitment", title: "报名与面试审核" },
  { value: "assessment", title: "新生阶段考核" },
  { value: "tasks", title: "作业、考试与题库" },
  { value: "knowledge", title: "知识库资料" },
  { value: "discussions", title: "讨论答疑与公开" },
  { value: "members", title: "查看组内成员" },
  { value: "records", title: "发放组内荣誉" },
  { value: "mail", title: "向组内批量发邮件" },
];
export const memberTypeNames = { formal: "正式队员", reserve: "梯队队员", freshman: "新生" };
export const groupLabel = (direction: string) => groupOptions.find(g => g.value === direction)?.title || direction || "全部组别";
export function hasPermission(permission: Permission, direction?: string, user: User | null = auth.user): boolean {
  if (!user) return false;
  if (user.role === "admin") return true;
  if (user.role !== "group_admin") return false;
  return Boolean(user.groupGrants?.some(grant => grant.permissions.includes(permission) && (direction === undefined || grant.direction === direction)));
}
export function manageableGroups(permission: Permission) {
  return groupOptions.filter(group => hasPermission(permission, group.value));
}
export function isParticipant(user: User | null = auth.user) {
  return Boolean(user && (["member", "reserve", "freshman"].includes(user.role) || user.role === "group_admin" && Boolean(user.memberType)));
}
