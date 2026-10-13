<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useDisplay } from "vuetify";
import { auth, roleNames, signOut } from "./lib/auth";
import { api } from "./lib/api";
import type { Settings } from "./lib/types";
const route = useRoute(),
  router = useRouter();
const { mobile } = useDisplay();
const drawer = ref(!mobile.value),
  settings = ref<Settings | null>(null),
  logoutError = ref(""),
  loggingOut = ref(false);
const navigation = computed(() => [
  {
    path: "/",
    title: "工作台",
    icon: "mdi-view-dashboard-outline",
    tag: "OVERVIEW",
  },
  {
    path: "/recruitment",
    title: auth.user?.role === "student" ? "我的报名" : "招新管理",
    icon: "mdi-account-multiple-plus-outline",
  },
  {
    path: "/learning",
    title: "学习与任务",
    icon: "mdi-book-open-page-variant-outline",
  },
  { path: "/assessment", title: "在线测评", icon: "mdi-code-braces" },
  ...(auth.user?.role !== "student"
    ? [
        {
          path: "/members",
          title: "战队成员",
          icon: "mdi-account-group-outline",
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
    await router.push("/login");
  } catch (e) {
    logoutError.value = (e as Error).message;
  } finally {
    loggingOut.value = false;
  }
}
function expired() {
  auth.user = null;
  router.push({
    path: "/login",
    query: { expired: "1", redirect: route.fullPath },
  });
}
onMounted(() => {
  window.addEventListener("session-expired", expired);
  window.addEventListener("settings-updated", loadSettings);
});
onUnmounted(() => {
  window.removeEventListener("session-expired", expired);
  window.removeEventListener("settings-updated", loadSettings);
});

const directions = [
  { title: "机械", icon: "mdi-cog-outline" },
  { title: "电控", icon: "mdi-chip" },
  { title: "视觉算法", icon: "mdi-eye-outline" },
  { title: "硬件", icon: "mdi-memory" },
  { title: "运营", icon: "mdi-bullhorn-outline" },
];
const sectionTabs = computed(() => {
  if (route.path === "/recruitment" && auth.user?.role !== "student")
    return [
      { value: "all", title: "全部报名" },
      { value: "submitted", title: "待筛选" },
      { value: "screening", title: "筛选中" },
      { value: "interview", title: "待面试" },
      { value: "accepted", title: "已录取" },
      { value: "rejected", title: "未录取" },
    ];
  if (route.path === "/learning")
    return [
      { value: "all", title: "全部任务" },
      { value: "code", title: "代码项目" },
      { value: "document", title: "文档作业" },
      { value: "hardware", title: "实机验收" },
      { value: "quiz", title: "知识测评" },
    ];
  return [];
});
const tabKey = computed(() =>
  route.path === "/recruitment" ? "status" : "kind",
);
const activeTab = computed(() => {
  const value = route.query[tabKey.value];
  return sectionTabs.value.some((item) => item.value === value) ? value : "all";
});
function selectTab(value: unknown) {
  const query = { ...route.query };
  delete query.applicant;
  delete query.task;
  if (value === "all") delete query[tabKey.value];
  else query[tabKey.value] = String(value);
  router.push({ path: route.path, query });
}
function directionLink(direction: string) {
  return {
    path: auth.user?.role === "student" ? "/learning" : "/recruitment",
    query: { direction },
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
        <router-link to="/" class="brand" aria-label="竞跃协作系统 · 工作台">
          <img src="/mark.svg" width="28" height="28" alt="" />
          <div class="brand-copy">
            <span class="brand-title">竞跃协作系统</span>
            <span
              class="brand-organization"
              :title="settings?.organization || '机器人实验室'"
              >{{ settings?.organization || "机器人实验室" }}</span
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
        <div class="nav-caption">技术分组</div>
        <v-list
          nav
          density="compact"
          :prepend-gap="18"
          class="nav-list direction-list"
        >
          <v-list-item
            v-for="item in directions"
            :key="item.title"
            :to="directionLink(item.title)"
            :active="
              route.query.direction === item.title &&
              ['/learning', '/recruitment'].includes(route.path)
            "
            :prepend-icon="item.icon"
            :title="item.title + '组'"
            color="primary"
            rounded="0"
          >
            <template #append
              ><v-icon
                v-if="auth.user.direction === item.title"
                color="amber-darken-1"
                size="18"
                title="我的方向"
                >mdi-star</v-icon
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
          route.path === "/recruitment" && auth.user.role === "student"
            ? "我的报名"
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
              <span class="profile-avatar">{{ auth.user.name.slice(-2) }}</span
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
    <v-main><router-view /></v-main>
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
