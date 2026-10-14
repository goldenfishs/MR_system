<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { api, ApiError } from "../lib/api";
import { auth } from "../lib/auth";
import type { PublicConfig, User } from "../lib/types";
const route = useRoute(),
  router = useRouter();
const register = ref(false),
  email = ref(""),
  password = ref(""),
  confirmPassword = ref(""),
  verificationCode = ref(""),
  error = ref(""),
  loading = ref(false),
  sending = ref(false),
  sentMessage = ref(""),
  showPassword = ref(false);
const config = ref<PublicConfig | null>(null);
const configLoading = ref(true),
  configError = ref("");
const competitions = computed(() => config.value?.competitions?.length ? config.value.competitions : ["RoboCon", "RoboMaster"]);
const normalizedEmail = computed(() => email.value.trim().toLowerCase());
const cooldowns = ref<Record<string, number>>({});
const now = ref(Date.now());
const cooldownSeconds = computed(() =>
  Math.max(
    0,
    Math.ceil(
      ((cooldowns.value[normalizedEmail.value] || 0) - now.value) / 1000,
    ),
  ),
);
const busy = computed(() => loading.value || sending.value);
const registrationAvailable = computed(
  () => !configLoading.value && config.value?.registrationEnabled === true,
);
let countdownTimer: ReturnType<typeof setInterval> | undefined;
const demoAccounts = [
  { title: "管理员", email: "admin@demo.local", password: "DemoAdmin!2026" },
  { title: "控制组管理员", email: "mentor@demo.local", password: "DemoMentor!2026" },
  { title: "新生", email: "student@demo.local", password: "DemoStudent!2026" },
  {
    title: "招新组管理员",
    email: "interviewer@demo.local",
    password: "DemoInterview!2026",
  },
];
watch(email, () => {
  verificationCode.value = "";
  sentMessage.value = "";
  error.value = "";
});
async function loadConfig() {
  configLoading.value = true;
  configError.value = "";
  try {
    config.value = await api.get<PublicConfig>("/public/config");
    document.title = `登录 · ${config.value.brandName || "QUT机器人战队"}`;
  } catch (e) {
    configError.value = (e as Error).message;
  } finally {
    configLoading.value = false;
  }
}
onMounted(() => {
  void loadConfig();
  countdownTimer = setInterval(() => (now.value = Date.now()), 1000);
});
onUnmounted(() => clearInterval(countdownTimer));
function selectMode(isRegister: boolean) {
  if (busy.value) return;
  register.value = isRegister;
  error.value = "";
}
function startCooldown(address: string, seconds: number) {
  now.value = Date.now();
  cooldowns.value[address] = now.value + Math.max(1, seconds) * 1000;
}
async function sendVerificationCode() {
  if (busy.value || !registrationAvailable.value || cooldownSeconds.value > 0)
    return;
  if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(normalizedEmail.value)) {
    error.value = "请先填写有效的邮箱地址。";
    return;
  }
  const address = normalizedEmail.value;
  sending.value = true;
  error.value = "";
  sentMessage.value = "";
  try {
    const result = await api.post<{
      message: string;
      cooldownSeconds: number;
      expiresInSeconds: number;
    }>("/auth/send-verification-code", { email: address });
    startCooldown(address, result.cooldownSeconds || 60);
    verificationCode.value = "";
    sentMessage.value = result.message.includes(address)
      ? result.message
      : `${result.message} 收件邮箱：${address}。`;
  } catch (e) {
    error.value = (e as Error).message;
    if (e instanceof ApiError && e.status === 429)
      startCooldown(address, e.retryAfterSeconds || 60);
  } finally {
    sending.value = false;
  }
}
async function submit() {
  if (busy.value) return;
  if (register.value && !registrationAvailable.value) {
    error.value = "当前暂不能注册，请稍后重试或联系战队管理员。";
    return;
  }
  if (!email.value.trim() || !password.value) {
    error.value = "请完整填写账号信息。";
    return;
  }
  if (register.value) {
    if (!/^\d{6}$/.test(verificationCode.value.trim())) {
      error.value = "请输入邮件中的 6 位验证码。";
      return;
    }
    const passwordBytes = new TextEncoder().encode(password.value).length;
    if (passwordBytes < 10) {
      error.value = "密码太短，请使用至少 10 位字母、数字或符号。";
      return;
    }
    if (passwordBytes > 72) {
      error.value = "密码过长，请缩短后重试。";
      return;
    }
    if (password.value !== confirmPassword.value) {
      error.value = "两次输入的密码不一致。";
      return;
    }
  }
  error.value = "";
  loading.value = true;
  try {
    const result = await api.post<{ user: User }>(
      register.value ? "/auth/register" : "/auth/login",
      {
        email: normalizedEmail.value,
        password: password.value,
        ...(register.value
          ? {
              verificationCode: verificationCode.value.trim(),
            }
          : {}),
      },
    );
    auth.user = result.user;
    auth.demoMode = config.value?.demoMode || false;
    auth.error = "";
    const dest =
      typeof route.query.redirect === "string" &&
      route.query.redirect.startsWith("/") &&
      !route.query.redirect.startsWith("//") &&
      !route.query.redirect.startsWith("/login")
        ? route.query.redirect
        : "/";
    await router.push(dest);
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}
</script>
<template>
  <div class="login-page">
    <header class="login-topbar">
      <a href="/" class="login-brand"
        ><img :src="config?.logoUrl || '/mark.png'" width="34" height="34" alt="M" @error="($event.target as HTMLImageElement).src = '/mark.png'" /><span
          >{{ config?.brandName || "QUT机器人战队" }}</span
        ></a
      ><span class="login-season">{{ config?.season || "机器人战队" }}</span>
    </header>
    <main class="login-main">
      <div class="login-heading">
        <div class="team-avatar">
          <img :src="config?.logoUrl || '/mark.png'" width="52" height="52" alt="M" @error="($event.target as HTMLImageElement).src = '/mark.png'" />
        </div>
        <h1>{{ config?.organization || "青岛理工大学机器人战队" }}</h1>
        <p class="team-name">{{ config?.teamName || "沧溟" }} <span>/</span> {{ config?.secondaryTeamName || "MOVE" }}</p>
        <p>同一支战队，奔赴 {{ competitions.join(" 与 ") }}。</p>
      </div>
      <v-card class="login-card">
        <div class="login-tabs" role="tablist" aria-label="登录或注册">
          <button
            type="button"
            role="tab"
            :aria-selected="!register"
            :class="{ selected: !register }"
            :disabled="busy"
            @click="selectMode(false)"
          >
            <v-icon size="19">mdi-login</v-icon>账号登录
          </button>
          <button
            type="button"
            role="tab"
            :aria-selected="register"
            :class="{ selected: register }"
            :disabled="busy"
            @click="selectMode(true)"
          >
            <v-icon size="19">mdi-account-plus-outline</v-icon>注册账号
          </button>
        </div>
        <div class="login-form">
          <h2>{{ register ? "创建你的账号" : "登录协作台" }}</h2>
          <p class="form-description">
            {{
              register
                ? "使用邮箱与密码注册，验证邮箱后即可登录。在个人中心补全资料后即可报名。"
                : "使用邮箱与密码登录，继续处理战队事务。"
            }}
          </p>
          <v-alert
            v-if="route.query.expired"
            type="info"
            variant="tonal"
            class="mb-4"
            density="compact"
            >登录已过期，请重新登录。</v-alert
          >
          <v-alert
            v-if="register && configError"
            type="warning"
            variant="tonal"
            class="mb-4"
            density="compact"
            >无法获取注册服务状态，请重试。已有账号可以正常登录。
            <v-btn
              type="button"
              variant="text"
              size="small"
              :loading="configLoading"
              @click="loadConfig"
              >重试</v-btn
            ></v-alert
          >
          <v-alert
            v-else-if="register && !configLoading && !registrationAvailable"
            type="info"
            variant="tonal"
            class="mb-4"
            density="compact"
            >邮箱验证服务暂未开放，请联系战队管理员。已有账号可以正常登录。</v-alert
          >
          <v-progress-linear
            v-if="register && configLoading"
            indeterminate
            color="primary"
            class="mb-4"
            aria-label="正在获取注册服务状态"
          />
          <v-alert
            v-if="error"
            type="error"
            variant="tonal"
            class="mb-4"
            density="compact"
            >{{ error }}</v-alert
          >
          <v-alert
            v-if="register && sentMessage"
            type="success"
            variant="tonal"
            class="mb-4"
            density="compact"
            >{{ sentMessage }}</v-alert
          >
          <form @submit.prevent="submit">
            <label for="login-email">邮箱</label
            ><v-text-field
              id="login-email"
              v-model="email"
              type="email"
              placeholder="you@example.com"
              autocomplete="username"
              prepend-inner-icon="mdi-email-outline"
              :disabled="busy"
              maxlength="254"
              hide-details
              class="mb-5"
              required
            />
            <template v-if="register">
              <label for="login-code">邮箱验证码</label>
              <div class="verification-row">
                <v-text-field
                  id="login-code"
                  v-model="verificationCode"
                  placeholder="6 位数字验证码"
                  autocomplete="one-time-code"
                  inputmode="numeric"
                  pattern="[0-9]{6}"
                  maxlength="6"
                  :disabled="busy"
                  hide-details
                  required
                />
                <v-btn
                  type="button"
                  color="primary"
                  variant="tonal"
                  :loading="sending"
                  :disabled="
                    loading || !registrationAvailable || cooldownSeconds > 0
                  "
                  @click="sendVerificationCode"
                  >{{
                    cooldownSeconds > 0
                      ? `${cooldownSeconds} 秒后重发`
                      : "发送验证码"
                  }}</v-btn
                >
              </div>
              <p class="verification-hint">
                验证码 10 分钟内有效，请留意垃圾邮件箱。
              </p>
            </template>
            <label for="login-password">密码</label
            ><v-text-field
              id="login-password"
              v-model="password"
              :type="showPassword ? 'text' : 'password'"
              :autocomplete="register ? 'new-password' : 'current-password'"
              :placeholder="
                register ? '建议至少 10 位字母、数字或符号' : '请输入密码'
              "
              :append-inner-icon="
                showPassword ? 'mdi-eye-off-outline' : 'mdi-eye-outline'
              "
              @click:append-inner="showPassword = !showPassword"
              prepend-inner-icon="mdi-lock-outline"
              :disabled="busy"
              :hint="register ? '建议组合使用字母、数字和符号。' : undefined"
              :persistent-hint="register"
              :hide-details="!register"
              class="mb-6"
              required
            />
            <template v-if="register">
              <label for="login-confirm-password">确认密码</label>
              <v-text-field
                id="login-confirm-password"
                v-model="confirmPassword"
                :type="showPassword ? 'text' : 'password'"
                autocomplete="new-password"
                placeholder="再次输入密码"
                prepend-inner-icon="mdi-lock-check-outline"
                :disabled="busy"
                hide-details
                class="mb-6"
                required
              />
            </template>
            <v-btn
              type="submit"
              color="primary"
              block
              height="48"
              :loading="loading"
              :disabled="sending || (register && !registrationAvailable)"
              >{{ register ? "验证邮箱并注册" : "登录协作台"
              }}<v-icon end size="18">mdi-arrow-right</v-icon></v-btn
            >
          </form>
          <div v-if="config?.demoMode" class="demo-box">
            <div>
              <v-icon size="16">mdi-flask-outline</v-icon> 本地演示空间
              <span>虚构数据</span>
            </div>
            <p>选择身份填入体验账号，再点击登录。</p>
            <div class="d-flex flex-wrap ga-2">
              <v-btn
                v-for="a in demoAccounts"
                :key="a.email"
                size="small"
                variant="outlined"
                :disabled="busy"
                @click="
                  email = a.email;
                  password = a.password;
                  register = false;
                  error = '';
                "
                >{{ a.title }}</v-btn
              >
            </div>
          </div>
        </div>
      </v-card>
      <footer class="login-footer">
        <template v-for="(competition, index) in competitions" :key="competition">
          <i v-if="index" /><span>{{ competition }}</span>
        </template>
        <p>{{ config?.brandName || "QUT机器人战队" }} · 战队协作平台</p>
      </footer>
    </main>
  </div>
</template>
<style scoped>
.login-page {
  min-height: 100vh;
  background: #f6f6f6;
}
.login-topbar {
  height: 64px;
  padding: 0 32px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  background: linear-gradient(
    110deg,
    #167d87,
    #2854ad 33%,
    #4630b3 65%,
    #8426b7
  );
  color: white;
  box-shadow: 0 2px 5px #0002;
}
.login-brand {
  display: flex;
  align-items: center;
  gap: 12px;
  font-size: 19px;
  color: white;
  text-decoration: none;
}
.login-brand img {
  object-fit: contain;
  background: white;
  border-radius: 6px;
  padding: 3px;
}
.login-season {
  font-size: 13px;
  color: #ffffffc4;
}
.login-main {
  max-width: 510px;
  margin: 0 auto;
  padding: 32px 16px 22px;
}
.login-heading {
  text-align: center;
  margin-bottom: 22px;
}
.team-avatar {
  width: 64px;
  height: 64px;
  background: #fff;
  color: #f42168;
  border: 1px solid #ece5ed;
  box-shadow: 0 3px 10px #e7d8e74d;
  border-radius: 50%;
  display: grid;
  place-items: center;
  margin: 0 auto 14px;
}
.login-heading h1 {
  font-size: 21px;
  font-weight: 500;
  margin: 0 0 6px;
}
.login-heading .team-name {
  font-size: 16px;
  font-weight: 500;
  color: #565060;
  letter-spacing: 0;
  margin: 8px 0 7px;
}
.team-name span { color: #c0b5c5; margin: 0 6px; }
.login-heading p {
  font-size: 13px;
  color: #757575;
  margin: 0;
}
.login-card {
  background: white;
}
.login-tabs {
  display: flex;
  height: 53px;
  border-bottom: 1px solid #eee;
}
.login-tabs button {
  flex: 1;
  font-size: 14px;
  color: #757575;
  border-bottom: 3px solid transparent;
  display: flex;
  gap: 8px;
  justify-content: center;
  align-items: center;
}
.login-tabs button.selected {
  color: #1976d2;
  border-bottom-color: #1976d2;
  background: #f5faff;
}
.login-tabs button:disabled {
  opacity: 0.6;
  cursor: wait;
}
.login-form {
  padding: 26px 32px;
}
.login-form h2 {
  font-size: 20px;
  font-weight: 500;
  margin-bottom: 7px;
}
.form-description {
  color: #757575;
  font-size: 13px;
  margin-bottom: 22px;
  line-height: 1.7;
}
.login-form label {
  display: block;
  font-size: 13px;
  color: #616161;
  margin-bottom: 1px;
}
.login-form :deep(.v-alert__content) {
  overflow-wrap: anywhere;
}
.verification-row {
  display: flex;
  align-items: center;
  gap: 12px;
}
.verification-row :deep(.v-input) {
  min-width: 0;
}
.verification-row :deep(.v-btn) {
  flex-shrink: 0;
  min-width: 112px;
}
.verification-hint {
  margin: 7px 0 20px;
  color: #757575;
  font-size: 12px;
  line-height: 1.6;
}
.demo-box {
  border-top: 1px solid #eee;
  margin-top: 24px;
  padding-top: 18px;
}
.demo-box > div:first-child {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: #616161;
}
.demo-box span {
  margin-left: auto;
  font-size: 11px;
  color: #9e9e9e;
}
.demo-box p {
  color: #757575;
  font-size: 12px;
  margin: 8px 0 12px;
}
.login-footer {
  text-align: center;
  padding-top: 23px;
  color: #757575;
  font-size: 12px;
}
.login-footer i {
  display: inline-block;
  height: 10px;
  width: 1px;
  background: #bbb;
  margin: 0 13px;
}
.login-footer p {
  font-size: 11px;
  color: #9e9e9e;
  margin-top: 9px;
}
@media (max-width: 600px) {
  .login-topbar {
    padding: 0 17px;
    height: 58px;
  }
  .login-brand {
    font-size: 16px;
    gap: 9px;
  }
  .login-season {
    font-size: 11px;
  }
  .login-main {
    padding-top: 24px;
  }
  .login-form {
    padding: 23px;
  }
}
</style>
