import { createRouter, createWebHistory } from "vue-router";
import { auth, loadSession } from "./lib/auth";
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
      meta: { title: "团队招募" },
    },
    {
      path: "/profile",
      component: () => import("./views/ProfileView.vue"),
      meta: { title: "个人资料" },
    },
    {
      path: "/records",
      component: () => import("./views/RecordsView.vue"),
      meta: { title: "个人档案" },
    },
    {
      path: "/files",
      component: () => import("./views/FilesView.vue"),
      meta: { title: "文件中心" },
    },
    {
      path: "/mail",
      component: () => import("./views/MailView.vue"),
      meta: { title: "批量邮件", admin: true },
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
        staff: true,
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
  if (to.meta.admin && auth.user?.role !== "admin") return "/";
  if (to.meta.staff && auth.user?.role === "student") return "/";
  document.title = `${to.meta.title || "工作台"} · 竞跃`;
});
export default router;
