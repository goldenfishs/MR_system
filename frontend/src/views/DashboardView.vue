<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import { api } from "../lib/api";
import { auth } from "../lib/auth";
import { dateTime, relativeTime } from "../lib/format";
import type { Dashboard, Settings, Task } from "../lib/types";
const router = useRouter();
const data = ref<Dashboard | null>(null),
  settings = ref<Settings | null>(null),
  tasks = ref<Task[]>([]),
  loading = ref(true),
  error = ref("");
const student = computed(() => auth.user?.role === "student");
const today = new Intl.DateTimeFormat("zh-CN", {
  month: "long",
  day: "numeric",
  weekday: "long",
}).format(new Date());
const stats = computed(() => [
  {
    label: student.value ? "我的报名" : "本期报名",
    value: data.value?.stats.applicants ?? 0,
    icon: "mdi-account-multiple-outline",
    detail: student.value ? "查看报名与面试进度" : "每一份热爱，值得被看见",
    color: "#626fc4",
    bg: "#eef0fb",
    path: "/recruitment",
  },
  {
    label: student.value ? "等待反馈" : "待审核提交",
    value: data.value?.stats.pendingReviews ?? 0,
    icon: "mdi-clipboard-check-outline",
    detail: "及时反馈，让成长更进一步",
    color: "#c19358",
    bg: "#fbf3e9",
    path: "/learning",
  },
  {
    label: "进行中任务",
    value: data.value?.stats.activeTasks ?? 0,
    icon: "mdi-folder-open-outline",
    detail: "把理论知识变成实践能力",
    color: "#529f8b",
    bg: "#eaf6f1",
    path: "/learning",
  },
  {
    label: student.value ? "个人成员状态" : "战队成员",
    value: data.value?.stats.members ?? 0,
    icon: "mdi-account-heart-outline",
    detail: student.value ? "录取后开启培养任务" : "汇聚专长，共同创造",
    color: "#8d7cbc",
    bg: "#f2eef9",
    path: student.value ? "/recruitment" : "/members",
  },
]);
const total = computed(
  () => data.value?.pipeline.reduce((sum, item) => sum + item.count, 0) || 0,
);
const stageColors: Record<string, string> = {
  submitted: "#9ba7dd",
  screening: "#7484cd",
  interview: "#c6a16c",
  accepted: "#6fb9a4",
  rejected: "#c8cedb",
};
const kindIcons: Record<string, string> = {
  code: "mdi-code-braces",
  document: "mdi-file-document-outline",
  hardware: "mdi-cube-outline",
  quiz: "mdi-text-box-check-outline",
};
const kindLabels: Record<string, string> = {
  code: "编程实践",
  document: "文档作业",
  hardware: "实机任务",
  quiz: "知识测评",
};
async function load() {
  loading.value = true;
  error.value = "";
  try {
    const [a, b, c] = await Promise.all([
      api.get<Dashboard>("/dashboard"),
      api.get<{ settings: Settings }>("/settings"),
      api.get<{ items: Task[] }>("/tasks"),
    ]);
    data.value = a;
    settings.value = b.settings;
    tasks.value = (c.items || []).slice(0, 3);
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}
onMounted(load);
</script>
<template>
  <div class="page-content dashboard">
    <div class="page-heading">
      <div>
        <h1 class="page-title">工作台<span class="heading-dot">.</span></h1>
        <p class="page-subtitle">从一个想法，到一台真正的机器人。</p>
      </div>
      <div class="date-label">
        <v-icon size="15">mdi-calendar-blank-outline</v-icon>{{ today
        }}<v-btn
          icon="mdi-refresh"
          size="x-small"
          variant="text"
          aria-label="刷新工作台"
          :loading="loading"
          @click="load"
        />
      </div>
    </div>
    <v-alert v-if="error" type="error" variant="tonal" class="mb-5"
      ><div class="d-flex align-center justify-space-between">
        {{ error }}<v-btn variant="text" @click="load">重试</v-btn>
      </div></v-alert
    >
    <section class="welcome-banner">
      <div class="banner-content">
        <div class="banner-label">
          <span /> {{ settings?.season || "新赛季" }} <i /> TEAM WORKSPACE
        </div>
        <h2>欢迎回来，{{ auth.user?.name }} <span class="wave">✦</span></h2>
        <p>
          {{
            student
              ? "保持好奇，动手验证。今天也向更好的工程师迈进一步。"
              : "新鲜的想法正在集结，一起把每一份热爱变成成长。"
          }}
        </p>
        <v-btn
          color="white"
          variant="flat"
          size="small"
          :to="student ? '/learning' : '/recruitment'"
          class="banner-action"
          >{{ student ? "查看我的任务" : "进入招新工作台"
          }}<v-icon end size="15">mdi-arrow-right</v-icon></v-btn
        >
      </div>
      <div class="banner-art" aria-hidden="true">
        <div class="ring r1" />
        <div class="ring r2" />
        <div class="ring r3" />
        <div class="robot-tile">
          <v-icon size="55">mdi-robot-outline</v-icon>
        </div>
        <span class="art-mark m1">+</span><span class="art-mark m2">+</span
        ><span class="art-text">ENGINEER THE NEXT</span>
      </div>
    </section>
    <div class="stats-grid">
      <button
        v-for="s in stats"
        :key="s.label"
        class="stat-card"
        @click="router.push(s.path)"
      >
        <div class="stat-label">
          {{ s.label
          }}<span
            class="stat-icon"
            :style="{ color: s.color, background: s.bg }"
            ><v-icon size="21">{{ s.icon }}</v-icon></span
          >
        </div>
        <div class="stat-value">
          {{ loading ? "—" : s.value
          }}<span>{{
            s.label.includes("任务") ||
            s.label.includes("提交") ||
            s.label.includes("反馈")
              ? "项"
              : "人"
          }}</span>
        </div>
        <div class="stat-detail">
          {{ s.detail }}<v-icon size="13">mdi-arrow-top-right</v-icon>
        </div>
      </button>
    </div>
    <div class="main-grid">
      <div class="main-column">
        <v-card class="pipeline-card"
          ><div class="card-heading">
            <div>
              <h2 class="section-title">
                {{ student ? "我的招新进度" : "招新进度" }}
              </h2>
              <span class="section-caption">从初次相遇，到成为并肩的伙伴</span>
            </div>
            <v-btn size="small" variant="text" color="primary" to="/recruitment"
              >查看全部<v-icon end size="14">mdi-arrow-right</v-icon></v-btn
            >
          </div>
          <div class="pipeline-body">
            <div v-if="!total" class="empty-state py-5">
              <v-icon size="30">mdi-account-plus-outline</v-icon>
              <p>还没有报名记录</p>
              <v-btn
                v-if="student"
                variant="text"
                color="primary"
                to="/recruitment"
                >填写第一份报名</v-btn
              >
            </div>
            <template v-else
              ><div class="pipeline-bar">
                <span
                  v-for="stage in data?.pipeline"
                  :key="stage.status"
                  :style="{
                    width: `${(stage.count / total) * 100}%`,
                    background: stageColors[stage.status],
                  }"
                  :title="`${stage.label} ${stage.count} 人`"
                />
              </div>
              <div class="pipeline-stages">
                <button
                  v-for="stage in data?.pipeline"
                  :key="stage.status"
                  @click="
                    router.push({
                      path: '/recruitment',
                      query: { status: stage.status },
                    })
                  "
                >
                  <span
                    ><i :style="{ background: stageColors[stage.status] }" />{{
                      stage.label
                    }}</span
                  ><strong>{{ stage.count }}<small>人</small></strong>
                </button>
              </div></template
            >
          </div></v-card
        >
        <v-card class="tasks-panel"
          ><div class="card-heading">
            <div>
              <h2 class="section-title">
                {{ student ? "开启你的下一项挑战" : "近期培养任务" }}
              </h2>
              <span class="section-caption">LEARNING BY BUILDING</span>
            </div>
            <v-btn size="small" variant="text" color="primary" to="/learning"
              >全部任务<v-icon end size="14">mdi-arrow-right</v-icon></v-btn
            >
          </div>
          <div v-if="!tasks.length && !loading" class="empty-state">
            <v-icon size="32">mdi-book-open-outline</v-icon>
            <p>新任务即将从这里开始</p>
            <v-btn
              v-if="auth.user?.role === 'admin' || auth.user?.role === 'mentor'"
              to="/learning"
              variant="text"
              color="primary"
              >发布培养任务</v-btn
            >
          </div>
          <router-link
            v-for="task in tasks"
            :key="task.id"
            :to="{ path: '/learning', query: { task: task.id } }"
            class="task-row"
            ><div class="task-kind" :class="task.kind">
              <v-icon size="24">{{ kindIcons[task.kind] }}</v-icon>
            </div>
            <div class="task-main">
              <h3>{{ task.title }}</h3>
              <p>
                {{ task.team || "全部赛队" }}<span>·</span
                >{{ task.direction || "通用基础" }}<span>·</span
                >{{ kindLabels[task.kind] }}
              </p>
            </div>
            <div class="task-meta">
              <v-chip
                size="x-small"
                variant="tonal"
                :color="task.difficulty === '入门' ? 'success' : 'primary'"
                >{{ task.difficulty }}</v-chip
              ><small>{{
                task.dueAt ? dateTime(task.dueAt) + " 截止" : "长期开放"
              }}</small>
            </div>
            <v-icon class="task-arrow" size="16"
              >mdi-chevron-right</v-icon
            ></router-link
          ></v-card
        >
        <v-card class="interview-panel"
          ><div class="card-heading">
            <div>
              <h2 class="section-title">近期面试</h2>
              <span class="section-caption">每一次交流，发现更多可能</span>
            </div>
            <v-chip size="x-small" variant="tonal" color="primary"
              >{{ data?.upcomingInterviews.length || 0 }} 场待进行</v-chip
            >
          </div>
          <v-table v-if="data?.upcomingInterviews.length"
            ><thead>
              <tr>
                <th>候选人</th>
                <th>意向方向</th>
                <th>面试时间</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="a in data.upcomingInterviews.slice(0, 4)" :key="a.id">
                <td>
                  <div class="person-name">
                    <span>{{ a.name.slice(-2) }}</span
                    ><strong>{{ a.name }}</strong>
                  </div>
                </td>
                <td>
                  {{ a.direction
                  }}<small class="d-block muted">{{ a.team }}</small>
                </td>
                <td>{{ dateTime(a.interviewAt) }}</td>
                <td>
                  <v-btn
                    variant="text"
                    size="small"
                    color="primary"
                    :to="{ path: '/recruitment', query: { applicant: a.id } }"
                    >查看</v-btn
                  >
                </td>
              </tr>
            </tbody></v-table
          >
          <div v-else class="empty-state py-6">
            <v-icon size="27">mdi-calendar-check-outline</v-icon>
            <p>暂时没有待进行的面试</p>
          </div></v-card
        >
      </div>
      <aside class="aside-column">
        <v-card class="announcement"
          ><div class="card-heading">
            <h2 class="section-title">
              <v-icon size="18" color="secondary" class="mr-2"
                >mdi-bullhorn-outline</v-icon
              >战队公告
            </h2>
            <span class="tiny-dot" />
          </div>
          <p>
            {{
              settings?.announcement ||
              "欢迎来到战队协作空间。保持好奇，认真记录，一起创造。"
            }}
          </p>
          <div class="announcement-footer">
            <span>{{ settings?.organization || "机器人实验室" }}</span
            ><v-icon size="16">mdi-arrow-top-right</v-icon>
          </div></v-card
        >
        <v-card class="activity-panel"
          ><div class="card-heading">
            <h2 class="section-title">最近动态</h2>
            <v-icon size="18" color="#a4adc0">mdi-history</v-icon>
          </div>
          <div v-if="!data?.recentActivity.length" class="empty-state py-6">
            <p>重要的进展，都会留在这里。</p>
          </div>
          <div class="activity-list">
            <div
              v-for="(a, i) in data?.recentActivity.slice(0, 5)"
              :key="a.id"
              class="activity-row"
            >
              <div class="activity-bullet" :class="{ first: i === 0 }" />
              <div>
                <p>
                  <strong>{{ a.actor }}</strong> {{ a.action }}
                </p>
                <span>{{ a.target }}</span
                ><small>{{ relativeTime(a.createdAt) }}</small>
              </div>
            </div>
          </div></v-card
        >
        <div class="quote-card">
          <div class="eyebrow">MORE THAN A COMPETITION</div>
          <p>好的机器人，<br />由彼此信任的团队造就。</p>
          <div>
            <span>BUILD WITH YOUR TEAM</span
            ><v-icon size="28">mdi-vector-link</v-icon>
          </div>
        </div>
      </aside>
    </div>
    <footer class="dashboard-footer">
      <span>MR SYSTEM <i /> 为热爱创造的人而设计</span
      ><span v-if="auth.demoMode">当前为演示空间 · 数据均为虚构</span>
    </footer>
  </div>
