<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useDisplay } from "vuetify";
import { auth, roleNames, signOut } from "./lib/auth";
import { canAccessLearning, canOpenLearning, canAccessDirection, canAccessLearningModule, learningAccess, learningDirections, refreshLearningAccess } from "./lib/learning-access";
import { api } from "./lib/api";
import { groupOptions, groupLabel, hasPermission } from "./lib/permissions";
import type { Permission, Settings } from "./lib/types";
import type { Profile } from "./lib/platform";
import ProfileCompletionReminder from "./components/ProfileCompletionReminder.vue";
const route = useRoute(),
  router = useRouter();
const { mobile } = useDisplay();
const drawer = ref(!mobile.value),
  settings = ref<Settings | null>(null),
  avatarFileId = ref(""),
  logoutError = ref(""),
  loggingOut = ref(false),
  redirectingToLogin = ref(false);
const navigation = computed(() => [
  {
    path: "/",
    title: "工作台",
    icon: "mdi-view-dashboard-outline",
    tag: "OVERVIEW",
  },
  {
    path: "/teams",
    title: "团队报名",
    icon: "mdi-account-multiple-plus-outline",
  },
  { path: "/training", title: "新生考核", icon: "mdi-account-school-outline" },
  ...(canOpenLearning.value ? [{
    path: "/learning",
    title: "学习与任务",
    icon: "mdi-book-open-page-variant-outline",
  }] : []),
  { path: "/discussions", title: "讨论区", icon: "mdi-forum-outline" },
  {
    path: "/records",
    title: "荣誉中心",
    icon: "mdi-certificate-outline",
  },
  { path: "/profile", title: "个人中心", icon: "mdi-account-circle-outline" },
  ...(hasPermission("mail")
    ? [{ path: "/mail", title: "批量邮件", icon: "mdi-email-multiple-outline" }]
    : []),
  ...(hasPermission("members")
    ? [
        {
          path: "/members",
          title: "战队成员",
          icon: "mdi-account-group-outline",
        },
      ]
    : []),
  ...(hasPermission("recruitment")
    ? [
        {
          path: "/recruitment",
          title: "面试记录",
          icon: "mdi-account-clock-outline",
        },
      ]
    : []),
]);
async function loadSettings() {
  if (!auth.user) return;
  try {
    settings.value = (
      await api.get<{ settings: Settings }>("/settings")
    ).settings;
  } catch {
    /* Pages provide request errors. */
  }
}
watch(() => auth.user?.id, loadSettings, { immediate: true });
let avatarRequestId = 0;
async function loadAvatar() {
  const requestId = ++avatarRequestId;
  avatarFileId.value = "";
  if (!auth.user) return;
  try {
    const result = await api.get<{ profile: Profile }>("/profile");
    if (requestId === avatarRequestId && auth.user) {
      avatarFileId.value = result.profile.avatarFileId || "";
    }
  } catch {
    // The initials fallback keeps the account menu usable if profile loading fails.
  }
}
watch(() => auth.user?.id, loadAvatar, { immediate: true });
watch([settings, () => route.fullPath], () => {
  document.title = `${route.meta.title || "工作台"} · ${settings.value?.brandName || "QUT机器人战队"}`;
});
watch(mobile, (value) => (drawer.value = !value));
watch(
  () => route.fullPath,
  () => {
    if (mobile.value) drawer.value = false;
  },
);
async function logout() {
  loggingOut.value = true;
  logoutError.value = "";
  try {
    await signOut();
    await router.replace("/login");
  } catch (e) {
    logoutError.value = (e as Error).message;
  } finally {
    loggingOut.value = false;
  }
}
async function expired() {
  if (!auth.user || auth.signingOut || loggingOut.value || redirectingToLogin.value || route.path === "/login") return;
  redirectingToLogin.value = true;
  const redirect = route.fullPath;
  auth.user = null;
  try {
    await router.replace({ path: "/login", query: { expired: "1", redirect } });
  } finally {
    redirectingToLogin.value = false;
  }
}
async function refreshAccess() {
  await refreshLearningAccess();
  if (!auth.user || auth.signingOut || loggingOut.value || redirectingToLogin.value) return;
  if (!canOpenLearning.value && ["/learning", "/assessment"].includes(route.path)) {
    await router.replace({ path: "/teams", query: { admissionRequired: "1" } });
  } else if (route.path === "/assessment" && !canAccessLearning.value && !hasPermission("tasks")) {
    await router.replace("/learning");
  } else if (route.path === "/learning") {
    const direction = typeof route.query.direction === "string" ? route.query.direction : undefined;
    const section = route.query.section || (route.query.kind ? "tasks" : "home");
    if (direction && !canAccessDirection(direction)) {
      await router.replace({ path: "/learning", query: { ...route.query, direction: learningDirections.value[0]?.value || "" } });
    } else if (section === "knowledge" && !canAccessLearningModule("knowledge", direction) || ["tasks", "exam", "bank"].includes(String(section)) && !canAccessLearningModule("tasks", direction)) {
      const query = { ...route.query };
      delete query.section; delete query.kind; delete query.task;
      await router.replace({ path: "/learning", query });
    }
  }
}
function refreshVisibleAccess() {
  if (document.visibilityState === "visible") void refreshAccess();
}
onMounted(() => {
  window.addEventListener("session-expired", expired);
  window.addEventListener("settings-updated", loadSettings);
  window.addEventListener("profile-updated", loadAvatar);
  window.addEventListener("focus", refreshAccess);
  document.addEventListener("visibilitychange", refreshVisibleAccess);
});
onUnmounted(() => {
  window.removeEventListener("session-expired", expired);
  window.removeEventListener("settings-updated", loadSettings);
  window.removeEventListener("profile-updated", loadAvatar);
  window.removeEventListener("focus", refreshAccess);
  document.removeEventListener("visibilitychange", refreshVisibleAccess);
});

