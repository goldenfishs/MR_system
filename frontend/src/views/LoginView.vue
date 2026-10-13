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
  <div class="login-layout">
    <section class="login-story">
      <a href="/" class="login-brand"
        ><img src="/mark.svg" width="42" height="42" alt="竞跃" />
        <div>竞跃<span>MR SYSTEM / 战队协作台</span></div></a
      >
      <div class="story-copy">
        <div class="eyebrow">BUILD. LEARN. TOGETHER.</div>
        <h1>让热爱集结，<br />让想法<span>真正落地。</span></h1>
        <p>
          从第一次报名，到第一次让机器人动起来。<br />在这里，记录每一份努力，也连接每一位伙伴。
        </p>
        <div class="story-tags">
          <span>ROBOCON</span><i /> <span>RoboMaster</span>
        </div>
      </div>
      <div class="blueprint" aria-hidden="true">
        <div class="orbit o1" />
        <div class="orbit o2" />
        <div class="orbit o3" />
        <div class="blueprint-core">
          <v-icon size="80">mdi-robot-outline</v-icon>
        </div>
        <span class="coord c1">IDEA → PROTOTYPE</span
        ><span class="coord c2">01 / COLLABORATE</span>
      </div>
      <div class="story-footer">
        为机器人战队打造的开放协作空间 <span>MR SYSTEM · OPEN SOURCE</span>
      </div>
    </section>
    <section class="login-form-area">
      <div class="login-form">
        <div class="eyebrow muted mb-3">WELCOME TO YOUR TEAM</div>
        <h2>{{ register ? "从这里加入战队" : "欢迎回到协作台" }}</h2>
        <p class="form-description">
          {{
            register
              ? "注册后即可填写报名资料，开启你的机器人之旅。"
              : "登录账号，继续今天的探索与创造。"
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
        <p class="switch-form">
          {{ register ? "已经有账号？" : "还没有账号？"
          }}<button
            @click="
              register = !register;
              error = '';
            "
          >
            {{ register ? "返回登录" : "注册报名" }}
          </button>
        </p>
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
        <div class="form-footer">
          {{ config?.organization || "机器人实验室" }} <span>·</span>
          {{ config?.season || "MR System" }}
        </div>
      </div>
    </section>
  </div>
</template>
<style scoped>
.login-layout {
  min-height: 100vh;
  display: grid;
  grid-template-columns: 1.1fr 1fr;
}
.login-story {
  color: white;
  padding: 45px 55px;
  position: relative;
  overflow: hidden;
  background: linear-gradient(140deg, #263b65 0%, #3a4d99 50%, #7163aa 100%);
}
.login-brand {
  display: flex;
  align-items: center;
  gap: 12px;
  text-decoration: none;
  color: white;
  font-weight: 700;
  font-size: 23px;
}
.login-brand span {
  display: block;
  font-size: 9px;
  letter-spacing: 1.3px;
  font-weight: 400;
  opacity: 0.6;
}
.story-copy {
  position: relative;
  z-index: 1;
  margin-top: 100px;
}
.story-copy > .eyebrow {
  color: #a7c5de;
  font-size: 10px;
}
.story-copy h1 {
  font-size: 44px;
  line-height: 1.5;
  margin: 22px 0;
  letter-spacing: 1px;
}
.story-copy h1 span {
  color: #b0e2d3;
}
.story-copy p {
  font-size: 13px;
  line-height: 2;
  color: #c2cce3;
}
.story-tags {
  display: flex;
  align-items: center;
  gap: 16px;
  letter-spacing: 1.5px;
  font-size: 11px;
  margin-top: 32px;
  color: #d2d8e7;
}
.story-tags i {
  height: 14px;
  width: 1px;
  background: #ffffff40;
}
.story-footer {
  position: absolute;
  bottom: 30px;
  left: 55px;
  font-size: 10px;
  color: #c7cce0;
}
.story-footer span {
  display: block;
  font-size: 8px;
  letter-spacing: 1px;
  margin-top: 7px;
  opacity: 0.5;
}
.blueprint {
  position: absolute;
  right: -80px;
  bottom: 70px;
  width: 390px;
  height: 390px;
  color: #c7ddf3;
  opacity: 0.4;
  transform: rotate(-15deg);
}
.orbit {
  border: 1px solid #cad5ff60;
  border-radius: 50%;
  position: absolute;
  inset: 0;
}
.o2 {
  inset: 40px;
  border-style: dashed;
}
.o3 {
  inset: 90px;
}
.blueprint-core {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
}
.coord {
  position: absolute;
  font-family: monospace;
  font-size: 9px;
  letter-spacing: 2px;
}
.c1 {
  top: 160px;
  left: -40px;
}
.c2 {
  bottom: 30px;
  right: 15px;
}
.login-form-area {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 48px 40px;
  background: white;
}
.login-form {
  width: 100%;
  max-width: 370px;
}
.login-form h2 {
  font-size: 27px;
  letter-spacing: -0.5px;
  margin-bottom: 10px;
}
.form-description {
  font-size: 13px;
  color: #939bae;
  margin-bottom: 35px;
  line-height: 1.8;
}
.login-form label {
  display: block;
  font-size: 12px;
  margin-bottom: 9px;
  font-weight: 600;
}
.switch-form {
  text-align: center;
  font-size: 12px;
  color: #98a0af;
  margin: 22px 0 30px;
}
.switch-form button {
  color: #5364b4;
  margin-left: 5px;
  font-weight: 600;
}
.demo-box {
  padding: 16px;
  border: 1px dashed #dce2ed;
  background: #f8f9fc;
  border-radius: 12px;
}
.demo-box > div:first-child {
  font-size: 11px;
  font-weight: 600;
  color: #65718a;
  display: flex;
  align-items: center;
  gap: 5px;
}
.demo-box span {
  margin-left: auto;
  font-size: 9px;
  color: #a1a9b8;
  font-weight: 400;
}
.demo-box p {
  font-size: 11px;
  color: #939daf;
  margin: 9px 0 12px;
}
.form-footer {
  text-align: center;
  font-size: 10px;
  color: #a2aabb;
  margin-top: 34px;
}
.form-footer span {
  margin: 0 9px;
}
@media (max-width: 960px) {
  .login-story {
    padding: 35px;
  }
  .story-copy h1 {
    font-size: 34px;
  }
  .story-footer {
    left: 35px;
  }
  .blueprint {
    right: -190px;
  }
}
@media (max-width: 700px) {
  .login-layout {
    grid-template-columns: 1fr;
  }
  .login-story {
    padding: 24px;
    min-height: 180px;
  }
  .story-copy {
    margin-top: 22px;
  }
  .story-copy h1 {
    font-size: 25px;
    margin: 10px 0;
  }
  .story-copy h1 br {
    display: none;
  }
  .story-copy p,
  .story-tags,
  .story-footer,
  .story-copy > .eyebrow {
    display: none;
  }
  .blueprint {
    bottom: -170px;
    right: -90px;
  }
  .login-form-area {
    padding: 35px 24px;
  }
  .login-brand {
    font-size: 19px;
  }
}
</style>