</template>
<style scoped>
.page-heading {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
}
.heading-dot {
  color: #7eb6a4;
  margin-left: 3px;
}
.date-label {
  display: flex;
  align-items: center;
  gap: 7px;
  color: #919bad;
  font-size: 11px;
  padding-top: 9px;
  white-space: nowrap;
}
.welcome-banner {
  position: relative;
  overflow: hidden;
  background: linear-gradient(108deg, #344c79 0%, #4d59a4 57%, #7660a7 100%);
  border-radius: 16px;
  color: white;
  padding: 27px 32px 26px;
  margin-bottom: 22px;
  min-height: 199px;
}
.banner-content {
  position: relative;
  z-index: 1;
}
.banner-label {
  display: flex;
  align-items: center;
  gap: 9px;
  font-size: 9px;
  letter-spacing: 1.5px;
  color: #c4cde7;
}
.banner-label > span {
  width: 5px;
  height: 5px;
  background: #b3dccb;
  border-radius: 50%;
}
.banner-label i {
  height: 9px;
  width: 1px;
  background: #ffffff45;
  margin: 0 2px;
}
.banner-content h2 {
  font-size: 25px;
  font-weight: 600;
  letter-spacing: 0.6px;
  margin: 14px 0 9px;
}
.wave {
  font-size: 22px;
  color: #dfdcaf;
  margin-left: 8px;
}
.banner-content p {
  font-size: 12px;
  color: #ccd4e7;
  margin-bottom: 20px;
}
.banner-action {
  color: #4c609b !important;
  font-size: 11px !important;
  padding: 0 15px !important;
}
.banner-art {
  position: absolute;
  right: 65px;
  top: -25px;
  width: 290px;
  height: 265px;
  color: #dee7ff;
}
.ring {
  position: absolute;
  border: 1px solid #ffffff22;
  border-radius: 50%;
  width: 260px;
  height: 260px;
  right: 0;
  top: 0;
}
.r2 {
  transform: rotate(-20deg);
  width: 315px;
  height: 165px;
  top: 50px;
  right: -25px;
}
.r3 {
  border-style: dashed;
  inset: 30px 0 0 55px;
  width: 200px;
  height: 200px;
}
.robot-tile {
  position: absolute;
  left: 100px;
  top: 82px;
  width: 108px;
  height: 108px;
  background: linear-gradient(145deg, #ffffff24, #ffffff07);
  border: 1px solid #ffffff50;
  border-radius: 24px;
  display: grid;
  place-items: center;
  transform: rotate(-12deg);
  box-shadow: 0 18px 40px #21376920;
}
.art-mark {
  position: absolute;
  font-family: monospace;
  font-size: 24px;
  font-weight: 200;
}
.m1 {
  top: 65px;
  left: 55px;
}
.m2 {
  bottom: 50px;
  right: 2px;
}
.art-text {
  position: absolute;
  bottom: 36px;
  left: 92px;
  font-size: 7px;
  letter-spacing: 3px;
  opacity: 0.65;
}
.stats-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 17px;
  margin-bottom: 25px;
}
.stat-card {
  background: white;
  border: 1px solid #e8ebf2;
  border-radius: 13px;
  padding: 18px 20px;
  text-align: left;
  transition:
    transform 0.18s,
    box-shadow 0.18s;
}
.stat-card:hover {
  transform: translateY(-2px);
  box-shadow: 0 7px 20px #2d426409;
}
.stat-label {
  font-size: 12px;
  color: #8a94a7;
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.stat-icon {
  width: 35px;
  height: 35px;
  border-radius: 11px;
  display: grid;
  place-items: center;
}
.stat-value {
  font-size: 34px;
  line-height: 1.4;
  font-weight: 650;
  letter-spacing: -1px;
  color: #33415d;
  margin: 3px 0 10px;
}
.stat-value > span {
  font-size: 10px;
  color: #a7afbd;
  margin-left: 8px;
  font-weight: 400;
}
.stat-detail {
  border-top: 1px solid #f1f3f7;
  padding-top: 12px;
  font-size: 10px;
  color: #a2aabc;
  display: flex;
  justify-content: space-between;
  gap: 4px;
}
.main-grid {
  display: grid;
  grid-template-columns: minmax(0, 2.25fr) minmax(255px, 1fr);
  gap: 23px;
}
.main-column,
.aside-column {
  display: flex;
  flex-direction: column;
  gap: 22px;
}
.card-heading {
  padding: 21px 23px 17px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.card-heading h2 {
  margin: 0 0 4px;
}
.card-heading .section-caption {
  font-size: 10px;
}
.pipeline-body {
  padding: 3px 24px 23px;
}
.pipeline-bar {
  height: 10px;
  display: flex;
  gap: 4px;
  border-radius: 5px;
  overflow: hidden;
  margin: 7px 0 24px;
}
.pipeline-bar > span {
  min-width: 0;
  border-radius: 3px;
}
.pipeline-stages {
  display: grid;
  grid-template-columns: repeat(5, 1fr);
  gap: 5px;
}
.pipeline-stages button {
  text-align: left;
  border-right: 1px solid #edf0f6;
  padding-left: 12px;
}
.pipeline-stages button:first-child {
  padding-left: 0;
}
.pipeline-stages button:last-child {
  border: 0;
}
.pipeline-stages button > span {
  font-size: 10px;
  color: #9da5b5;
  display: flex;
  align-items: center;
  gap: 6px;
}
.pipeline-stages i {
  height: 5px;
  width: 5px;
  border-radius: 50%;
}
.pipeline-stages strong {
  font-size: 24px;
  font-weight: 550;
  display: block;
  margin-top: 9px;
}
.pipeline-stages small {
  font-size: 9px;
  color: #aab0be;
  margin-left: 7px;
  font-weight: 400;
}
.task-row {
  display: flex;
  align-items: center;
  gap: 13px;
  margin: 0 22px;
  padding: 17px 0;
  border-top: 1px solid #eff1f6;
  text-decoration: none;
  color: inherit;
}
.task-kind {
  width: 43px;
  height: 43px;
  border-radius: 11px;
  background: #eef1fb;
  color: #8694ca;
  flex-shrink: 0;
}
.task-kind {
  display: grid;
  place-items: center;
}
.task-kind.hardware {
  background: #eaf5f0;
  color: #7aa894;
}
.task-kind.document {
  background: #f9f1e7;
  color: #c4a271;
}
.task-main {
  min-width: 0;
  flex: 1;
}
.task-main h3 {
  font-size: 13px;
  font-weight: 600;
  margin: 0 0 7px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.task-main p {
  font-size: 10px;
  color: #9ca5b5;
  margin: 0;
}
.task-main p span {
  margin: 0 7px;
}
.task-meta {
  text-align: right;
}
.task-meta small {
  display: block;
  font-size: 9px;
  color: #a9b0c0;
  margin-top: 8px;
}
.task-arrow {
  color: #b1b8c6;
}
.person-name {
  display: flex;
  align-items: center;
  gap: 9px;
}
.person-name > span {
  width: 27px;
  height: 27px;
  background: #edf1f8;
  border-radius: 8px;
  font-size: 9px;
  display: grid;
  place-items: center;
  color: #7c8eb7;
}
.person-name strong {
  font-size: 12px;
  font-weight: 550;
}
.interview-panel .v-table td {
  height: 62px !important;
  font-size: 11px;
}
.interview-panel .v-table td:first-child,
.interview-panel .v-table th:first-child {
  padding-left: 23px;
}
.announcement {
  background: #f5faf8 !important;
  border-color: #e2eeea !important;
}
.announcement .card-heading {
  padding-bottom: 8px;
}
.announcement .section-title {
  font-size: 14px;
}
.tiny-dot {
  height: 5px;
  width: 5px;
  background: #7caf9b;
  border-radius: 50%;
}
.announcement > p {
  font-size: 12px;
  line-height: 1.95;
  color: #7c958b;
  padding: 0 23px;
  white-space: pre-line;
  margin: 0 0 15px;
}
.announcement-footer {
  margin: 0 23px 19px;
  padding-top: 12px;
  border-top: 1px solid #e3ece8;
  display: flex;
  justify-content: space-between;
  color: #9aaea5;
  font-size: 9px;
}
.activity-panel .card-heading {
  padding-bottom: 13px;
}
.activity-list {
  padding: 4px 24px 15px;
}
.activity-row {
  display: flex;
  gap: 13px;
  position: relative;
  padding-bottom: 20px;
  min-height: 73px;
}
.activity-row:not(:last-child):before {
  content: "";
  position: absolute;
  left: 3px;
  top: 10px;
  bottom: 0;
  width: 1px;
  background: #ebedf4;
}
.activity-bullet {
  width: 7px;
  height: 7px;
  flex-shrink: 0;
  border-radius: 50%;
  background: #ced5e5;
  margin-top: 5px;
  z-index: 1;
}
.activity-bullet.first {
  background: #7d8dca;
  box-shadow: 0 0 0 4px #eef1fa;
}
.activity-row p {
  font-size: 11px;
  color: #99a2b5;
  margin: 0 0 5px;
  line-height: 1.6;
}
.activity-row strong {
  font-weight: 550;
  color: #626e87;
}
.activity-row span {
  display: block;
  font-size: 10px;
  color: #8c98ae;
  line-height: 1.6;
}
.activity-row small {
  display: block;
  font-size: 9px;
  color: #b1b9c8;
  margin-top: 5px;
}
.quote-card {
  padding: 22px 23px;
  background: linear-gradient(130deg, #eef0f8, #eeedf6);
  border: 1px solid #e6e8f1;
  border-radius: 14px;
}
.quote-card > .eyebrow {
  font-size: 8px;
  letter-spacing: 1.6px;
  color: #9ba4bc;
}
.quote-card p {
  font-size: 16px;
  font-weight: 550;
  color: #667592;
  line-height: 1.8;
  margin: 14px 0 13px;
}
.quote-card > div:last-child {
  display: flex;
  justify-content: space-between;
  align-items: center;
  color: #adb6ce;
}
.quote-card span {
  font-size: 7px;
  letter-spacing: 1.3px;
}
.dashboard-footer {
  display: flex;
  justify-content: space-between;
  margin-top: 28px;
  color: #b4bbc9;
  font-size: 9px;
}
.dashboard-footer i {
  display: inline-block;
  height: 8px;
  width: 1px;
  background: #cbd0db;
  margin: 0 9px;
}
@media (min-width: 1500px) {
  .welcome-banner {
    min-height: 215px;
    padding-top: 31px;
  }
  .banner-art {
    right: 95px;
  }
}
@media (max-width: 1200px) {
  .stat-card {
    padding: 15px;
  }
  .stat-detail {
    font-size: 9px;
  }
  .main-grid {
    grid-template-columns: minmax(0, 1.7fr) minmax(240px, 1fr);
    gap: 17px;
  }
  .banner-art {
    right: 0;
    opacity: 0.7;
  }
  .task-meta small {
    display: none;
  }
  .task-main p span {
    margin: 0 3px;
  }
}
@media (max-width: 960px) {
  .stats-grid {
    gap: 12px;
  }
  .main-grid {
    grid-template-columns: minmax(0, 1fr);
  }
  .aside-column {
    display: grid;
    grid-template-columns: 1fr 1fr;
  }
  .quote-card {
    display: none;
  }
  .banner-art {
    right: 20px;
  }
  .task-meta small {
    display: block;
  }
}
@media (max-width: 600px) {
  .stats-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .stat-detail {
    font-size: 8px;
  }
  .stat-value {
    font-size: 29px;
  }
  .welcome-banner {
    padding: 25px 21px;
  }
  .banner-content h2 {
    font-size: 22px;
  }
  .banner-content p {
    max-width: 240px;
    line-height: 1.8;
    font-size: 11px;
  }
  .banner-art {
    right: -110px;
    opacity: 0.35;
  }
  .date-label {
    font-size: 9px;
    gap: 3px;
  }
  .date-label > .v-icon {
    display: none;
  }
  .aside-column {
    display: flex;
  }
  .card-heading {
    padding: 20px 17px 14px;
  }
  .pipeline-body {
    padding: 0 17px 20px;
  }
  .pipeline-stages button {
    padding-left: 6px;
  }
  .pipeline-stages button > span {
    font-size: 9px;
    gap: 3px;
  }
  .pipeline-stages strong {
    font-size: 22px;
  }
  .task-row {
    margin: 0 17px;
    gap: 10px;
  }
  .task-meta small {
    display: none;
  }
  .task-arrow {
    display: none;
  }
  .task-main h3 {
    font-size: 12px;
  }
  .task-kind {
    width: 36px;
    height: 36px;
  }
  .dashboard-footer {
    gap: 12px;
    font-size: 8px;
  }
  .page-heading {
    gap: 5px;
  }
  .page-subtitle {
    font-size: 11px;
  }
}
</style>
