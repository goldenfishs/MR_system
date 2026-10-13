<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useDisplay } from "vuetify";
import { api, ApiError } from "../lib/api";
import { groupOptions, groupLabel, hasPermission, isParticipant } from "../lib/permissions";
import { canAccessLearning, canAccessLearningModule, learningDirections } from "../lib/learning-access";
import { auth } from "../lib/auth";
import type { Task, Submission, Applicant } from "../lib/types";
import type { JudgeConfig, JudgeRun, Membership, TeamSpace } from "../lib/platform";
import FileUpload from "../components/FileUpload.vue";
import AttachmentList from "../components/AttachmentList.vue";
import JudgePanel from "../components/JudgePanel.vue";
import JudgeFields from "../components/JudgeFields.vue";
import KnowledgeLibrary from "../components/KnowledgeLibrary.vue";

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
const editingTaskId = ref("");
const removingTask = ref<Task | null>(null);
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
  category: "assignment" as "assignment" | "exam" | "problem",
  tags: [] as string[],
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
const sections = ["home", "knowledge", "tasks", "exam", "bank"] as const;
const section = computed(() => {
  const value = String(route.query.section || "");
  if (value === "knowledge" && !canViewKnowledge.value || ["tasks", "exam", "bank"].includes(value) && !canViewTasks.value) return "home";
  if (sections.includes(value as (typeof sections)[number])) return value;
  // Preserve old deep links such as /learning?kind=code.
  return route.query.kind && canViewTasks.value ? "tasks" : "home";
});
const categoryOf = (task: Task) => task.category || "assignment";
const taskTypeLabel = (task: Task) => categoryOf(task) === "exam" ? "定时考试" : categoryOf(task) === "problem" ? "题库练习" : kindOf(task.kind).title;
const competitionLabel = (value: string) => value === "ROBOCON" ? "RoboCon" : value;
const taskCompetition = (task: Task) => {
  const team = teamsForTasks.value.find(team => team.id === task.teamId);
  return team?.competitions?.length ? team.competitions.map(competitionLabel).join(" / ") : competitionLabel(task.team) || "RoboCon / RoboMaster";
};
const categoryFilter = computed(() =>
  section.value === "exam" ? "exam" : section.value === "bank" ? "problem" : section.value === "tasks" ? "assignment" : "",
);
const taskDirections = computed(
  () => {
    const available = (teamsForTasks.value.find((t) => t.id === taskForm.teamId)?.directions || directions).filter(direction => hasPermission("tasks", direction));
    return taskForm.category === "problem" ? available.filter(direction => ["电控", "视觉算法", "硬件"].includes(direction)) : available;
  },
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
  { value: "code", title: "代码作业", icon: "mdi-code-braces" },
  { value: "document", title: "文档作业", icon: "mdi-file-document-outline" },
  {
    value: "hardware",
    title: "设计与实践",
    icon: "mdi-robot-industrial-outline",
  },
  { value: "quiz", title: "书面考核", icon: "mdi-clipboard-text-outline" },
];
const availableKinds = computed(() => kinds.filter(kind => kind.value !== "all" && (taskForm.direction !== "运营" || !["code", "quiz"].includes(kind.value)) && (taskForm.direction !== "机械" || kind.value !== "code")));
const availableSubmissionTypes = computed(() => ["机械", "运营"].includes(taskForm.direction) ? [
  { title: taskForm.direction === "机械" ? "图纸 / 文件 / 工程材料" : "图片 / 设计稿 / 素材文件", value: "files" },
  { title: "完整项目 / 压缩包", value: "project" },
] : [
  { title: "文件 / 压缩包", value: "files" },
  { title: "单文件代码评测", value: "code" },
  { title: "完整工程 / 项目", value: "project" },
]);
watch(() => taskForm.category, value => {
  if (value === "problem") {
    taskForm.kind = "code";
    taskForm.submissionType = "code";
    taskForm.judge.enabled = true;
    if (!["电控", "视觉算法", "硬件"].includes(taskForm.direction)) taskForm.direction = "";
  }
});
watch(() => taskForm.direction, value => {
  if (["机械", "运营"].includes(value)) {
    if (taskForm.kind === "code" || value === "运营" && taskForm.kind === "quiz") taskForm.kind = "document";
    if (taskForm.submissionType === "code") taskForm.submissionType = "files";
    taskForm.judge.enabled = false;
  }
});
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
const taskFitsDirection = (task: Task) =>
  (directionFilter.value === "全部方向" || !task.direction || task.direction === directionFilter.value) &&
  (task.direction !== "运营" || !["code", "quiz"].includes(task.kind)) &&
  (task.direction !== "机械" || task.kind !== "code") &&
  (directionFilter.value !== "运营" || !["code", "quiz"].includes(task.kind)) &&
  (directionFilter.value !== "机械" || task.kind !== "code");
