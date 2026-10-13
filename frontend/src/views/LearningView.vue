<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useDisplay } from "vuetify";
import { api, ApiError } from "../lib/api";
import { auth } from "../lib/auth";
import type { Task, Submission, Applicant } from "../lib/types";
import type { JudgeConfig, Membership, TeamSpace } from "../lib/platform";
import FileUpload from "../components/FileUpload.vue";
import AttachmentList from "../components/AttachmentList.vue";
import JudgePanel from "../components/JudgePanel.vue";
import JudgeFields from "../components/JudgeFields.vue";

const { smAndDown } = useDisplay();
const route = useRoute();
const router = useRouter();
const tasks = ref<Task[]>([]);
const loading = ref(true);
const error = ref("");
const notice = ref("");
const currentTime = ref(Date.now());
let timeTicker: ReturnType<typeof setInterval> | undefined;
const search = ref("");
const selected = ref<Task | null>(null);
const submissions = ref<Submission[]>([]);
const submissionsLoading = ref(false);
const submissionsError = ref("");
const saving = ref(false);
const createOpen = ref(false);
const admitted = ref(false);
const memberScope = reactive({ team: "", direction: "" });
const reviewTarget = ref<Submission | null>(null);
const detailTab = ref("brief");
const submissionForm = reactive({
  content: "",
  repoUrl: "",
  artifactUrl: "",
  fileIds: [] as string[],
  judgeRunId: "",
});
const uploadBusy = ref(false);
const judgeBusy = ref(false);
const sourceOnly = computed(
  () =>
    selected.value?.submissionType === "code" && selected.value.judge?.enabled,
);
const taskFiles = ref<string[]>([]);
const teamsForTasks = ref<TeamSpace[]>([]);
const memberships = ref<Membership[]>([]);

const reviewForm = reactive({
  score: 0,
  feedback: "",
  status: "approved" as "approved" | "changes_requested",
});
const taskForm = reactive({
  title: "",
  description: "",
  direction: "",
  team: "",
  kind: "document",
  difficulty: "入门",
  dueAt: "",
  resourcesUrl: "",
  maxScore: 100,
  required: true,
  teamId: "",
  submissionType: "files" as "files" | "code" | "project",
  startsAt: "",
  judge: {
    enabled: false,
    testCases: [],
    languageIds: [],
    timeLimitSeconds: 2,
    totalCases: 0,
  } as JudgeConfig,
});
watch(
  () => taskForm.submissionType,
  (value) => {
    if (value === "code" && !["code", "quiz"].includes(taskForm.kind))
      taskForm.kind = "code";
  },
);
const directions = ["机械", "电控", "视觉算法", "硬件", "运营"];
const taskDirections = computed(
  () =>
    teamsForTasks.value.find((t) => t.id === taskForm.teamId)?.directions ||
    directions,
);
watch(
  () => taskForm.teamId,
  (id) => {
    const team = teamsForTasks.value.find((t) => t.id === id);
    if (team) {
      taskForm.team = team.competition;
      if (!team.directions.includes(taskForm.direction))
        taskForm.direction = "";
    }
  },
);
const kinds = [
  { value: "all", title: "全部任务", icon: "mdi-view-grid-outline" },
  { value: "code", title: "代码项目", icon: "mdi-code-braces" },
  { value: "document", title: "文档作业", icon: "mdi-file-document-outline" },
  {
    value: "hardware",
    title: "实机验收",
    icon: "mdi-robot-industrial-outline",
  },
  { value: "quiz", title: "知识测评", icon: "mdi-clipboard-text-outline" },
];
const kindFilter = computed(() =>
  kinds.some((kind) => kind.value === route.query.kind)
    ? String(route.query.kind)
    : "all",
);
const directionFilter = computed({
  get: () =>
    typeof route.query.direction === "string" &&
    directions.includes(route.query.direction)
      ? route.query.direction
      : "全部方向",
  set: (direction: string) => {
    router.replace({
      query: {
        ...route.query,
        direction: direction === "全部方向" ? undefined : direction,
      },
    });
  },
});
function clearFilters() {
  search.value = "";
  router.replace({
    query: { ...route.query, kind: undefined, direction: undefined },
  });
}
const isStudent = computed(() => auth.user?.role === "student");
const canManage = computed(() =>
  ["admin", "mentor"].includes(auth.user?.role || ""),
);
const filtered = computed(() =>
  tasks.value.filter(
    (t) =>
      (kindFilter.value === "all" || t.kind === kindFilter.value) &&
      (directionFilter.value === "全部方向" ||
        !t.direction ||
        t.direction === directionFilter.value) &&
      `${t.title} ${t.description} ${t.team}`
        .toLowerCase()
        .includes(search.value.toLowerCase()),
  ),
);
const completed = computed(
  () => tasks.value.filter((t) => t.mySubmission?.status === "approved").length,
);
const pending = computed(
  () => tasks.value.filter((t) => t.mySubmission?.status === "pending").length,
);
const revisions = computed(
  () =>
    tasks.value.filter((t) => t.mySubmission?.status === "changes_requested")
      .length,
);
const latestSubmissions = computed(() => {
  const byUser = new Map<string, Submission>();
  for (const sub of submissions.value)
    if (
      !byUser.has(sub.userId) ||
      byUser.get(sub.userId)!.version < sub.version
    )
      byUser.set(sub.userId, sub);
  return [...byUser.values()].sort(
    (a, b) =>
      (a.status === "pending" ? -1 : 1) - (b.status === "pending" ? -1 : 1) ||
      b.createdAt.localeCompare(a.createdAt),
  );
});
const selectedSubmission = computed(() => selected.value?.mySubmission);
const eligibility = computed(() => {
  if (!isStudent.value || !selected.value) return "";
  if (!admitted.value)
    return "加入并通过审核后可提交学习任务。你现在可以先了解任务要求与学习资料。";
  if (
    selected.value.teamId &&
    !memberships.value.some(
      (m) =>
        m.teamId === selected.value?.teamId &&
        (!selected.value?.direction ||
          m.direction === selected.value.direction),
    )
  )
    return `此任务面向${selected.value.direction || "该"}方向团队成员。请先加入对应团队。`;
  if (
    !selected.value.teamId &&
    selected.value.team &&
    selected.value.team !== memberScope.team
  )
    return `此任务面向 ${selected.value.team} 成员。`;
  if (
    !selected.value.teamId &&
    selected.value.direction &&
    selected.value.direction !== memberScope.direction
  )
    return `此任务面向${selected.value.direction}方向成员。`;
  if (
    selected.value.startsAt &&
    new Date(selected.value.startsAt).getTime() > currentTime.value
  )
    return `考试开始时间：${formatDate(selected.value.startsAt)}。`;
  if (
    selected.value.dueAt &&
    new Date(selected.value.dueAt).getTime() < currentTime.value
  )
    return "此任务已截止，仍可查看任务说明、提交历史与反馈。";
  return "";
});
const kindOf = (value: string) =>
  kinds.find((k) => k.value === value) || kinds[1]!;