const accessIdentity = computed(() => auth.user ? JSON.stringify([auth.user.id, auth.user.role, auth.user.memberType, auth.user.direction, auth.user.groupGrants]) : "signed-out");
watch(accessIdentity, async () => {
  if (loggingOut.value || auth.signingOut || redirectingToLogin.value) return;
  if (!auth.user && route.path !== "/login") await router.replace({ path: "/login", query: { redirect: route.fullPath } });
  else if (route.meta.admin && auth.user?.role !== "admin" || route.meta.permission && !hasPermission(route.meta.permission as Permission)) await router.replace("/");
  else if (auth.user) await refreshAccess();
});
const directions = groupOptions;
const sectionTabs = computed(() => {
  if (route.path === "/recruitment" && hasPermission("recruitment"))
    return [
      { value: "all", title: "全部报名" },
      { value: "submitted", title: "待筛选" },
      { value: "screening", title: "筛选中" },
      { value: "interview", title: "待面试" },
      { value: "accepted", title: "已录取" },
      { value: "rejected", title: "未录取" },
    ];
  if (route.path === "/learning") {
    const direction = typeof route.query.direction === "string" ? route.query.direction : undefined;
    return [
      { value: "home", title: "首页" },
      ...(canAccessLearningModule("knowledge", direction) ? [{ value: "knowledge", title: "知识库" }] : []),
      ...(canAccessLearningModule("tasks", direction) ? [{ value: "tasks", title: "作业" }, { value: "exam", title: "考试" }] : []),
      ...(canAccessLearningModule("tasks", direction) && ["电控", "视觉算法", "硬件"].includes(String(route.query.direction))
        ? [{ value: "bank", title: "题库" }]
        : []),
    ];
  }
  return [];
});
const tabKey = computed(() =>
  route.path === "/recruitment" ? "status" : "section",
);
const activeTab = computed(() => {
  const value = route.query[tabKey.value];
  return sectionTabs.value.some((item) => item.value === value)
    ? value
    : route.path === "/learning"
      ? route.query.kind ? "tasks" : "home"
      : "all";
});
function selectTab(value: unknown) {
  const query = { ...route.query };
  delete query.applicant;
  delete query.task;
  if (route.path === "/learning") delete query.kind;
  if (value === "all" || value === "home") delete query[tabKey.value];
  else query[tabKey.value] = String(value);
  router.push({ path: route.path, query });
}
function directionLink(direction: string) {
  if (!canAccessDirection(direction)) return { path: "/teams", query: { admissionRequired: "1", direction } };
  return {
    path: "/learning",
    query: { direction, section: "home" },
  };
}
</script>

