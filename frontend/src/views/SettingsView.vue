<script setup lang="ts">
import { onMounted, reactive, ref } from "vue";
import { api } from "../lib/api";
import type { EmailServiceStatus, Settings } from "../lib/types";
const form = reactive<Settings>({
  organization: "",
  season: "",
  brandName: "QUT机器人战队",
  logoUrl: "/mark.svg",
  teamName: "沧溟",
  secondaryTeamName: "MOVE",
  competitions: ["RoboCon", "RoboMaster"],
  applicationOpen: false,
  applicationDeadline: "",
  hydroUrl: "",
  announcement: "",
});
const deadline = ref(""),
  logoFailed = ref(false),
  loading = ref(true),
  loaded = ref(false),
  saving = ref(false),
  error = ref(""),
  saved = ref(false),
  savedMessage = ref("设置已保存");
const emailStatus = ref<EmailServiceStatus | null>(null),
  emailStatusLoading = ref(true),
  emailStatusError = ref("");
const logoFile = ref<File | null>(null),
  uploadingLogo = ref(false);
async function uploadLogo() {
  if (!logoFile.value || saving.value || uploadingLogo.value) return;
  const file = logoFile.value;
  if (!["image/png", "image/jpeg"].includes(file.type) || file.size > 2 * 1024 * 1024) {
    error.value = "请选择 2 MiB 以内的 PNG 或 JPEG 标志。";
    return;
  }
  uploadingLogo.value = true;
  error.value = "";
  try {
    const contentBase64 = await new Promise<string>((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => resolve(String(reader.result).split(",")[1] || "");
      reader.onerror = () => reject(new Error("无法读取标志文件，请重新选择。"));
      reader.readAsDataURL(file);
    });
    const result = await api.post<{ settings: Settings }>("/settings/logo", {
      name: file.name,
      contentBase64,
    });
    form.logoUrl = result.settings.logoUrl;
    logoFailed.value = false;
    logoFile.value = null;
    savedMessage.value = "标志已更新，其他修改请点击保存设置。";
    saved.value = true;
    window.dispatchEvent(new Event("settings-updated"));
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    uploadingLogo.value = false;
  }
}
async function loadEmailStatus() {
  emailStatusLoading.value = true;
  emailStatusError.value = "";
  try {
    emailStatus.value = await api.get<EmailServiceStatus>("/settings/email");
  } catch (e) {
    emailStatusError.value = (e as Error).message;
  } finally {
    emailStatusLoading.value = false;
  }
}
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
    form.brandName ||= "QUT机器人战队";
    form.logoUrl ||= "/mark.svg";
    form.teamName ||= "沧溟";
    form.secondaryTeamName ||= "MOVE";
    form.competitions ||= ["RoboCon", "RoboMaster"];
    deadline.value = localDateTime(form.applicationDeadline);
    loaded.value = true;
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}
async function save() {
  if (!loaded.value || loading.value || saving.value || uploadingLogo.value) return;
  if (!form.organization.trim() || !form.season.trim()) {
    error.value = "请填写空间名称和赛季。";
    return;
  }
  if (!form.brandName?.trim() || !form.teamName?.trim() || !form.secondaryTeamName?.trim()) {
    error.value = "请填写平台名称和战队名称。";
    return;
  }
  if (form.logoUrl && (!/^(https?:\/\/|\/(?!\/))/i.test(form.logoUrl) || (form.logoUrl.startsWith("/api/") && !/^\/api\/public\/logo\/[A-Za-z0-9_-]+$/.test(form.logoUrl)))) {
    error.value = "标志地址需为公开静态路径，或以 https:// / http:// 开头；不能使用需登录的个人附件地址。";
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
    savedMessage.value = "设置已保存";
    saved.value = true;
    window.dispatchEvent(new Event("settings-updated"));
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    saving.value = false;
  }
}
onMounted(() => {
  void load();
  void loadEmailStatus();
});
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
          label="学校 / 战队全称"
          maxlength="80"
          required />
        <v-text-field
          v-model="form.brandName"
          label="平台展示名称"
          placeholder="QUT机器人战队"
          maxlength="80"
          required />
        <v-row><v-col cols="12" sm="6"><v-text-field v-model="form.teamName" label="战队中文名称" maxlength="60" required /></v-col><v-col cols="12" sm="6"><v-text-field v-model="form.secondaryTeamName" label="战队英文名称" maxlength="60" required /></v-col></v-row>
        <v-alert type="info" variant="tonal" density="compact" class="mb-5">中文名称与英文名称属于同一支战队，成员、资料和报名统一管理。</v-alert>
        <v-select v-model="form.competitions" :items="['RoboCon', 'RoboMaster']" label="同时参与的赛事" multiple chips hint="可随战队实际参赛安排调整。" persistent-hint class="mb-5" />
        <div class="logo-preview"><img :src="logoFailed ? '/mark.svg' : form.logoUrl || '/mark.svg'" alt="当前战队标志" @error="logoFailed = true"/><div><strong>{{ form.brandName || 'QUT机器人战队' }}</strong><span>{{ form.teamName || '沧溟' }} / {{ form.secondaryTeamName || 'MOVE' }}</span></div></div>
        <v-file-input v-model="logoFile" accept="image/png,image/jpeg" label="上传战队标志" prepend-icon="mdi-image-outline" show-size :disabled="uploadingLogo || saving" hint="PNG / JPEG，最多 2 MiB。点击上传后立即替换登录页和侧栏标志。" persistent-hint />
        <v-btn type="button" class="mb-5" color="primary" variant="tonal" prepend-icon="mdi-upload" :loading="uploadingLogo" :disabled="!logoFile || saving" @click="uploadLogo">上传并替换标志</v-btn>
        <v-text-field v-model="form.logoUrl" label="战队标志地址" placeholder="/mark.svg 或 https://example.com/logo.png" hint="登录页和侧栏使用同一标志。推荐透明背景 PNG 或 SVG。" persistent-hint :disabled="uploadingLogo" @update:model-value="logoFailed = false" />
        <v-btn type="button" class="mb-5" variant="text" size="small" prepend-icon="mdi-restore" :disabled="uploadingLogo" @click="form.logoUrl = '/mark.svg'; logoFailed = false">恢复默认 M 标志</v-btn>
        <v-text-field
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
      <v-card class="settings-card">
        <div class="settings-heading">
          <v-icon color="primary">mdi-email-check-outline</v-icon>
          <div>
            <h2>注册邮箱验证</h2>
            <p>新账号需通过邮箱验证码验证后才能注册</p>
          </div>
        </div>
        <v-progress-linear
          v-if="emailStatusLoading"
          indeterminate
          color="primary"
        />
        <v-alert
          v-else-if="emailStatusError"
          type="warning"
          variant="tonal"
          density="compact"
        >
          邮件服务状态读取失败。
          <v-btn
            type="button"
            variant="text"
            size="small"
            @click="loadEmailStatus"
            >重试</v-btn
          >
        </v-alert>
        <template v-else-if="emailStatus">
          <v-chip
            :color="emailStatus.configured ? 'success' : 'warning'"
            size="small"
            class="mb-3"
            :prepend-icon="
              emailStatus.configured
                ? 'mdi-check-circle-outline'
                : 'mdi-clock-outline'
            "
            >{{
              emailStatus.configured ? "已配置发信服务" : "发信服务未配置"
            }}</v-chip
          >
          <p v-if="emailStatus.configured" class="email-status-text">
            发件邮箱：{{ emailStatus.from }}
          </p>
          <p class="email-status-text">
            {{
              emailStatus.configured
                ? "账号注册已开放，验证码有效期为 10 分钟。"
                : "暂不开放新账号注册，已有账号仍可登录。"
            }}
            如需调整发信服务，请联系部署管理员。
          </p>
        </template>
      </v-card>
      <div class="d-flex align-center justify-space-between ga-4">
        <span class="save-hint">修改会写入操作记录</span
        ><v-btn
          type="submit"
          color="primary"
          prepend-icon="mdi-check"
          :loading="saving"
          :disabled="uploadingLogo"
          >保存设置</v-btn
        >
      </div>
    </form>
    <v-snackbar v-model="saved" color="success">{{ savedMessage }}</v-snackbar>
  </div>
</template>
<style scoped>
.logo-preview {
  display: flex;
  align-items: center;
  gap: 16px;
  border: 1px solid #eee;
  border-radius: 6px;
  padding: 16px;
  margin-bottom: 20px;
  background: #fafafa;
}
.logo-preview img { width: 56px; height: 56px; object-fit: contain; }
.logo-preview strong, .logo-preview span { display: block; }
.logo-preview strong { font-size: 16px; font-weight: 500; }
.logo-preview span { font-size: 13px; color: #757575; margin-top: 5px; }
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
.email-status-text {
  margin: 0 0 6px;
  font-size: 13px;
  line-height: 1.7;
  color: #616161;
  overflow-wrap: anywhere;
}
@media (max-width: 600px) {
  .settings-card {
    padding: 22px 18px;
  }
}
</style>