const statusOf = (value?: string) =>
  ({
    approved: {
      label: "已通过",
      color: "teal",
      icon: "mdi-check-circle-outline",
    },
    pending: {
      label: "待审核",
      color: "amber-darken-3",
      icon: "mdi-clock-outline",
    },
    changes_requested: {
      label: "待修改",
      color: "deep-orange",
      icon: "mdi-pencil-circle-outline",
    },
  })[value || ""] || {
    label: "未提交",
    color: "blue-grey",
    icon: "mdi-circle-outline",
  };
const formatDate = (value: string) =>
  value
    ? new Date(value).toLocaleString("zh-CN", {
        month: "long",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
      })
    : "不限时";
const isExpired = (date: string) =>
  Boolean(date && new Date(date).getTime() < Date.now());
const isSafeUrl = (url: string) => /^https?:\/\//i.test(url);
const messageOf = (e: unknown) =>
  e instanceof ApiError || e instanceof Error
    ? e.message
    : "操作失败，请稍后重试";
const draftKey = (id: string) => `robotlab:submission:${auth.user?.id}:${id}`;

async function load() {
  loading.value = true;
  error.value = "";
  try {
    tasks.value = (await api.get<{ items: Task[] }>("/tasks")).items;
    if (isStudent.value) {
      memberships.value = (
        await api.get<{ items: Membership[] }>("/memberships")
      ).items;
      const legacy = (
        await api.get<{ items: Applicant[] }>("/applicants")
      ).items.find(
        (a) => a.userId === auth.user?.id && a.status === "accepted",
      );
      admitted.value = memberships.value.length > 0 || Boolean(legacy);
      memberScope.team = legacy?.team || "";
      memberScope.direction =
        legacy?.direction || memberships.value[0]?.direction || "";
    } else if (canManage.value) {
      teamsForTasks.value = (
        await api.get<{ items: TeamSpace[] }>("/teams")
      ).items;
    }
  } catch (e) {
    error.value = messageOf(e);
  } finally {
    loading.value = false;
  }
}
async function loadSubmissions(taskId: string) {
  submissionsLoading.value = true;
  submissionsError.value = "";
  try {
    const data = await api.get<{ items: Submission[] }>(
      `/tasks/${taskId}/submissions`,
    );
    if (selected.value?.id === taskId)
      submissions.value = data.items.sort(
        (a, b) =>
          b.version - a.version || b.createdAt.localeCompare(a.createdAt),
      );
  } catch (e) {
    if (selected.value?.id === taskId) submissionsError.value = messageOf(e);
  } finally {
    if (selected.value?.id === taskId) submissionsLoading.value = false;
  }
}
function openTask(task: Task) {
  if (saving.value || uploadBusy.value || judgeBusy.value) return;
  selected.value = null;
  submissions.value = [];
  submissionsError.value = "";
  submissionForm.content = task.mySubmission?.content || "";
  submissionForm.repoUrl = task.mySubmission?.repoUrl || "";
  submissionForm.artifactUrl = task.mySubmission?.artifactUrl || "";
  submissionForm.fileIds = [...(task.mySubmission?.fileIds || [])];
  submissionForm.judgeRunId = task.mySubmission?.judgeRunId || "";
  taskFiles.value = [...(task.fileIds || [])];

  if (isStudent.value)
    try {
      const saved = localStorage.getItem(draftKey(task.id));
      if (saved) {
        const parsed: unknown = JSON.parse(saved);
        if (parsed && typeof parsed === "object")
          for (const key of ["content", "repoUrl", "artifactUrl"] as const) {
            const value = (parsed as Record<string, unknown>)[key];
            if (typeof value === "string") submissionForm[key] = value;
          }
        if (parsed && typeof parsed === "object") {
          const saved = parsed as Record<string, unknown>;
          if (Array.isArray(saved.fileIds))
            submissionForm.fileIds = saved.fileIds.filter(
              (id): id is string => typeof id === "string",
            );
          if (typeof saved.judgeRunId === "string")
            submissionForm.judgeRunId = saved.judgeRunId;
        }
      }
    } catch {
      /* The form still works without local storage. */
    }
  selected.value = task;
  detailTab.value = "brief";
  if (isStudent.value || canManage.value) loadSubmissions(task.id);
}
let appliedTaskLink = "";
watch(
  [() => route.query.task, loading, saving, uploadBusy, judgeBusy],
  ([query]) => {
    const id = typeof query === "string" ? query : "";
    if (
      loading.value ||
      saving.value ||
      uploadBusy.value ||
      judgeBusy.value ||
      error.value
    )
      return;
    if (!id) {
      if (selected.value?.id === appliedTaskLink) selected.value = null;
      appliedTaskLink = "";
      return;
    }
    // Consume route changes, not data refreshes, so closing a dialog sticks.
    if (id === appliedTaskLink) return;
    appliedTaskLink = id;
    const task = tasks.value.find((item) => item.id === id);
    if (task) openTask(task);
    else {
      selected.value = null;
      notice.value = "未找到该任务，或该任务不在你的学习范围内";
    }
  },
  { immediate: true },
);
watch(submissionForm, () => {
  if (!selected.value || !isStudent.value) return;
  try {
    localStorage.setItem(
      draftKey(selected.value.id),
      JSON.stringify(submissionForm),
    );
  } catch {
    /* Drafts are a convenience; server submissions remain authoritative. */
  }
});
async function submitWork() {
  const task = selected.value;
  if (
    !task ||
    saving.value ||
    eligibility.value ||
    uploadBusy.value ||
    judgeBusy.value
  )
    return;
  if (task.submissionType && !submissionForm.fileIds.length) {
    notice.value = "请上传成果文件，或运行源码评测后提交";
    return;
  }
  if (sourceOnly.value && !submissionForm.judgeRunId) {
    notice.value = "请先完成代码自动评测";
    return;
  }
  if (
    !submissionForm.content.trim() &&
    !submissionForm.repoUrl.trim() &&
    !submissionForm.artifactUrl.trim() &&
    submissionForm.fileIds.length === 0 &&
    !(sourceOnly.value && submissionForm.judgeRunId)
  ) {
    notice.value = "请填写完成说明，或提供代码、作品链接";
    return;
  }
  saving.value = true;
  try {
    const { item } = await api.post<{ item: Submission }>(
      `/tasks/${task.id}/submissions`,
      {
        ...submissionForm,
        judgeRunId: sourceOnly.value ? submissionForm.judgeRunId : "",
      },
    );
    const updated = { ...task, mySubmission: item };
    tasks.value = tasks.value.map((t) => (t.id === task.id ? updated : t));
    selected.value = updated;
    try {
      localStorage.removeItem(draftKey(task.id));
    } catch {
      /* Submission is safely persisted by the server. */
    }
    submissions.value = [item, ...submissions.value];
    detailTab.value = "history";
    notice.value = `第 ${item.version} 版已提交，等待导师审核`;
  } catch (e) {
    notice.value = messageOf(e);
  } finally {
    saving.value = false;
  }
}
function startCreate() {
  if (saving.value || uploadBusy.value || judgeBusy.value) return;
  Object.assign(taskForm, {
    title: "",
    description: "",
    direction: "",
    team: "",
    kind: "document",
    difficulty: "入门",
    dueAt: "",
    resourcesUrl: "",
    maxScore: 100,
    required: true,
    teamId: "",
    submissionType: "files",
    startsAt: "",
    judge: {
      enabled: false,
      testCases: [],
      languageIds: [],
      timeLimitSeconds: 2,
      totalCases: 0,
    },
  });
  taskFiles.value = [];
  createOpen.value = true;
}
async function createTask() {
  if (saving.value || uploadBusy.value) return;
  if (!taskForm.title.trim() || !taskForm.description.trim()) {
    notice.value = "请填写任务名称和完整要求";
    return;
  }
  if (
    !Number.isFinite(Number(taskForm.maxScore)) ||
    Number(taskForm.maxScore) <= 0 ||
    Number(taskForm.maxScore) > 1000
  ) {
    notice.value = "满分须大于 0 且不超过 1000";
    return;
  }
  if (
    taskForm.startsAt &&
    taskForm.dueAt &&
    new Date(taskForm.startsAt).getTime() >= new Date(taskForm.dueAt).getTime()
  ) {
    notice.value = "考试开始时间需早于截止时间";
    return;
  }
  if (taskForm.dueAt && !Number.isFinite(new Date(taskForm.dueAt).getTime())) {
    notice.value = "截止时间无效";
    return;
  }
  if (!taskForm.teamId) {
    notice.value = "请选择已发布的团队；如无团队，请先在团队与报名中发布。";
    return;
  }
  if (
    taskForm.submissionType === "code" &&
    taskForm.judge.enabled &&
    taskForm.kind === "quiz" &&
    (!taskForm.startsAt || !taskForm.dueAt)
  ) {
    notice.value = "编程考试必须设置开始和截止时间";
    return;
  }
  if (
    taskForm.submissionType === "code" &&
    taskForm.judge.enabled &&
    (!taskForm.judge.testCases.length || !taskForm.judge.languageIds.length)
  ) {
    notice.value = "自动评测需要至少一个测试用例和一种语言";
    return;
  }
  saving.value = true;
  try {
    const { item } = await api.post<{ item: Task }>("/tasks", {
      ...taskForm,
      fileIds: taskFiles.value,
      judge:
        taskForm.submissionType === "code" && taskForm.judge.enabled
          ? { ...taskForm.judge, totalCases: taskForm.judge.testCases.length }
          : null,
      startsAt: taskForm.startsAt
        ? new Date(taskForm.startsAt).toISOString()
        : "",
      maxScore: Number(taskForm.maxScore),
      dueAt: taskForm.dueAt ? new Date(taskForm.dueAt).toISOString() : "",
    });
    tasks.value = [item, ...tasks.value];
    createOpen.value = false;
    notice.value = "任务已发布，成员可以查看任务并提交成果";
  } catch (e) {
    notice.value = messageOf(e);
  } finally {
    saving.value = false;
  }
}
function startReview(submission: Submission) {
  reviewTarget.value = submission;
  reviewForm.score = submission.score ?? 0;
  reviewForm.feedback = submission.feedback;
  reviewForm.status =
    submission.status === "changes_requested"
      ? "changes_requested"
      : "approved";
}
async function saveReview() {
  if (!selected.value || !reviewTarget.value) return;
  if (
    !Number.isFinite(Number(reviewForm.score)) ||
    Number(reviewForm.score) < 0 ||
    Number(reviewForm.score) > selected.value.maxScore
  ) {
    notice.value = `分数须在 0–${selected.value.maxScore} 之间`;
    return;
  }
  if (
    reviewForm.status === "changes_requested" &&
    !reviewForm.feedback.trim()
  ) {
    notice.value = "请说明需要修改的内容";
    return;
  }
  saving.value = true;
  try {
    const { item } = await api.patch<{ item: Submission }>(
      `/submissions/${reviewTarget.value.id}`,
      { ...reviewForm, score: Number(reviewForm.score) },
    );
    submissions.value = submissions.value.map((s) =>
      s.id === item.id ? item : s,
    );
    reviewTarget.value = null;
    notice.value = "审核结果已保存";
  } catch (e) {
    notice.value = messageOf(e);
  } finally {
    saving.value = false;
  }
}
onMounted(() => {
  void load();
  timeTicker = setInterval(() => {
    currentTime.value = Date.now();
  }, 1000);
});
onUnmounted(() => clearInterval(timeTicker));
</script>