<template>
  <v-app>
    <template v-if="auth.user && route.path !== '/login'">
      <v-navigation-drawer
        v-model="drawer"
        :temporary="mobile"
        width="276"
        class="side-panel"
      >
        <router-link to="/" class="brand" :aria-label="`${settings?.brandName || 'QUT机器人战队'} · 工作台`">
          <img :src="settings?.logoUrl || '/mark.png'" width="34" height="34" alt="M" @error="($event.target as HTMLImageElement).src = '/mark.png'" />
          <div class="brand-copy">
            <span class="brand-title">{{ settings?.brandName || "QUT机器人战队" }}</span>
            <span
              class="brand-organization"
              :title="settings?.organization || '青岛理工大学机器人战队'"
              >{{ settings?.teamName || "沧溟" }} / {{ settings?.secondaryTeamName || "MOVE" }} · {{ settings?.organization || "青岛理工大学机器人战队" }}</span
            >
          </div>
        </router-link>
        <v-divider />
        <div class="nav-caption">工作空间</div>
        <v-list nav :prepend-gap="18" class="nav-list">
          <v-list-item
            v-for="item in navigation"
            :key="item.path"
            :to="item.path"
            :exact="item.path === '/'"
            :prepend-icon="item.icon"
            :title="item.title"
            color="primary"
            rounded="0"
          />
        </v-list>
        <v-divider class="mx-4 my-2" />
        <div class="nav-caption">技术分组 <span class="group-legend">🌟 已报名</span></div>
        <v-list
          nav
          density="compact"
          :prepend-gap="18"
          class="nav-list direction-list"
        >
          <v-list-item
            v-for="item in directions"
            :key="item.value"
            :to="directionLink(item.value)"
            :active="
              route.query.direction === item.value &&
              ['/learning', '/recruitment'].includes(route.path)
            "
            :prepend-icon="item.icon"
            :title="item.title"
            color="primary"
            rounded="0"
          >
            <template #append
              ><span
                v-if="learningAccess.appliedDirections.includes(item.value)"
                class="applied-star"
                role="img"
                :aria-label="`已报名${item.title}`"
                title="已报名"
                >🌟</span
              ></template
            >
          </v-list-item>
        </v-list>
        <template #append>
          <div class="sidebar-bottom">
            <v-btn
              v-if="auth.user.role === 'admin'"
              to="/settings"
              variant="text"
              prepend-icon="mdi-cog-outline"
              block
              class="settings-link"
              :color="route.path === '/settings' ? 'primary' : undefined"
              >空间设置</v-btn
            >
            <div class="sidebar-meta">
              <span>{{ settings?.season || "新赛季" }}</span
              ><span v-if="auth.demoMode" class="demo-label">演示环境</span
              ><span v-else>MR System</span>
            </div>
          </div>
        </template>
      </v-navigation-drawer>
      <v-app-bar
        height="64"
        :extended="!!sectionTabs.length"
        extension-height="48"
        class="topbar"
        elevation="2"
      >
        <v-app-bar-nav-icon
          @click="drawer = !drawer"
          :aria-label="drawer ? '收起导航' : '打开导航'"
        />
        <v-app-bar-title>{{
          route.path === "/recruitment" && !hasPermission("recruitment")
            ? "我的报名"
            : route.path === "/learning" && route.query.direction
              ? groupLabel(String(route.query.direction))
              : route.meta.title
        }}</v-app-bar-title>
        <span class="topbar-season d-none d-sm-flex">{{
          settings?.season || "机器人战队"
        }}</span>
        <v-menu>
          <template #activator="{ props }"
            ><button
              v-bind="props"
              class="profile-button"
              aria-label="个人账户"
            >
              <span class="profile-avatar"><img v-if="avatarFileId" :src="`/api/files/${encodeURIComponent(avatarFileId)}/preview`" alt="" /><template v-else>{{ auth.user.name.slice(-2) }}</template></span
              ><span class="profile-text"
                >{{ auth.user.name
                }}<small>{{ roleNames[auth.user.role] }}</small></span
              ><v-icon size="18">mdi-chevron-down</v-icon>
            </button></template
          >
          <v-list min-width="230"
            ><v-list-item
              :title="auth.user.name"
              :subtitle="auth.user.email" /><v-divider /><v-list-item
              to="/profile"
              prepend-icon="mdi-account-circle-outline"
              title="个人中心" /><v-list-item
              to="/records"
              prepend-icon="mdi-certificate-outline"
              title="荣誉中心" /><v-divider /><v-list-item
              prepend-icon="mdi-logout"
              title="退出登录"
              :disabled="loggingOut"
              @click="logout"
          /></v-list>
        </v-menu>
        <template v-if="sectionTabs.length" #extension>
          <v-tabs
            :model-value="activeTab"
            @update:model-value="selectTab"
            color="white"
            slider-color="yellow-accent-2"
            class="context-tabs"
            show-arrows
            height="48"
            aria-label="页面分类"
          >
            <v-tab
              v-for="item in sectionTabs"
              :key="item.value"
              :value="item.value"
              >{{ item.title }}</v-tab
            >
          </v-tabs>
        </template>
      </v-app-bar>
    </template>
    <v-main><router-view :key="accessIdentity" /></v-main>
    <ProfileCompletionReminder />
    <v-snackbar
      :model-value="!!logoutError"
      color="error"
      @update:model-value="!$event && (logoutError = '')"
      >{{ logoutError }}</v-snackbar
    >
  </v-app>
