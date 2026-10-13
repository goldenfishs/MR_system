<script setup lang="ts">
import { onMounted, ref } from "vue";
import { api } from "../lib/api";
import { auth } from "../lib/auth";
import type { Settings } from "../lib/types";
const settings = ref<Settings | null>(null),
  error = ref(""),
  loading = ref(true);
async function load() {
  error.value = "";
  loading.value = true;
  try {
    settings.value = (
      await api.get<{ settings: Settings }>("/settings")
    ).settings;
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}
onMounted(load);
</script>
<template>
  <div class="page-content assessment-page">
    <h1 class="page-title">在线测评</h1>
    <p class="page-subtitle">进入战队题库，完成编程练习与能力测评。</p>
    <v-alert v-if="error" type="error" variant="tonal" class="mb-5"
      >{{ error }}<v-btn variant="text" @click="load">重试</v-btn></v-alert
    >
    <v-progress-linear v-if="loading" color="primary" indeterminate />
    <v-card v-else-if="settings" class="hydro-card">
      <div class="hydro-heading">
        <div class="hydro-icon"><v-icon size="35">mdi-code-braces</v-icon></div>
        <div>
          <h2>Hydro 在线评测</h2>
          <p>编程、算法与自动判题</p>
        </div>
        <v-chip v-if="settings.hydroUrl" color="success" size="small"
          >已配置</v-chip
        >
      </div>
      <p class="hydro-description">
        战队编程练习由独立的 Hydro
        平台提供。机械、电控实机和文档任务，请在「学习与任务」中提交成果，由导师验收。
      </p>
      <template v-if="settings.hydroUrl"
        ><v-btn
          :href="settings.hydroUrl"
          target="_blank"
          rel="noopener noreferrer"
          color="primary"
          append-icon="mdi-open-in-new"
          >进入测评平台</v-btn
        >
        <p class="integration-note">
          在新页面打开，使用 Hydro 账号登录。成绩暂不自动同步。
        </p></template
      >
      <template v-else
        ><v-alert type="info" variant="tonal" density="compact" class="mb-5"
          >尚未配置 Hydro 站点，培养任务仍可正常使用。</v-alert
        ><v-btn
          v-if="auth.user?.role === 'admin'"
          to="/settings"
          color="primary"
          prepend-icon="mdi-cog-outline"
          >配置测评入口</v-btn
        ><v-btn v-else to="/learning" color="primary"
          >查看培养任务</v-btn
        ></template
      >
    </v-card>
    <div class="assessment-grid">
      <router-link
        v-for="item in [
          {
            title: '代码项目',
            description: '基础编程、数据结构与算法练习',
            icon: 'mdi-code-json',
            color: '#1976d2',
            kind: 'code',
          },
          {
            title: '工程与实机',
            description: '图纸、固件与实机实验成果',
            icon: 'mdi-robot-outline',
            color: '#43a047',
            kind: 'hardware',
          },
          {
            title: '规则与知识',
            description: '赛季规则阅读与知识检验',
            icon: 'mdi-book-check-outline',
            color: '#f39c28',
            kind: 'quiz',
          },
        ]"
        :key="item.kind"
        :to="{ path: '/learning', query: { kind: item.kind } }"
        class="assessment-link"
        ><div>
          <h3>{{ item.title }}</h3>
          <p>{{ item.description }}</p>
        </div>
        <v-icon :color="item.color" size="38">{{
          item.icon
        }}</v-icon></router-link
      >
    </div>
  </div>
</template>
<style scoped>
.assessment-page {
  max-width: 1250px;
}
.hydro-card {
  padding: 28px;
  margin-bottom: 24px;
}
.hydro-heading {
  display: flex;
  gap: 18px;
  align-items: center;
  margin-bottom: 22px;
}
.hydro-icon {
  width: 65px;
  height: 65px;
  background: #1976d2;
  color: white;
  border-radius: 6px;
  display: grid;
  place-items: center;
  box-shadow: 0 3px 8px #1976d230;
}
.hydro-heading h2 {
  font-size: 22px;
  font-weight: 400;
  margin: 0 0 5px;
}
.hydro-heading p {
  color: #757575;
  font-size: 13px;
  margin: 0;
}
.hydro-heading .v-chip {
  margin-left: auto;
}
.hydro-description {
  font-size: 14px;
  line-height: 1.9;
  color: #616161;
  max-width: 760px;
  margin-bottom: 23px;
}
.integration-note {
  font-size: 12px;
  color: #757575;
  margin-top: 15px;
  margin-bottom: 0;
}
.assessment-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 18px;
}
.assessment-link {
  display: flex;
  align-items: center;
  gap: 10px;
  justify-content: space-between;
  padding: 24px;
  color: inherit;
  text-decoration: none;
  background: white;
  border: 1px solid #e7e7e7;
  border-radius: 6px;
  box-shadow: 0 1px 3px #0000000a;
}
.assessment-link:hover {
  border-color: #90caf9;
}
.assessment-link h3 {
  font-size: 18px;
  font-weight: 400;
  margin: 0 0 9px;
}
.assessment-link p {
  color: #757575;
  font-size: 12px;
  line-height: 1.7;
  margin: 0;
}
@media (max-width: 1100px) {
  .assessment-grid {
    grid-template-columns: 1fr;
  }
}
@media (max-width: 600px) {
  .hydro-card {
    padding: 22px;
  }
  .hydro-heading h2 {
    font-size: 19px;
  }
  .hydro-heading {
    gap: 13px;
  }
  .hydro-icon {
    width: 52px;
    height: 52px;
  }
}
</style>
