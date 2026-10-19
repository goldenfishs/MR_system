<script setup lang="ts">
import { nextTick, onMounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { api } from "../lib/api";
import type { MailServiceSettings, Settings } from "../lib/types";
const route = useRoute();
const router = useRouter();
const form = reactive<Settings>({
  description: "",
  organization: "",
  season: "",
  brandName: "QUT机器人战队",
  logoUrl: "/mark.png",
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
const emailStatus = ref<MailServiceSettings | null>(null),
  emailStatusLoading = ref(true),
  emailStatusError = ref("");
const mailForm = reactive({ enabled: false, host: "", port: 587, username: "", from: "", fromName: "QUT机器人战队", security: "starttls", password: "" });
const savingMail = ref(false), showPassword = ref(false), clearPassword = ref(false);
const securityOptions = [{ title: "STARTTLS（通常 587）", value: "starttls" }, { title: "TLS（通常 465）", value: "tls" }];
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
    emailStatus.value = await api.get<MailServiceSettings>("/settings/mail-service");
    const status = emailStatus.value;
    Object.assign(mailForm, { enabled: status.enabled, host: status.host, port: status.port || 587, username: status.username, from: status.from, fromName: status.fromName, security: status.security || "starttls", password: "" });
    clearPassword.value = false;
  } catch (e) {
    emailStatusError.value = (e as Error).message;
  } finally {
    emailStatusLoading.value = false;
  }
}
async function saveMail() {
  if (savingMail.value || !emailStatus.value) return;
  if ((mailForm.enabled || mailForm.host) && (!mailForm.host.trim() || !mailForm.from.trim() || !Number.isInteger(Number(mailForm.port)) || Number(mailForm.port) < 1 || Number(mailForm.port) > 65535)) {
    emailStatusError.value = "请填写 SMTP 主机、有效端口和发件邮箱。";
    return;
  }
  savingMail.value = true;
  emailStatusError.value = "";
  try {
    emailStatus.value = await api.patch<MailServiceSettings>("/settings/mail-service", { ...mailForm, port: Number(mailForm.port), clearPassword: clearPassword.value });
    mailForm.password = "";
    clearPassword.value = false;
    savedMessage.value = "邮件设置已保存并生效";
    saved.value = true;
  } catch (e) {
    emailStatusError.value = (e as Error).message;
  } finally {
    savingMail.value = false;
  }
}
async function scrollToSection() {
  if (["#finance-recognition", "#ai"].includes(route.hash)) { await router.replace("/ai-settings"); return; }
  await nextTick();
  if (["#branding", "#email", "#general", "#finance-recognition"].includes(route.hash)) document.getElementById(route.hash.slice(1))?.scrollIntoView({ behavior: "smooth", block: "start" });
}
watch(() => route.hash, () => void scrollToSection());
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
    form.logoUrl ||= "/mark.png";
    form.teamName ||= "沧溟";
    form.secondaryTeamName ||= "MOVE";
    form.competitions ||= ["RoboCon", "RoboMaster"];
    deadline.value = localDateTime(form.applicationDeadline);
    loaded.value = true;
    void scrollToSection();
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}
async function save() {
  if (!loaded.value || loading.value || saving.value || uploadingLogo.value) return;
  if (!form.organization.trim() || !form.season.trim()) {
    error.value = "请填写网站全称和赛季。";
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
    <h1 class="page-title">系统设置</h1>
    <p class="page-subtitle">管理网站名称、标志、招新安排、发信邮箱与发票识别。修改后立即生效。</p>
    <div class="settings-shortcuts mb-6">
      <v-btn to="/admin" variant="tonal" prepend-icon="mdi-shield-crown-outline">系统管理</v-btn>
      <v-btn to="/members" variant="tonal" prepend-icon="mdi-account-cog-outline">用户管理</v-btn>
      <v-btn href="#branding" variant="text" prepend-icon="mdi-palette-outline">网站外观</v-btn>
      <v-btn href="#email" variant="text" prepend-icon="mdi-email-outline">邮件服务</v-btn>
      <v-btn to="/ai-settings" variant="text" prepend-icon="mdi-creation-outline">AI 配置</v-btn>
    </div>
    <v-alert v-if="error" type="error" variant="tonal" class="mb-5"
      >{{ error
      }}<v-btn v-if="!loading && !loaded" @click="load" variant="text"
        >重试</v-btn
      ></v-alert
    ><v-progress-linear v-if="loading" indeterminate color="primary" />
    <form v-else-if="loaded" @submit.prevent="save" class="settings-form">
      <v-card id="branding" class="settings-card"
        ><div class="settings-heading">
          <v-icon color="primary">mdi-office-building-outline</v-icon>
          <div>
            <h2>网站信息与标志</h2>
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
          label="网站简短名称"
          placeholder="QUT机器人战队"
          maxlength="50"
          hint="用于侧栏、登录页和浏览器标题。"
          persistent-hint
          required />
        <v-textarea v-model="form.description" label="网站简介" rows="2" maxlength="400" counter hint="显示在登录页；留空使用默认介绍。" persistent-hint />
        <v-row><v-col cols="12" sm="6"><v-text-field v-model="form.teamName" label="战队中文名称" maxlength="60" required /></v-col><v-col cols="12" sm="6"><v-text-field v-model="form.secondaryTeamName" label="战队英文名称" maxlength="60" required /></v-col></v-row>
        <v-alert type="info" variant="tonal" density="compact" class="mb-5">中文名称与英文名称属于同一支战队，成员、资料和报名统一管理。</v-alert>
        <v-select v-model="form.competitions" :items="['RoboCon', 'RoboMaster']" label="同时参与的赛事" multiple chips hint="可随战队实际参赛安排调整。" persistent-hint class="mb-5" />
        <div class="logo-preview"><img :src="logoFailed ? '/mark.png' : form.logoUrl || '/mark.png'" alt="当前战队标志" @error="logoFailed = true"/><div><strong>{{ form.brandName || 'QUT机器人战队' }}</strong><span>{{ form.teamName || '沧溟' }} / {{ form.secondaryTeamName || 'MOVE' }}</span></div></div>
        <v-file-input v-model="logoFile" accept="image/png,image/jpeg" label="上传战队标志" prepend-icon="mdi-image-outline" show-size :disabled="uploadingLogo || saving" hint="PNG / JPEG，最多 2 MiB。点击上传后立即替换登录页和侧栏标志。" persistent-hint />
        <v-btn type="button" class="mb-5" color="primary" variant="tonal" prepend-icon="mdi-upload" :loading="uploadingLogo" :disabled="!logoFile || saving" @click="uploadLogo">上传并替换标志</v-btn>
        <v-text-field v-model="form.logoUrl" label="战队标志地址" placeholder="/mark.png 或 https://example.com/logo.png" hint="登录页和侧栏使用同一标志。推荐透明背景 PNG 或 SVG。" persistent-hint :disabled="uploadingLogo" @update:model-value="logoFailed = false" />
        <v-btn type="button" class="mb-5" variant="text" size="small" prepend-icon="mdi-restore" :disabled="uploadingLogo" @click="form.logoUrl = '/mark.png'; logoFailed = false">恢复默认 M 标志</v-btn>
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
      ><v-card id="general" class="settings-card"
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
          :disabled="uploadingLogo"
          >保存网站设置</v-btn
        >
      </div>
    </form>
    <form v-if="loaded" @submit.prevent="saveMail" class="settings-form">
      <v-card id="email" class="settings-card">
        <div class="settings-heading">
          <v-icon color="primary">mdi-email-outline</v-icon>
          <div><h2>邮件服务</h2><p>用于注册邮箱验证码和批量邮件，只有系统管理员可以查看与修改。</p></div>
        </div>
        <v-progress-linear v-if="emailStatusLoading" indeterminate color="primary" />
        <v-alert v-if="emailStatusError" type="error" variant="tonal" density="compact" class="mb-4">
          {{ emailStatusError }}
          <v-btn v-if="!emailStatus" type="button" variant="text" size="small" @click="loadEmailStatus">重试</v-btn>
        </v-alert>
        <template v-if="emailStatus && !emailStatusLoading">
          <div class="d-flex flex-wrap align-center ga-2 mb-4">
            <v-chip :color="emailStatus.configured ? 'success' : 'warning'" size="small">{{ emailStatus.configured ? '发信已启用' : '发信已关闭' }}</v-chip>
            <span class="email-status-text mb-0">{{ emailStatus.source === 'database' ? '使用后台保存的配置' : '使用服务器初始配置' }}</span>
          </div>
          <v-alert type="info" variant="tonal" density="compact" class="mb-4">保存仅更新设置，不会发送邮件。关闭发信后，新用户将无法获取注册验证码。</v-alert>
          <v-switch v-model="mailForm.enabled" label="启用邮件服务" color="primary" inset hide-details class="mb-4" :disabled="savingMail" />
          <v-row>
            <v-col cols="12" sm="8"><v-text-field v-model="mailForm.host" label="SMTP 服务器" placeholder="smtp.163.com" maxlength="253" :disabled="savingMail" /></v-col>
            <v-col cols="12" sm="4"><v-text-field v-model.number="mailForm.port" label="端口" type="number" min="1" max="65535" :disabled="savingMail" /></v-col>
            <v-col cols="12" sm="6"><v-select v-model="mailForm.security" :items="securityOptions" label="连接加密方式" :disabled="savingMail" /></v-col>
            <v-col cols="12" sm="6"><v-text-field v-model="mailForm.username" label="SMTP 账号" autocomplete="off" maxlength="254" :disabled="savingMail" /></v-col>
            <v-col cols="12" sm="6"><v-text-field v-model="mailForm.from" label="发件邮箱" type="email" maxlength="254" :disabled="savingMail" /></v-col>
            <v-col cols="12" sm="6"><v-text-field v-model="mailForm.fromName" label="发件人名称" maxlength="40" :disabled="savingMail" /></v-col>
          </v-row>
          <v-text-field v-model="mailForm.password" :type="showPassword ? 'text' : 'password'" label="SMTP 授权码 / 密码" autocomplete="new-password" :append-inner-icon="showPassword ? 'mdi-eye-off-outline' : 'mdi-eye-outline'" @click:append-inner="showPassword = !showPassword" :disabled="savingMail || clearPassword" :hint="emailStatus.passwordSet ? '已保存授权码，留空则保留；不会显示已保存的密钥。' : '尚未保存授权码。163 等邮箱请使用 SMTP 授权码。'" persistent-hint />
          <v-checkbox v-if="emailStatus.passwordSet" v-model="clearPassword" label="清除已保存的授权码（仅供无需认证的邮件服务）" hide-details density="compact" :disabled="savingMail" class="mb-4" />
          <div class="d-flex justify-end mt-4"><v-btn type="submit" color="primary" variant="tonal" prepend-icon="mdi-content-save-outline" :loading="savingMail">保存邮件设置</v-btn></div>
        </template>
      </v-card>
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
  margin-bottom: 22px;
}
.settings-shortcuts { display: flex; flex-wrap: wrap; gap: 8px; }
.settings-card {
  scroll-margin-top: 90px;
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
