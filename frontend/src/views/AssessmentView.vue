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
  <div class="page-content">
    <h1 class="page-title">在线测评<span class="text-secondary">.</span></h1>
    <p class="page-subtitle">连接题库与实践，积累每一次进步。</p>
    <v-alert v-if="error" type="error" variant="tonal" class="mb-5"
      >{{ error }}<v-btn variant="text" @click="load">重试</v-btn></v-alert
    ><v-progress-linear v-if="loading" color="primary" indeterminate /><v-card
      v-else
      class="hydro-card"
      ><div class="hydro-illustration" aria-hidden="true">
        <div class="code-icon">&lt;/&gt;</div>
        <span>LEARN · CODE · VERIFY</span>
      </div>
      <div class="hydro-content">
        <div class="eyebrow">POWERED BY YOUR HYDRO INSTANCE</div>
        <h2>把想法写成代码，<br />让结果验证思考。</h2>
        <p>
          编程题和自动判题由独立的 Hydro
          平台提供。机械、电控实机等任务，在「学习与任务」中提交作品并由导师验收。
        </p>
        <template v-if="settings?.hydroUrl"
          ><v-chip color="success" variant="tonal" class="mb-4"
            >已配置测评入口</v-chip
          >
          <div>
            <v-btn
              :href="settings.hydroUrl"
              target="_blank"
              rel="noopener noreferrer"
              color="primary"
              append-icon="mdi-open-in-new"
              >打开 Hydro 测评平台</v-btn
            >
          </div>
          <p class="integration-note">
            将在新页面打开，使用 Hydro 自己的账号登录；本系统暂不自动同步成绩。
          </p></template
        ><template v-else
          ><v-alert type="info" variant="tonal" density="compact" class="mb-4"
            >战队尚未配置 Hydro 站点。培养任务仍可正常使用。</v-alert
          ><v-btn
            v-if="auth.user?.role === 'admin'"
            to="/settings"
            color="primary"
            append-icon="mdi-arrow-right"
            >配置测评入口</v-btn
          ><v-btn
            v-else
            to="/learning"
            color="primary"
            append-icon="mdi-arrow-right"
            >查看培养任务</v-btn
          ></template
        >
      </div></v-card
    >
    <div class="assessment-grid">
      <v-card
        ><v-icon color="primary" size="26">mdi-code-json</v-icon>
        <h3>编程与算法</h3>
        <p>
          基础编程、数据结构、算法与测试用例。通过自动判题获得反馈。
        </p></v-card
      ><v-card
        ><v-icon color="secondary" size="26">mdi-robot-outline</v-icon>
        <h3>工程与实机</h3>
        <p>
          图纸、固件、实验报告和实机视频。结合评分标准与导师反馈成长。
        </p></v-card
      ><v-card
        ><v-icon color="warning" size="26">mdi-book-check-outline</v-icon>
        <h3>规则与知识</h3>
        <p>
          阅读对应赛事与赛季的正式规则。以有来源、有版本的资料为依据。
        </p></v-card
      >
    </div>
  </div>
</template>
<style scoped>
.hydro-card {
  display: grid;
  grid-template-columns: 1fr 1.5fr;
  overflow: hidden;
}
.hydro-illustration {
  background: linear-gradient(140deg, #e8edf9, #eceafa);
  display: flex;
  align-items: center;
  justify-content: center;
  flex-direction: column;
  gap: 32px;
  min-height: 340px;
  background-image:
    radial-gradient(#bac5e244 1px, transparent 1px),
    linear-gradient(140deg, #e8edf9, #eceafa);
  background-size:
    20px 20px,
    100% 100%;
}
.code-icon {
  width: 130px;
  height: 130px;
  display: grid;
  place-items: center;
  border-radius: 30px;
  background: #ffffffaa;
  box-shadow: 0 15px 35px #647da516;
  color: #7b8bca;
  font-size: 50px;
  font-family: monospace;
  transform: rotate(-7deg);
  border: 1px solid white;
}
.hydro-illustration > span {
  font-size: 9px;
  letter-spacing: 2px;
  color: #9aa7c6;
}
.hydro-content {
  padding: 40px;
}
.hydro-content .eyebrow {
  font-size: 8px;
  letter-spacing: 1px;
  color: #acb3c5;
}
.hydro-content h2 {
  font-size: 27px;
  line-height: 1.6;
  margin: 16px 0;
}
.hydro-content p {
  font-size: 13px;
  line-height: 1.9;
  color: #8e9ab0;
}
.hydro-content .integration-note {
  font-size: 10px;
  margin: 14px 0 0;
}
.assessment-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 20px;
  margin-top: 24px;
}
.assessment-grid > .v-card {
  padding: 25px;
}
.assessment-grid h3 {
  font-size: 15px;
  margin: 17px 0 12px;
}
.assessment-grid p {
  font-size: 12px;
  color: #95a0b4;
  line-height: 1.9;
  margin: 0;
}
@media (max-width: 960px) {
  .hydro-content {
    padding: 28px;
  }
  .hydro-card {
    grid-template-columns: 0.8fr 1.2fr;
  }
  .hydro-content h2 {
    font-size: 22px;
  }
}
@media (max-width: 600px) {
  .hydro-card {
    grid-template-columns: 1fr;
  }
  .hydro-illustration {
    min-height: 180px;
    gap: 15px;
  }
  .code-icon {
    width: 80px;
    height: 80px;
    font-size: 32px;
    border-radius: 20px;
  }
  .assessment-grid {
    grid-template-columns: 1fr;
  }
  .hydro-content {
    padding: 24px;
  }
}
</style>
