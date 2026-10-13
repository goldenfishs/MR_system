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
  () => route.path,
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
</script>

<template>
  <v-app>
    <template v-if="auth.user && route.path !== '/login'">
      <v-navigation-drawer
        v-model="drawer"
        :temporary="mobile"
        width="238"
        class="side-panel"
      >
        <router-link to="/" class="brand"
          ><img src="/mark.svg" alt="竞跃" width="38" height="38" />
          <div>
            <strong>竞跃<span>MR SYSTEM</span></strong
            ><small>战队协作台</small>
          </div></router-link
        >
        <div class="space-label">
          <span class="space-dot" />
          {{ settings?.organization || "机器人实验室" }}
        </div>
        <div class="nav-caption">协作空间</div>
        <v-list nav class="nav-list">
          <v-list-item
            v-for="item in navigation"
            :key="item.path"
            :to="item.path"
            :exact="item.path === '/'"
            :prepend-icon="item.icon"
            :title="item.title"
            color="primary"
            rounded="lg"
          />
        </v-list>
        <div class="side-divider" />
        <div class="nav-caption">战队方向</div>
        <div class="team-tag">
          <span class="team-square rm">R</span>
          <div>RoboMaster<small>机甲大师高校系列赛</small></div>
        </div>
        <div class="team-tag">
          <span class="team-square rc">R</span>
          <div>ROBOCON<small>大学生机器人大赛</small></div>
        </div>
        <template #append>
          <div class="season-card">
            <div class="d-flex align-center ga-2">
              <v-icon size="17" color="primary">mdi-flag-outline</v-icon
              ><strong>{{ settings?.season || "新赛季" }}</strong>
            </div>
            <p>一起，让想法落地。</p>
            <div class="season-line"><i /></div>
          </div>
          <v-list nav class="px-3 pt-0" v-if="auth.user.role === 'admin'"
            ><v-list-item
              to="/settings"
              prepend-icon="mdi-cog-outline"
              title="空间设置"
              color="primary"
              rounded="lg"
          /></v-list>
          <div class="side-footer">
            MR System <span>v0.1</span
            ><v-chip
              v-if="auth.demoMode"
              size="x-small"
              color="warning"
              class="ml-2"
              >演示</v-chip
            >
          </div>
        </template>
      </v-navigation-drawer>

      <v-app-bar flat height="68" class="topbar">
        <v-app-bar-nav-icon
          v-if="mobile"
          @click="drawer = !drawer"
          aria-label="打开导航"
        />
        <div class="topbar-path">
          <span>协作空间</span><v-icon size="14">mdi-chevron-right</v-icon
          ><strong>{{ route.meta.title }}</strong>
        </div>
        <v-spacer />
        <span class="topbar-season d-none d-sm-flex"
          ><span class="status-dot" />{{
            settings?.season || "机器人战队"
          }}</span
        >
        <v-divider vertical inset class="mx-5 d-none d-sm-block" />
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
              ><v-icon size="17">mdi-chevron-down</v-icon>
            </button></template
          >
          <v-list min-width="210"
            ><v-list-item
              :title="auth.user.name"
              :subtitle="auth.user.email" /><v-divider /><v-list-item
              prepend-icon="mdi-logout"
              title="退出登录"
              :disabled="loggingOut"
              @click="logout"
          /></v-list>
        </v-menu>
      </v-app-bar>
    </template>
    <v-main><router-view /></v-main>
    <v-snackbar
      :model-value="!!logoutError"
      color="error"
      @update:model-value="logoutError = ''"
      >{{ logoutError }}</v-snackbar
    >
  </v-app>
</template>