const scopedTasks = computed(() => tasks.value.filter(taskFitsDirection));
const listedTasks = computed(() => scopedTasks.value.filter(task => !categoryFilter.value || categoryOf(task) === categoryFilter.value));
const selectedDirection = computed(() => directionFilter.value === "全部方向" ? "" : directionFilter.value);
const canViewTasks = computed(() => canAccessLearningModule("tasks", selectedDirection.value || undefined));
const canViewKnowledge = computed(() => canAccessLearningModule("knowledge", selectedDirection.value || undefined));
const supportsBank = computed(() => !selectedDirection.value || ["电控", "视觉算法", "硬件"].includes(selectedDirection.value));
const groupName = computed(() => selectedDirection.value ? groupLabel(selectedDirection.value) : "技术组");
const groupIntro = computed(() => ({
  机械: "从机构方案到加工装配，在实践中完成一台可靠的机器人。",
  电控: "连接传感、控制与执行，让代码成为机器人的行动。",
  视觉算法: "从图像与数据中理解环境，为机器人建立感知与决策。",
  硬件: "从原理图到电路板，为机器人建立稳定的硬件基础。",
  运营: "记录团队、讲好故事，让每一次探索被更多人看见。",
})[selectedDirection.value as "机械" | "电控" | "视觉算法" | "硬件" | "运营"] || "共享知识与项目经验，一起参加 RoboCon 与 RoboMaster。" );
const isFuture = (task: Task) => Boolean(task.locked || task.startsAt && new Date(task.startsAt).getTime() > currentTime.value);
const exams = computed(() => scopedTasks.value.filter((task) => categoryOf(task) === "exam"));
const assignments = computed(() => scopedTasks.value.filter((task) => categoryOf(task) === "assignment"));
const upcomingExams = computed(() => exams.value.filter((task) => !isExpired(task.dueAt)).sort((a, b) => (a.startsAt || "").localeCompare(b.startsAt || "")).slice(0, 4));
const recentTasks = computed(() => assignments.value.slice(0, 4));
const problemTag = ref("全部标签"), problemDifficulty = ref("全部难度"), problemSort = ref("newest"), problemPage = ref(1);
const problemTags = computed(() => [...new Set(scopedTasks.value.filter(task => categoryOf(task) === "problem").flatMap(task => task.tags || []))].sort());
const problems = computed(() => {
  const result = filtered.value.filter(task =>
    (problemTag.value === "全部标签" || task.tags?.includes(problemTag.value)) &&
    (problemDifficulty.value === "全部难度" || task.difficulty === problemDifficulty.value),
  );
  if (problemSort.value === "title") result.sort((a, b) => a.title.localeCompare(b.title, "zh-CN"));
  else if (problemSort.value === "accepted") result.sort((a, b) => (b.acceptedCount || 0) - (a.acceptedCount || 0));
  else result.sort((a, b) => b.createdAt.localeCompare(a.createdAt));
  return result;
});
const pagedProblems = computed(() => problems.value.slice((problemPage.value - 1) * 15, problemPage.value * 15));
watch([search, problemTag, problemDifficulty, problemSort, directionFilter], () => problemPage.value = 1);
function goSection(value: string) {
  router.push({ path: "/learning", query: { direction: selectedDirection.value || undefined, section: value } });
}
function randomProblem() {
  if (problems.value.length) openTask(problems.value[Math.floor(Math.random() * problems.value.length)]!);
}
function clearFilters() {
  search.value = "";
  router.replace({
    query: { ...route.query, kind: undefined, direction: undefined },
  });
}
const isStudent = computed(() => isParticipant() && canAccessLearning.value);
const canSubmitSelected = computed(() => isStudent.value && Boolean(selected.value) && selected.value?.canSubmit !== false);
const canManage = computed(() => hasPermission("tasks", selected.value ? selected.value.direction : selectedDirection.value || undefined));
const canManageTask = (task: Task) => hasPermission("tasks", task.direction);
const filtered = computed(() =>
  scopedTasks.value.filter(
    (t) =>
      (!categoryFilter.value || categoryOf(t) === categoryFilter.value) &&
      (kindFilter.value === "all" || t.kind === kindFilter.value) &&
      `${t.title} ${t.description} ${t.team} ${(t.tags || []).join(' ')}`
        .toLowerCase()
        .includes(search.value.toLowerCase()),
  ),
);
const completed = computed(
  () => listedTasks.value.filter((t) => t.mySubmission?.status === "approved").length,
);
const pending = computed(
  () => listedTasks.value.filter((t) => t.mySubmission?.status === "pending").length,
);
const revisions = computed(
  () =>
    listedTasks.value.filter((t) => t.mySubmission?.status === "changes_requested")
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
  if (selected.value.canSubmit === false) return "可查看本任务；成果提交面向所属组别的成员。";
  if (!admitted.value)
    return "报名审核通过后，才能访问知识库、作业和考试。";
  if (
    selected.value.canSubmit === undefined && selected.value.teamId &&
    !memberships.value.some(
      (m) =>
        m.teamId === selected.value?.teamId &&
        (!selected.value?.direction ||
          m.direction === selected.value.direction),
    )
  )
    return `此任务面向${selected.value.direction || "该"}方向团队成员。请先加入对应团队。`;
  if (
    selected.value.canSubmit === undefined && !selected.value.teamId &&
    selected.value.team &&
    selected.value.team !== memberScope.team
  )
    return `此任务面向 ${selected.value.team} 成员。`;
  if (
    selected.value.canSubmit === undefined && !selected.value.teamId &&
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

let loadVersion = 0;
let lastUnlockRefresh = 0;
async function load(quiet = false) {
  if (quiet && loading.value) return;
  const version = ++loadVersion;
  loading.value = true;
  error.value = "";
  try {
    const category = categoryFilter.value;
    const taskPath = category ? `/tasks?category=${category}` : "/tasks";
    const [result, membershipData, legacyData, teamData] = await Promise.all([
      canViewTasks.value && section.value !== "knowledge" ? api.get<{ items: Task[] }>(taskPath) : Promise.resolve({ items: [] as Task[] }),
      isStudent.value ? api.get<{ items: Membership[] }>("/memberships") : Promise.resolve({ items: [] as Membership[] }),
      isStudent.value ? api.get<{ items: Applicant[] }>("/applicants") : Promise.resolve({ items: [] as Applicant[] }),
      api.get<{ items: TeamSpace[] }>("/teams"),
    ]);
    if (version !== loadVersion) return;
    tasks.value = result.items;
    if (selected.value) {
      const refreshed = tasks.value.find(task => task.id === selected.value?.id);
      selected.value = refreshed || null;
    }
    memberships.value = membershipData.items;
    admitted.value = canAccessLearning.value;
    memberScope.team = ""; memberScope.direction = "";
    if (isStudent.value) {
      const legacy = legacyData.items.find(
        (a) => a.userId === auth.user?.id && a.status === "accepted",
      );
      memberScope.team = legacy?.team || auth.user?.team || "";
      memberScope.direction = legacy?.direction || memberships.value[0]?.direction || auth.user?.direction || "";
    }
    teamsForTasks.value = teamData.items;
  } catch (e) {
    if (version === loadVersion) error.value = messageOf(e);
  } finally {
    if (version === loadVersion) loading.value = false;
  }
}
watch([section, selectedDirection, canViewTasks, canViewKnowledge, () => JSON.stringify([auth.user?.id, auth.user?.role, auth.user?.groupGrants, canAccessLearning.value])], () => {
  selected.value = null; submissions.value = []; createOpen.value = false; reviewTarget.value = null; removingTask.value = null; search.value = "";
  void load();
});
watch(currentTime, (time) => {
  if (loading.value || time - lastUnlockRefresh < 15000) return;
  if (tasks.value.some(task => task.locked && task.startsAt && new Date(task.startsAt).getTime() <= time)) {
    lastUnlockRefresh = time;
    void load(true);
  }
});
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
async function onJudgeRun(run: JudgeRun) {
  submissionForm.fileIds = [...new Set([...submissionForm.fileIds, run.sourceFileId])];
  if (run.status !== "completed" || section.value !== "bank") return;
  const version = loadVersion;
  try {
    const result = await api.get<{ items: Task[] }>("/tasks?category=problem");
    if (version !== loadVersion || section.value !== "bank") return;
    tasks.value = result.items;
    if (selected.value) selected.value = tasks.value.find(task => task.id === selected.value?.id) || selected.value;
  } catch { /* Judge results remain available even if the list refresh fails. */ }
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
    !canSubmitSelected.value ||
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
    notice.value = `第 ${item.version} 版已提交，等待管理员审核`;
  } catch (e) {
    notice.value = messageOf(e);
  } finally {
    saving.value = false;
  }
}
function startCreate() {
  if (saving.value || uploadBusy.value || judgeBusy.value) return;
  editingTaskId.value = "";
  Object.assign(taskForm, {
    title: "",
    description: "",
    direction: selectedDirection.value || (auth.user?.role === "admin" ? "" : groupOptions.find(group => hasPermission("tasks", group.value))?.value || ""),
    team: "",
    kind: "document",
    category: section.value === "exam" ? "exam" : section.value === "bank" ? "problem" : "assignment",
    tags: [],
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
  if (taskForm.category === "problem") {
    taskForm.kind = "code";
    taskForm.submissionType = "code";
    taskForm.judge.enabled = true;
  }
  if (teamsForTasks.value.length === 1) taskForm.teamId = teamsForTasks.value[0]!.id;
  createOpen.value = true;
}
function localDate(value?: string) {
  if (!value) return "";
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return "";
  return new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
}
function startEdit(task: Task, asNew = false) {
  if (!canManageTask(task)) return;
  if (saving.value || uploadBusy.value || judgeBusy.value) return;
  editingTaskId.value = asNew ? "" : task.id;
  Object.assign(taskForm, {
    title: `${task.title}${asNew ? ' · 新版本' : ''}`, description: task.description,
    direction: task.direction, team: task.team, kind: task.kind,
    category: categoryOf(task), tags: [...(task.tags || [])], difficulty: task.difficulty,
    dueAt: localDate(task.dueAt), resourcesUrl: task.resourcesUrl, maxScore: task.maxScore,
    required: task.required, teamId: task.teamId || "", submissionType: task.submissionType || "files",
    startsAt: localDate(task.startsAt),
    judge: task.judge ? { ...task.judge, testCases: task.judge.testCases.map(test => ({ ...test })), languageIds: [...task.judge.languageIds] } : { enabled: false, testCases: [], languageIds: [], timeLimitSeconds: 2, totalCases: 0 },
  });
  taskFiles.value = [...(task.fileIds || [])];
  createOpen.value = true;
}
async function removeTask() {
  if (!removingTask.value || saving.value) return;
  const id = removingTask.value.id;
  saving.value = true;
  try {
    await api.delete(`/tasks/${id}`);
    tasks.value = tasks.value.filter(task => task.id !== id);
    if (selected.value?.id === id) selected.value = null;
    removingTask.value = null;
    notice.value = "内容已移除";
  } catch (e) { notice.value = messageOf(e); }
  finally { saving.value = false; }
}
async function createTask() {
  if (saving.value || uploadBusy.value) return;
  if (!hasPermission("tasks", taskForm.direction)) { notice.value = "没有该组作业与考试的管理权限"; return; }
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
  if (taskForm.category === "exam" && (!taskForm.startsAt || !taskForm.dueAt)) {
    notice.value = "考试必须设置发布开始时间和截止时间";
    return;
  }
  if (taskForm.category === "problem" && (!["电控", "视觉算法", "硬件"].includes(taskForm.direction) || !taskForm.judge.enabled)) {
    notice.value = "题库题目请选择电控、视觉算法或硬件组，并启用自动评测";
    return;
  }
  if (["机械", "运营"].includes(taskForm.direction) && (taskForm.submissionType === "code" || taskForm.judge.enabled)) {
    notice.value = "机械组和运营组使用文件、工程或作品提交，不设置代码自动评测";
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
    const body = {
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
    };
    const { item } = editingTaskId.value
      ? await api.patch<{ item: Task }>(`/tasks/${editingTaskId.value}`, body)
      : await api.post<{ item: Task }>("/tasks", body);
    tasks.value = [item, ...tasks.value.filter(task => task.id !== item.id)];
    if (selected.value?.id === item.id) selected.value = item;
    createOpen.value = false;
    notice.value = editingTaskId.value ? "内容已更新" : taskForm.category === "exam" ? "考试已排期，题目将在开始时间开放" : taskForm.category === "problem" ? "题目已发布到题库" : "作业已发布，成员可以查看要求并提交成果";
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
  if (!selected.value || !reviewTarget.value || !canManageTask(selected.value)) return;
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
          <h1>{{ groupName }} · {{ section === 'knowledge' ? '知识库' : section === 'exam' ? '考试' : section === 'bank' ? '题库' : section === 'tasks' ? '作业' : '首页' }}</h1>
          <p>
            {{
              section === 'knowledge'
                ? '下载文件、阅读 PDF，观看技术组学习资料'
                : section === 'bank'
                  ? '按题目练习代码，查看通过率并运行自动评测'
                  : section === 'exam'
                    ? '管理员定时发布考核内容，按开始和截止时间参加考试'
                    : section === 'home'
                      ? '从知识库到实践任务，记录每一步机器人能力成长'
                      : canManage
                        ? '发布训练任务，查看成员成果与审核进度'
                        : '查看训练要求，提交成果并跟进管理员反馈'
            }}
          </p>
        </div>
        <div class="heading-actions">
          <v-btn
            variant="text"
            prepend-icon="mdi-refresh"
            :loading="loading"
            @click="load()"
            >刷新</v-btn
          >
          <v-btn
            v-if="canManage && section !== 'knowledge' && (section !== 'bank' || supportsBank)"
            color="primary"
            prepend-icon="mdi-plus"
            @click="startCreate"
            >{{ section === 'exam' ? '安排考试' : section === 'bank' ? '发布题目' : '发布作业' }}</v-btn
          >
        </div>
      </div>
      <v-alert v-if="error" type="error" variant="tonal" class="mb-5"
        >{{ error
        }}<template #append
          ><v-btn variant="text" @click="load()">重试</v-btn></template
        ></v-alert
      >
      <v-alert v-if="!canAccessLearning" type="info" variant="tonal" density="compact" class="mb-5">当前开放已授权组别的管理功能。个人学习、成果提交与代码测评需取得成员学习资格。</v-alert>
      <div v-if="loading" class="loading-panel">
        <v-progress-circular indeterminate color="primary" /><span
          >正在整理你的学习任务…</span
        >
      </div>
      <template v-else>
        <template v-if="section === 'home'">
          <v-card class="group-welcome" elevation="1">
            <div><span class="welcome-caption">QUT ROBOT TEAM</span><h2>{{ groupName }}的成长空间</h2><p>{{ groupIntro }}</p><div class="welcome-links"><v-btn v-if="canViewKnowledge" color="primary" variant="tonal" prepend-icon="mdi-bookshelf" @click="goSection('knowledge')">进入知识库</v-btn><v-btn color="primary" variant="text" prepend-icon="mdi-message-question-outline" :to="{ path: '/discussions', query: { direction: selectedDirection || undefined } }">向{{ groupName }}提问</v-btn></div></div>
            <div v-if="canViewTasks" class="welcome-stats"><div><strong>{{ assignments.length }}</strong><span>作业与考核</span></div><div><strong>{{ exams.length }}</strong><span>考试安排</span></div><div><strong>{{ scopedTasks.filter(task => task.mySubmission?.status === 'approved').length }}</strong><span>{{ isStudent ? '已完成任务' : '通过提交' }}</span></div></div>
          </v-card>
          <div class="group-shortcuts">
            <v-card v-if="canViewKnowledge" elevation="1" @click="goSection('knowledge')"><v-icon color="teal">mdi-bookshelf</v-icon><div><h3>知识库</h3><p>讲义、PDF、视频与资料下载</p></div><v-icon color="grey">mdi-chevron-right</v-icon></v-card>
            <v-card v-if="canViewTasks" elevation="1" @click="goSection('tasks')"><v-icon color="primary">mdi-clipboard-text-outline</v-icon><div><h3>作业</h3><p>{{ selectedDirection === '运营' ? '图片、设计稿与运营实践' : '训练任务、实践成果与阶段考核' }}</p></div><v-icon color="grey">mdi-chevron-right</v-icon></v-card>
            <v-card v-if="canViewTasks" elevation="1" @click="goSection('exam')"><v-icon color="deep-purple">mdi-calendar-clock-outline</v-icon><div><h3>考试</h3><p>查看排期，按时参加考试</p></div><v-icon color="grey">mdi-chevron-right</v-icon></v-card>
            <v-card v-if="canViewTasks && supportsBank" elevation="1" @click="goSection('bank')"><v-icon color="deep-orange">mdi-code-braces</v-icon><div><h3>题库</h3><p>电控、视觉与硬件编程练习</p></div><v-icon color="grey">mdi-chevron-right</v-icon></v-card>
          </div>
          <div v-if="canViewTasks" class="group-home-grid">
            <v-card class="group-home-panel" elevation="1"><div class="home-panel-heading"><h2>近期作业</h2><v-btn variant="text" color="primary" size="small" @click="goSection('tasks')">全部作业</v-btn></div>
              <button v-for="task in recentTasks" :key="task.id" class="home-task-row" @click="openTask(task)"><v-icon color="primary" size="21">{{ kindOf(task.kind).icon }}</v-icon><div><strong>{{ task.title }}</strong><span>{{ task.direction || '全组共享' }} · {{ task.dueAt ? formatDate(task.dueAt) + ' 截止' : '不限截止时间' }}</span></div><v-chip v-if="isStudent" size="x-small" :color="statusOf(task.mySubmission?.status).color" variant="tonal">{{ statusOf(task.mySubmission?.status).label }}</v-chip><v-icon v-else color="grey" size="18">mdi-chevron-right</v-icon></button>
              <p v-if="!recentTasks.length" class="home-empty">{{ canManage ? '发布作业后，成员将在这里查看最新的训练安排。' : '管理员发布作业后，将在这里展示最新安排。' }}</p>
            </v-card>
            <v-card class="group-home-panel" elevation="1"><div class="home-panel-heading"><h2>考试日程</h2><v-btn variant="text" color="primary" size="small" @click="goSection('exam')">全部考试</v-btn></div>
              <button v-for="task in upcomingExams" :key="task.id" class="home-task-row" @click="openTask(task)"><v-icon color="deep-purple" size="21">mdi-calendar-clock-outline</v-icon><div><strong>{{ task.title }}</strong><span>{{ task.startsAt ? formatDate(task.startsAt) + ' 开始' : '等待安排' }}</span></div><v-chip size="x-small" :color="isFuture(task) ? 'blue-grey' : 'teal'" variant="tonal">{{ isFuture(task) ? '待开考' : '进行中' }}</v-chip></button>
              <p v-if="!upcomingExams.length" class="home-empty">暂无即将开始或进行中的考试。</p>
            </v-card>
          </div>
        </template>
        <KnowledgeLibrary v-else-if="section === 'knowledge' && canViewKnowledge" :direction="selectedDirection" />
        <v-card v-else-if="section === 'bank' && !supportsBank" class="empty-panel" elevation="1"><v-icon size="42" color="indigo-lighten-3">mdi-notebook-outline</v-icon><h2>{{ groupName }}以文件与实践任务为主</h2><p>查看知识库、作业和考试安排，完成本组的训练内容。</p><v-btn color="primary" variant="tonal" @click="goSection('tasks')">进入作业</v-btn></v-card>
        <template v-else>
        <div class="learning-overview" aria-label="任务数量概要">
          <div class="overview-stats">
            <div>
              <span>{{ section === 'bank' ? '题库题目' : section === 'exam' ? '考试数量' : '全部作业' }}</span><strong>{{ listedTasks.length }}</strong>
            </div>
            <div>
              <span>{{ section === 'bank' ? '已通过题目' : isStudent ? "已完成" : "进行中" }}</span>
              <strong>{{
                section === 'bank' ? listedTasks.filter(task => task.myAccepted).length : isStudent
                  ? completed
                  : listedTasks.filter((t) => !isExpired(t.dueAt)).length
              }}</strong>
            </div>
            <div>
              <span>{{ section === 'bank' ? '通过次数' : isStudent ? "待审核" : "必修任务" }}</span>
              <strong>{{
                section === 'bank' ? listedTasks.reduce((sum, task) => sum + (task.acceptedCount || 0), 0) : isStudent ? pending : listedTasks.filter((t) => t.required).length
              }}</strong>
            </div>
            <div>
              <span>{{ section === 'bank' ? '评测次数' : isStudent ? "待修改" : "成果提交" }}</span>
              <strong>{{
                section === 'bank' ? listedTasks.reduce((sum, task) => sum + (task.attemptCount || 0), 0) : isStudent
                  ? revisions
                  : listedTasks.reduce((sum, t) => sum + t.submissionCount, 0)
              }}</strong>
            </div>
          </div>
        </div>
        <div class="task-toolbar">
          <div class="filter-summary">
            {{ section === 'bank' ? '编程题目' : section === 'exam' ? '考试安排' : '作业与考核' }}<span>{{ filtered.length }} 项</span>
          </div>
          <div class="task-filters">
            <v-text-field
              v-model="search"
              :placeholder="section === 'bank' ? '搜索题目或标签' : section === 'exam' ? '搜索考试' : '搜索作业'"
              prepend-inner-icon="mdi-magnify"
              variant="outlined"
              density="compact"
              hide-details
              aria-label="搜索任务"
            /><v-select
              v-model="directionFilter"
              :items="[{ value: '全部方向', title: '全部组别' }, ...learningDirections]"
              variant="outlined"
              density="compact"
              hide-details
              aria-label="筛选任务方向"
            />
          </div>
        </div>
        <div v-if="section === 'bank'" class="problem-layout">
          <v-card class="problem-list" elevation="1">
            <div class="problem-list-tools"><v-select v-model="problemSort" :items="[{value:'newest',title:'最新发布'},{value:'title',title:'题目名称'},{value:'accepted',title:'通过次数'}]" density="compact" variant="outlined" hide-details aria-label="题库排序" /><span>{{ problems.length }} 道题目</span></div>
            <div class="problem-table-scroll"><table class="problem-table"><thead><tr><th>状态</th><th>题目</th><th>标签</th><th>AC / 尝试</th><th>难度</th></tr></thead><tbody><tr v-for="task in pagedProblems" :key="task.id"><td><v-icon size="19" :color="task.myAccepted ? 'teal' : 'grey-lighten-1'">{{ task.myAccepted ? 'mdi-check-circle-outline' : 'mdi-circle-outline' }}</v-icon></td><td><button class="problem-title" @click="openTask(task)"><span>#{{ task.id.slice(0, 6).toUpperCase() }}</span><strong>{{ task.title }}</strong></button></td><td><div class="problem-tags"><v-chip v-for="tag in task.tags || []" :key="tag" size="x-small" color="primary" variant="tonal" @click="problemTag = tag">{{ tag }}</v-chip><span v-if="!task.tags?.length">{{ task.direction }}</span></div></td><td>{{ task.acceptedCount || 0 }} / {{ task.attemptCount || 0 }}</td><td><v-chip size="x-small" :color="task.difficulty === '挑战' ? 'deep-orange' : task.difficulty === '进阶' ? 'amber-darken-2' : 'teal'" variant="tonal">{{ task.difficulty }}</v-chip></td></tr></tbody></table></div>
            <div v-if="!problems.length" class="problem-empty"><v-icon size="38" color="indigo-lighten-3">mdi-code-braces</v-icon><h2>暂无匹配的题目</h2><p>{{ canManage ? '发布带测试用例的题目，成员即可在题库中练习。' : '更换搜索条件，或等待管理员发布新的练习题目。' }}</p></div>
            <v-pagination v-if="problems.length > 15" v-model="problemPage" :length="Math.ceil(problems.length / 15)" :total-visible="5" density="compact" class="my-3" />
          </v-card>
          <aside class="problem-sidebar"><v-card elevation="1"><h3>题目分类</h3><v-select v-model="problemTag" :items="['全部标签', ...problemTags]" label="知识标签" density="compact" variant="outlined" hide-details /><v-select v-model="problemDifficulty" :items="['全部难度', '入门', '进阶', '挑战']" label="题目难度" density="compact" variant="outlined" hide-details class="mt-4" /><v-btn color="primary" variant="text" class="mt-3" @click="search = ''; problemTag = '全部标签'; problemDifficulty = '全部难度'">清除筛选</v-btn></v-card><v-card elevation="1"><h3>练习一道题</h3><p>从当前筛选结果中随机挑选，开始今天的练习。</p><v-btn color="primary" variant="tonal" block prepend-icon="mdi-shuffle-variant" :disabled="!problems.length" @click="randomProblem">随机题目</v-btn></v-card></aside>
        </div>
        <div v-else-if="filtered.length" class="task-grid">
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
                <span>{{ taskTypeLabel(task) }}</span>
              </div>
              <v-chip
                v-if="isStudent"
                :color="statusOf(task.mySubmission?.status).color"
                size="small"
                variant="tonal"
                >{{ statusOf(task.mySubmission?.status).label }}</v-chip
              ><v-chip
                v-else
                :color="isExpired(task.dueAt) ? 'grey' : isFuture(task) ? 'blue-grey' : 'teal'"
                size="small"
                variant="tonal"
                >{{ isExpired(task.dueAt) ? "已截止" : isFuture(task) ? "待开考" : "进行中" }}</v-chip
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
            <p class="task-description">{{ task.locked ? '考试题目将在开始时间开放。你可以先查看考试排期和分值。' : task.description }}</p>
            <p v-if="task.startsAt" class="exam-start"><v-icon size="14">mdi-clock-outline</v-icon>{{ formatDate(task.startsAt) }} 开始</p>
            <div class="task-meta">
              <span
                ><v-icon size="14">mdi-account-group-outline</v-icon
                >{{ taskCompetition(task) }}</span
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
                  : "管理员发布任务后，你可以在这里查看要求并提交成果。"
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
                {{ taskTypeLabel(selected) }} ·
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
            ><v-tab v-if="canSubmitSelected" value="submit">提交成果</v-tab
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
                    taskCompetition(selected)
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
                <v-alert v-if="selected.locked" type="info" variant="tonal" class="mb-5">本场考试尚未开始，题目和附件将在开考时间开放。</v-alert>
                <template v-else><div class="brief-description">{{ selected.description }}</div><AttachmentList :ids="selected.fileIds" /></template>
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
                  v-if="!selected.locked && selected.submissionType === 'code' && selected.direction !== '运营'"
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
                  <div v-if="canManageTask(selected)" class="task-management-actions"><v-btn color="primary" variant="tonal" prepend-icon="mdi-pencil-outline" :disabled="selected.submissionCount > 0" @click="startEdit(selected)">编辑{{ categoryOf(selected) === 'exam' ? '考试' : categoryOf(selected) === 'problem' ? '题目' : '作业' }}</v-btn><v-btn variant="text" prepend-icon="mdi-content-copy" @click="startEdit(selected, true)">发布新版本</v-btn><v-btn color="error" variant="text" :disabled="selected.submissionCount > 0" @click="removingTask = selected">删除</v-btn><p>已有提交或评测记录的内容需发布新版本，以保留原始成绩。</p></div>
                </div></v-window-item
              >
              <v-window-item v-if="canSubmitSelected" value="submit"
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
                    ><strong>上一版管理员反馈</strong>
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
                      @run="onJudgeRun"
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
                      v-if="selected.direction !== '运营' && selected.direction !== '机械'"
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
                          : "可上传多个成果附件，单个文件最多 25 MB。"
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
                        : "完成任务后提交成果，管理员的反馈也会保留在这里。"
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
                        <strong>管理员反馈</strong
                        ><v-chip size="small" color="teal" variant="tonal"
                          >{{ submission.score }} /
                          {{ selected?.maxScore }}</v-chip
                        >
                      </div>
                      <p>{{ submission.feedback || "管理员已完成审核。" }}</p>
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
              <h2>{{ editingTaskId ? '编辑' : '发布' }}{{ taskForm.category === 'exam' ? '考试' : taskForm.category === 'problem' ? '题库题目' : '作业任务' }}</h2>
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
              ><v-select v-model="taskForm.category" :items="[{ value: 'assignment', title: '作业与阶段考核' }, { value: 'exam', title: '定时考试' }, ...(supportsBank ? [{ value: 'problem', title: '题库练习' }] : [])]" label="发布到栏目" variant="outlined" />
              <v-text-field
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
                    :items="availableKinds"
                    :disabled="taskForm.category === 'problem'"
                    label="任务类型"
                    variant="outlined" /></v-col
                ><v-col cols="12" sm="6"
                  ><v-select
                    v-model="taskForm.submissionType"
                    :items="availableSubmissionTypes"
                    :disabled="taskForm.category === 'problem'"
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
                    :label="taskForm.category === 'exam' ? '考试发布开始时间 *' : '开放时间（选填）'"
                    :required="taskForm.category === 'exam'"
                    variant="outlined" /></v-col
                ><v-col cols="12" sm="6"
                  ><v-text-field
                    v-model="taskForm.dueAt"
                    type="datetime-local"
                    :label="taskForm.category === 'exam' ? '考试截止时间 *' : '截止时间（选填）'"
                    :required="taskForm.category === 'exam'"
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
              ><v-combobox v-if="taskForm.category === 'problem'" v-model="taskForm.tags" multiple chips closable-chips :items="problemTags" label="知识标签（输入后回车添加）" variant="outlined" />
              <v-text-field
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
                v-if="taskForm.submissionType === 'code' && !['机械', '运营'].includes(taskForm.direction)"
                v-model="taskForm.judge"
              /><v-switch
                v-model="taskForm.required"
                color="primary"
                label="设为必修作业"
                v-if="taskForm.category === 'assignment'"
                hide-details
              />
              <div class="submit-footer">
                <span>{{ taskForm.category === 'exam' ? '题目与附件将在开始时间自动开放' : '发布后，对团队内适用组别可见' }}</span
                ><v-btn
                  color="primary"
                  :loading="saving"
                  :disabled="uploadBusy"
                  type="submit"
                  >{{ editingTaskId ? '保存修改' : taskForm.category === 'exam' ? '安排考试' : taskForm.category === 'problem' ? '发布题目' : '发布作业' }}</v-btn
                >
              </div></v-form
            ></v-card-text
          ></v-card
        ></v-dialog
      >
      <v-dialog :model-value="Boolean(removingTask)" max-width="440" :persistent="saving" @update:model-value="!$event && (removingTask = null)"><v-card><v-card-title>删除这项内容？</v-card-title><v-card-text>“{{ removingTask?.title }}”将不再显示。已有提交或评测记录的内容无法删除。</v-card-text><v-card-actions><v-spacer /><v-btn :disabled="saving" @click="removingTask = null">取消</v-btn><v-btn color="error" :loading="saving" @click="removeTask">确认删除</v-btn></v-card-actions></v-card></v-dialog>
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
.group-welcome,
.group-shortcuts > .v-card,
.group-home-panel,
.problem-list,
.problem-sidebar > .v-card {
  border-radius: 6px !important;
  box-shadow: 0 1px 4px #2437461a !important;
}
.group-welcome {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 32px;
  padding: 28px;
  margin-bottom: 20px;
  border-top: 3px solid #1976d2;
}
.group-welcome > div:first-child {
  min-width: 0;
}
.welcome-caption {
  display: block;
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 1.4px;
  color: #1976d2;
  margin-bottom: 8px;
}
.group-welcome h2 {
  font-size: 23px;
  line-height: 1.5;
  font-weight: 500;
  color: #263238;
}
.group-welcome p {
  font-size: 14px;
  line-height: 1.8;
  color: #687680;
  margin-top: 8px;
}
.welcome-links {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 20px;
}
.welcome-stats {
  display: grid;
  grid-template-columns: repeat(3, minmax(72px, 1fr));
  flex-shrink: 0;
  min-width: 290px;
  padding: 20px 0;
}
.welcome-stats > div {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  padding: 0 14px;
}
.welcome-stats > div + div {
  border-left: 1px solid #e8ecef;
}
.welcome-stats strong {
  font-size: 29px;
  line-height: 1.2;
  font-weight: 500;
  color: #1976d2;
}
.welcome-stats span {
  color: #687680;
  font-size: 12px;
  white-space: nowrap;
}
.group-shortcuts {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 18px;
  margin-bottom: 20px;
}
.group-shortcuts:has(> :nth-child(4)) {
  grid-template-columns: repeat(4, minmax(0, 1fr));
}
.group-shortcuts > .v-card {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 20px 16px;
  cursor: pointer;
}
.group-shortcuts > .v-card > .v-icon:first-child {
  font-size: 27px;
}
.group-shortcuts > .v-card > div {
  min-width: 0;
  flex: 1;
}
.group-shortcuts h3 {
  color: #37474f;
  font-size: 16px;
  line-height: 1.5;
  font-weight: 500;
}
.group-shortcuts p {
  color: #839099;
  font-size: 12px;
  line-height: 1.6;
  margin-top: 4px;
}
.group-shortcuts > .v-card > .v-icon:last-child {
  font-size: 19px;
}
.group-home-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 20px;
}
.group-home-panel {
  min-width: 0;
}
.home-panel-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 16px 20px;
  border-bottom: 1px solid #edf0f2;
}
.home-panel-heading h2 {
  font-size: 16px;
  font-weight: 500;
  color: #37474f;
}
.home-task-row {
  display: flex;
  align-items: center;
  width: 100%;
  gap: 12px;
  padding: 17px 20px;
  text-align: left;
  transition: background-color .15s;
}
.home-task-row + .home-task-row {
  border-top: 1px solid #edf0f2;
}
.home-task-row:hover {
  background: #f6f9fc;
}
.home-task-row > div {
  flex: 1;
  min-width: 0;
}
.home-task-row strong {
  display: block;
  font-size: 14px;
  font-weight: 500;
  color: #455a64;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.home-task-row > div > span {
  display: block;
  font-size: 12px;
  line-height: 1.6;
  color: #839099;
  margin-top: 4px;
}
.home-task-row > .v-chip {
  flex-shrink: 0;
}
.home-empty {
  font-size: 13px;
  line-height: 1.8;
  color: #839099;
  padding: 26px 20px;
}
.problem-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 225px;
  align-items: start;
  gap: 20px;
}
.problem-list {
  min-width: 0;
}
.problem-list-tools {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  padding: 16px 18px;
  border-bottom: 1px solid #edf0f2;
}
.problem-list-tools > .v-select {
  flex: 0 1 170px;
  min-width: 0;
}
.problem-list-tools > span {
  color: #839099;
  font-size: 12px;
  white-space: nowrap;
}
.problem-table-scroll {
  width: 100%;
  overflow-x: auto;
}
.problem-table {
  width: 100%;
  min-width: 590px;
  border-collapse: collapse;
  text-align: left;
}
.problem-table th {
  font-size: 12px;
  font-weight: 500;
  color: #687680;
  background: #fafbfc;
  padding: 13px 14px;
  white-space: nowrap;
}
.problem-table td {
  font-size: 12px;
  color: #687680;
  padding: 15px 14px;
  border-top: 1px solid #edf0f2;
}
.problem-table th:first-child,
.problem-table td:first-child {
  width: 54px;
  text-align: center;
}
.problem-table th:nth-child(4),
.problem-table td:nth-child(4) {
  white-space: nowrap;
}
.problem-table tbody tr:hover {
  background: #f6f9fc;
}
.problem-title {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 120px;
  text-align: left;
}
.problem-title > span {
  font-size: 10px;
  color: #99a5ad;
}
.problem-title strong {
  font-size: 14px;
  line-height: 1.5;
  font-weight: 500;
  color: #1976d2;
}
.problem-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}
.problem-empty {
  text-align: center;
  padding: 48px 24px;
}
.problem-empty h2 {
  font-size: 18px;
  font-weight: 500;
  color: #455a64;
  margin: 14px 0 8px;
}
.problem-empty p,
.problem-sidebar p {
  font-size: 13px;
  color: #839099;
  line-height: 1.8;
}
.problem-sidebar {
  display: grid;
  gap: 18px;
  min-width: 0;
}
.problem-sidebar > .v-card {
  padding: 20px 16px;
}
.problem-sidebar h3 {
  font-size: 15px;
  color: #455a64;
  font-weight: 500;
  margin-bottom: 18px;
}
.problem-sidebar p {
  margin-bottom: 18px;
}
.exam-start {
  display: flex;
  align-items: center;
  gap: 5px;
  font-size: 12px;
  color: #7e57c2;
  margin: -6px 0 15px;
}
.task-management-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  padding-top: 20px;
  margin-top: 24px;
  border-top: 1px solid #e8ecef;
}
.task-management-actions > p {
  flex-basis: 100%;
  font-size: 12px;
  line-height: 1.8;
  color: #839099;
  margin-top: 4px;
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
  .group-shortcuts:has(> :nth-child(4)) {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .group-welcome {
    gap: 20px;
    padding: 24px;
  }
  .welcome-stats {
    min-width: 248px;
  }
  .task-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .overview-stats > div {
    padding: 0 18px;
  }
}
@media (max-width: 800px) {
  .group-welcome {
    flex-direction: column;
    align-items: stretch;
  }
  .welcome-stats {
    width: 100%;
    padding: 18px 0 0;
    border-top: 1px solid #e8ecef;
  }
  .group-shortcuts,
  .group-home-grid {
    grid-template-columns: 1fr;
  }
  .problem-layout {
    grid-template-columns: 1fr;
  }
  .problem-sidebar {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
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
  .group-welcome {
    padding: 20px;
  }
  .group-welcome h2 {
    font-size: 21px;
  }
  .group-shortcuts,
  .group-shortcuts:has(> :nth-child(4)),
  .problem-sidebar {
    grid-template-columns: 1fr;
  }
  .group-shortcuts,
  .group-home-grid {
    gap: 14px;
  }
  .home-task-row,
  .home-panel-heading {
    padding-left: 16px;
    padding-right: 16px;
  }
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
