<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { api } from "../lib/api";
import { hasPermission, isParticipant } from "../lib/permissions";
import { auth } from "../lib/auth";
import { canAccessLearning, canAccessLearningModule, canOpenLearning, learningAccess, learningDirections, refreshLearningAccess } from "../lib/learning-access";
import type { Task, Settings } from "../lib/types";
import type { TeamSpace } from "../lib/platform";

type Summary = {
  stats: Record<string, number>;
  pipeline: { status: string; label: string; count: number }[];
  profileComplete: boolean;
};
const brand = ref<Settings | null>(null);
const competitionLabel = (team: TeamSpace) => (team.competitions?.length ? team.competitions : [team.competition]).map(c => c === "ROBOCON" ? "RoboCon" : c).join(" / ");
const summary = ref<Summary | null>(null);
const teams = ref<TeamSpace[]>([]);
const taskCompetitionLabel = (task: Task) => {
  const team = teams.value.find(item => item.id === task.teamId);
  return team ? competitionLabel(team) : task.team === "ROBOCON" ? "RoboCon" : task.team || "所有团队";
};
const tasks = ref<Task[]>([]);
const loading = ref(true);
const error = ref("");
const isAdmin = computed(() => auth.user?.role === "admin");
const canManage = computed(() => hasPermission("tasks"));
const canReadTasks = computed(() => canAccessLearning.value || canManage.value);
const taskDirections = computed(() => learningDirections.value.filter(group => canAccessLearningModule("tasks", group.value)));
const knowledgeDirections = computed(() => learningDirections.value.filter(group => canAccessLearningModule("knowledge", group.value)));
const learningTarget = computed(() => ({
  path: "/learning",
  query: { direction: (canReadTasks.value ? taskDirections.value : knowledgeDirections.value)[0]?.value, section: canReadTasks.value ? "tasks" : "knowledge" },
}));
const showAdmissionNotice = computed(() => !canAccessLearning.value && (isParticipant() || auth.user?.role === "group_admin"));
const admissionNotice = computed(() => canOpenLearning.value
  ? "授权组别的管理权限已可使用。你的个人学习资格仍需报名审核通过后开通，当前不能以学员身份提交作业或参加考试。"
  : "报名审核通过后，技术分组、知识库、作业和考试将向你开放。"
);
const canRecruit = computed(() => hasPermission("recruitment"));
const canIssueRecords = computed(() => hasPermission("records"));
const today = new Intl.DateTimeFormat("zh-CN", {
  month: "long",
  day: "numeric",
  weekday: "long",
}).format(new Date());

let loadGeneration = 0;
async function load() {
  const generation = ++loadGeneration;
  loading.value = true;
  error.value = "";
  try {
    await refreshLearningAccess();
    if (generation !== loadGeneration || !auth.user) return;
    const [space, teamList, learning, settings] = await Promise.all([
      api.get<Summary>("/workspace-summary"),
      api.get<{ items: TeamSpace[] }>("/teams"),
      canReadTasks.value
        ? api.get<{ items: Task[] }>("/tasks")
        : Promise.resolve({ items: [] as Task[] }),
      api.get<{ settings: Settings }>("/settings"),
    ]);
    if (generation !== loadGeneration) return;
    summary.value = space;
    teams.value = teamList.items || [];
    tasks.value = learning.items || [];
    brand.value = settings.settings;
  } catch (e) {
    if (generation === loadGeneration) error.value = (e as Error).message;
  } finally {
    if (generation === loadGeneration) loading.value = false;
  }
}
const counts = (key: string) => summary.value?.stats[key] ?? 0;
watch(() => JSON.stringify([auth.user?.id, auth.user?.role, auth.user?.groupGrants, canAccessLearning.value]), () => {
  tasks.value = [];
  void load();
});
onMounted(load);
</script>

