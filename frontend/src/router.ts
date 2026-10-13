import { createRouter, createWebHistory } from "vue-router";
import { auth, loadSession } from "./lib/auth";
import { canAccessLearning, canOpenLearning, canAccessDirection, canAccessLearningModule, learningDirections, refreshLearningAccess } from "./lib/learning-access";
import { hasPermission } from "./lib/permissions";
const router = createRouter({
  history: createWebHistory(),
  scrollBehavior: () => ({ top: 0 }),
  routes: [
    {
      path: "/login",
      component: () => import("./views/LoginView.vue"),
      meta: { title: "登录" },
    },
    {
      path: "/",
      component: () => import("./views/DashboardView.vue"),
      meta: { title: "工作台", subtitle: "从一个想法，到一台真正的机器人。" },
    },
    {
      path: "/recruitment",
      component: () => import("./views/RecruitmentView.vue"),
      meta: { title: "招新管理", subtitle: "让每一份热爱，找到合适的位置。" },
    },
    {
      path: "/teams",
      component: () => import("./views/TeamsView.vue"),
      meta: { title: "团队报名" },
    },
    {
      path: "/profile",
      component: () => import("./views/ProfileView.vue"),
      meta: { title: "个人中心" },
    },
    {
      path: "/training",
      component: () => import("./views/TrainingView.vue"),
      meta: { title: "新生考核" },
    },
    {
      path: "/records",
      component: () => import("./views/RecordsView.vue"),
      meta: { title: "荣誉中心" },
    },
    {
      path: "/discussions",
      component: () => import("./views/DiscussionView.vue"),
      meta: { title: "讨论区" },
    },
    {
      path: "/files",
      component: () => import("./views/FilesView.vue"),
      meta: { title: "我的附件" },
    },
    {
      path: "/mail",
      component: () => import("./views/MailView.vue"),
      meta: { title: "批量邮件", permission: "mail" },
    },
    {
      path: "/learning",
      component: () => import("./views/LearningView.vue"),
      meta: {
        title: "学习与任务",
        subtitle: "把知识变成作品，把尝试变成经验。",
      },
    },
    {
      path: "/assessment",
      component: () => import("./views/AssessmentView.vue"),
      meta: { title: "在线测评", subtitle: "连接题库与实践，积累每一次进步。" },
    },
    {
      path: "/members",
      component: () => import("./views/MembersView.vue"),
      meta: {
        title: "战队成员",
        subtitle: "不同的专长，共同的目标。",
        permission: "members",
      },
    },
    {
      path: "/settings",
      component: () => import("./views/SettingsView.vue"),
      meta: {
        title: "设置",
        subtitle: "为战队配置自己的协作空间。",
        admin: true,
      },
    },
    { path: "/:pathMatch(.*)*", redirect: "/" },
  ],
});
router.beforeEach(async (to) => {
  await loadSession();
  if (!auth.user && to.path !== "/login")
    return { path: "/login", query: { redirect: to.fullPath } };
  if (auth.user && to.path === "/login") return "/";
  await refreshLearningAccess();
  if (!auth.user && to.path !== "/login") return { path: "/login", query: { redirect: to.fullPath } };
  if (to.meta.admin && auth.user?.role !== "admin") return "/";
  if (to.meta.permission && !hasPermission(to.meta.permission as import("./lib/types").Permission)) return "/";
  if (["/learning", "/assessment"].includes(to.path) && !canOpenLearning.value)
    return { path: "/teams", query: { admissionRequired: "1" } };
  if (to.path === "/assessment" && !canAccessLearning.value && !hasPermission("tasks")) return "/learning";
  if (to.path === "/learning" && to.query.direction && !canAccessDirection(String(to.query.direction)))
    return { path: "/learning", query: { ...to.query, direction: learningDirections.value[0]?.value || "" } };
  if (to.path === "/learning") {
    const direction = typeof to.query.direction === "string" ? to.query.direction : undefined;
    const section = to.query.section || (to.query.kind ? "tasks" : "home");
    if (section === "knowledge" && !canAccessLearningModule("knowledge", direction) || ["tasks", "exam", "bank"].includes(String(section)) && !canAccessLearningModule("tasks", direction)) {
      const query = { ...to.query };
      delete query.section; delete query.kind; delete query.task;
      return { path: "/learning", query };
    }
  }
  document.title = `${to.meta.title || "工作台"} · QUT机器人战队`;
});
export default router;