</template>
<style scoped>
.side-panel {
  border-right: 1px solid #e4e6e9 !important;
}
.brand {
  height: 64px;
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 0 22px;
  color: #30343b;
  text-decoration: none;
}
.brand img {
  flex-shrink: 0;
  object-fit: contain;
}
.brand-copy {
  min-width: 0;
}
.brand-title,
.brand-organization {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.brand-title {
  font-size: 16px;
  font-weight: 500;
  line-height: 22px;
}
.brand-organization {
  color: #757575;
  font-size: 12px;
  line-height: 18px;
}
.nav-caption {
  font-size: 12px;
  color: #858585;
  padding: 20px 23px 5px;
}
.group-legend {
  float: right;
  font-size: 11px;
}
.applied-star {
  font-size: 17px;
  line-height: 1;
}
.nav-list {
  padding: 4px 10px 8px;
}
.nav-list :deep(.v-list-item) {
  min-height: 45px;
  margin-bottom: 2px;
  padding-left: 14px;
}
.nav-list :deep(.v-list-item-title) {
  font-size: 14px;
  font-weight: 400;
}
.nav-list :deep(.v-list-item__prepend > .v-icon) {
  font-size: 22px;
  opacity: 0.7;
  margin-inline-end: 0;
}
.nav-list :deep(.v-list-item--active .v-list-item-title) {
  font-weight: 500;
}
.direction-list :deep(.v-list-item) {
  min-height: 40px;
}
.sidebar-bottom {
  border-top: 1px solid #eee;
  padding: 10px 10px 14px;
  background: white;
}
.settings-link {
  justify-content: flex-start;
  height: 40px;
  padding-inline: 14px;
}
.settings-link :deep(.v-btn__prepend) {
  margin-inline: 0 18px;
}
.settings-link :deep(.v-icon) {
  font-size: 22px;
}
.sidebar-meta {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 10px 14px 0;
  font-size: 11px;
  color: #8b8b8b;
}
.demo-label {
  color: #9c6c13;
  background: #fff6dc;
  padding: 2px 7px;
  border-radius: 3px;
}
.topbar {
  color: white !important;
  background: linear-gradient(
    110deg,
    #167d87 0%,
    #2854ad 33%,
    #4630b3 65%,
    #8426b7 100%
  ) !important;
}
.topbar :deep(.v-toolbar__content) {
  padding-right: 18px;
}
.topbar :deep(.v-toolbar-title) {
  font-size: 20px;
  font-weight: 400;
  margin-inline-start: 12px !important;
}
.topbar-season {
  font-size: 13px;
  color: #ffffffbd;
  margin-right: 24px;
}
.profile-button {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 5px 7px;
  border-radius: 4px;
  color: white;
}
.profile-button:hover {
  background: #ffffff12;
}
.profile-avatar {
  border-radius: 50%;
  width: 34px;
  height: 34px;
  background: #ffffff26;
  border: 1px solid #ffffff50;
  display: grid;
  place-items: center;
  font-size: 12px;
  overflow: hidden;
}
.profile-avatar img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.profile-text {
  text-align: left;
  font-size: 13px;
}
.profile-text small {
  display: block;
  font-size: 10px;
  margin-top: 1px;
  opacity: 0.7;
}
.context-tabs {
  width: 100%;
  padding-left: 18px;
  background: #ffffff07;
}
.context-tabs :deep(.v-tab) {
  min-width: 110px;
  font-size: 14px;
  font-weight: 400;
  opacity: 0.8;
}
.context-tabs :deep(.v-tab--selected) {
  opacity: 1;
}
.context-tabs :deep(.v-tab__slider) {
  height: 3px;
}
@media (max-width: 600px) {
  .topbar :deep(.v-toolbar__content) {
    padding-right: 7px;
  }
  .topbar :deep(.v-toolbar-title) {
    font-size: 18px;
    margin-inline-start: 0 !important;
  }
  .profile-text small {
    display: none;
  }
  .profile-avatar {
    width: 30px;
    height: 30px;
  }
  .profile-button {
    gap: 7px;
  }
  .profile-text {
    font-size: 12px;
  }
  .context-tabs {
    padding-left: 0;
  }
  .context-tabs :deep(.v-tab) {
    min-width: 94px;
    font-size: 13px;
  }
}
</style>