<template>
  <div class="page-content dashboard" :style="{ '--shortcut-columns': canOpenLearning ? 4 : 3, '--main-columns': canOpenLearning ? 2 : 1 }">
    <header class="welcome-row">
      <div class="welcome-person">
        <v-avatar size="48" color="primary" variant="tonal"
          ><v-icon size="27">mdi-robot-outline</v-icon></v-avatar
        >
        <div>
          <h1>你好，{{ auth.user?.name }}</h1>
          <p>团队协作台 · {{ today }}</p>
        </div>
      </div>
      <v-btn
        icon="mdi-refresh"
        variant="text"
        aria-label="刷新工作台"
        :loading="loading"
        @click="load"
      />
    </header>
    <v-alert v-if="error" type="error" variant="tonal" class="mb-5"
      >{{ error }} <v-btn variant="text" @click="load">重试</v-btn></v-alert
    >
    <v-alert
      v-if="
        isParticipant() && summary && !summary.profileComplete
      "
      type="warning"
      variant="tonal"
      class="profile-alert mb-5"
    >
      完成个人资料后即可申请加入团队。姓名、学校、专业、班级、学号、联系电话和本人证件照为必填。
      <v-btn to="/profile" color="primary" variant="text">补全资料</v-btn>
    </v-alert>

    <v-alert v-if="showAdmissionNotice" :type="learningAccess.error ? 'warning' : 'info'" variant="tonal" class="mb-5">
      {{ learningAccess.error }} {{ admissionNotice }}
      <v-btn v-if="learningAccess.error" variant="text" @click="load">重试</v-btn>
      <v-btn v-else to="/teams" color="primary" variant="text">查看团队报名</v-btn>
    </v-alert>

    <div class="shortcut-grid">
      <router-link to="/teams" class="shortcut-card"
        ><div>
          <span class="eyebrow">团队报名</span>
          <h2>{{ canRecruit ? "团队与申请" : "开放团队" }}</h2>
          <p>
            {{ canRecruit ? "查看和审核授权组别的报名" : "选择方向，提交 PDF 简历" }}
          </p>
          <strong>{{
            loading
              ? "—"
              : isAdmin
                ? counts("pendingApplications") + " 份待审"
                : counts("openTeams") + " 个开放"
          }}</strong>
        </div>
        <v-icon color="primary" size="43"
          >mdi-account-multiple-outline</v-icon
        ></router-link
      >
      <router-link v-if="canOpenLearning" :to="learningTarget" class="shortcut-card"
        ><div>
          <span class="eyebrow">{{ canReadTasks ? "任务培养" : "知识资料" }}</span>
          <h2>{{ canReadTasks ? canManage ? "任务与评阅" : "我的作业" : "知识库管理" }}</h2>
          <p>{{ canReadTasks ? canManage ? "管理授权组作业、批改提交" : "查看任务、上传成果" : "维护授权组资料、文件与视频" }}</p>
          <strong>{{
            loading
              ? "—"
              : !canReadTasks
                ? knowledgeDirections.length + " 个授权组别"
              : canManage
                ? counts("pendingReviews") + " 项待评"
                : counts("activeTasks") + " 项进行中"
          }}</strong>
        </div>
        <v-icon color="teal" size="43"
          >{{ canReadTasks ? "mdi-clipboard-text-outline" : "mdi-book-open-page-variant-outline" }}</v-icon
        ></router-link
      >
      <router-link to="/records" class="shortcut-card"
        ><div>
          <span class="eyebrow">荣誉中心</span>
          <h2>{{ canIssueRecords ? "荣誉发放" : "我的证书与奖状" }}</h2>
          <p>
            {{
              canIssueRecords ? "将证书和奖励发给指定成员" : "统一查询个人奖励与材料"
            }}
          </p>
          <strong>{{
            loading
              ? "—"
              : isAdmin
                ? counts("members") + " 位团队成员"
                : counts("records") + " 份荣誉记录"
          }}</strong>
        </div>
        <v-icon color="deep-purple" size="43"
          >mdi-certificate-outline</v-icon
        ></router-link
      >
      <router-link to="/discussions" class="shortcut-card"
        ><div>
          <span class="eyebrow">交流答疑</span>
          <h2>讨论区</h2>
          <p>向指定组别提问，查阅精选解答</p>
          <strong>一起解决问题</strong>
        </div>
        <v-icon color="orange-darken-1" size="43"
          >mdi-forum-outline</v-icon
        ></router-link
      >
    </div>

    <div class="main-grid">
      <v-card class="dashboard-card">
        <div class="card-heading">
          <h2>
            <v-icon color="primary" size="20">mdi-account-group-outline</v-icon
            >{{ isAdmin ? "团队申请进度" : "招募中的团队" }}
          </h2>
          <v-btn to="/teams" variant="text" size="small" color="primary"
            >{{ isAdmin ? "管理团队" : "浏览全部"
            }}<v-icon end>mdi-chevron-right</v-icon></v-btn
          >
        </div>
        <div v-if="isAdmin" class="pipeline">
          <div
            v-for="stage in summary?.pipeline"
            :key="stage.status"
            class="pipeline-row"
          >
            <span>{{ stage.label }}</span>
            <div class="track">
              <i
                :style="{
                  width: `${Math.min(100, (stage.count / Math.max(1, counts('applications'))) * 100)}%`,
                }"
              />
            </div>
            <strong>{{ stage.count }}</strong>
          </div>
          <p class="privacy-note">
            只统计新团队报名流程；历史面试记录可在「面试记录」中查询。
          </p>
        </div>
        <div v-else-if="teams.length" class="team-list">
          <router-link
            v-for="team in teams
              .filter((item) => item.status === 'open')
              .slice(0, 4)"
            :key="team.id"
            :to="'/teams'"
            class="team-row"
          >
            <span class="team-mark"><v-icon>mdi-trophy-outline</v-icon></span
            ><span class="team-copy"
              ><strong>{{ team.name }}</strong
              ><small
                >{{ competitionLabel(team) }} ·
                {{ team.directions.join("、") }}</small
              ></span
            ><v-chip size="small" variant="tonal" color="success"
              >{{ team.memberCount }} 位成员</v-chip
            ><v-icon size="18" color="grey">mdi-chevron-right</v-icon>
          </router-link>
          <div
            v-if="!teams.some((item) => item.status === 'open')"
            class="empty-state"
          >
            <v-icon size="30">mdi-account-search-outline</v-icon>
            <p>当前暂无开放报名的团队</p>
          </div>
        </div>
        <div v-else class="empty-state">
          <v-icon size="32">mdi-account-search-outline</v-icon>
          <p>
            {{ loading ? "正在加载团队…" : "暂无团队，等候管理员发布招募" }}
          </p>
          <v-btn v-if="isAdmin" to="/teams" color="primary" variant="text"
            >发布团队</v-btn
          >
        </div>
      </v-card>

      <v-card v-if="canReadTasks" class="dashboard-card">
        <div class="card-heading">
          <h2>
            <v-icon color="teal" size="20">mdi-clipboard-list-outline</v-icon
            >{{ canManage ? "授权组培养任务" : "可参与的任务" }}
          </h2>
          <v-btn :to="learningTarget" variant="text" size="small" color="primary"
            >全部任务<v-icon end>mdi-chevron-right</v-icon></v-btn
          >
        </div>
        <div v-if="tasks.length" class="task-list">
          <router-link
            v-for="task in tasks.slice(0, 4)"
            :key="task.id"
            :to="{ path: '/learning', query: { task: task.id, direction: task.direction || undefined, section: task.category === 'exam' ? 'exam' : task.category === 'problem' ? 'bank' : 'tasks' } }"
            class="task-row"
          >
            <span class="task-mark"
              ><v-icon>{{
                task.kind === "code"
                  ? "mdi-code-braces"
                  : task.kind === "hardware"
                    ? "mdi-cube-outline"
                    : task.kind === "quiz"
                      ? "mdi-text-box-check-outline"
                      : "mdi-file-document-outline"
              }}</v-icon></span
            >
            <span class="task-copy"
              ><strong>{{ task.title }}</strong
              ><small
                >{{ taskCompetitionLabel(task) }} ·
                {{ task.direction || "综合方向"
                }}{{
                  task.dueAt
                    ? ` · 截止 ${new Date(task.dueAt).toLocaleDateString("zh-CN")}`
                    : ""
                }}</small
              ></span
            ><v-icon size="18" color="grey">mdi-chevron-right</v-icon>
          </router-link>
        </div>
        <div v-else class="empty-state">
          <v-icon size="32">mdi-book-open-outline</v-icon>
          <p>{{ loading ? "正在加载任务…" : "暂无培养任务" }}</p>
          <v-btn v-if="canManage" :to="learningTarget" color="primary" variant="text"
            >发布作业</v-btn
          >
        </div>
      </v-card>
      <v-card v-else-if="canOpenLearning" class="dashboard-card">
        <div class="card-heading">
          <h2><v-icon color="teal" size="20">mdi-book-open-page-variant-outline</v-icon>授权组知识库</h2>
          <v-btn :to="learningTarget" variant="text" size="small" color="primary">管理资料<v-icon end>mdi-chevron-right</v-icon></v-btn>
        </div>
        <div class="task-list">
          <router-link v-for="group in knowledgeDirections" :key="group.value" :to="{ path: '/learning', query: { direction: group.value, section: 'knowledge' } }" class="task-row">
            <span class="task-mark"><v-icon>{{ group.icon }}</v-icon></span>
            <span class="task-copy"><strong>{{ group.title }}知识库</strong><small>管理本组资料、文件与视频</small></span>
            <v-icon size="18" color="grey">mdi-chevron-right</v-icon>
          </router-link>
        </div>
      </v-card>
    </div>
    <footer class="dashboard-footer">
      <span>{{ brand?.organization || "青岛理工大学机器人战队" }} · {{ brand?.teamName || "沧溟" }} / {{ brand?.secondaryTeamName || "MOVE" }} · {{ (brand?.competitions?.length ? brand.competitions : ["RoboCon", "RoboMaster"]).join(" & ") }}</span
      ><span v-if="auth.demoMode">演示空间 · 数据均为虚构</span>
    </footer>
  </div>
