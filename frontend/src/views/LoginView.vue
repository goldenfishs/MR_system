<script setup lang="ts">
import { onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { api } from "../lib/api";
import { auth } from "../lib/auth";
import type { PublicConfig, User } from "../lib/types";
const route = useRoute(),
  router = useRouter();
const register = ref(false),
  name = ref(""),
  email = ref(""),
  password = ref(""),
  error = ref(""),
  loading = ref(false),
  showPassword = ref(false);
const config = ref<PublicConfig | null>(null);
const demoAccounts = [
  { title: "管理员", email: "admin@demo.local", password: "DemoAdmin!2026" },
  { title: "导师", email: "mentor@demo.local", password: "DemoMentor!2026" },
  { title: "学员", email: "student@demo.local", password: "DemoStudent!2026" },
  {
    title: "面试官",
    email: "interviewer@demo.local",
    password: "DemoInterview!2026",
  },
];
onMounted(async () => {
  try {
    config.value = await api.get<PublicConfig>("/public/config");
  } catch (e) {
    error.value = (e as Error).message;
  }
});
async function submit() {
  if (
    !email.value.trim() ||
    !password.value ||
    (register.value && !name.value.trim())
  ) {
    error.value = "请完整填写账号信息。";
    return;
  }
  error.value = "";
  loading.value = true;
  try {
    const result = await api.post<{ user: User }>(
      register.value ? "/auth/register" : "/auth/login",
      {
        email: email.value.trim(),
        password: password.value,
        ...(register.value ? { name: name.value.trim() } : {}),
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
        ><img src="/mark.svg" width="32" height="32" alt="竞跃" /><span
          >竞跃 · 战队协作系统</span
        ></a
      ><span class="login-season">{{ config?.season || "机器人战队" }}</span>
    </header>
    <main class="login-main">
      <div class="login-heading">
        <div class="team-avatar">
          <v-icon size="38">mdi-account-group</v-icon>
        </div>
        <h1>{{ config?.organization || "机器人实验室" }}</h1>
        <p>招新、学习与协作，从这里开始。</p>
      </div>
      <v-card class="login-card">
        <div class="login-tabs" role="tablist" aria-label="登录或注册">
          <button
            role="tab"
            :aria-selected="!register"
            :class="{ selected: !register }"
            @click="
              register = false;
              error = '';
            "
          >
            <v-icon size="19">mdi-login</v-icon>账号登录
          </button>
          <button
            role="tab"
            :aria-selected="register"
            :class="{ selected: register }"
            @click="
              register = true;
              error = '';
            "
          >
            <v-icon size="19">mdi-account-plus-outline</v-icon>注册报名
          </button>
        </div>
        <div class="login-form">
          <h2>{{ register ? "创建你的报名账号" : "登录协作台" }}</h2>
          <p class="form-description">
            {{
              register
                ? "注册后填写个人资料，查看报名和面试进度。"
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
            v-if="error"
            type="error"
            variant="tonal"
            class="mb-4"
            density="compact"
            >{{ error }}</v-alert
          >
          <form @submit.prevent="submit">
            <label v-if="register" for="login-name">姓名</label
            ><v-text-field
              v-if="register"
              id="login-name"
              v-model="name"
              placeholder="请输入你的姓名"
              autocomplete="name"
              maxlength="60"
              hide-details
              class="mb-5"
              required
            />
            <label for="login-email">邮箱</label
            ><v-text-field
              id="login-email"
              v-model="email"
              type="email"
              placeholder="you@example.com"
              autocomplete="username"
              prepend-inner-icon="mdi-email-outline"
              hide-details
              class="mb-5"
              required
            />
            <label for="login-password">密码</label
            ><v-text-field
              id="login-password"
              v-model="password"
              :type="showPassword ? 'text' : 'password'"
              :autocomplete="register ? 'new-password' : 'current-password'"
              :placeholder="register ? '至少 10 位字符' : '请输入密码'"
              :append-inner-icon="
                showPassword ? 'mdi-eye-off-outline' : 'mdi-eye-outline'
              "
              @click:append-inner="showPassword = !showPassword"
              prepend-inner-icon="mdi-lock-outline"
              hide-details
              class="mb-6"
              required
            />
            <v-btn
              type="submit"
              color="primary"
              block
              height="48"
              :loading="loading"
              >{{ register ? "创建账号" : "登录协作台"
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
        <span>RoboMaster</span><i /> <span>ROBOCON</span>
        <p>竞跃 MR System · 开源战队协作平台</p>
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
  border: 1px solid #ffffff55;
  border-radius: 50%;
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
  background: #e3f2fd;
  color: #1976d2;
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
