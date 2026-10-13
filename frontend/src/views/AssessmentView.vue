<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { api } from "../lib/api";
import { hasPermission } from "../lib/permissions";
import type { Settings, Task } from "../lib/types";
import { dateTime, type JudgeService } from "../lib/platform";
const settings = ref<Settings | null>(null),
  service = ref<JudgeService | null>(null),
  tasks = ref<Task[]>([]),
  loading = ref(true),
  error = ref("");
const exams = computed(() =>
  tasks.value.filter((t) => t.judge?.enabled || t.kind === "quiz"),
);
async function load() {
  loading.value = true;
  error.value = "";
  try {
    const [s, j, t] = await Promise.all([
      api.get<{ settings: Settings }>("/settings"),
      api.get<JudgeService>("/judge/config"),
      api.get<{ items: Task[] }>("/tasks"),
    ]);
    settings.value = s.settings;
    service.value = j;
    tasks.value = t.items;
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}
const state = (task: Task) =>
  task.startsAt && Date.parse(task.startsAt) > Date.now()
    ? "未开始"
    : task.dueAt && Date.parse(task.dueAt) < Date.now()
      ? "已截止"
      : "进行中";
onMounted(load);
</script>
<template>
  <div class="page-content assessment-page">
    <div class="assessment-heading">
      <div>
        <h1 class="page-title">在线测评与考试</h1>
        <p class="page-subtitle">
          查看题目、上传源码，在线编译并运行测试用例。
        </p>
      </div>
      <v-btn
        v-if="hasPermission('tasks')"
        to="/learning"
        color="primary"
        >发布任务与考试</v-btn
      >
    </div>
    <v-alert v-if="error" type="error" variant="tonal" class="mb-4"
      >{{ error }}<v-btn variant="text" @click="load">重试</v-btn></v-alert
    ><v-progress-linear v-if="loading" indeterminate /><template v-else
      ><v-alert
        :type="service?.configured ? 'success' : 'info'"
        variant="tonal"
        class="mb-5"
        >{{
          service?.configured
            ? "代码编译服务已连接，可以在支持自动评测的任务中运行代码。"
            : "代码编译服务尚未配置。作业上传和管理员人工评分仍可使用。"
        }}</v-alert
      >
      <div v-if="exams.length" class="assessment-grid">
        <v-card v-for="task in exams" :key="task.id" class="exam-card"
          ><div class="d-flex align-center ga-2 mb-3">
            <v-icon color="primary">mdi-code-braces</v-icon
            ><v-chip size="small">{{
              task.kind === "quiz" ? "考试测验" : "代码练习"
            }}</v-chip
            ><v-chip
              :color="state(task) === '进行中' ? 'success' : 'grey'"
              size="small"
              class="ml-auto"
              >{{ state(task) }}</v-chip
            >
          </div>
          <h2>{{ task.title }}</h2>
          <p>{{ task.description }}</p>
          <div class="exam-meta">
            <span
              >{{ task.direction || "所有方向" }} · 满分
              {{ task.maxScore }}</span
            ><span v-if="task.startsAt"
              >开始：{{ dateTime(task.startsAt) }}</span
            ><span v-if="task.dueAt">截止：{{ dateTime(task.dueAt) }}</span
            ><span v-if="task.judge?.enabled"
              >{{
                task.judge.totalCases || task.judge.testCases.length
              }}
              个测试用例</span
            >
          </div>
          <v-btn
            :to="{ path: '/learning', query: { task: task.id } }"
            color="primary"
            variant="text"
            append-icon="mdi-arrow-right"
            class="mt-4 align-self-end"
            >查看题目与提交</v-btn
          ></v-card
        >
      </div>
      <v-card v-else class="pa-10 text-center text-medium-emphasis mb-5"
        >暂时没有代码测评或考试。管理员发布后会显示在这里。</v-card
      ><v-card v-if="settings?.hydroUrl" class="pa-5 mt-6"
        ><div class="d-flex align-center flex-wrap ga-4">
          <v-icon size="32" color="primary">mdi-open-in-new</v-icon>
          <div>
            <h2 class="text-subtitle-1">Hydro 外部题库</h2>
            <p class="text-caption text-medium-emphasis">
              在独立平台登录，成绩暂不自动同步。
            </p>
          </div>
          <v-spacer /><v-btn
            :href="settings.hydroUrl"
            target="_blank"
            rel="noopener noreferrer"
            color="primary"
            variant="tonal"
            >打开外部题库</v-btn
          >
        </div></v-card
      ></template
    >
  </div>
</template>
<style scoped>
.assessment-page {
  max-width: 1300px;
}
.assessment-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}
.assessment-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 18px;
}
.exam-card {
  padding: 22px;
  display: flex;
  flex-direction: column;
}
.exam-card h2 {
  font-size: 18px;
  font-weight: 500;
  margin-bottom: 10px;
}
.exam-card > p {
  color: #616161;
  font-size: 14px;
  line-height: 1.8;
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
  overflow-wrap: anywhere;
  margin-bottom: 18px;
}
.exam-meta {
  font-size: 12px;
  color: #757575;
  display: flex;
  flex-direction: column;
  gap: 5px;
  margin-top: auto;
}
@media (max-width: 700px) {
  .assessment-heading {
    flex-wrap: wrap;
    margin-bottom: 18px;
  }
  .assessment-grid {
    grid-template-columns: 1fr;
  }
}
</style>