<template>
  <v-defaults-provider
    :defaults="{
      VTextField: { density: 'compact' },
      VSelect: { density: 'compact' },
      VTextarea: { density: 'compact' },
    }"
  >
    <div class="page-content learning-page">
      <div class="page-heading">
        <div>
          <h1>学习与任务</h1>
          <p>
            {{
              canManage
                ? "发布训练任务，查看成员成果与审核进度"
                : "查看训练要求，提交成果并跟进导师反馈"
            }}
          </p>
        </div>
        <div class="heading-actions">
          <v-btn
            variant="text"
            prepend-icon="mdi-refresh"
            :loading="loading"
            @click="load"
            >刷新</v-btn
          >
          <v-btn
            v-if="canManage"
            color="primary"
            prepend-icon="mdi-plus"
            @click="startCreate"
            >发布任务</v-btn
          >
        </div>
      </div>
      <v-alert v-if="error" type="error" variant="tonal" class="mb-5"
        >{{ error
        }}<template #append
          ><v-btn variant="text" @click="load">重试</v-btn></template
        ></v-alert
      >
      <div v-if="loading" class="loading-panel">
        <v-progress-circular indeterminate color="primary" /><span
          >正在整理你的学习任务…</span
        >
      </div>
      <template v-else>
        <div class="learning-overview" aria-label="任务数量概要">
          <div class="overview-stats">
            <div>
              <span>全部任务</span><strong>{{ tasks.length }}</strong>
            </div>
            <div>
              <span>{{ isStudent ? "已完成" : "进行中" }}</span>
              <strong>{{
                isStudent
                  ? completed
                  : tasks.filter((t) => !isExpired(t.dueAt)).length
              }}</strong>
            </div>
            <div>
              <span>{{ isStudent ? "待审核" : "必修任务" }}</span>
              <strong>{{
                isStudent ? pending : tasks.filter((t) => t.required).length
              }}</strong>
            </div>
            <div>
              <span>{{ isStudent ? "待修改" : "成果提交" }}</span>
              <strong>{{
                isStudent
                  ? revisions
                  : tasks.reduce((sum, t) => sum + t.submissionCount, 0)
              }}</strong>
            </div>
          </div>
        </div>
        <div class="task-toolbar">
          <div class="filter-summary">
            {{ kindOf(kindFilter).title }}<span>{{ filtered.length }} 项</span>
          </div>
          <div class="task-filters">
            <v-text-field
              v-model="search"
              placeholder="搜索任务"
              prepend-inner-icon="mdi-magnify"
              variant="outlined"
              density="compact"
              hide-details
              aria-label="搜索任务"
            /><v-select
              v-model="directionFilter"
              :items="['全部方向', ...directions]"
              variant="outlined"
              density="compact"
              hide-details
              aria-label="筛选任务方向"
            />
          </div>
        </div>
        <div v-if="filtered.length" class="task-grid">
          <v-card
            v-for="task in filtered"
            :key="task.id"
            rounded="lg"
            elevation="1"
            class="task-card"
            :class="`task-kind-${task.kind}`"
            ><div class="task-top">
              <div :class="['task-icon', `icon-${task.kind}`]">
                <v-icon size="24">{{ kindOf(task.kind).icon }}</v-icon>
                <span>{{ kindOf(task.kind).title }}</span>
              </div>
              <v-chip
                v-if="isStudent"
                :color="statusOf(task.mySubmission?.status).color"
                size="small"
                variant="tonal"
                >{{ statusOf(task.mySubmission?.status).label }}</v-chip
              ><v-chip
                v-else
                :color="isExpired(task.dueAt) ? 'grey' : 'teal'"
                size="small"
                variant="tonal"
                >{{ isExpired(task.dueAt) ? "已截止" : "进行中" }}</v-chip
              >
            </div>
            <div class="task-labels">
              <span>{{ task.direction || "全方向" }}</span
              ><i>·</i><span>{{ task.difficulty }}</span
              ><v-chip
                v-if="task.required"
                size="x-small"
                color="primary"
                variant="tonal"
                >必修</v-chip
              >
            </div>
            <h2>{{ task.title }}</h2>
            <p class="task-description">{{ task.description }}</p>
            <div class="task-meta">
              <span
                ><v-icon size="14">mdi-account-group-outline</v-icon
                >{{ task.team || "全赛队" }}</span
              ><span
                ><v-icon size="14">mdi-star-outline</v-icon
                >{{ task.maxScore }} 分</span
              >
              <span v-if="canManage"
                ><v-icon size="14">mdi-tray-arrow-up</v-icon
                >{{ task.submissionCount }} 次提交</span
              >
            </div>
            <div class="task-card-footer">
              <div :class="{ overdue: isExpired(task.dueAt) }">
                <v-icon size="14">mdi-calendar-clock-outline</v-icon
                >{{
                  task.dueAt ? `${formatDate(task.dueAt)} 截止` : "不限截止时间"
                }}
              </div>
              <v-btn
                color="primary"
                variant="text"
                size="small"
                append-icon="mdi-arrow-right"
                @click="openTask(task)"
                >{{
                  canManage
                    ? "查看成果"
                    : task.mySubmission
                      ? "查看提交"
                      : "查看任务"
                }}</v-btn
              >
            </div></v-card
          >
        </div>
        <v-card v-else class="empty-panel" elevation="1" rounded="lg"
          ><v-icon size="42" color="indigo-lighten-3"
            >mdi-notebook-outline</v-icon
          >
          <h2>
            {{ tasks.length ? "没有匹配的学习任务" : "新的挑战即将开始" }}
          </h2>
          <p>
            {{
              tasks.length
                ? "更换关键词或方向，发现其他学习任务。"
                : canManage
                  ? "发布第一项任务，明确目标、提交要求与验收标准。"
                  : "导师发布任务后，你可以在这里查看要求并提交成果。"
            }}
          </p>
          <v-btn
            v-if="tasks.length"
            variant="text"
            color="primary"
            @click="clearFilters"
            >清除筛选</v-btn
          ><v-btn v-else-if="canManage" color="primary" @click="startCreate"
            >发布第一项任务</v-btn
          ></v-card
        >
      </template>

      <v-dialog
        :model-value="!!selected"
        max-width="1050"
        :fullscreen="smAndDown"
        :persistent="saving || uploadBusy || judgeBusy"
        scrollable
        @update:model-value="!$event && (selected = null)"
        ><v-card v-if="selected" class="learning-dialog"
          ><v-card-title class="dialog-heading"
            ><div>
              <div class="dialog-caption">
                {{ kindOf(selected.kind).title }} ·
                {{ selected.direction || "全方向" }}
              </div>
              <h2>{{ selected.title }}</h2>
            </div>
            <v-btn
              icon="mdi-close"
              variant="text"
              aria-label="关闭任务详情"
              :disabled="saving || uploadBusy || judgeBusy"
              @click="selected = null" /></v-card-title
          ><v-tabs v-model="detailTab" color="primary" class="px-5"
            ><v-tab value="brief">任务要求</v-tab
            ><v-tab v-if="isStudent" value="submit">提交成果</v-tab
            ><v-tab v-if="isStudent || canManage" value="history"
              >{{ canManage ? "成果审核" : "提交历史" }}
              <v-chip size="x-small" class="ml-2">{{
                canManage ? latestSubmissions.length : submissions.length
              }}</v-chip></v-tab
            ></v-tabs
          ><v-divider /><v-card-text class="task-dialog-body"
            ><v-window v-model="detailTab"
              ><v-window-item value="brief"
                ><div class="brief-meta">
                  <v-chip size="small" variant="tonal" color="primary">{{
                    selected.team || "全赛队"
                  }}</v-chip
                  ><v-chip size="small" variant="tonal">{{
                    selected.difficulty
                  }}</v-chip
                  ><v-chip size="small" variant="tonal" color="teal"
                    >满分 {{ selected?.maxScore }}</v-chip
                  ><span
                    ><v-icon size="15">mdi-calendar-clock-outline</v-icon
                    >{{
                      selected.dueAt
                        ? `${formatDate(selected.dueAt)} 截止`
                        : "不限截止时间"
                    }}</span
                  >
                </div>
                <div class="brief-description">{{ selected.description }}</div>
                <AttachmentList :ids="selected.fileIds" />
                <v-alert
                  v-if="selected.startsAt"
                  type="info"
                  variant="tonal"
                  class="my-4"
                  >考试开始：{{ formatDate(selected.startsAt)
                  }}{{
                    selected.dueAt
                      ? ` · 截止：${formatDate(selected.dueAt)}`
                      : ""
                  }}</v-alert
                >
                <v-alert
                  v-if="selected.kind === 'quiz' || selected.kind === 'code'"
                  type="info"
                  variant="tonal"
                  class="my-5"
                  density="comfortable"
                  >{{
                    selected.judge?.enabled
                      ? "本任务支持代码自动评测，请在提交成果页上传源码并运行测试。"
                      : "上传源码、完整项目或说明材料后，由管理员打分与反馈。"
                  }}</v-alert
                ><v-btn
                  v-if="isSafeUrl(selected.resourcesUrl)"
                  :href="selected.resourcesUrl"
                  target="_blank"
                  rel="noopener noreferrer"
                  color="primary"
                  variant="tonal"
                  prepend-icon="mdi-open-in-new"
                  >打开学习资料</v-btn
                >
                <div class="brief-actions">
                  <v-alert v-if="eligibility" type="info" variant="tonal">{{
                    eligibility
                  }}</v-alert
                  ><v-btn
                    v-else-if="isStudent"
                    color="primary"
                    append-icon="mdi-arrow-right"
                    @click="detailTab = 'submit'"
                    >{{
                      selected.mySubmission ? "提交新版本" : "开始提交成果"
                    }}</v-btn
                  ><v-btn
                    v-else-if="canManage"
                    color="primary"
                    append-icon="mdi-arrow-right"
                    @click="detailTab = 'history'"
                    >查看成员提交</v-btn
                  >
                </div></v-window-item
              >
              <v-window-item value="submit"
                ><v-alert v-if="eligibility" type="info" variant="tonal">{{
                  eligibility
                }}</v-alert
                ><template v-else
                  ><v-alert
                    v-if="selectedSubmission?.feedback"
                    :type="
                      selectedSubmission.status === 'approved'
                        ? 'success'
                        : 'info'
                    "
                    variant="tonal"
                    class="mb-5"
                    ><strong>上一版导师反馈</strong>
                    <p class="feedback-text">
                      {{ selectedSubmission.feedback }}
                    </p></v-alert
                  >
                  <h3 class="content-title">让你的成果有迹可循</h3>
                  <p class="content-hint">
                    说明做了什么、如何验证，以及还有哪些问题。每次提交都会保留独立版本。
                  </p>
                  <v-form @submit.prevent="submitWork"
                    ><v-textarea
                      v-model="submissionForm.content"
                      label="完成说明与验证过程"
                      placeholder="实现内容、测试结果、遇到的问题和改进思路…"
                      rows="7"
                      variant="outlined"
                      maxlength="20000"
                      counter
                    /><JudgePanel
                      v-if="sourceOnly"
                      :key="selected.id"
                      v-model="submissionForm.judgeRunId"
                      :task="selected"
                      :disabled="saving || Boolean(eligibility)"
                      @busy="judgeBusy = $event"
                      @run="
                        submissionForm.fileIds = [
                          ...new Set([
                            ...submissionForm.fileIds,
                            $event.sourceFileId,
                          ]),
                        ]
                      "
                    /><FileUpload
                      v-model="submissionForm.fileIds"
                      :label="
                        selected.submissionType === 'project'
                          ? '完整工程或项目压缩包'
                          : selected.kind === 'hardware'
                            ? 'PCB / 电路设计文件、Gerber 或说明材料'
                            : selected.direction === '运营'
                              ? '图片、设计稿或运营素材'
                              : selected.submissionType === 'code'
                                ? '源码文件或代码说明附件'
                                : '作业文件与附件'
                      "
                      :disabled="saving || judgeBusy"
                      @busy="uploadBusy = $event"
                    /><v-text-field
                      v-model="submissionForm.repoUrl"
                      label="代码仓库链接（选填）"
                      type="url"
                      placeholder="https://github.com/..."
                      variant="outlined"
                      prepend-inner-icon="mdi-source-repository"
                    /><v-text-field
                      v-model="submissionForm.artifactUrl"
                      label="作品、视频或实验报告链接（选填）"
                      type="url"
                      placeholder="https://..."
                      variant="outlined"
                      prepend-inner-icon="mdi-link-variant"
                    />
                    <p class="content-hint">
                      {{
                        sourceOnly
                          ? "请在上方运行代码评测后提交评测结果；自动分数与管理员人工成绩分开展示。"
                          : "可上传多个附件，单个文件最多 25 MB；也可附代码仓库链接。"
                      }}
                    </p>
                    <div class="submit-footer">
                      <span>编辑内容会尝试保存到当前浏览器</span
                      ><v-btn
                        color="primary"
                        :loading="saving"
                        :disabled="uploadBusy || judgeBusy"
                        type="submit"
                        >提交第
                        {{ (selectedSubmission?.version || 0) + 1 }} 版</v-btn
                      >
                    </div></v-form
                  ></template
                ></v-window-item
              >
              <v-window-item value="history"
                ><div v-if="submissionsLoading" class="loading-panel">
                  <v-progress-circular
                    indeterminate
                    color="primary"
                    size="28"
                  /><span>正在读取提交记录…</span>
                </div>
                <v-alert
                  v-else-if="submissionsError"
                  type="error"
                  variant="tonal"
                  >{{ submissionsError
                  }}<template #append
                    ><v-btn
                      @click="selected && loadSubmissions(selected.id)"
                      variant="text"
                      >重试</v-btn
                    ></template
                  ></v-alert
                >
                <div v-else-if="!submissions.length" class="empty-panel">
                  <v-icon size="40" color="grey-lighten-1"
                    >mdi-tray-arrow-up</v-icon
                  >
                  <h2>还没有提交记录</h2>
                  <p>
                    {{
                      canManage
                        ? "成员提交成果后，将在这里按人展示最新版本。"
                        : "完成任务后提交成果，导师的反馈也会保留在这里。"
                    }}
                  </p>
                </div>
                <div v-else class="submission-list">
                  <div v-if="canManage" class="review-summary">
                    <span
                      >共 {{ latestSubmissions.length }} 位成员提交，{{
                        latestSubmissions.filter((s) => s.status === "pending")
                          .length
                      }}
                      项待审核</span
                    ><v-btn
                      size="small"
                      variant="text"
                      prepend-icon="mdi-refresh"
                      @click="selected && loadSubmissions(selected.id)"
                      >刷新</v-btn
                    >
                  </div>
                  <article
                    class="submission-card"
                    v-for="submission in canManage
                      ? latestSubmissions
                      : submissions"
                    :key="submission.id"
                  >
                    <div class="submission-heading">
                      <div>
                        <strong>{{
                          canManage ? submission.userName : "我的提交"
                        }}</strong
                        ><span
                          >第 {{ submission.version }} 版 ·
                          {{ formatDate(submission.createdAt) }}</span
                        >
                      </div>
                      <v-chip
                        :color="statusOf(submission.status).color"
                        size="small"
                        >{{ statusOf(submission.status).label }}</v-chip
                      >
                    </div>
                    <p class="submission-content">
                      {{ submission.content || "已提交附件或评测结果。" }}
                    </p>
                    <AttachmentList :ids="submission.fileIds" />
                    <JudgePanel
                      v-if="submission.judgeRunId && selected"
                      :key="submission.id"
                      :task="selected"
                      :model-value="submission.judgeRunId"
                      readonly
                    />
                    <div class="submission-links">
                      <v-btn
                        v-if="isSafeUrl(submission.repoUrl)"
                        :href="submission.repoUrl"
                        target="_blank"
                        rel="noopener noreferrer"
                        variant="tonal"
                        size="small"
                        color="primary"
                        prepend-icon="mdi-source-repository"
                        >代码仓库</v-btn
                      ><v-btn
                        v-if="isSafeUrl(submission.artifactUrl)"
                        :href="submission.artifactUrl"
                        target="_blank"
                        rel="noopener noreferrer"
                        variant="tonal"
                        size="small"
                        color="primary"
                        prepend-icon="mdi-open-in-new"
                        >成果资料</v-btn
                      >
                    </div>
                    <div
                      v-if="submission.status !== 'pending'"
                      class="review-feedback"
                    >
                      <div>
                        <strong>导师反馈</strong
                        ><v-chip size="small" color="teal" variant="tonal"
                          >{{ submission.score }} /
                          {{ selected?.maxScore }}</v-chip
                        >
                      </div>
                      <p>{{ submission.feedback || "导师已完成审核。" }}</p>
                      <span
                        >审核于 {{ formatDate(submission.reviewedAt) }}</span
                      >
                    </div>
                    <div v-if="canManage" class="review-actions">
                      <span>审核对象为当前最新提交版本</span
                      ><v-btn
                        color="primary"
                        size="small"
                        :variant="
                          submission.status === 'pending' ? 'flat' : 'tonal'
                        "
                        @click="startReview(submission)"
                        >{{
                          submission.status === "pending"
                            ? "审核成果"
                            : "修改评价"
                        }}</v-btn
                      >
                    </div>
                    <details
                      v-if="canManage && submission.version > 1"
                      class="older-versions"
                    >
                      <summary>
                        查看历史版本（{{
                          submissions.filter(
                            (s) =>
                              s.userId === submission.userId &&
                              s.id !== submission.id,
                          ).length
                        }}）
                      </summary>
                      <div
                        v-for="old in submissions.filter(
                          (s) =>
                            s.userId === submission.userId &&
                            s.id !== submission.id,
                        )"
                        :key="old.id"
                        class="older-version"
                      >
                        <strong
                          >第 {{ old.version }} 版 ·
                          {{ formatDate(old.createdAt) }}</strong
                        >
                        <p>{{ old.content || "此版本包含文件或评测结果。" }}</p>
                        <AttachmentList :ids="old.fileIds" />
                        <div class="submission-links">
                          <a
                            v-if="isSafeUrl(old.repoUrl)"
                            :href="old.repoUrl"
                            target="_blank"
                            rel="noopener noreferrer"
                            >代码仓库</a
                          ><a
                            v-if="isSafeUrl(old.artifactUrl)"
                            :href="old.artifactUrl"
                            target="_blank"
                            rel="noopener noreferrer"
                            >成果资料</a
                          >
                        </div>
                        <p v-if="old.feedback">反馈：{{ old.feedback }}</p>
                      </div>
                    </details>
                  </article>
                </div></v-window-item
              >
            </v-window></v-card-text
          ></v-card
        ></v-dialog
      >

      <v-dialog
        v-model="createOpen"
        max-width="800"
        :fullscreen="smAndDown"
        :persistent="saving || uploadBusy"
        scrollable
        ><v-card class="learning-dialog"
          ><v-card-title class="dialog-heading"
            ><div>
              <h2>发布学习任务</h2>
              <p>把目标、提交要求与验收标准说明清楚。</p>
            </div>
            <v-btn
              icon="mdi-close"
              variant="text"
              aria-label="关闭任务发布"
              :disabled="saving || uploadBusy"
              @click="createOpen = false" /></v-card-title
          ><v-card-text
            ><v-form @submit.prevent="createTask"
              ><v-text-field
                v-model="taskForm.title"
                label="任务名称 *"
                variant="outlined"
                maxlength="200"
                required
              /><v-textarea
                v-model="taskForm.description"
                label="任务要求与验收标准 *"
                placeholder="学习目标、前置知识、实施步骤、提交要求与评分标准…"
                rows="7"
                variant="outlined"
                maxlength="20000"
                counter
                required
              /><v-row
                ><v-col cols="12" sm="6"
                  ><v-select
                    v-model="taskForm.team"
                    :items="[
                      { title: '所有赛队', value: '' },
                      { title: 'RoboMaster', value: 'RoboMaster' },
                      { title: 'ROBOCON', value: 'ROBOCON' },
                    ]"
                    label="所属赛事"
                    :disabled="Boolean(taskForm.teamId)"
                    variant="outlined" /></v-col
                ><v-col cols="12" sm="6"
                  ><v-select
                    v-model="taskForm.teamId"
                    :items="
                      teamsForTasks.map((t) => ({ title: t.name, value: t.id }))
                    "
                    label="所属团队 *"
                    variant="outlined" /></v-col
                ><v-col cols="12" sm="6"
                  ><v-select
                    v-model="taskForm.direction"
                    :items="[
                      { title: '所有方向', value: '' },
                      ...taskDirections.map((d) => ({ title: d, value: d })),
                    ]"
                    label="面向方向"
                    variant="outlined" /></v-col
                ><v-col cols="12" sm="6"
                  ><v-select
                    v-model="taskForm.kind"
                    :items="kinds.filter((k) => k.value !== 'all')"
                    label="任务类型"
                    variant="outlined" /></v-col
                ><v-col cols="12" sm="6"
                  ><v-select
                    v-model="taskForm.submissionType"
                    :items="[
                      { title: '文件 / 压缩包', value: 'files' },
                      { title: '单文件代码评测', value: 'code' },
                      { title: '完整工程 / 项目', value: 'project' },
                    ]"
                    label="提交方式"
                    variant="outlined"
                /></v-col>
                <v-col cols="12" sm="6"
                  ><v-select
                    v-model="taskForm.difficulty"
                    :items="['入门', '进阶', '挑战']"
                    label="难度"
                    variant="outlined" /></v-col
                ><v-col cols="12" sm="6"
                  ><v-text-field
                    v-model="taskForm.startsAt"
                    type="datetime-local"
                    label="考试开始时间（选填）"
                    variant="outlined" /></v-col
                ><v-col cols="12" sm="6"
                  ><v-text-field
                    v-model="taskForm.dueAt"
                    type="datetime-local"
                    label="截止时间（选填）"
                    variant="outlined" /></v-col
                ><v-col cols="12" sm="6"
                  ><v-text-field
                    v-model.number="taskForm.maxScore"
                    type="number"
                    min="1"
                    max="1000"
                    label="满分"
                    variant="outlined"
                    required /></v-col></v-row
              ><v-text-field
                v-model="taskForm.resourcesUrl"
                label="学习资料 / 外部测评链接（选填）"
                type="url"
                placeholder="https://"
                variant="outlined"
              /><FileUpload
                v-model="taskFiles"
                label="任务说明附件（选填）"
                :disabled="saving"
                @busy="uploadBusy = $event"
              /><JudgeFields
                v-if="taskForm.submissionType === 'code'"
                v-model="taskForm.judge"
              /><v-switch
                v-model="taskForm.required"
                color="primary"
                label="设为必修任务"
                hide-details
              />
              <div class="submit-footer">
                <span>任务发布后，对适用赛队与方向可见</span
                ><v-btn
                  color="primary"
                  :loading="saving"
                  :disabled="uploadBusy"
                  type="submit"
                  >发布任务</v-btn
                >
              </div></v-form
            ></v-card-text
          ></v-card
        ></v-dialog
      >
      <v-dialog
        :model-value="!!reviewTarget"
        max-width="580"
        :persistent="saving"
        @update:model-value="!$event && (reviewTarget = null)"
        ><v-card v-if="reviewTarget && selected" class="learning-dialog"
          ><v-card-title class="dialog-heading"
            ><div>
              <h2>审核 {{ reviewTarget.userName }} 的成果</h2>
              <p>第 {{ reviewTarget.version }} 版 · {{ selected.title }}</p>
            </div></v-card-title
          ><v-card-text
            ><v-form @submit.prevent="saveReview"
              ><v-select
                v-model="reviewForm.status"
                :items="[
                  { title: '通过验收', value: 'approved' },
                  { title: '需要修改', value: 'changes_requested' },
                ]"
                label="审核结果"
                variant="outlined"
              /><v-text-field
                v-model.number="reviewForm.score"
                type="number"
                min="0"
                :max="selected.maxScore"
                :label="`成绩（满分 ${selected.maxScore}）`"
                variant="outlined"
                required
              /><v-textarea
                v-model="reviewForm.feedback"
                label="具体反馈与改进建议"
                rows="5"
                variant="outlined"
                maxlength="10000"
                counter
                :required="reviewForm.status === 'changes_requested'"
              />
              <div class="review-form-actions">
                <v-btn :disabled="saving" @click="reviewTarget = null"
                  >取消</v-btn
                ><v-btn color="primary" type="submit" :loading="saving"
                  >保存审核结果</v-btn
                >
              </div></v-form
            ></v-card-text
          ></v-card
        ></v-dialog
      >
      <v-snackbar
        :model-value="!!notice"
        timeout="5000"
        @update:model-value="!$event && (notice = '')"
        >{{ notice
        }}<template #actions
          ><v-btn variant="text" @click="notice = ''">关闭</v-btn></template
        ></v-snackbar
      >
    </div>
  </v-defaults-provider>