</template>

<style scoped>
.dashboard {
  color: #364153;
}
.welcome-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 22px;
}
.welcome-person {
  display: flex;
  align-items: center;
  gap: 14px;
}
.welcome-person h1 {
  font-size: 23px;
  font-weight: 600;
  margin: 0 0 4px;
  color: #263348;
}
.welcome-person p {
  font-size: 12px;
  color: #8490a3;
  margin: 0;
}
.profile-alert {
  display: flex;
  align-items: center;
  gap: 6px;
}
.shortcut-grid {
  display: grid;
  grid-template-columns: repeat(var(--shortcut-columns), minmax(0, 1fr));
  gap: 15px;
  margin-bottom: 20px;
}
.shortcut-card {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
  min-height: 148px;
  padding: 19px 18px;
  background: white;
  border: 1px solid #e7eaf0;
  border-radius: 10px;
  box-shadow: 0 3px 12px #27344b08;
  text-decoration: none;
  color: inherit;
  transition: 0.18s;
}
.shortcut-card:hover {
  transform: translateY(-2px);
  border-color: #b9d8eb;
  box-shadow: 0 8px 20px #27344b12;
}
.shortcut-card > div {
  min-width: 0;
}
.eyebrow {
  font-size: 10px;
  color: #8190a4;
  letter-spacing: 0.04em;
}
.shortcut-card h2 {
  font-size: 16px;
  margin: 7px 0 4px;
  font-weight: 600;
}
.shortcut-card p {
  font-size: 11px;
  color: #8a95a5;
  margin: 0 0 12px;
  white-space: nowrap;
}
.shortcut-card strong {
  font-size: 14px;
  color: #397ca2;
  font-weight: 600;
}
.main-grid {
  display: grid;
  grid-template-columns: repeat(var(--main-columns), minmax(0, 1fr));
  gap: 18px;
}
.dashboard-card {
  border: 1px solid #e7eaf0 !important;
  border-radius: 10px !important;
  box-shadow: 0 3px 12px #27344b08 !important;
  min-width: 0;
}
.card-heading {
  min-height: 59px;
  padding: 13px 18px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  border-bottom: 1px solid #eff1f5;
}
.card-heading h2 {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  font-weight: 600;
  margin: 0;
}
.team-row,
.task-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 14px 17px;
  min-height: 68px;
  color: inherit;
  text-decoration: none;
  border-bottom: 1px solid #f0f2f5;
}
.team-row:last-child,
.task-row:last-child {
  border: 0;
}
.team-row:hover,
.task-row:hover {
  background: #f8fafc;
}
.team-mark,
.task-mark {
  width: 37px;
  height: 37px;
  border-radius: 10px;
  background: #edf5fb;
  color: #4083a8;
  display: grid;
  place-items: center;
  flex-shrink: 0;
}
.task-mark {
  background: #eaf6f4;
  color: #378e7c;
}
.team-copy,
.task-copy {
  flex: 1;
  min-width: 0;
}
.team-copy strong,
.task-copy strong {
  display: block;
  font-size: 13px;
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.team-copy small,
.task-copy small {
  display: block;
  font-size: 11px;
  color: #8691a1;
  margin-top: 5px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.empty-state {
  min-height: 155px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  color: #a0aaba;
}
.empty-state p {
  font-size: 12px;
  margin: 0;
}
.pipeline {
  padding: 21px 20px 14px;
}
.pipeline-row {
  display: grid;
  grid-template-columns: 70px 1fr 28px;
  align-items: center;
  gap: 12px;
  min-height: 36px;
  font-size: 12px;
  color: #667387;
}
.pipeline-row strong {
  text-align: right;
  font-weight: 600;
  color: #354257;
}
.track {
  height: 8px;
  background: #edf1f5;
  border-radius: 9px;
  overflow: hidden;
}
.track i {
  display: block;
  height: 100%;
  background: linear-gradient(90deg, #39a7a1, #387cb0);
  border-radius: 9px;
}
.privacy-note {
  margin: 13px 0 0;
  color: #98a2b0;
  font-size: 10px;
}
.dashboard-footer {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  margin-top: 22px;
  color: #9aa3b0;
  font-size: 10px;
}
@media (max-width: 1100px) {
  .shortcut-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .main-grid {
    grid-template-columns: 1fr;
  }
}
@media (max-width: 600px) {
  .welcome-row {
    margin-bottom: 16px;
  }
  .welcome-person h1 {
    font-size: 20px;
  }
  .shortcut-grid {
    gap: 10px;
  }
  .shortcut-card {
    min-height: 133px;
    padding: 14px;
  }
  .shortcut-card p {
    white-space: normal;
  }
  .shortcut-card > :deep(.v-icon) {
    font-size: 34px !important;
  }
  .main-grid {
    gap: 14px;
  }
  .dashboard-footer {
    flex-direction: column;
  }
}
</style>