<style scoped>
.side-panel {
  border-right: 1px solid #e8ecf4 !important;
}
.brand {
  display: flex;
  align-items: center;
  gap: 11px;
  padding: 25px 23px 22px;
  text-decoration: none;
  color: #26304a;
}
.brand strong {
  font-size: 20px;
  letter-spacing: 1px;
  display: flex;
  align-items: center;
  gap: 8px;
}
.brand strong span {
  color: #a5adbd;
  font-size: 8px;
  letter-spacing: 1px;
}
.brand small {
  color: #929bb0;
  font-size: 10px;
  letter-spacing: 2px;
}
.space-label {
  margin: 0 19px 24px;
  padding: 10px 12px;
  border: 1px solid #edf0f5;
  border-radius: 8px;
  font-size: 12px;
  color: #6c7690;
  display: flex;
  align-items: center;
  gap: 8px;
}
.space-dot {
  height: 6px;
  width: 6px;
  border-radius: 50%;
  background: #68bba1;
}
.nav-caption {
  color: #a1a9b9;
  font-size: 10px;
  letter-spacing: 1px;
  padding: 0 28px 9px;
}
.nav-list {
  padding: 0 15px;
}
.nav-list .v-list-item {
  margin-bottom: 7px;
  min-height: 46px;
}
.nav-list :deep(.v-list-item__prepend > .v-icon) {
  font-size: 20px;
  margin-inline-end: 0;
}
.nav-list :deep(.v-list-item__spacer) {
  width: 15px;
}
.nav-list :deep(.v-list-item-title) {
  font-size: 13px;
  font-weight: 550;
}
.side-divider {
  height: 1px;
  background: #eef0f5;
  margin: 24px 25px;
}
.team-tag {
  display: flex;
  align-items: center;
  gap: 11px;
  padding: 10px 26px;
  color: #58647d;
  font-size: 12px;
  font-weight: 600;
}
.team-tag small {
  display: block;
  color: #a1a8b8;
  font-size: 9px;
  margin-top: 3px;
  font-weight: 400;
}
.team-square {
  width: 30px;
  height: 30px;
  display: grid;
  place-items: center;
  border-radius: 8px;
  font-family: monospace;
  font-weight: 700;
}
.rm {
  background: #fff2e9;
  color: #c88b56;
}
.rc {
  background: #eaf6f2;
  color: #5a9a85;
}
.season-card {
  margin: 16px 20px 18px;
  padding: 15px;
  background: #f6f7fc;
  border: 1px solid #eef0f7;
  border-radius: 10px;
}
.season-card strong {
  font-size: 11px;
}
.season-card p {
  font-size: 10px;
  color: #989fb2;
  margin: 9px 0 13px;
}
.season-line {
  height: 3px;
  background: #e5e9f5;
  border-radius: 2px;
}
.season-line i {
  display: block;
  width: 36%;
  height: 100%;
  background: #8c9bdb;
  border-radius: 2px;
}
.side-footer {
  padding: 0 26px 18px;
  font-size: 10px;
  color: #aeb5c3;
}
.side-footer span {
  margin-left: 4px;
}
.topbar {
  background: #fff !important;
  border-bottom: 1px solid #e8ecf4;
  padding-right: 25px;
}
.topbar-path {
  display: flex;
  gap: 14px;
  align-items: center;
  padding-left: 36px;
  font-size: 12px;
  color: #a1a8b9;
}
.topbar-path strong {
  color: #5f6b82;
  font-weight: 500;
}
.topbar-season {
  align-items: center;
  gap: 8px;
  font-size: 11px;
  color: #8590a6;
}
.status-dot {
  width: 6px;
  height: 6px;
  background: #6db9a1;
  border-radius: 50%;
}
.profile-button {
  display: flex;
  align-items: center;
  gap: 10px;
  border-radius: 8px;
  padding: 4px;
}
.profile-avatar {
  width: 34px;
  height: 34px;
  border-radius: 11px;
  display: grid;
  place-items: center;
  background: #edf0fa;
  color: #6675b6;
  font-size: 11px;
  font-weight: 700;
}
.profile-text {
  font-size: 12px;
  text-align: left;
}
.profile-text small {
  display: block;
  font-size: 9px;
  color: #9ba3b5;
  margin-top: 2px;
}
@media (max-width: 600px) {
  .topbar {
    padding-right: 10px;
  }
  .topbar-path {
    padding-left: 0;
    gap: 7px;
  }
  .topbar-path > span {
    display: none;
  }
}
</style>