</template>

<style scoped>
.page-heading {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 20px;
  margin-bottom: 20px;
}
.page-heading h1 {
  font-size: 24px;
  font-weight: 500;
  line-height: 1.4;
  margin: 0 0 6px;
  color: #263238;
}
.page-heading p,
.dialog-heading p {
  color: #687680;
  font-size: 14px;
  line-height: 1.6;
}
.heading-actions {
  display: flex;
  gap: 8px;
  flex-shrink: 0;
}
.loading-panel {
  min-height: 240px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 14px;
  color: #687680;
  font-size: 14px;
}
.learning-overview {
  border: 1px solid #e2e6e9;
  border-radius: 6px;
  background: #fff;
  margin-bottom: 20px;
}
.overview-stats {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  padding: 16px 0;
}
.overview-stats > div {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 0 22px;
}
.overview-stats > div + div {
  border-left: 1px solid #e8ecef;
}
.overview-stats strong {
  font-size: 25px;
  font-weight: 500;
  color: #1976d2;
  line-height: 1.3;
}
.overview-stats span {
  font-size: 13px;
  color: #687680;
}
.task-toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 20px;
  margin-bottom: 16px;
}
.filter-summary {
  font-size: 16px;
  font-weight: 500;
  color: #37474f;
}
.filter-summary span {
  font-size: 12px;
  color: #687680;
  margin-left: 10px;
  font-weight: 400;
}
.task-filters {
  display: flex;
  gap: 12px;
  width: 390px;
}
.task-filters > .v-text-field {
  min-width: 0;
  flex: 1.6;
}
.task-filters > .v-select {
  min-width: 0;
  flex: 1;
}
.task-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 18px;
}
.task-card {
  padding: 18px;
  display: flex;
  flex-direction: column;
  border-radius: 6px !important;
  border-top: 3px solid #1976d2;
  box-shadow: 0 1px 4px #2437461a !important;
  transition: box-shadow 0.2s;
}
.task-card:hover {
  box-shadow: 0 3px 9px #24374624 !important;
}
.task-kind-document {
  border-top-color: #009688;
}
.task-kind-hardware {
  border-top-color: #f5a623;
}
.task-kind-quiz {
  border-top-color: #7e57c2;
}
.task-top {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
  margin-bottom: 16px;
}
.task-icon {
  display: flex;
  align-items: center;
  gap: 8px;
  color: #1976d2;
}
.task-icon span {
  font-size: 12px;
  color: #687680;
}
.icon-document {
  color: #00897b;
}
.icon-hardware {
  color: #d48a12;
}
.icon-quiz {
  color: #7e57c2;
}
.task-labels {
  display: flex;
  align-items: center;
  gap: 7px;
  font-size: 12px;
  color: #687680;
  margin-bottom: 8px;
}
.task-labels i {
  font-style: normal;
  color: #aeb7bd;
}
.task-labels > .v-chip {
  margin-left: 3px;
}
.task-card h2 {
  font-size: 17px;
  font-weight: 500;
  line-height: 1.5;
  color: #263238;
  margin-bottom: 10px;
}
.task-description {
  font-size: 14px;
  color: #687680;
  line-height: 1.75;
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
  margin-bottom: 18px;
  min-height: 74px;
  overflow-wrap: anywhere;
}
.task-meta {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
  font-size: 12px;
  color: #687680;
  margin-top: auto;
  margin-bottom: 14px;
}
.task-meta > span {
  display: flex;
  align-items: center;
  gap: 4px;
}
.task-card-footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 4px;
  padding-top: 10px;
  border-top: 1px solid #edf0f2;
}
.task-card-footer > div {
  display: flex;
  align-items: center;
  gap: 4px;
  color: #687680;
  font-size: 12px;
}
.task-card-footer > div.overdue {
  color: #ad5b4e;
}
.empty-panel {
  padding: 48px 24px;
  text-align: center;
  border-radius: 6px !important;
}
.empty-panel h2 {
  font-size: 18px;
  font-weight: 500;
  color: #455a64;
  margin-top: 16px;
}
.empty-panel p {
  font-size: 14px;
  color: #687680;
  margin: 12px 0 20px;
  line-height: 1.8;
}
.learning-dialog {
  border-radius: 6px !important;
}
.dialog-heading {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 14px;
  padding: 20px 24px !important;
  white-space: normal !important;
}
.dialog-caption {
  font-size: 12px;
  font-weight: 400;
  color: #687680;
  margin-bottom: 4px;
}
.dialog-heading h2 {
  font-size: 20px;
  font-weight: 500;
  line-height: 1.5;
  color: #263238;
}
.dialog-heading p {
  font-size: 12px;
  margin-top: 4px;
}
.task-dialog-body {
  padding: 24px !important;
}
.brief-meta {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
  margin-bottom: 20px;
}
.brief-meta > span {
  font-size: 12px;
  color: #687680;
  display: flex;
  align-items: center;
  gap: 5px;
  margin-left: auto;
}
.brief-description {
  white-space: pre-wrap;
  font-size: 14px;
  color: #455a64;
  line-height: 1.9;
  overflow-wrap: anywhere;
}
.brief-actions {
  margin-top: 24px;
  padding-top: 20px;
  border-top: 1px solid #e8ecef;
}
.content-title {
  font-size: 17px;
  font-weight: 500;
  color: #37474f;
  margin-bottom: 8px;
}
.content-hint {
  font-size: 12px;
  color: #687680;
  line-height: 1.8;
  margin-bottom: 20px;
}
.submit-footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 20px;
  border-top: 1px solid #e8ecef;
  padding-top: 20px;
  margin: 12px 0;
}
.submit-footer > span {
  font-size: 12px;
  color: #687680;
}
.review-summary {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 12px;
  color: #687680;
  margin-bottom: 14px;
}
.submission-card {
  border: 1px solid #e0e6e9;
  border-radius: 6px;
  padding: 20px;
  margin-bottom: 16px;
}
.submission-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}
.submission-heading strong {
  display: block;
  font-size: 15px;
  color: #37474f;
}
.submission-heading span {
  font-size: 12px;
  color: #687680;
  display: block;
  margin-top: 5px;
}
.submission-content {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  font-size: 14px;
  line-height: 1.8;
  color: #455a64;
  margin: 18px 0;
}
.submission-links {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
}
.review-feedback {
  background: #f4f7f9;
  border-radius: 4px;
  padding: 16px;
  margin-top: 18px;
}
.review-feedback > div {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
}
.review-feedback strong {
  font-size: 13px;
  color: #455a64;
}
.review-feedback > p,
.feedback-text {
  font-size: 14px;
  line-height: 1.8;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  margin-top: 10px;
  color: #455a64;
}
.review-feedback > span {
  display: block;
  font-size: 12px;
  color: #687680;
  margin-top: 10px;
}
.review-actions {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 15px;
  border-top: 1px solid #e8ecef;
  padding-top: 16px;
  margin-top: 16px;
}
.review-actions > span {
  font-size: 12px;
  color: #687680;
}
.review-form-actions {
  display: flex;
  gap: 10px;
  justify-content: flex-end;
}
.older-versions {
  margin-top: 18px;
  font-size: 12px;
  color: #687680;
}
.older-versions summary {
  cursor: pointer;
  color: #1976d2;
}
.older-version {
  padding: 16px;
  margin-top: 12px;
  background: #f7f9fa;
  border-radius: 4px;
}
.older-version strong {
  font-size: 12px;
  font-weight: 500;
  color: #455a64;
}
.older-version p {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  line-height: 1.8;
  font-size: 14px;
  margin-top: 10px;
}
.older-version a {
  color: #1976d2;
  margin-top: 10px;
}
@media (max-width: 1350px) {
  .task-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .overview-stats > div {
    padding: 0 18px;
  }
}
@media (max-width: 800px) {
  .overview-stats > div {
    padding: 0 14px;
    flex-direction: column;
    align-items: flex-start;
    gap: 5px;
  }
  .task-filters {
    width: 340px;
  }
}
@media (max-width: 600px) {
  .page-heading {
    gap: 12px;
    align-items: flex-start;
    flex-wrap: wrap;
  }
  .page-heading h1 {
    font-size: 22px;
  }
  .page-heading p {
    font-size: 13px;
  }
  .heading-actions {
    margin-left: auto;
  }
  .overview-stats {
    padding: 12px 0;
  }
  .overview-stats > div {
    padding: 0 12px;
  }
  .overview-stats strong {
    font-size: 22px;
  }
  .overview-stats span {
    font-size: 12px;
  }
  .task-toolbar {
    flex-direction: column;
    align-items: stretch;
    gap: 12px;
  }
  .task-filters {
    width: 100%;
  }
  .task-grid {
    grid-template-columns: 1fr;
    gap: 14px;
  }
  .task-description {
    min-height: auto;
  }
  .dialog-heading {
    padding: 16px !important;
  }
  .dialog-heading h2 {
    font-size: 18px;
  }
  .task-dialog-body {
    padding: 18px !important;
  }
  .brief-meta > span {
    margin-left: 0;
    flex-basis: 100%;
    margin-top: 6px;
  }
  .submit-footer {
    flex-direction: column;
    align-items: stretch;
  }
  .submission-card {
    padding: 16px;
  }
  .review-actions {
    align-items: flex-start;
  }
  .review-actions > span {
    max-width: 145px;
  }
  .review-summary {
    gap: 12px;
  }
  .submission-heading {
    align-items: flex-start;
  }
}
</style>
