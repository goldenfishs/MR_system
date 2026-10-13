<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import { api } from "../lib/api";
import { auth } from "../lib/auth";
import { dateTime, relativeTime } from "../lib/format";
import type { Dashboard, Settings, Task } from "../lib/types";

const router = useRouter();
const data = ref<Dashboard | null>(null);
const settings = ref<Settings | null>(null);
const tasks = ref<Task[]>([]);
const loading = ref(true);
const error = ref("");
const student = computed(() => auth.user?.role === "student");
const today = new Intl.DateTimeFormat("zh-CN", {
  month: "long",
  day: "numeric",
  weekday: "long",
}).format(new Date());
const shortcuts = computed(() => [
  {
    label: student.value ? "我的报名" : "招新管理",
    value: data.value?.stats.applicants ?? 0,
    unit: "份报名",
    detail: student.value ? "查看报名与面试进度" : "报名审核、面试安排与录取",
    icon: "mdi-account-multiple",
    color: "#42a5f5",
    path: "/recruitment",
  },
  {
    label: student.value ? "任务反馈" : "提交审核",
    value: data.value?.stats.pendingReviews ?? 0,
    unit: "项待审核",
    detail: student.value
      ? "查看提交记录与导师反馈"
      : "查看学员提交，记录培养反馈",
    icon: "mdi-clipboard-check",
    color: "#ef5350",
    path: "/learning",
  },
  {
    label: "学习培养",
    value: data.value?.stats.activeTasks ?? 0,
    unit: "项进行中",
    detail: "编程实践、文档与实机任务",
    icon: "mdi-school",
    color: "#26a69a",
    path: "/learning",
  },
  {
    label: student.value ? "成员状态" : "战队成员",
    value: data.value?.stats.members ?? 0,
    unit: student.value ? "" : "位成员",
    detail: student.value
      ? "录取后开启战队培养任务"
      : "查看赛队、方向与成员信息",
    icon: "mdi-account-group",
    color: "#ab47bc",
    path: student.value ? "/recruitment" : "/members",
  },
]);
const total = computed(
  () => data.value?.pipeline.reduce((sum, stage) => sum + stage.count, 0) || 0,
);
const stageColors: Record<string, string> = {
  submitted: "#a5d6a7",
  screening: "#66bb6a",
  interview: "#43a047",
  accepted: "#1b5e20",
  rejected: "#dce8dd",
};
const circumference = 2 * Math.PI * 61;
const chartSegments = computed(() => {
  let offset = 0;
  return (data.value?.pipeline || []).map((stage) => {
    const length = total.value
      ? (stage.count / total.value) * circumference
      : 0;
    const segment = { ...stage, length, offset };
    offset += length;
    return segment;
  });
});
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
const taskDistribution = computed(() =>
  Object.entries(kindLabels).map(([kind, label]) => ({
    kind,
    label,
    count: tasks.value.filter((task) => task.kind === kind).length,
  })),
);
const taskMaximum = computed(() =>
  Math.max(1, ...taskDistribution.value.map((item) => item.count)),
);
const recentTasks = computed(() => tasks.value.slice(0, 4));
async function load() {
  loading.value = true;
  error.value = "";
  try {
    const [dashboard, space, learning] = await Promise.all([
      api.get<Dashboard>("/dashboard"),
      api.get<{ settings: Settings }>("/settings"),
      api.get<{ items: Task[] }>("/tasks"),
    ]);
    data.value = dashboard;
    settings.value = space.settings;
    tasks.value = learning.items || [];
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
    <header class="welcome-row">
      <div class="welcome-person">
        <v-avatar size="48" color="primary" variant="tonal"
          ><v-icon size="29">mdi-account</v-icon></v-avatar
        >
        <div>
          <h1>你好，{{ auth.user?.name }}</h1>
          <p>
            {{ settings?.organization || "机器人实验室" }} ·
            {{ settings?.season || "战队协作台" }}
          </p>
        </div>
      </div>
      <div class="date-label">
        <span>{{ today }}</span
        ><v-btn
          icon="mdi-refresh"
          size="small"
          variant="text"
          aria-label="刷新工作台"
          :loading="loading"
          @click="load"
        />
      </div>
    </header>
    <v-alert v-if="error" type="error" variant="tonal" class="mb-5"
      ><div class="d-flex align-center justify-space-between">
        {{ error }}<v-btn variant="text" @click="load">重试</v-btn>
      </div></v-alert
    >
    <div class="shortcut-grid">
      <router-link
        v-for="entry in shortcuts"
        :key="entry.label"
        :to="entry.path"
        class="shortcut-card"
      >
        <div class="shortcut-copy">
          <h2>{{ entry.label }}</h2>
          <p>{{ entry.detail }}</p>
          <div class="shortcut-count">
            <template v-if="student && entry.label === '成员状态'"
              ><strong>{{
                loading ? "—" : entry.value ? "已录取" : "待录取"
              }}</strong></template
            >
            <template v-else
              ><strong>{{ loading ? "—" : entry.value }}</strong
              ><span>{{ entry.unit }}</span></template
            >
          </div>
        </div>
        <v-icon class="shortcut-icon" size="55" :color="entry.color">{{
          entry.icon
        }}</v-icon>
      </router-link>
    </div>
    <div class="main-grid">
      <div class="main-column">
        <v-card class="dashboard-card tasks-panel">
          <div class="card-heading">
            <h2>
              <v-icon size="21" color="primary"
                >mdi-book-open-page-variant-outline</v-icon
              >{{ student ? "我的学习任务" : "近期培养任务" }}
            </h2>
            <v-btn size="small" variant="text" color="primary" to="/learning"
              >全部任务<v-icon end size="16">mdi-chevron-right</v-icon></v-btn
            >
          </div>
          <div v-if="!tasks.length" class="empty-state">
            <v-icon size="30">mdi-book-open-outline</v-icon>
            <p>{{ loading ? "正在加载任务…" : "暂无培养任务" }}</p>
            <v-btn
              v-if="
                !loading &&
                (auth.user?.role === 'admin' || auth.user?.role === 'mentor')
              "
              to="/learning"
              variant="text"
              color="primary"
              >发布培养任务</v-btn
            >
          </div>
          <router-link
            v-for="task in recentTasks"
            :key="task.id"
            :to="{ path: '/learning', query: { task: task.id } }"
            class="task-row"
          >
            <div class="task-kind" :class="task.kind">
              <v-icon size="23">{{ kindIcons[task.kind] }}</v-icon>
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
            <v-icon class="task-arrow" size="19">mdi-chevron-right</v-icon>
          </router-link>
        </v-card>
        <v-card class="dashboard-card interview-panel">
          <div class="card-heading">
            <h2>
              <v-icon size="21" color="primary"
                >mdi-calendar-clock-outline</v-icon
              >近期面试
            </h2>
            <span class="card-caption"
              >{{ data?.upcomingInterviews.length || 0 }} 场待进行</span
            >
          </div>
          <v-table v-if="data?.upcomingInterviews.length"
            ><thead>
              <tr>
                <th>候选人</th>
                <th>意向方向</th>
                <th>面试时间</th>
                <th aria-label="操作"></th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="applicant in data.upcomingInterviews.slice(0, 4)"
                :key="applicant.id"
              >
                <td>
                  <div class="person-name">
                    <span>{{ applicant.name.slice(-1) }}</span
                    >{{ applicant.name }}
                  </div>
                </td>
                <td>
                  {{ applicant.direction
                  }}<small class="d-block muted">{{ applicant.team }}</small>
                </td>
                <td class="interview-date">
                  {{ dateTime(applicant.interviewAt) }}
                </td>
                <td>
                  <v-btn
                    variant="text"
                    size="small"
                    color="primary"
                    :to="{
                      path: '/recruitment',
                      query: { applicant: applicant.id },
                    }"
                    >查看</v-btn
                  >
                </td>
              </tr>
            </tbody></v-table
          >
          <div v-else class="empty-state py-7">
            <v-icon size="28">mdi-calendar-check-outline</v-icon>
            <p>{{ loading ? "正在加载面试安排…" : "暂无待进行的面试" }}</p>
          </div>
        </v-card>
        <v-card class="dashboard-card announcement">
          <div class="card-heading">
            <h2>
              <v-icon size="21" color="primary">mdi-bullhorn-outline</v-icon
              >战队公告
            </h2>
            <span class="card-caption">{{ settings?.organization }}</span>
          </div>
          <p>{{ settings?.announcement || "暂无公告" }}</p>
          <div v-if="settings?.applicationOpen" class="announcement-status">
            <v-icon size="17" color="success">mdi-check-circle-outline</v-icon
            >本期招新报名开放中<span v-if="settings.applicationDeadline">
              · {{ dateTime(settings.applicationDeadline) }} 截止</span
            >
          </div>
        </v-card>
      </div>
      <aside class="aside-column">
        <v-card class="dashboard-card recruitment-chart">
          <div class="card-heading">
            <h2>{{ student ? "我的报名进度" : "招新状态分布" }}</h2>
            <v-btn
              icon="mdi-dots-horizontal"
              size="x-small"
              variant="text"
              to="/recruitment"
              aria-label="查看招新详情"
            />
          </div>
          <div v-if="total" class="donut-chart">
            <div
              class="donut-figure"
              role="img"
              :aria-label="`报名共 ${total} 份。${data?.pipeline.map((stage) => `${stage.label} ${stage.count} 份`).join('，')}`"
            >
              <svg viewBox="0 0 158 158" aria-hidden="true">
                <circle
                  cx="79"
                  cy="79"
                  r="61"
                  fill="none"
                  stroke="#eef4ee"
                  stroke-width="24"
                />
                <circle
                  v-for="stage in chartSegments"
                  :key="stage.status"
                  cx="79"
                  cy="79"
                  r="61"
                  fill="none"
                  :stroke="stageColors[stage.status]"
                  stroke-width="24"
                  :stroke-dasharray="`${stage.length} ${circumference - stage.length}`"
                  :stroke-dashoffset="-stage.offset"
                  transform="rotate(-90 79 79)"
                />
              </svg>
              <div class="donut-total">
                <strong>{{ total }}</strong
                ><span>{{ student ? "我的报名" : "报名总数" }}</span>
              </div>
            </div>
            <div class="chart-legend">
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
                ><strong>{{ stage.count }}</strong>
              </button>
            </div>
          </div>
          <div v-else class="empty-state py-7">
            <v-icon size="30">mdi-chart-donut</v-icon>
            <p>{{ loading ? "正在加载报名数据…" : "暂无报名记录" }}</p>
            <v-btn
              v-if="student && !loading"
              variant="text"
              color="primary"
              to="/recruitment"
              >填写报名</v-btn
            >
          </div>
          <div class="chart-footer">
            {{ student ? "仅展示你自己的报名数据" : "按当前报名状态统计" }}
          </div>
        </v-card>
        <v-card class="dashboard-card task-chart">
          <div class="card-heading">
            <h2>培养任务分布</h2>
            <span class="card-caption">共 {{ tasks.length }} 项</span>
          </div>
          <div
            class="bar-chart"
            role="img"
            :aria-label="
              taskDistribution
                .map((item) => `${item.label} ${item.count} 项`)
                .join('，')
            "
          >
            <div
              v-for="item in taskDistribution"
              :key="item.kind"
              class="bar-row"
            >
              <span>{{ item.label }}</span>
              <div class="bar-track">
                <div
                  :style="{ width: `${(item.count / taskMaximum) * 100}%` }"
                />
              </div>
              <strong>{{ loading ? "—" : item.count }}</strong>
            </div>
          </div>
          <div class="chart-footer">
            {{
              student ? "按你可参与的任务类型统计" : "按已发布的任务类型统计"
            }}
          </div>
        </v-card>
        <v-card class="dashboard-card activity-panel">
          <div class="card-heading">
            <h2>最近动态</h2>
            <v-icon size="20" color="grey">mdi-history</v-icon>
          </div>
          <div v-if="!data?.recentActivity.length" class="empty-state py-5">
            <p>{{ loading ? "正在加载动态…" : "暂无动态" }}</p>
          </div>
          <div class="activity-list">
            <div
              v-for="activity in data?.recentActivity.slice(0, 4)"
              :key="activity.id"
              class="activity-row"
            >
              <span class="activity-dot" />
              <div>
                <p>
                  <strong>{{ activity.actor }}</strong> {{ activity.action }}
                </p>
                <span>{{ activity.target }}</span>
              </div>
              <small>{{ relativeTime(activity.createdAt) }}</small>
            </div>
          </div>
        </v-card>
      </aside>
    </div>
    <footer class="dashboard-footer">
      <span>{{ settings?.organization || "机器人实验室" }} · 战队协作台</span
      ><span v-if="auth.demoMode">演示空间 · 数据均为虚构</span>
    </footer>
  </div>
</template>

<style scoped>
.dashboard {
  color: #424242;
}
.welcome-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 24px;
}
.welcome-person {
  display: flex;
  align-items: center;
  gap: 15px;
}
.welcome-person h1 {
  font-size: 24px;
  font-weight: 500;
  letter-spacing: 0;
  margin: 0 0 4px;
  color: #303030;
}
.welcome-person p {
  margin: 0;
  color: #757575;
  font-size: 13px;
}
.date-label {
  display: flex;
  align-items: center;
  gap: 7px;
  font-size: 12px;
  color: #757575;
  white-space: nowrap;
}
.shortcut-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 18px;
  margin-bottom: 22px;
}
.shortcut-card {
  min-height: 137px;
  background: #fff;
  border-radius: 6px;
  border: 1px solid #e8e8e8;
  box-shadow: 0 2px 5px #00000009;
  text-decoration: none;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
  padding: 19px 20px;
  color: inherit;
  transition:
    box-shadow 0.16s,
    border-color 0.16s;
}
.shortcut-card:hover {
  border-color: #bbdefb;
  box-shadow: 0 3px 10px #00000013;
}
.shortcut-copy {
  min-width: 0;
}
.shortcut-copy h2 {
  font-size: 17px;
  font-weight: 500;
  color: #333;
  margin: 0 0 7px;
}
.shortcut-copy p {
  font-size: 12px;
  line-height: 1.65;
  margin: 0 0 11px;
  color: #858585;
}
.shortcut-count {
  display: flex;
  align-items: baseline;
  gap: 6px;
}
.shortcut-count strong {
  font-size: 22px;
  font-weight: 400;
  color: #424242;
  line-height: 1;
}
.shortcut-count span {
  font-size: 12px;
  color: #757575;
}
.shortcut-icon {
  flex-shrink: 0;
  opacity: 0.95;
}
.main-grid {
  display: grid;
  grid-template-columns: minmax(0, 1.5fr) minmax(320px, 1fr);
  gap: 22px;
}
.main-column,
.aside-column {
  display: flex;
  flex-direction: column;
  gap: 22px;
  min-width: 0;
}
.dashboard-card {
  border: 1px solid #e7e7e7 !important;
  border-radius: 6px !important;
  box-shadow: 0 2px 5px #00000008 !important;
}
.card-heading {
  min-height: 60px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 14px 20px;
  border-bottom: 1px solid #f0f0f0;
}
.card-heading h2 {
  display: flex;
  align-items: center;
  gap: 9px;
  font-size: 16px;
  font-weight: 500;
  margin: 0;
  color: #424242;
}
.card-caption {
  font-size: 12px;
  color: #8a8a8a;
}
.task-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 17px 20px;
  min-height: 82px;
  color: inherit;
  text-decoration: none;
  border-bottom: 1px solid #f2f2f2;
}
.task-row:last-child {
  border-bottom: 0;
}
.task-row:hover {
  background: #fafafa;
}
.task-kind {
  width: 39px;
  height: 39px;
  border-radius: 50%;
  background: #e3f2fd;
  color: #1976d2;
  display: grid;
  place-items: center;
  flex-shrink: 0;
}
.task-kind.document {
  background: #fff3e0;
  color: #ef8b16;
}
.task-kind.hardware {
  background: #e8f5e9;
  color: #43a047;
}
.task-kind.quiz {
  background: #f3e5f5;
  color: #9c27b0;
}
.task-main {
  min-width: 0;
  flex: 1;
}
.task-main h3 {
  font-size: 14px;
  font-weight: 500;
  margin: 0 0 5px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.task-main p {
  font-size: 12px;
  color: #8a8a8a;
  margin: 0;
}
.task-main p span {
  margin: 0 5px;
}
.task-meta {
  text-align: right;
  flex-shrink: 0;
}
.task-meta small {
  display: block;
  font-size: 11px;
  color: #999;
  margin-top: 5px;
}
.task-arrow {
  color: #aaa;
}
.person-name {
  display: flex;
  align-items: center;
  gap: 9px;
  white-space: nowrap;
}
.person-name > span {
  width: 29px;
  height: 29px;
  display: grid;
  place-items: center;
  background: #e3f2fd;
  color: #1976d2;
  border-radius: 50%;
  font-size: 12px;
}
.interview-panel :deep(td) {
  height: 63px !important;
  font-size: 13px;
}
.interview-date {
  white-space: nowrap;
}
.announcement > p {
  padding: 18px 20px;
  font-size: 14px;
  line-height: 1.9;
  margin: 0;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  color: #616161;
}
.announcement-status {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 5px;
  font-size: 12px;
  padding: 0 20px 18px;
  color: #757575;
}
.donut-chart {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 20px;
  padding: 19px 20px;
  min-height: 208px;
}
.donut-figure {
  position: relative;
  width: 158px;
  height: 158px;
  flex-shrink: 0;
}
.donut-figure svg {
  display: block;
  width: 100%;
  height: 100%;
}
.donut-total {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  pointer-events: none;
}
.donut-total strong {
  font-size: 30px;
  line-height: 1.1;
  font-weight: 400;
  color: #424242;
}
.donut-total span {
  font-size: 11px;
  color: #9e9e9e;
  margin-top: 5px;
}
.chart-legend {
  min-width: 115px;
  flex: 1;
  max-width: 168px;
}
.chart-legend button {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  padding: 6px 3px;
  gap: 15px;
  border-radius: 3px;
  font-size: 12px;
  color: #616161;
}
.chart-legend button:hover {
  background: #f5f5f5;
}
.chart-legend button > span {
  display: flex;
  align-items: center;
  gap: 8px;
}
.chart-legend i {
  width: 9px;
  height: 9px;
  border-radius: 2px;
}
.chart-legend strong {
  font-size: 13px;
  font-weight: 500;
  color: #424242;
}
.chart-footer {
  padding: 10px 20px;
  border-top: 1px solid #f1f1f1;
  color: #9e9e9e;
  font-size: 11px;
}
.bar-chart {
  padding: 16px 22px 19px;
}
.bar-row {
  display: flex;
  align-items: center;
  gap: 13px;
  min-height: 34px;
}
.bar-row > span {
  font-size: 12px;
  color: #757575;
  width: 52px;
  flex-shrink: 0;
}
.bar-track {
  height: 10px;
  flex: 1;
  background: #fce4ec;
  border-radius: 2px;
  overflow: hidden;
}
.bar-track > div {
  height: 100%;
  background: #e91e63;
  border-radius: 2px;
}
.bar-row strong {
  min-width: 14px;
  font-size: 12px;
  font-weight: 500;
  text-align: right;
}
.activity-list {
  padding: 7px 20px 12px;
}
.activity-row {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 11px 0;
}
.activity-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: #42a5f5;
  flex-shrink: 0;
  margin-top: 7px;
}
.activity-row > div {
  flex: 1;
  min-width: 0;
}
.activity-row p {
  font-size: 12px;
  margin: 0 0 4px;
  color: #616161;
}
.activity-row strong {
  font-weight: 500;
  color: #424242;
}
.activity-row div > span {
  display: block;
  color: #9e9e9e;
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.activity-row small {
  font-size: 11px;
  color: #aaa;
  white-space: nowrap;
  padding-top: 2px;
}
.dashboard-footer {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  margin-top: 26px;
  color: #9e9e9e;
  font-size: 11px;
}
@media (max-width: 1280px) {
  .shortcut-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .shortcut-card {
    min-height: 125px;
  }
  .task-meta small {
    max-width: 100px;
  }
  .task-arrow {
    display: none;
  }
}
@media (max-width: 1050px) {
  .main-grid {
    grid-template-columns: minmax(0, 1fr);
  }
  .aside-column {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    align-items: start;
  }
  .activity-panel {
    grid-column: 1 / -1;
  }
  .donut-chart {
    gap: 10px;
  }
  .donut-figure {
    width: 140px;
    height: 140px;
  }
  .task-meta small {
    max-width: none;
  }
}
@media (max-width: 650px) {
  .welcome-row {
    align-items: flex-start;
  }
  .welcome-person {
    gap: 10px;
  }
  .welcome-person h1 {
    font-size: 21px;
  }
  .welcome-person p {
    font-size: 12px;
    max-width: 210px;
  }
  .date-label > span {
    display: none;
  }
  .shortcut-grid {
    gap: 12px;
    margin-bottom: 16px;
  }
  .shortcut-card {
    padding: 15px;
    min-height: 136px;
    position: relative;
    align-items: flex-start;
  }
  .shortcut-copy h2 {
    font-size: 16px;
  }
  .shortcut-copy p {
    max-width: 115px;
    font-size: 11px;
    padding-right: 10px;
  }
  .shortcut-icon {
    position: absolute;
    bottom: 14px;
    right: 12px;
    font-size: 33px !important;
  }
  .shortcut-count strong {
    font-size: 20px;
  }
  .shortcut-count span {
    font-size: 11px;
  }
  .main-grid,
  .main-column,
  .aside-column {
    gap: 16px;
  }
  .aside-column {
    grid-template-columns: minmax(0, 1fr);
  }
  .activity-panel {
    grid-column: auto;
  }
  .card-heading {
    padding: 12px 15px;
    min-height: 57px;
  }
  .card-heading h2 {
    font-size: 15px;
  }
  .task-row {
    padding: 14px 15px;
    gap: 9px;
    flex-wrap: wrap;
  }
  .task-kind {
    width: 34px;
    height: 34px;
  }
  .task-main h3 {
    white-space: normal;
    font-size: 13px;
  }
  .task-main p {
    font-size: 11px;
  }
  .task-meta {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 8px;
    padding-left: 43px;
    text-align: left;
  }
  .task-meta small {
    margin: 0;
  }
  .donut-chart {
    gap: 28px;
  }
  .dashboard-footer {
    flex-direction: column;
    gap: 6px;
  }
  .interview-panel :deep(td),
  .interview-panel :deep(th) {
    padding: 0 10px !important;
  }
}
</style>
