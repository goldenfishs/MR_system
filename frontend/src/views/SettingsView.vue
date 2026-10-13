<script setup lang="ts">
import { onMounted, reactive, ref } from "vue";
import { api } from "../lib/api";
import type { Settings } from "../lib/types";
const form = reactive<Settings>({
  organization: "",
  season: "",
  applicationOpen: false,
  applicationDeadline: "",
  hydroUrl: "",
  announcement: "",
});
const deadline = ref(""),
  loading = ref(true),
  loaded = ref(false),
  saving = ref(false),
  error = ref(""),
  saved = ref(false);
function localDateTime(value: string) {
  if (!value) return "";
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return "";
  return new Date(d.getTime() - d.getTimezoneOffset() * 60000)
    .toISOString()
    .slice(0, 16);
}
async function load() {
  error.value = "";
  loading.value = true;
  try {
    Object.assign(
      form,
      (await api.get<{ settings: Settings }>("/settings")).settings,
    );
    deadline.value = localDateTime(form.applicationDeadline);
    loaded.value = true;
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}
async function save() {
  if (!loaded.value || loading.value || saving.value) return;
  if (!form.organization.trim() || !form.season.trim()) {
    error.value = "请填写空间名称和赛季。";
    return;
  }
  if (form.hydroUrl && !/^https?:\/\//i.test(form.hydroUrl)) {
    error.value = "Hydro 地址需以 https:// 或 http:// 开头。";
    return;
  }
  saving.value = true;
  error.value = "";
  try {
    const payload = {
      ...form,
      applicationDeadline: deadline.value
        ? new Date(deadline.value).toISOString()
        : "",
    };
    Object.assign(
      form,
      (await api.patch<{ settings: Settings }>("/settings", payload)).settings,
    );
    saved.value = true;
    window.dispatchEvent(new Event("settings-updated"));
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    saving.value = false;
  }
}
onMounted(load);
</script>
<template>
  <div class="page-content">
    <h1 class="page-title">空间设置<span class="text-secondary">.</span></h1>
    <p class="page-subtitle">让协作空间，适应你的战队。</p>
    <v-alert v-if="error" type="error" variant="tonal" class="mb-5"
      >{{ error
      }}<v-btn v-if="!loading && !loaded" @click="load" variant="text"
        >重试</v-btn
      ></v-alert
    ><v-progress-linear v-if="loading" indeterminate color="primary" />
    <form v-else-if="loaded" @submit.prevent="save" class="settings-form">
      <v-card class="settings-card"
        ><div class="settings-heading">
          <v-icon color="primary">mdi-office-building-outline</v-icon>
          <div>
            <h2>空间信息</h2>
            <p>这些信息将展示在工作台和登录页</p>
          </div>
        </div>
        <v-text-field
          v-model="form.organization"
          label="实验室 / 战队名称"
          maxlength="80"
          required /><v-text-field
          v-model="form.season"
          label="当前赛季"
          placeholder="例如：2026—2027 赛季"
          maxlength="60"
          required /><v-textarea
          v-model="form.announcement"
          label="战队公告"
          placeholder="向队员分享重要安排和提醒"
          rows="3"
          maxlength="2000"
          counter /></v-card
      ><v-card class="settings-card"
        ><div class="settings-heading">
          <v-icon color="secondary">mdi-account-multiple-plus-outline</v-icon>
          <div>
            <h2>招新开放设置</h2>
            <p>截止后保留报名记录，停止新的报名与修改</p>
          </div>
        </div>
        <v-switch
          v-model="form.applicationOpen"
          label="开放报名"
          color="secondary"
          inset
          hide-details
          class="mb-5" /><v-text-field
          v-model="deadline"
          type="datetime-local"
          label="报名截止时间（本地时间）"
          hint="留空表示不设时间限制；关闭报名开关仍会立即停止接收。"
          persistent-hint /></v-card
      ><v-card class="settings-card"
        ><div class="settings-heading">
          <v-icon color="primary">mdi-code-braces</v-icon>
          <div>
            <h2>Hydro 测评入口</h2>
            <p>配置独立部署的在线评测平台地址</p>
          </div>
        </div>
        <v-text-field
          v-model="form.hydroUrl"
          label="Hydro 站点地址"
          placeholder="https://oj.example.com"
          type="url"
          hint="当前版本提供外部入口，成绩与账号同步尚未接入。"
          persistent-hint
      /></v-card>
      <div class="d-flex align-center justify-space-between ga-4">
        <span class="save-hint">修改会写入操作记录</span
        ><v-btn
          type="submit"
          color="primary"
          prepend-icon="mdi-check"
          :loading="saving"
          >保存设置</v-btn
        >
      </div>
    </form>
    <v-snackbar v-model="saved" color="success">设置已保存</v-snackbar>
  </div>
</template>
<style scoped>
.settings-form {
  max-width: 850px;
}
.settings-card {
  padding: 27px 30px;
  margin-bottom: 22px;
}
.settings-heading {
  display: flex;
  gap: 14px;
  align-items: center;
  margin-bottom: 24px;
}
.settings-heading h2 {
  font-size: 16px;
  margin: 0 0 6px;
}
.settings-heading p {
  font-size: 13px;
  color: #757575;
  margin: 0;
}
.save-hint {
  font-size: 13px;
  color: #757575;
}
@media (max-width: 600px) {
  .settings-card {
    padding: 22px 18px;
  }
}
</style>
